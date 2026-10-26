package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func settingsTestApp(t *testing.T) (*App, *webRemote) {
	t.Helper()
	cfg := defaultConfig()
	cfg.LastFM = LastFMConfig{APIKey: "test-key", APISecret: "test-secret", SessionKey: "test-session", Username: "listener"}
	app := &App{cfg: &cfg, settingsRequests: make(chan settingsTask), playerRefresh: make(chan struct{}, 1), webNowPlaying: make(chan struct{}, 1), webStop: make(chan struct{}, 1)}
	oldApp, oldSMB, oldRadio, oldPlex := appConfigPath, smbConfigPath, radioConfigPath, plexConfigPath
	dir := t.TempDir()
	appConfigPath = filepath.Join(dir, "config.json")
	smbConfigPath = filepath.Join(dir, "smb.json")
	radioConfigPath = filepath.Join(dir, "radio.json")
	plexConfigPath = filepath.Join(dir, "plex.json")
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case task := <-app.settingsRequests:
				task.run()
			case <-stop:
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		<-done
		appConfigPath, smbConfigPath, radioConfigPath, plexConfigPath = oldApp, oldSMB, oldRadio, oldPlex
	})
	return app, &webRemote{app: app, done: make(chan struct{}), clients: make(map[net.Conn]struct{})}
}
func settingsRequestTest(handler http.HandlerFunc, method, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://mister.local/api/settings", strings.NewReader(body))
	if method == "POST" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}
