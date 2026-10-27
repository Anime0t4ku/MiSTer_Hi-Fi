package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func exportTestPaths(t *testing.T) {
	t.Helper()
	oldFile, oldCover := nowPlayingFile, nowPlayingCover
	dir := t.TempDir()
	nowPlayingFile = filepath.Join(dir, "now.json")
	nowPlayingCover = filepath.Join(dir, "cover.jpg")
	clearNowPlaying()
	t.Cleanup(func() { clearNowPlaying(); nowPlayingFile, nowPlayingCover = oldFile, oldCover })
}
func readExport(t *testing.T) nowPlayingInfo {
	t.Helper()
	b, err := os.ReadFile(nowPlayingFile)
	if err != nil {
		t.Fatal(err)
	}
	var info nowPlayingInfo
	if err = json.Unmarshal(b, &info); err != nil {
		t.Fatal(err)
	}
	return info
}
func TestNowPlayingSameAlbumTitleArtAndState(t *testing.T) {
	exportTestPaths(t)
	red := image.NewRGBA(image.Rect(0, 0, 2, 2))
	red.Set(0, 0, color.RGBA{R: 255, A: 255})
	track := Track{Title: "First", Artist: "Artist", Album: "Album", Path: "one.flac", Art: red}
	if err := publishNowPlayingState(track, "playing", 0, 180); err != nil {
		t.Fatal(err)
	}
	track.Title = "Second"
	track.Path = "two.flac"
	if err := publishNowPlayingState(track, "paused", 30, 180); err != nil {
		t.Fatal(err)
	}
	if got := readExport(t); got.Title != "Second" || got.State != "paused" || got.Position != 30 || !got.Cover {
		t.Fatalf("stale export: %+v", got)
	}
	blue := image.NewRGBA(image.Rect(0, 0, 2, 2))
	blue.Set(0, 0, color.RGBA{B: 255, A: 255})
	track.Art = blue
	old, _ := os.ReadFile(nowPlayingCover)
	if err := publishNowPlayingState(track, "playing", 31, 180); err != nil {
		t.Fatal(err)
	}
	newArt, _ := os.ReadFile(nowPlayingCover)
	if string(old) == string(newArt) {
		t.Fatal("replacement art not published")
	}
	clearNowPlaying()
	if _, err := os.Stat(nowPlayingFile); !os.IsNotExist(err) {
		t.Fatal("metadata not cleared")
	}
	if _, err := os.Stat(nowPlayingCover); !os.IsNotExist(err) {
		t.Fatal("art not cleared")
	}
}
func TestNowPlayingRetriesFailedWrite(t *testing.T) {
	exportTestPaths(t)
	file := nowPlayingFile
	nowPlayingFile = filepath.Join(filepath.Dir(file), "missing", "now.json")
	track := Track{Title: "Retry", Artist: "Artist", Album: "Album"}
	if err := publishNowPlayingState(track, "playing", 0, 100); err == nil {
		t.Fatal("expected failed write")
	}
	if nowPlayingState.key != "" {
		t.Fatal("failed write cached as success")
	}
	if err := os.MkdirAll(filepath.Dir(nowPlayingFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := publishNowPlayingState(track, "playing", 0, 100); err != nil {
		t.Fatal(err)
	}
	if readExport(t).Title != "Retry" {
		t.Fatal("retry failed")
	}
}
func TestPlexListPaginationAndHTTPError(t *testing.T) {
	var pages atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "secret" {
			t.Error("token missing")
		}
		if r.URL.Path == "/error" {
			w.WriteHeader(503)
			fmt.Fprint(w, `{"MediaContainer":{}}`)
			return
		}
		if r.URL.Path == "/unauthorized" {
			w.WriteHeader(401)
			return
		}
		pages.Add(1)
		if r.URL.Query().Get("X-Plex-Container-Start") == "0" {
			fmt.Fprint(w, `{"MediaContainer":{"totalSize":2,"Metadata":[{"ratingKey":"1","title":"One"}]}}`)
		} else {
			fmt.Fprint(w, `{"MediaContainer":{"totalSize":2,"Metadata":[{"ratingKey":"2","title":"Two"}]}}`)
		}
	}))
	defer server.Close()
	cfg := PlexConfig{Server: server.URL, Token: "secret"}
	list, err := cfg.list("/list", nil, 0)
	if err != nil || len(list) != 2 || pages.Load() != 2 {
		t.Fatalf("pagination: %v, %v", list, err)
	}
	if _, err = cfg.list("/error", nil, 0); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("HTTP error ignored: %v", err)
	}
	if _, err = cfg.list("/unauthorized", nil, 0); err == nil || !strings.Contains(err.Error(), "token rejected") {
		t.Fatalf("401: %v", err)
	}
}
func TestPlexDownloadConcurrentAndCancellation(t *testing.T) {
	cacheTestPath(t)
	_ = os.RemoveAll(srvTmpDir)
	defer os.RemoveAll(srvTmpDir)
	var requests atomic.Int32
	entered := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			entered <- struct{}{}
			<-r.Context().Done()
			return
		}
		requests.Add(1)
		fmt.Fprint(w, "audio bytes")
	}))
	defer server.Close()
	cfg := PlexConfig{Server: server.URL, Token: "secret", MaxTrackMB: 1}
	track := Track{SrvID: "plex", SrvKey: "42", SrvPart: "/audio", SrvExt: ".flac"}
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			file, err := srvEnsureLocal(context.Background(), &plexSession{cfg: cfg}, track)
			if err != nil {
				t.Error(err)
				return
			}
			data, _ := os.ReadFile(file)
			if string(data) != "audio bytes" {
				t.Error("partial audio")
			}
		}()
	}
	group.Wait()
	if requests.Load() != 1 {
		t.Fatalf("duplicate downloads: %d", requests.Load())
	}
	other := cfg
	other.Server += "/other"
	if srvCachePath(&plexSession{cfg: cfg}, track) == srvCachePath(&plexSession{cfg: other}, track) {
		t.Fatal("cache key ignores server")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	track.SrvKey = "43"
	track.SrvPart = "/slow"
	go func() { _, err := srvEnsureLocal(ctx, &plexSession{cfg: cfg}, track); done <- err }()
	<-entered
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("download not cancelled")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation blocked")
	}
	files, _ := os.ReadDir(srvTmpDir)
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".part") {
			t.Fatal("orphaned partial download")
		}
	}
}
func TestPlexDownloadSizeLimit(t *testing.T) {
	cacheTestPath(t)
	_ = os.RemoveAll(srvTmpDir)
	defer os.RemoveAll(srvTmpDir)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "2097152")
		w.WriteHeader(200)
	}))
	defer server.Close()
	_, err := srvEnsureLocal(context.Background(), &plexSession{cfg: PlexConfig{Server: server.URL, Token: "secret", MaxTrackMB: 1}}, Track{SrvID: "plex", SrvKey: "99", SrvPart: "/large", SrvExt: ".flac"})
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("oversized download accepted: %v", err)
	}
}
func TestPlexPrefetchChoiceMatchesPlayback(t *testing.T) {
	p := newPlayer(Queue{Tracks: make([]Track, 4), Index: 1, Shuffle: true}, Config{}, false)
	p.q.Tracks[1].SrvKey = "1"
	next := p.srvNextLocked(false)
	if next == 1 || next < 0 || next >= 4 {
		t.Fatal(next)
	}
	if got := p.srvNextLocked(true); got != next {
		t.Fatalf("prefetched %d, next plays %d", next, got)
	}
	p.q.Shuffle = false
	p.q.Index = 3
	if p.srvNextLocked(false) != -1 {
		t.Fatal("queue should end")
	}
	p.q.Repeat = true
	if p.srvNextLocked(false) != 0 {
		t.Fatal("repeat should wrap")
	}
	p.q.RepeatOne = true
	if p.srvNextLocked(false) != 3 {
		t.Fatal("repeat one should keep current track")
	}
}
func TestPlexNaturalEndClosesSessionAndExport(t *testing.T) {
	exportTestPaths(t)
	stop := make(chan struct{})
	p := newPlayer(Queue{Tracks: []Track{{Title: "Song", SrvKey: "42"}}}, Config{}, false)
	p.stopped = false
	p.stop = stop
	p.generation = 5
	publishNowPlaying(p.q.Tracks[0])
	p.advanceUnlocked(5, stop)
	select {
	case <-stop:
	default:
		t.Fatal("session channel left open")
	}
	if !p.stopped || p.stop != nil {
		t.Fatal("player not stopped")
	}
	if _, err := os.Stat(nowPlayingFile); !os.IsNotExist(err) {
		t.Fatal("stale now-playing file")
	}
}
func TestPlexPlayThresholdUsesHeardTime(t *testing.T) {
	track := Track{Duration: 180}
	if srvPlayedEnough(track, 10*time.Second) {
		t.Fatal("short listen scrobbled")
	}
	if !srvPlayedEnough(track, 90*time.Second) {
		t.Fatal("half-track not counted")
	}
	if srvPlayedEnough(Track{Duration: 1000}, 239*time.Second) {
		t.Fatal("long track counted too early")
	}
	if !srvPlayedEnough(Track{Duration: 1000}, 240*time.Second) {
		t.Fatal("four minutes not counted")
	}
}
func TestPlexWebBrowseAndPlaylistOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/library/sections/1/all":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"12","title":"Album","parentTitle":"Artist"}]}}`)
		case "/playlists/7/items":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"20","title":"Second","grandparentTitle":"Artist","Media":[{"Part":[{"key":"/part/20","container":"flac"}]}]},{"ratingKey":"10","title":"First","Media":[{"Part":[{"key":"/part/10","container":"flac"}]}]}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	cfg := PlexConfig{Server: server.URL, Token: "secret", Library: "1"}
	entries, _, err := srvWebBrowse(context.Background(), &plexSession{cfg: cfg}, "Plex", "/")
	if err != nil || len(entries) != 4 {
		t.Fatal(entries, err)
	}
	entries, _, err = srvWebBrowse(context.Background(), &plexSession{cfg: cfg}, "Plex", "/albums")
	if err != nil || len(entries) != 1 || entries[0].Name != "Artist - Album" {
		t.Fatal(entries, err)
	}
	entries, tracks, err := srvWebBrowse(context.Background(), &plexSession{cfg: cfg}, "Plex", "/playlists/7")
	if err != nil || len(tracks) != 2 || tracks[0].Title != "Second" || entries[1].Path != "/playlists/7/track/1" {
		t.Fatal(entries, tracks, err)
	}
	if _, _, err = srvWebBrowse(context.Background(), &plexSession{cfg: cfg}, "Plex", "/albums/http:evil"); err == nil {
		t.Fatal("invalid path accepted")
	}
}
func TestPlexSessionStopReportsStopped(t *testing.T) {
	exportTestPaths(t)
	var playing, stopped atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/:/timeline" {
			switch r.URL.Query().Get("state") {
			case "playing":
				playing.Add(1)
			case "stopped":
				stopped.Add(1)
			}
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	cfg := PlexConfig{Server: server.URL, Token: "secret", Scrobbling: "none"}
	track := Track{SrvID: "plex", SrvKey: "1", Duration: 180, Srv: &plexSession{cfg: cfg}}
	p := newPlayer(Queue{Tracks: []Track{track}}, Config{}, false)
	stop := make(chan struct{})
	p.stop = stop
	p.stopped = false
	p.generation = 1
	done := make(chan struct{})
	go func() { p.srvSession(track, stop, 1); close(done) }()
	deadline := time.Now().Add(time.Second)
	for playing.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	p.stopAndReset()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("session leaked")
	}
	if stopped.Load() != 1 {
		t.Fatalf("stopped reports: %d", stopped.Load())
	}
}
func cacheTestPath(t *testing.T) {
	t.Helper()
	old := srvTmpDir
	srvTmpDir = t.TempDir()
	t.Cleanup(func() { srvTmpDir = old })
}

