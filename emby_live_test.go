package main

import (
	"context"
	"io"
	"os"
	"testing"
	"time"
)

// Against a real Emby server: HIFI_LIVE_EMBY_SERVER=http://host:8096
// HIFI_LIVE_EMBY_USER=... HIFI_LIVE_EMBY_PASS=... go test -run EmbyLive -v
func TestEmbyLive(t *testing.T) {
	server, user, pass := os.Getenv("HIFI_LIVE_EMBY_SERVER"), os.Getenv("HIFI_LIVE_EMBY_USER"), os.Getenv("HIFI_LIVE_EMBY_PASS")
	if server == "" || user == "" {
		t.Skip("set HIFI_LIVE_EMBY_SERVER, HIFI_LIVE_EMBY_USER and HIFI_LIVE_EMBY_PASS to run against a real server")
	}
	withServers(t, map[string]string{"emby.json": `{"server":"` + server + `"}`})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cfg, err := loadEmby()
	if err != nil {
		t.Fatal(err)
	}
	if err := testJellyfinConnection(ctx, cfg); err == nil {
		t.Fatal("the connection test must say 'not signed in' before signing in")
	}
	if _, err := cfg.embySignIn(ctx, user, pass); err != nil {
		t.Fatalf("sign-in: %v", err)
	}
	cfg, _ = loadEmby()
	if err := testJellyfinConnection(ctx, cfg); err != nil {
		t.Fatalf("connection test: %v", err)
	}
	sess, err := jellyfinServer{emby: true}.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	artists, err := sess.Artists(ctx)
	if err != nil || len(artists) == 0 {
		t.Fatalf("artists = %d, %v", len(artists), err)
	}
	t.Logf("%d artists, the first: %q", len(artists), artists[0].Title)
	recent, err := sess.Albums(ctx, true)
	if err != nil || len(recent) == 0 {
		t.Fatalf("recent albums = %d, %v", len(recent), err)
	}
	t.Logf("%d recent albums, the first: %q by %q", len(recent), recent[0].Title, recent[0].Artist)
	byArtist, err := sess.ArtistAlbums(ctx, artists[0].ID)
	if err != nil || len(byArtist) == 0 {
		t.Fatalf("albums of %q = %d, %v", artists[0].Title, len(byArtist), err)
	}
	tracks, err := sess.AlbumTracks(ctx, recent[0].ID)
	if err != nil || len(tracks) == 0 {
		t.Fatalf("tracks = %d, %v", len(tracks), err)
	}
	tr := tracks[0]
	t.Logf("%d tracks, the first: %q (%s, %.0fs) %s", len(tracks), tr.Title, tr.SrvExt, tr.Duration, tr.Path)
	rc, size, err := sess.Download(ctx, tr)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	head := make([]byte, 4096)
	n, _ := io.ReadFull(rc, head)
	rc.Close()
	if n < 1024 {
		t.Fatalf("download gave only %d bytes", n)
	}
	t.Logf("download works: %d bytes of %d", n, size)
	if im := sess.Art(ctx, tr); im == nil {
		t.Log("no cover for this album")
	} else {
		t.Logf("cover %v", im.Bounds())
	}
	found, err := sess.FindAlbum(ctx, recent[0].Artist+" "+recent[0].Title)
	if err != nil || found.ID != recent[0].ID {
		t.Fatalf("FindAlbum = %+v, %v (want %s)", found, err, recent[0].ID)
	}
	if err := sess.Report(ctx, tr, "playing", 0); err != nil {
		t.Fatalf("Report playing: %v", err)
	}
	if err := sess.Report(ctx, tr, "stopped", 5*time.Second); err != nil {
		t.Fatalf("Report stopped: %v", err)
	}
	pls, err := sess.Playlists(ctx)
	t.Logf("%d playlists, %v", len(pls), err)
}