func TestWebSettingsCoversConfigAndProtectsSecrets(t *testing.T) {
	_, w := settingsTestApp(t)
	rec := settingsRequestTest(w.handleAppSettings, "GET", "")
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "test-secret") || strings.Contains(rec.Body.String(), "test-session") || strings.Contains(rec.Body.String(), "test-key") {
		t.Fatal("app settings exposed credentials")
	}
	var result struct {
		Config map[string]any  `json:"config"`
		Fields []settingsField `json:"fields"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	fields := map[string]bool{}
	for _, f := range result.Fields {
		fields[f.Path] = true
	}
	var visit func(map[string]any, string)
	visit = func(obj map[string]any, prefix string) {
		for key, val := range obj {
			path := prefix + key
			if sub, ok := val.(map[string]any); ok {
				visit(sub, path+".")
			} else if !fields[path] {
				t.Errorf("missing setting: %s", path)
			}
		}
	}
	visit(result.Config, "")
}
func TestWebAppSettingsSaveApplyAndRetainCredentials(t *testing.T) {
	app, w := settingsTestApp(t)
	rec := settingsRequestTest(w.handleAppSettings, "POST", `{"oled_mode":true,"swap_ab":true,"screensaver_seconds":120,"navigation":{"jump_entries":25,"hold_repeat":false},"eq":{"enabled":true,"Bass":3.5},"lastfm":{"api_secret":"","enabled":true}}`)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var saved Config
	b, err := os.ReadFile(appConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.OLEDMode || !saved.SwapAB || saved.Navigation.Jump != 25 || saved.Navigation.HoldRepeat || saved.EQ.Bass != 3.5 || saved.LastFM.APISecret != "test-secret" {
		t.Fatal("settings not retained")
	}
	_, err = app.settingsCall(context.Background(), func() (any, error) {
		if !app.cfg.OLEDMode || app.cfg.Navigation.Jump != 25 {
			t.Error("settings not live")
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !swapABInput.Load() || screenSaverSeconds.Load() != 120 || navigationHold.Load() {
		t.Fatal("input/screensaver settings not applied")
	}
	rec = settingsRequestTest(w.handleAppSettings, "POST", `{"web_remote_port":8190}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"restart_required":true`) {
		t.Fatal(rec.Code, rec.Body.String())
	}
}
func TestWebAppSettingsValidationAndPartialUpdates(t *testing.T) {
	_, w := settingsTestApp(t)
	for _, body := range []string{`{"volume":101}`, `{"navigation":{"hold_delay_ms":1}}`, `{"eq":{"Bass":7}}`, `{"custom_font":"../escape.ttf"}`, `{"unknown":true}`, `{"eq":null}`, `{"oled_mode":null}`, `{"volume":1.5}`, `{"volume":25} {"volume":30}`} {
		rec := settingsRequestTest(w.handleAppSettings, "POST", body)
		if rec.Code != 400 {
			t.Errorf("accepted %s: %d", body, rec.Code)
		}
	}
	rec := settingsRequestTest(w.handleAppSettings, "POST", `{"volume":35}`)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	rec = settingsRequestTest(w.handleAppSettings, "POST", `{"show_clock":true}`)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	rec = settingsRequestTest(w.handleAppSettings, "GET", "")
	if !strings.Contains(rec.Body.String(), `"volume":35`) {
		t.Fatal("partial update reset existing value")
	}
}
func TestWebSettingsCancelledTaskDoesNotApply(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	applied := false
	task := settingsTask{ctx: ctx, fn: func() (any, error) { applied = true; return nil, nil }, reply: make(chan settingsResult, 1)}
	task.run()
	if applied {
		t.Fatal("cancelled task applied")
	}
}
func TestWebSMBEditsPreserveSecretsAcrossReordering(t *testing.T) {
	_, w := settingsTestApp(t)
	old := SMBConfig{Shares: []SMBShare{{Name: "First", Server: "nas", Share: "Music", Username: "user", Password: "secret-one"}, {Name: "Second", Server: "nas2", Share: "Music", Username: "user2", Password: "secret-two"}}}
	if err := settingsJSONWrite(smbConfigPath, old); err != nil {
		t.Fatal(err)
	}
	rec := settingsRequestTest(w.handleSMBSettings, "GET", "")
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "secret-one") || strings.Contains(rec.Body.String(), "secret-two") {
		t.Fatal("SMB secrets leaked")
	}
	var loaded struct {
		Revision string             `json:"revision"`
		Shares   []smbSettingsEntry `json:"shares"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &loaded)
	loaded.Shares[1].Name = "Renamed"
	loaded.Shares[1].Path = "Albums"
	loaded.Shares = []smbSettingsEntry{loaded.Shares[1], loaded.Shares[0]}
	body, _ := json.Marshal(loaded)
	rec = settingsRequestTest(w.handleSMBSettings, "POST", string(body))
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	saved := loadSMB()
	if len(saved.Shares) != 2 || saved.Shares[0].Name != "Renamed" || saved.Shares[0].Password != "secret-two" || saved.Shares[1].Password != "secret-one" {
		t.Fatal("SMB password retention/order failed")
	}
	rec = settingsRequestTest(w.handleSMBSettings, "POST", string(body))
	if rec.Code != 409 {
		t.Fatal("stale SMB update accepted")
	}
}
func TestWebSMBServerChangeAndClearPassword(t *testing.T) {
	_, w := settingsTestApp(t)
	_ = settingsJSONWrite(smbConfigPath, SMBConfig{Shares: []SMBShare{{Server: "nas", Share: "Music", Username: "u", Password: "secret"}}})
	var loaded struct {
		Revision string             `json:"revision"`
		Shares   []smbSettingsEntry `json:"shares"`
	}
	rec := settingsRequestTest(w.handleSMBSettings, "GET", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &loaded)
	loaded.Shares[0].Server = "other"
	body, _ := json.Marshal(loaded)
	rec = settingsRequestTest(w.handleSMBSettings, "POST", string(body))
	if rec.Code != 400 {
		t.Fatal("old credential reused for new server")
	}
	loaded.Shares[0].Server = "nas"
	loaded.Shares[0].ClearPassword = true
	body, _ = json.Marshal(loaded)
	rec = settingsRequestTest(w.handleSMBSettings, "POST", string(body))
	if rec.Code != 200 || loadSMB().Shares[0].Password != "" {
		t.Fatal("password not cleared")
	}
}
func TestWebRadioCRUDAndValidation(t *testing.T) {
	_, w := settingsTestApp(t)
	body := `{"revision":"missing","stations":[{"name":"Station","url":"https://radio.example/live.flac","genre":"Jazz"}]}`
	rec := settingsRequestTest(w.handleRadioSettings, "POST", body)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	radio, err := loadRadio()
	if err != nil || len(radio.Stations) != 1 || radio.Stations[0].Genre != "Jazz" {
		t.Fatal("radio file incompatible")
	}
	rec = settingsRequestTest(w.handleRadioSettings, "GET", "")
	var loaded struct {
		Revision string         `json:"revision"`
		Stations []RadioStation `json:"stations"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &loaded)
	invalid := fmt.Sprintf(`{"revision":%q,"stations":[{"name":"Bad","url":"file:///etc/passwd"}]}`, loaded.Revision)
	rec = settingsRequestTest(w.handleRadioSettings, "POST", invalid)
	if rec.Code != 400 {
		t.Fatal("invalid radio URL accepted")
	}
	empty := fmt.Sprintf(`{"revision":%q,"stations":[]}`, loaded.Revision)
	rec = settingsRequestTest(w.handleRadioSettings, "POST", empty)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	radio, err = loadRadio()
	if err != nil || len(radio.Stations) != 0 {
		t.Fatal("radio removal failed")
	}
}
func TestWebSourceConnectionTests(t *testing.T) {
	_, w := settingsTestApp(t)
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/library/sections" {
			fmt.Fprint(rw, `{"MediaContainer":{"Directory":[{"type":"artist","key":"40"}]}}`)
		} else if r.URL.Path == "/page" {
			rw.Header().Set("Content-Type", "text/html")
			fmt.Fprint(rw, "<html>page</html>")
		} else {
			rw.Header().Set("Content-Type", "audio/mpeg")
			fmt.Fprint(rw, strings.Repeat("audio", 30))
		}
	}))
	defer server.Close()
	for _, body := range []string{fmt.Sprintf(`{"kind":"plex","plex":{"server":%q,"token":"dummy","library":"40"}}`, server.URL), fmt.Sprintf(`{"kind":"radio","radio":{"name":"Test","url":%q}}`, server.URL+"/stream")} {
		rec := settingsRequestTest(w.handleSourceTest, "POST", body)
		if rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body.String())
		}
	}
	rec := settingsRequestTest(w.handleSourceTest, "POST", fmt.Sprintf(`{"kind":"radio","radio":{"name":"Test","url":%q}}`, server.URL+"/page"))
	if rec.Code != 502 {
		t.Fatal("HTML page accepted as a stream")
	}
	previous := testSMBConnection
	defer func() { testSMBConnection = previous }()
	called := false
	testSMBConnection = func(ctx context.Context, s SMBShare) error { called = true; return nil }
	rec = settingsRequestTest(w.handleSourceTest, "POST", `{"kind":"smb","smb":{"server":"nas","share":"Music","guest":true}}`)
	if rec.Code != 200 || !called {
		t.Fatal("SMB test not invoked")
	}
}
func TestWebSettingsConcurrentStateAndChanges(t *testing.T) {
	_, w := settingsTestApp(t)
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			if i%2 == 0 {
				_ = w.state()
			} else {
				rec := settingsRequestTest(w.handleAppSettings, "POST", fmt.Sprintf(`{"volume":%d}`, i))
				if rec.Code != 200 {
					t.Error(rec.Code)
				}
			}
		}(i)
	}
	group.Wait()
}

// Optional local preview exercises the actual HTTP settings routes with a stub audio backend.
func TestWebSettingsPreview(t *testing.T) {
	if os.Getenv("HIFI_SETTINGS_PREVIEW") == "" {
		t.Skip("local preview only")
	}
	_, w := settingsTestApp(t)
	mux := http.NewServeMux()
	registerWebSettings(mux, w)
	mux.HandleFunc("/", w.handleIndex)
	mux.HandleFunc("/api/state", w.handleState)
	mux.HandleFunc("/api/sources", w.handleSources)
	mux.HandleFunc("/api/plex/config", w.handlePlexConfig)
	mux.HandleFunc("/ws", w.handleWS)
	server := httptest.NewServer(mux)
	defer server.Close()
	fmt.Println("PREVIEW_URL=" + server.URL)
	select {
	case <-time.After(180 * time.Second):
	}
}

func TestWebSettingsUpdatesLoadedPlayer(t *testing.T) {
	app, w := settingsTestApp(t)
	player := newPlayer(Queue{Tracks: []Track{{Title: "Loaded"}}}, defaultConfig(), false)
	_, err := app.settingsCall(context.Background(), func() (any, error) { app.player = player; return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	rec := settingsRequestTest(w.handleAppSettings, "POST", `{"volume":42,"gapless_playback":true,"prioritize_external_art":true,"eq":{"enabled":true,"Bass":4}}`)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	player.mu.Lock()
	cfg := player.cfg
	player.mu.Unlock()
	if cfg.Volume != 42 || !cfg.GaplessPlayback || !cfg.PrioritizeExternalArt || !cfg.EQ.Enabled || cfg.EQ.Bass != 4 {
		t.Fatal("loaded player config not updated")
	}
}
