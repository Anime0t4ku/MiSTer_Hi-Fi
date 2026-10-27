package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func postJSON(h func(http.ResponseWriter, *http.Request), path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h(rec, req)
	return rec
}

func TestJellyfinWebConfigTokenHandling(t *testing.T) {
	withServers(t, map[string]string{"jellyfin.json": fmt.Sprintf(`{"server":"http://jf.local:8096","token":"saved-secret","user_id":%q,"library":%q}`, hexid(0xaa), jfLib)})
	w := &webRemote{}
	rec := httptest.NewRecorder()
	w.handleJellyfinConfig(rec, httptest.NewRequest("GET", "/api/jellyfin/config", nil))
	if strings.Contains(rec.Body.String(), "saved-secret") || strings.Contains(rec.Body.String(), hexid(0xaa)) {
		t.Fatal("settings exposed the token or the user id")
	}
	if !strings.Contains(rec.Body.String(), `"signed_in":true`) {
		t.Fatalf("signed-in state not shown: %s", rec.Body.String())
	}

	// the same server: token and user are kept, the rest changes
	rec = postJSON(w.handleJellyfinConfig, "/api/jellyfin/config", `{"server":"http://jf.local:8096/","scrobbling":"lastfm"}`)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	saved, err := loadJellyfin()
	if err != nil || saved.Token != "saved-secret" || saved.UserID != hexid(0xaa) || saved.Scrobbling != "lastfm" || saved.Server != "http://jf.local:8096" {
		t.Fatalf("saved = %+v, %v", saved, err)
	}

	// another server: the old token was issued by someone else and must not travel
	rec = postJSON(w.handleJellyfinConfig, "/api/jellyfin/config", `{"server":"http://other.local:8096"}`)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	saved, _ = loadJellyfin()
	if saved.Token != "" || saved.UserID != "" {
		t.Fatalf("token followed the user to another server: %+v", saved)
	}

	for name, body := range map[string]string{
		"not a url":  `{"server":"not a url"}`,
		"file url":   `{"server":"file:///etc/passwd"}`,
		"bad lib":    `{"server":"http://x:8096","library":"../x"}`,
		"bad scrob":  `{"server":"http://x:8096","scrobbling":"all"}`,
		"huge limit": `{"server":"http://x:8096","max_track_mb":99999}`,
		"unknown":    `{"server":"http://x:8096","evil":true}`,
	} {
		if rec := postJSON(w.handleJellyfinConfig, "/api/jellyfin/config", body); rec.Code != 400 {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
		}
	}

	req := httptest.NewRequest("POST", "http://mister.local/api/jellyfin/config", strings.NewReader(`{"server":"http://x:8096"}`))
	req.Header.Set("Origin", "http://evil.example")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	w.handleJellyfinConfig(rec, req)
	if rec.Code != 403 {
		t.Error("a settings write from another site was accepted")
	}
}

func TestJellyfinWebQuickConnectFlow(t *testing.T) {
	f := newFakeJellyfin(t)
	withServers(t, map[string]string{"jellyfin.json": fmt.Sprintf(`{"server":%q,"library":%q}`, f.URL, jfLib)})
	old := jellyfinPollInterval
	jellyfinPollInterval = 20 * time.Millisecond
	t.Cleanup(func() { jellyfinPollInterval = old; jellyfinLoginCancel("") })
	w := &webRemote{}

	rec := postJSON(w.handleJellyfinQuickConnect, "/api/jellyfin/quickconnect", `{"action":"start"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "123456") {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	status := func() string {
		r := httptest.NewRecorder()
		w.handleJellyfinQuickConnect(r, httptest.NewRequest("GET", "/api/jellyfin/quickconnect", nil))
		return r.Body.String()
	}
	if !strings.Contains(status(), `"state":"pending"`) {
		t.Fatalf("not pending: %s", status())
	}
	f.mu.Lock()
	f.approved = true
	f.mu.Unlock()
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(status(), "signed_in") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(status(), `"state":"signed_in"`) {
		t.Fatalf("not signed in: %s", status())
	}
	saved, err := loadJellyfin()
	if err != nil || saved.Token != "tok" || saved.UserID != hexid(0xaa) || saved.Library != jfLib {
		t.Fatalf("saved = %+v, %v (the library chosen in the form must survive the sign-in)", saved, err)
	}
}

func TestJellyfinWebQuickConnectCancelAndNoServer(t *testing.T) {
	withServers(t, nil)
	w := &webRemote{}
	if rec := postJSON(w.handleJellyfinQuickConnect, "/api/jellyfin/quickconnect", `{"action":"start"}`); rec.Code != 400 {
		t.Errorf("start without a saved server: %d", rec.Code)
	}
	f := newFakeJellyfin(t)
	withServers(t, map[string]string{"jellyfin.json": fmt.Sprintf(`{"server":%q}`, f.URL)})
	t.Cleanup(func() { jellyfinLoginCancel("") })
	postJSON(w.handleJellyfinQuickConnect, "/api/jellyfin/quickconnect", `{"action":"start"}`)
	postJSON(w.handleJellyfinQuickConnect, "/api/jellyfin/quickconnect", `{"action":"cancel"}`)
	r := httptest.NewRecorder()
	w.handleJellyfinQuickConnect(r, httptest.NewRequest("GET", "/api/jellyfin/quickconnect", nil))
	if !strings.Contains(r.Body.String(), `"state":"error"`) {
		t.Errorf("cancel not shown: %s", r.Body.String())
	}
	if rec := postJSON(w.handleJellyfinQuickConnect, "/api/jellyfin/quickconnect", `{"action":"dance"}`); rec.Code != 400 {
		t.Errorf("unknown action: %d", rec.Code)
	}
}

func TestJellyfinSourceConnectionTest(t *testing.T) {
	f := newFakeJellyfin(t)
	withServers(t, jellyfinCfgFor(f.URL))
	w := &webRemote{}
	ok := postJSON(w.handleSourceTest, "/api/settings/test", fmt.Sprintf(`{"kind":"jellyfin","jellyfin":{"server":%q}}`, f.URL))
	if ok.Code != 200 {
		t.Fatalf("test of a signed-in server: %d %s", ok.Code, ok.Body.String())
	}
	// not signed in on this server: say what to do
	withServers(t, map[string]string{"jellyfin.json": fmt.Sprintf(`{"server":%q}`, f.URL)})
	bad := postJSON(w.handleSourceTest, "/api/settings/test", fmt.Sprintf(`{"kind":"jellyfin","jellyfin":{"server":%q}}`, f.URL))
	if bad.Code != 502 || !strings.Contains(bad.Body.String(), "Quick Connect") {
		t.Fatalf("test without sign-in: %d %s", bad.Code, bad.Body.String())
	}
	if rec := postJSON(w.handleSourceTest, "/api/settings/test", `{"kind":"jellyfin","jellyfin":{"server":"ftp://x"}}`); rec.Code != 400 {
		t.Errorf("a non-http server was accepted: %d", rec.Code)
	}
}