func TestPlexPlaybackPreparationDoesNotBlockStop(t *testing.T) {
	exportTestPaths(t)
	cacheTestPath(t)
	entered := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-r.Context().Done() }))
	defer server.Close()
	cfg := PlexConfig{Server: server.URL, Token: "secret", MaxTrackMB: 1}
	track := Track{Title: "Slow", SrvID: "plex", SrvKey: "1", SrvPart: "/slow", SrvExt: ".flac", Srv: &plexSession{cfg: cfg}}
	p := newPlayer(Queue{Tracks: []Track{track}}, Config{}, false)
	start := time.Now()
	if err := p.playCurrent(); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("start blocked on download")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("download not started")
	}
	start = time.Now()
	p.stopAndReset()
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("stop blocked on download")
	}
	done := make(chan struct{})
	go func() { srvWorkers.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("download worker did not exit")
	}
	if !p.stopped || p.isLoading() {
		t.Fatal("loading state survived stop")
	}
}
func TestOldPlexWorkerCannotRemoveNewCache(t *testing.T) {
	cacheTestPath(t)
	file := filepath.Join(srvTmpDir, "current.flac")
	if err := os.WriteFile(file, []byte("current"), 0600); err != nil {
		t.Fatal(err)
	}
	p := newPlayer(Queue{Tracks: []Track{{}}}, Config{}, false)
	p.stop = make(chan struct{})
	p.stopped = false
	p.generation = 2
	if p.srvTrimCache(context.Background(), map[string]bool{}, make(chan struct{}), 1) {
		t.Fatal("stale worker accepted")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("stale worker removed current cache")
	}
}
func TestPlexWebSettingsTokenHandling(t *testing.T) {
	oldPath := plexConfigPath
	plexConfigPath = filepath.Join(t.TempDir(), "plex.json")
	t.Cleanup(func() { plexConfigPath = oldPath })
	config := PlexConfig{Server: "http://plex.local:32400", Token: "saved-secret", Scrobbling: "plex", MaxTrackMB: 128}
	b, _ := json.Marshal(config)
	if err := os.WriteFile(plexConfigPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	w := &webRemote{}
	rec := httptest.NewRecorder()
	w.handlePlexConfig(rec, httptest.NewRequest("GET", "/api/plex/config", nil))
	if strings.Contains(rec.Body.String(), "saved-secret") {
		t.Fatal("settings exposed token")
	}
	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/plex/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w.handlePlexConfig(rec, req)
		return rec
	}
	rec = post(`{"server":"http://plex.local:32400","token":"","scrobbling":"lastfm"}`)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	saved, err := loadPlex()
	if err != nil || saved.Token != "saved-secret" || saved.Scrobbling != "lastfm" {
		t.Fatal(saved, err)
	}
	rec = post(`{"server":"http://other.local:32400","token":""}`)
	if rec.Code != 400 {
		t.Fatal("server change reused old token")
	}
	req := httptest.NewRequest("POST", "http://mister.local/api/plex/config", strings.NewReader(`{}`))
	req.Header.Set("Origin", "http://other.local")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	w.handlePlexConfig(rec, req)
	if rec.Code != 403 {
		t.Fatal("cross-origin settings write accepted")
	}
}
