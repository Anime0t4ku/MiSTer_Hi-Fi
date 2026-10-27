package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// hexid is a Jellyfin-style item id (32 hex digits) for tests.
func hexid(n int) string { return fmt.Sprintf("%032x", n) }

// withServers points the server config files and the track cache at temp
// folders; files maps config file name -> content.
func withServers(t *testing.T, files map[string]string) {
	t.Helper()
	dir := t.TempDir()
	oldJ, oldE, oldP, oldTmp := jellyfinConfigPath, embyConfigPath, plexConfigPath, srvTmpDir
	jellyfinConfigPath = filepath.Join(dir, "jellyfin.json")
	embyConfigPath = filepath.Join(dir, "emby.json")
	plexConfigPath = filepath.Join(dir, "plex.json")
	srvTmpDir = filepath.Join(dir, "cache")
	t.Cleanup(func() { jellyfinConfigPath, embyConfigPath, plexConfigPath, srvTmpDir = oldJ, oldE, oldP, oldTmp })
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func jellyfinCfgFor(url string) map[string]string {
	return map[string]string{"jellyfin.json": fmt.Sprintf(`{"server":%q,"token":"tok","user_id":%q}`, url, hexid(0xaa))}
}

func TestServersOnlyShowWhenConfigured(t *testing.T) {
	withServers(t, nil)
	if n := len(configuredMediaServers()); n != 0 {
		t.Fatalf("no config files, but %d servers are configured", n)
	}
	withServers(t, map[string]string{"jellyfin.json": `{"server":"http://x:8096"}`})
	got := configuredMediaServers()
	if len(got) != 1 || got[0].ID() != "jellyfin" {
		t.Fatalf("configured servers = %v, want only jellyfin", got)
	}
	if configuredServerByID("plex") != nil {
		t.Error("plex is not configured but was found")
	}
}

func TestIsServerPath(t *testing.T) {
	for p, want := range map[string]bool{
		"plex:123": true, "jellyfin:abc": true, "http://x/y": false,
		"/media/fat/a.flac": false, "cdda:1": false, "nosuch:1": false,
	} {
		if got := isServerPath(p); got != want {
			t.Errorf("isServerPath(%q) = %v, want %v", p, got, want)
		}
	}
}

// ---- a fake Jellyfin ----

type fakeJellyfin struct {
	*httptest.Server
	mu       sync.Mutex
	calls    []string
	approved bool
}

var (
	jfLib   = hexid(0x11)
	jfArt   = hexid(0xa1)
	jfAlb1  = hexid(0xb1)
	jfAlb2  = hexid(0xb2)
	jfAlb3  = hexid(0xb3)
	jfAlbC  = hexid(0xbc)
	jfTrk1  = hexid(0xc1)
	jfTrk2  = hexid(0xc2)
	jfTrkC1 = hexid(0xcc)
)

func newFakeJellyfin(t *testing.T) *fakeJellyfin {
	f := &fakeJellyfin{}
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.Contains(r.Header.Get("Authorization"), `Token="tok"`) {
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
	js := func(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/System/Info/Public", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"ProductName": "Jellyfin Server", "Version": "12.3.0"})
	})
	mux.HandleFunc("/UserViews", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		js(w, map[string]any{"TotalRecordCount": 2, "Items": []map[string]any{
			{"Id": hexid(0x99), "Name": "Movies", "CollectionType": "movies"},
			{"Id": jfLib, "Name": "Music", "CollectionType": "music"}}})
	})
	mux.HandleFunc("/Artists/AlbumArtists", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		if r.URL.Query().Get("parentId") != jfLib {
			t.Errorf("artists asked outside the music library")
		}
		js(w, map[string]any{"TotalRecordCount": 1, "Items": []map[string]any{{"Id": jfArt, "Name": "Czarface"}}})
	})
	mux.HandleFunc("/Items", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		q := r.URL.Query()
		switch {
		case q.Get("parentId") == jfAlb1:
			js(w, map[string]any{"TotalRecordCount": 2, "Items": []map[string]any{
				{"Id": jfTrk1, "Name": "Intro", "Album": "Meets Frankie", "AlbumId": jfAlb1, "Artists": []string{"Czarface"},
					"IndexNumber": 1, "RunTimeTicks": 1800000000,
					"MediaSources": []map[string]any{{"Container": "flac", "Bitrate": 900000,
						"MediaStreams": []map[string]any{{"Type": "Audio", "BitDepth": 16, "SampleRate": 44100}}}}},
				{"Id": jfTrk2, "Name": "Second"}}}) // sparse: no sources, no duration
		case q.Get("includeItemTypes") == "Audio" && q.Get("searchTerm") != "":
			// a library without artists on its albums: only track names say who it is
			var out []map[string]any
			if strings.Contains("01. cassius - hi water", strings.ToLower(q.Get("searchTerm"))) {
				out = append(out, map[string]any{"Id": jfTrkC1, "Name": "01. Cassius - Hi Water", "Album": "Au rêve (2002)", "AlbumId": jfAlbC})
			}
			js(w, map[string]any{"TotalRecordCount": len(out), "Items": out})
		case q.Get("includeItemTypes") == "MusicAlbum":
			all := []map[string]any{
				{"Id": jfAlb1, "Name": "Czarface Meets Frankie Pulitzer", "AlbumArtists": []map[string]any{{"Name": "Czarface"}}},
				{"Id": jfAlb2, "Name": "Czarface Meets Frankie Pulitzer (Deluxe)", "AlbumArtists": []map[string]any{{"Name": "Czarface"}}},
				{"Id": jfAlb3, "Name": "Other", "AlbumArtists": []map[string]any{{"Name": "Someone"}}},
				{"Id": jfAlbC, "Name": "Au rêve (2002)"},
			}
			term := strings.ToLower(q.Get("searchTerm"))
			var out []map[string]any
			for _, a := range all {
				if term == "" || strings.Contains(strings.ToLower(a["Name"].(string)), term) {
					out = append(out, a)
				}
			}
			js(w, map[string]any{"TotalRecordCount": len(out), "Items": out})
		default:
			js(w, map[string]any{"TotalRecordCount": 0, "Items": []any{}})
		}
	})
	mux.HandleFunc("/Items/"+jfTrk1+"/Download", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			w.Write([]byte("FLACDATA"))
		}
	})
	for _, p := range []string{"/Sessions/Playing", "/Sessions/Playing/Progress", "/Sessions/Playing/Stopped", "/UserPlayedItems/" + jfTrk1} {
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			if auth(w, r) {
				rec(r)
			}
		})
	}
	mux.HandleFunc("/QuickConnect/Initiate", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"Secret": "sec", "Code": "123456"})
	})
	mux.HandleFunc("/QuickConnect/Connect", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		ok := f.approved
		f.mu.Unlock()
		js(w, map[string]any{"Secret": "sec", "Authenticated": ok})
	})
	mux.HandleFunc("/Users/AuthenticateWithQuickConnect", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		if b["Secret"] != "sec" {
			http.Error(w, "bad", 400)
			return
		}
		js(w, map[string]any{"AccessToken": "tok", "User": map[string]any{"Id": hexid(0xaa)}})
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func jellyfinSessionFor(t *testing.T, f *fakeJellyfin) mediaSession {
	t.Helper()
	withServers(t, jellyfinCfgFor(f.URL))
	sess, err := jellyfinServer{}.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestJellyfinBrowseAndPlay(t *testing.T) {
	f := newFakeJellyfin(t)
	sess := jellyfinSessionFor(t, f)
	ctx := context.Background()

	artists, err := sess.Artists(ctx)
	if err != nil || len(artists) != 1 || artists[0].Title != "Czarface" {
		t.Fatalf("artists = %v, %v", artists, err)
	}
	albums, err := sess.Albums(ctx, false)
	if err != nil || len(albums) != 4 || albums[0].Artist != "Czarface" {
		t.Fatalf("albums = %v, %v", albums, err)
	}
	tracks, err := sess.AlbumTracks(ctx, jfAlb1)
	if err != nil || len(tracks) != 2 {
		t.Fatalf("tracks = %v, %v", tracks, err)
	}
	t1 := tracks[0]
	if t1.SrvKey != jfTrk1 || t1.Title != "Intro" || t1.Artist != "Czarface" ||
		t1.Number != 1 || t1.Duration != 180 || t1.SrvExt != ".flac" || t1.BitRate != 900 || t1.BitDepth != 16 || t1.SampleRate != 44100 {
		t.Errorf("track mapped wrongly: %+v", t1)
	}
	if t1.SrvThumb != jfAlb1 {
		t.Errorf("cover should come from the album, got %q", t1.SrvThumb)
	}
	if tracks[1].Title != "Second" || tracks[1].Duration != 0 {
		t.Errorf("sparse track mapped wrongly: %+v", tracks[1])
	}

	// the download goes through the cache, with the auth header
	local, err := srvEnsureLocal(ctx, sess, t1)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(local); string(b) != "FLACDATA" || !strings.HasSuffix(local, ".flac") {
		t.Errorf("cached %s = %q", local, b)
	}

	// reports: start, progress, stop, played
	sess.Report(ctx, t1, "playing", 0)
	sess.Report(ctx, t1, "paused", 5*time.Second)
	sess.Report(ctx, t1, "stopped", 6*time.Second)
	sess.Played(ctx, t1)
	f.mu.Lock()
	got := strings.Join(f.calls, ",")
	f.mu.Unlock()
	want := "POST /Sessions/Playing,POST /Sessions/Playing/Progress,POST /Sessions/Playing/Stopped,POST /UserPlayedItems/" + jfTrk1
	if got != want {
		t.Errorf("reports = %s\nwant      %s", got, want)
	}
}

func TestJellyfinFindAlbum(t *testing.T) {
	f := newFakeJellyfin(t)
	sess := jellyfinSessionFor(t, f)
	ctx := context.Background()
	// "artist album" is in no title: the longest word finds the candidates, the shortest title wins
	if a, err := sess.FindAlbum(ctx, "Czarface Meets Frankie Pulitzer"); err != nil || a.ID != jfAlb1 {
		t.Fatalf("FindAlbum = %+v, %v", a, err)
	}
	// no artist on the album: the track's name carries it
	if a, err := sess.FindAlbum(ctx, "Cassius Au rêve"); err != nil || a.ID != jfAlbC {
		t.Fatalf("FindAlbum(sparse library) = %+v, %v", a, err)
	}
	if _, err := sess.FindAlbum(ctx, "nothing like this"); err == nil {
		t.Error("expected an error for no match")
	}
}

func TestJellyfinNotSignedIn(t *testing.T) {
	f := newFakeJellyfin(t)
	withServers(t, map[string]string{"jellyfin.json": fmt.Sprintf(`{"server":%q}`, f.URL)})
	if _, err := (jellyfinServer{}).Connect(context.Background()); err != errNotSignedIn {
		t.Fatalf("Connect without a token = %v, want errNotSignedIn", err)
	}
}

func TestJellyfinQuickConnect(t *testing.T) {
	f := newFakeJellyfin(t)
	withServers(t, map[string]string{"jellyfin.json": fmt.Sprintf(`{"server":%q}`, f.URL)})
	ctx := context.Background()
	c, _ := loadJellyfin()
	qc, err := c.quickConnectStart(ctx)
	if err != nil || qc.Code != "123456" {
		t.Fatalf("start = %+v, %v", qc, err)
	}
	if ok, _ := c.quickConnectApproved(ctx, qc.Secret); ok {
		t.Error("approved before the code was entered")
	}
	f.mu.Lock()
	f.approved = true
	f.mu.Unlock()
	if ok, _ := c.quickConnectApproved(ctx, qc.Secret); !ok {
		t.Error("not approved after the code was entered")
	}
	if _, err := c.quickConnectFinish(ctx, qc.Secret); err != nil {
		t.Fatal(err)
	}
	c2, err := loadJellyfin()
	if err != nil || c2.Token != "tok" || c2.UserID != hexid(0xaa) || c2.Server != f.URL {
		t.Fatalf("saved config = %+v, %v", c2, err)
	}
	if st, _ := os.Stat(jellyfinConfigPath); st.Mode().Perm()&0o077 != 0 {
		t.Errorf("config mode = %v: it holds a token, others must not read it", st.Mode().Perm())
	}
}

func TestJellyfinConfigValidation(t *testing.T) {
	for name, body := range map[string]string{
		"no server":            `{"token":"t"}`,
		"bad library":          `{"server":"http://x:8096","library":"../etc"}`,
		"bad user":             `{"server":"http://x:8096","user_id":"nope"}`,
		"bad scrobbling":       `{"server":"http://x:8096","scrobbling":"everyone"}`,
		"track limit":          `{"server":"http://x:8096","max_track_mb":5000}`,
		"file instead of http": `{"server":"file:///etc/passwd"}`,
	} {
		withServers(t, map[string]string{"jellyfin.json": body})
		if _, err := loadJellyfin(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	withServers(t, map[string]string{"jellyfin.json": `{"server":"http://x:8096/","scrobbling":"plex"}`})
	c, err := loadJellyfin()
	if err != nil || c.Scrobbling != "server" || c.Server != "http://x:8096" || c.MaxTrackMB != 128 {
		t.Fatalf("defaults = %+v, %v", c, err)
	}
}

func TestJellyfinIDs(t *testing.T) {
	for id, want := range map[string]bool{
		hexid(1): true, "0123456789abcdef0123456789ABCDEF": true,
		"01234567-89ab-cdef-0123-456789abcdef": true,
		"":                                     false, "short": false, "../../etc/passwd": false, hexid(1) + "x": false,
		"http:evil": false, strings.Repeat("g", 32): false,
	} {
		if got := jellyfinID(id); got != want {
			t.Errorf("jellyfinID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestExternalTargetsAcrossServers(t *testing.T) {
	f := newFakeJellyfin(t)
	withServers(t, jellyfinCfgFor(f.URL))
	q, handled, err := srvExternalQueue("jellyfin://album/" + jfAlb1)
	if !handled || err != nil || len(q.Tracks) != 2 {
		t.Fatalf("jellyfin://album: handled=%v err=%v tracks=%d", handled, err, len(q.Tracks))
	}
	if q.Tracks[0].Srv == nil || q.Tracks[0].SrvID != "jellyfin" {
		t.Errorf("tracks are not attached to their session: %+v", q.Tracks[0])
	}
	q, handled, err = srvExternalQueue("jellyfin://search/Czarface%20Meets%20Frankie%20Pulitzer")
	if !handled || err != nil || len(q.Tracks) != 2 {
		t.Fatalf("jellyfin://search: handled=%v err=%v tracks=%d", handled, err, len(q.Tracks))
	}
	if _, handled, _ := srvExternalQueue("smb://x/y"); handled {
		t.Error("smb:// is not a media server target")
	}
	if _, handled, err := srvExternalQueue("jellyfin://bogus/1"); !handled || err == nil {
		t.Errorf("unknown kind: handled=%v err=%v", handled, err)
	}
	if _, _, err := srvExternalQueue("jellyfin://album/../../etc"); err == nil {
		t.Error("an id that is no id was accepted")
	}
}

func TestWebTreeAcrossServers(t *testing.T) {
	f := newFakeJellyfin(t)
	sess := jellyfinSessionFor(t, f)
	ctx := context.Background()
	entries, _, err := srvWebBrowse(ctx, sess, "Jellyfin", "/")
	if err != nil || len(entries) != 4 || entries[0].Path != "/artists" {
		t.Fatalf("menu = %+v, %v", entries, err)
	}
	entries, _, err = srvWebBrowse(ctx, sess, "Jellyfin", "/albums")
	if err != nil || len(entries) != 4 || entries[0].Name != "Czarface - Czarface Meets Frankie Pulitzer" || entries[0].Path != "/albums/"+jfAlb1 {
		t.Fatalf("albums = %+v, %v", entries, err)
	}
	entries, tracks, err := srvWebBrowse(ctx, sess, "Jellyfin", "/albums/"+jfAlb1)
	if err != nil || len(tracks) != 2 || entries[0].IsDir || entries[1].Path != "/albums/"+jfAlb1+"/track/1" {
		t.Fatalf("tracks = %+v %v, %v", entries, tracks, err)
	}
	if _, _, err = srvWebBrowse(ctx, sess, "Jellyfin", "/albums/http:evil"); err == nil {
		t.Error("an invalid path was accepted")
	}
	if _, _, err = srvWebBrowse(ctx, sess, "Jellyfin", "/bogus"); err == nil {
		t.Error("an unknown category was accepted")
	}
	q, err := srvWebQueue(ctx, jellyfinServer{}, "/albums/"+jfAlb1+"/track/1")
	if err != nil || q.Index != 1 || len(q.Tracks) != 2 {
		t.Fatalf("queue = %+v, %v", q, err)
	}
	if _, err := srvWebQueue(ctx, jellyfinServer{}, "/albums/"+jfAlb1+"/track/9"); err == nil {
		t.Error("an unknown track was accepted")
	}
}

func TestOnlyConfiguredServersAreBrowsable(t *testing.T) {
	withServers(t, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/browse?source=jellyfin&path=/", nil)
	(&webRemote{}).handleBrowse(rec, req)
	if rec.Code == 200 {
		t.Error("an unconfigured server answered a browse request")
	}
	rec = httptest.NewRecorder()
	(&webRemote{}).handleSources(rec, httptest.NewRequest("GET", "/api/sources", nil))
	for _, id := range []string{"plex", "jellyfin"} {
		if strings.Contains(rec.Body.String(), `"`+id+`"`) {
			t.Errorf("unconfigured %s is listed as a source: %s", id, rec.Body.String())
		}
	}
}

func TestTrackCacheKeepsServersApart(t *testing.T) {
	f := newFakeJellyfin(t)
	sess := jellyfinSessionFor(t, f)
	tr := Track{SrvKey: hexid(7), SrvPart: "/x", SrvExt: ".flac"}
	a := srvCachePath(sess, tr)
	other := &jellyfinSession{cfg: JellyfinConfig{Server: f.URL + "/other", UserID: hexid(0xaa)}}
	if a == srvCachePath(other, tr) {
		t.Error("cache path ignores the server")
	}
	if p := (&plexSession{cfg: PlexConfig{Server: f.URL, Token: "x"}}); a == srvCachePath(p, tr) {
		t.Error("cache path ignores the kind of server")
	}
}

func TestEnsureLocalLimitAndBadBody(t *testing.T) {
	f := newFakeJellyfin(t)
	sess := jellyfinSessionFor(t, f)
	js := sess.(*jellyfinSession)
	js.cfg.MaxTrackMB = 1
	tr := Track{SrvID: "jellyfin", SrvKey: jfTrk1, SrvPart: "/x", SrvExt: ".flac"}
	if _, err := srvEnsureLocal(context.Background(), js, tr); err != nil {
		t.Fatalf("small track refused: %v", err)
	}
	// a track the server has no file for
	tr.SrvKey = hexid(0xdead)
	if _, err := srvEnsureLocal(context.Background(), js, tr); err == nil {
		t.Error("a missing file was cached")
	}
	files, _ := os.ReadDir(srvTmpDir)
	for _, e := range files {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Errorf("orphaned partial download %s", e.Name())
		}
	}
	_ = bytes.MinRead
}

func TestGaplessForServerTracks(t *testing.T) {
	for ext, want := range map[string]bool{".flac": true, ".FLAC": true, ".wav": true, ".mp3": false, ".m4a": false, "": false} {
		if got := gaplessTrackSupported(Track{SrvKey: "1", SrvExt: ext, Path: "plex:1"}); got != want {
			t.Errorf("gaplessTrackSupported(server track %q) = %v, want %v", ext, got, want)
		}
	}
	// the queued next track is the one the prefetch fetches, also when shuffled; Repeat One never queues
	p := &Player{}
	p.q = Queue{Tracks: []Track{{SrvKey: "a"}, {SrvKey: "b"}, {SrvKey: "c"}}, Index: 0}
	if n := p.nextGaplessIndexLocked(0); n != 1 {
		t.Errorf("next = %d, want 1", n)
	}
	p.q.Shuffle = true
	n := p.nextGaplessIndexLocked(0)
	if n != p.srvNextLocked(false) || n == 0 {
		t.Errorf("shuffled next = %d, not the prefetch's choice", n)
	}
	p.q.Repeat, p.q.RepeatOne = true, true
	if n := p.nextGaplessIndexLocked(0); n != -1 {
		t.Errorf("repeat one queued %d, want none", n)
	}
	// local files do not queue gaplessly when shuffled
	p.q = Queue{Tracks: []Track{{Path: "/a.flac"}, {Path: "/b.flac"}}, Shuffle: true}
	if n := p.nextGaplessIndexLocked(0); n != -1 {
		t.Errorf("shuffled local next = %d, want none", n)
	}
}

func TestSrvExtFromContainer(t *testing.T) {
	for in, want := range map[string]string{
		"mp4": ".m4a", "m4a": ".m4a", "MP4": ".m4a", "mov,mp4,m4a,3gp,3g2,mj2": ".m4a",
		"flac": ".flac", "mp3": ".mp3", "ogg": ".ogg", "wav": ".wav", "": "",
	} {
		if got := srvExtFromContainer(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
	if !supported[srvExtFromContainer("mp4")] {
		t.Error("an mp4 container must map onto a supported extension")
	}
}
