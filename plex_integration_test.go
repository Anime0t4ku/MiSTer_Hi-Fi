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
	_ = os.RemoveAll(plexTmpDir)
	defer os.RemoveAll(plexTmpDir)
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
	track := Track{PlexKey: "42", PlexPart: "/audio", PlexExt: ".flac"}
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			file, err := plexEnsureLocal(context.Background(), cfg, track)
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
	if plexCachePath(cfg, track) == plexCachePath(other, track) {
		t.Fatal("cache key ignores server")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	track.PlexKey = "43"
	track.PlexPart = "/slow"
	go func() { _, err := plexEnsureLocal(ctx, cfg, track); done <- err }()
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
	files, _ := os.ReadDir(plexTmpDir)
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".part") {
			t.Fatal("orphaned partial download")
		}
	}
}
func TestPlexDownloadSizeLimit(t *testing.T) {
	cacheTestPath(t)
	_ = os.RemoveAll(plexTmpDir)
	defer os.RemoveAll(plexTmpDir)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "2097152")
		w.WriteHeader(200)
	}))
	defer server.Close()
	_, err := plexEnsureLocal(context.Background(), PlexConfig{Server: server.URL, Token: "secret", MaxTrackMB: 1}, Track{PlexKey: "99", PlexPart: "/large", PlexExt: ".flac"})
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("oversized download accepted: %v", err)
	}
}
func TestPlexPrefetchChoiceMatchesPlayback(t *testing.T) {
	p := newPlayer(Queue{Tracks: make([]Track, 4), Index: 1, Shuffle: true}, Config{}, false)
	p.q.Tracks[1].PlexKey = "1"
	next := p.plexNextLocked(false)
	if next == 1 || next < 0 || next >= 4 {
		t.Fatal(next)
	}
	if got := p.plexNextLocked(true); got != next {
		t.Fatalf("prefetched %d, next plays %d", next, got)
	}
	p.q.Shuffle = false
	p.q.Index = 3
	if p.plexNextLocked(false) != -1 {
		t.Fatal("queue should end")
	}
	p.q.Repeat = true
	if p.plexNextLocked(false) != 0 {
		t.Fatal("repeat should wrap")
	}
	p.q.RepeatOne = true
	if p.plexNextLocked(false) != 3 {
		t.Fatal("repeat one should keep current track")
	}
}
func TestPlexNaturalEndClosesSessionAndExport(t *testing.T) {
	exportTestPaths(t)
	stop := make(chan struct{})
	p := newPlayer(Queue{Tracks: []Track{{Title: "Song", PlexKey: "42"}}}, Config{}, false)
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
	if plexPlayedEnough(track, 10*time.Second) {
		t.Fatal("short listen scrobbled")
	}
	if !plexPlayedEnough(track, 90*time.Second) {
		t.Fatal("half-track not counted")
	}
	if plexPlayedEnough(Track{Duration: 1000}, 239*time.Second) {
		t.Fatal("long track counted too early")
	}
	if !plexPlayedEnough(Track{Duration: 1000}, 240*time.Second) {
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
	entries, _, err := plexBrowse(context.Background(), cfg, "/")
	if err != nil || len(entries) != 4 {
		t.Fatal(entries, err)
	}
	entries, _, err = plexBrowse(context.Background(), cfg, "/albums")
	if err != nil || len(entries) != 1 || entries[0].Name != "Artist - Album" {
		t.Fatal(entries, err)
	}
	entries, tracks, err := plexBrowse(context.Background(), cfg, "/playlists/7")
	if err != nil || len(tracks) != 2 || tracks[0].Title != "Second" || entries[1].Path != "/playlists/7/track/1" {
		t.Fatal(entries, tracks, err)
	}
	if _, _, err = plexBrowse(context.Background(), cfg, "/albums/http:evil"); err == nil {
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
	track := Track{PlexKey: "1", Duration: 180, PlexSource: &cfg}
	p := newPlayer(Queue{Tracks: []Track{track}}, Config{}, false)
	stop := make(chan struct{})
	p.stop = stop
	p.stopped = false
	p.generation = 1
	done := make(chan struct{})
	go func() { p.plexSession(track, stop, 1); close(done) }()
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
	old := plexTmpDir
	plexTmpDir = t.TempDir()
	t.Cleanup(func() { plexTmpDir = old })
}

func TestPlexPlaybackPreparationDoesNotBlockStop(t *testing.T) {
	exportTestPaths(t)
	cacheTestPath(t)
	entered := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-r.Context().Done() }))
	defer server.Close()
	cfg := PlexConfig{Server: server.URL, Token: "secret", MaxTrackMB: 1}
	track := Track{Title: "Slow", PlexKey: "1", PlexPart: "/slow", PlexExt: ".flac", PlexSource: &cfg}
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
	go func() { plexWorkers.Wait(); close(done) }()
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
	file := filepath.Join(plexTmpDir, "current.flac")
	if err := os.WriteFile(file, []byte("current"), 0600); err != nil {
		t.Fatal(err)
	}
	p := newPlayer(Queue{Tracks: []Track{{}}}, Config{}, false)
	p.stop = make(chan struct{})
	p.stopped = false
	p.generation = 2
	if p.plexTrimCache(context.Background(), map[string]bool{}, make(chan struct{}), 1) {
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
