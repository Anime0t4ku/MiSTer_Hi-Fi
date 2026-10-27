package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// A fake Emby: everything under /emby, the token in X-Emby-Token, item queries
// under /Users/<id>/, numeric item ids, a user name and password sign-in.

type fakeEmby struct {
	*httptest.Server
	mu    sync.Mutex
	calls []string
}

const embyUser = "0123456789abcdef0123456789abcdef"

func newFakeEmby(t *testing.T) *fakeEmby {
	f := &fakeEmby{}
	mux := http.NewServeMux()
	js := func(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("X-Emby-Token") != "etok" || r.Header.Get("Authorization") != "" {
			http.Error(w, "no", 401)
			return false
		}
		return true
	}
	rec := func(r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
	}
	mux.HandleFunc("/emby/System/Info/Public", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"ProductName": "Emby Server", "Version": "4.9.0.0"})
	})
	mux.HandleFunc("/emby/Sessions/Playing", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		var b map[string]any // Emby answers 400 to a start without PlaySessionId
		json.NewDecoder(r.Body).Decode(&b)
		if b["PlaySessionId"] == nil || b["PlaySessionId"] == "" {
			http.Error(w, "Value cannot be null. (Parameter 'key')", 400)
			return
		}
		rec(r)
	})
	mux.HandleFunc("/emby/Users/AuthenticateByName", func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("X-Emby-Authorization"), `Client="MiSTer Hi-Fi"`) {
			t.Errorf("sign-in without X-Emby-Authorization: %q", r.Header.Get("X-Emby-Authorization"))
		}
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		if b["Username"] != "rick" || b["Pw"] != "secret" {
			http.Error(w, "bad", 401)
			return
		}
		js(w, map[string]any{"AccessToken": "etok", "User": map[string]any{"Id": embyUser}})
	})
	mux.HandleFunc("/emby/Users/"+embyUser+"/Views", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			js(w, map[string]any{"TotalRecordCount": 2, "Items": []map[string]any{
				{"Id": "7", "Name": "Movies", "CollectionType": "movies"},
				{"Id": "8", "Name": "Music", "CollectionType": "music"}}})
		}
	})
	mux.HandleFunc("/emby/Users/"+embyUser+"/Items", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		q := r.URL.Query()
		switch {
		case q.Get("parentId") == "501":
			js(w, map[string]any{"TotalRecordCount": 1, "Items": []map[string]any{
				{"Id": "9001", "Name": "Intro", "Album": "Meets Frankie", "AlbumId": "501", "Artists": []string{"Czarface"},
					"IndexNumber": 1, "RunTimeTicks": 1800000000,
					"MediaSources": []map[string]any{{"Container": "flac", "Bitrate": 900000}}}}})
		case q.Get("includeItemTypes") == "MusicAlbum":
			if q.Get("parentId") != "8" {
				t.Errorf("albums asked outside the music library: %v", q)
			}
			js(w, map[string]any{"TotalRecordCount": 1, "Items": []map[string]any{
				{"Id": "501", "Name": "Meets Frankie", "AlbumArtists": []map[string]any{{"Name": "Czarface"}}}}})
		default:
			js(w, map[string]any{"TotalRecordCount": 0, "Items": []any{}})
		}
	})
	mux.HandleFunc("/emby/Items/9001/Download", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			w.Write([]byte("FLACDATA"))
		}
	})
	for _, p := range []string{"/emby/Sessions/Playing/Stopped", "/emby/Users/" + embyUser + "/PlayedItems/9001"} {
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			if auth(w, r) {
				rec(r)
			}
		})
	}
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func TestEmbySignInBrowseAndPlay(t *testing.T) {
	f := newFakeEmby(t)
	withServers(t, map[string]string{"emby.json": `{"server":"` + f.URL + `"}`})
	srv := jellyfinServer{emby: true}
	if !srv.Configured() || srv.ID() != "emby" || srv.Label() != "EMBY" {
		t.Fatalf("emby.json should make the EMBY entry appear (%v %q)", srv.Configured(), srv.Label())
	}
	ctx := context.Background()
	if _, err := srv.Connect(ctx); err != errNotSignedIn {
		t.Fatalf("Connect before signing in = %v, want errNotSignedIn", err)
	}
	cfg, _ := loadEmby()
	if _, err := cfg.embySignIn(ctx, "rick", "wrong"); err == nil {
		t.Fatal("a wrong password must fail")
	}
	if _, err := cfg.embySignIn(ctx, "rick", "secret"); err != nil {
		t.Fatalf("sign-in: %v", err)
	}
	saved, err := loadEmby()
	if err != nil || saved.Token != "etok" || saved.UserID != embyUser || !saved.emby {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	if _, err := loadJellyfin(); err == nil {
		t.Fatal("signing in to Emby must not create jellyfin.json")
	}

	sess, err := srv.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	albums, err := sess.Albums(ctx, false)
	if err != nil || len(albums) != 1 || albums[0].ID != "501" || albums[0].Artist != "Czarface" {
		t.Fatalf("albums = %+v, %v (numeric Emby ids must be accepted)", albums, err)
	}
	tracks, err := sess.AlbumTracks(ctx, "501")
	if err != nil || len(tracks) != 1 {
		t.Fatalf("tracks = %+v, %v", tracks, err)
	}
	tr := tracks[0]
	if tr.Path != "emby:9001" || tr.SrvKey != "9001" || tr.Title != "Intro" || tr.SrvExt != ".flac" {
		t.Fatalf("track = %+v", tr)
	}
	rc, _, err := sess.Download(ctx, tr)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "FLACDATA" {
		t.Fatalf("downloaded %q", b)
	}
	if err := sess.Report(ctx, tr, "playing", 3*time.Second); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if err := sess.Played(ctx, tr); err != nil {
		t.Fatalf("Played: %v", err)
	}
	f.mu.Lock()
	got := strings.Join(f.calls, ",")
	f.mu.Unlock()
	if got != "POST /emby/Sessions/Playing,POST /emby/Users/"+embyUser+"/PlayedItems/9001" {
		t.Fatalf("server calls = %s", got)
	}
	if isServerPath("emby:9001") != true {
		t.Fatal("emby:<id> must be a server path")
	}
}

func TestEmbyWebConfigAndSignIn(t *testing.T) {
	f := newFakeEmby(t)
	withServers(t, nil)
	w := &webRemote{}
	rec := postJSON(w.handleEmbyConfig, "/api/emby/config", `{"server":"`+f.URL+`/","scrobbling":"lastfm"}`)
	if rec.Code != 200 {
		t.Fatalf("save = %d %s", rec.Code, rec.Body)
	}
	if _, err := loadJellyfin(); err == nil {
		t.Fatal("the Emby form wrote jellyfin.json")
	}
	if c, err := loadEmby(); err != nil || c.Server != f.URL || c.Scrobbling != "lastfm" {
		t.Fatalf("emby.json = %+v, %v", c, err)
	}
	if rec := postJSON(w.handleEmbySignIn, "/api/emby/signin", `{"user":"rick","password":"nope"}`); rec.Code == 200 {
		t.Fatal("a wrong password signed in")
	}
	if rec := postJSON(w.handleEmbySignIn, "/api/emby/signin", `{"user":"rick","password":"secret"}`); rec.Code != 200 {
		t.Fatalf("sign-in = %d %s", rec.Code, rec.Body)
	}
	get := httptest.NewRecorder()
	w.handleEmbyConfig(get, httptest.NewRequest("GET", "/api/emby/config", nil))
	if strings.Contains(get.Body.String(), "etok") || strings.Contains(get.Body.String(), embyUser) || !strings.Contains(get.Body.String(), `"signed_in":true`) {
		t.Fatalf("GET leaks credentials or hides the state: %s", get.Body)
	}
	ok := postJSON(w.handleSourceTest, "/api/settings/test", `{"kind":"emby","emby":{"server":"`+f.URL+`"}}`)
	if ok.Code != 200 {
		t.Fatalf("connection test = %d %s", ok.Code, ok.Body)
	}
}
