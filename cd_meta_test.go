package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// The example of python-discid's documentation: 15 tracks.
func exampleTOC() cdTOC {
	t := cdTOC{First: 1, Last: 15}
	t.Offsets[0] = 258725
	for i, o := range []int{150, 17510, 33275, 45910, 57805, 78310, 94650, 109580, 132010, 149160, 165115, 177710, 203325, 215555, 235590} {
		t.Offsets[1+i] = o
	}
	return t
}

func TestMusicBrainzDiscID(t *testing.T) {
	if got, want := musicBrainzDiscID(exampleTOC()), "TqvKjMu7dMliSfmVEBtrL7sBSno-"; got != want {
		t.Fatalf("disc id = %s, want %s", got, want)
	}
}

func fakeCDServers(t *testing.T, id string) (mb *httptest.Server, hits *atomic.Int32) {
	t.Helper()
	hits = &atomic.Int32{}
	pngBytes := func() []byte {
		im := image.NewRGBA(image.Rect(0, 0, 8, 8))
		im.Set(1, 1, color.RGBA{R: 200, A: 255})
		var b bytes.Buffer
		png.Encode(&b, im)
		return b.Bytes()
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/2/discid/"+id, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("User-Agent") == "" {
			t.Error("MusicBrainz asks for a User-Agent")
		}
		json.NewEncoder(w).Encode(map[string]any{"releases": []map[string]any{{
			"id": "rel-1", "title": "Au rêve", "date": "2002-04-01",
			"artist-credit": []map[string]any{{"name": "Cassius", "joinphrase": ""}},
			"release-group": map[string]any{"id": "rg-1"},
			"media": []map[string]any{{
				"discs": []map[string]any{{"id": id}},
				"tracks": []map[string]any{
					{"position": 1, "title": "Hi Water"},
					{"position": 2, "title": "The Sound of Violence", "artist-credit": []map[string]any{{"name": "Cassius feat. Guest"}}},
				}}},
		}}})
	})
	mux.HandleFunc("/ws/2/discid/unknown", func(w http.ResponseWriter, r *http.Request) { hits.Add(1); http.NotFound(w, r) })
	mux.HandleFunc("/ws/2/discid/busy", func(w http.ResponseWriter, r *http.Request) { hits.Add(1); http.Error(w, "busy", 503) })
	mux.HandleFunc("/caa/release/rel-1/front-500", func(w http.ResponseWriter, r *http.Request) { w.Write(pngBytes) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, hits
}

func withCDServers(t *testing.T, srv *httptest.Server) {
	t.Helper()
	oldMB, oldCAA, oldIT, oldDir := musicBrainzURL, coverArtURL, itunesSearchURL, cdCacheDir
	musicBrainzURL, coverArtURL, itunesSearchURL = srv.URL+"/ws/2", srv.URL+"/caa", srv.URL+"/itunes"
	cdCacheDir = filepath.Join(t.TempDir(), "cd-cache")
	t.Cleanup(func() { musicBrainzURL, coverArtURL, itunesSearchURL, cdCacheDir = oldMB, oldCAA, oldIT, oldDir })
}

func twoTracks() []Track {
	return []Track{{Path: "cdda:/dev/sr0:0:100", Title: "Track 01", Album: "Audio CD", Number: 1}, {Path: "cdda:/dev/sr0:100:200", Title: "Track 02", Album: "Audio CD", Number: 2}}
}

func TestCDAnnotateFillsTitlesAndCoverAndCaches(t *testing.T) {
	toc := exampleTOC()
	id := musicBrainzDiscID(toc)
	srv, hits := fakeCDServers(t, id)
	withCDServers(t, srv)

	out := cdAnnotate(context.Background(), toc, twoTracks())
	if out[0].Title != "Hi Water" || out[0].Album != "Au rêve" || out[0].Artist != "Cassius" {
		t.Fatalf("track 1 = %+v", out[0])
	}
	if out[1].Artist != "Cassius feat. Guest" {
		t.Errorf("a track with its own artist credit: %q", out[1].Artist)
	}
	if out[0].Art == nil {
		t.Error("no cover")
	}
	if out[0].Path != "cdda:/dev/sr0:0:100" {
		t.Error("the playable path must not change")
	}

	// the second time the network is not needed at all
	srv.Close()
	again := cdAnnotate(context.Background(), toc, twoTracks())
	if again[0].Title != "Hi Water" || again[0].Art == nil || hits.Load() != 1 {
		t.Fatalf("not served from the cache: %+v (%d requests)", again[0], hits.Load())
	}
	if _, err := os.Stat(filepath.Join(cdCacheDir, id+".json")); err != nil {
		t.Error("no cache file")
	}
}

func TestCDAnnotateUnknownDiscIsRememberedBriefly(t *testing.T) {
	toc := cdTOC{First: 1, Last: 2}
	toc.Offsets = [100]int{0: 400, 1: 150, 2: 250}
	srv, hits := fakeCDServers(t, "unused")
	withCDServers(t, srv)
	// route this disc to the 404 handler
	oldURL := musicBrainzURL
	musicBrainzURL = srv.URL + "/ws/2"
	_ = oldURL
	id := musicBrainzDiscID(toc)
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/ws/2/discid/"+id, func(w http.ResponseWriter, r *http.Request) { hits.Add(1); http.NotFound(w, r) })

	out := cdAnnotate(context.Background(), toc, twoTracks())
	if out[0].Title != "Track 01" || out[0].Album != "Audio CD" {
		t.Fatalf("unknown disc changed the tracks: %+v", out[0])
	}
	cdAnnotate(context.Background(), toc, twoTracks())
	if hits.Load() != 1 {
		t.Errorf("an unknown disc was asked about %d times, want once", hits.Load())
	}
}

func TestCDAnnotateOfflineOrBusyChangesNothingAndCachesNothing(t *testing.T) {
	toc := cdTOC{First: 1, Last: 2}
	toc.Offsets = [100]int{0: 500, 1: 150, 2: 300}
	srv, hits := fakeCDServers(t, "unused")
	withCDServers(t, srv)
	id := musicBrainzDiscID(toc)
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/ws/2/discid/"+id, func(w http.ResponseWriter, r *http.Request) { hits.Add(1); http.Error(w, "busy", 503) })
	out := cdAnnotate(context.Background(), toc, twoTracks())
	if out[0].Title != "Track 01" {
		t.Fatalf("busy server changed the tracks: %+v", out[0])
	}
	if _, err := os.Stat(filepath.Join(cdCacheDir, id+".json")); err == nil {
		t.Error("a failed lookup was cached")
	}
	srv.Close()
	if out := cdAnnotate(context.Background(), toc, twoTracks()); out[0].Title != "Track 01" {
		t.Fatalf("offline changed the tracks: %+v", out[0])
	}
}

func TestCDAnnotateMapsByTrackNumber(t *testing.T) {
	toc := exampleTOC()
	id := musicBrainzDiscID(toc)
	srv, _ := fakeCDServers(t, id)
	withCDServers(t, srv)
	// a mixed-mode disc: the data track 1 is not in the list, the audio starts at 2
	tracks := []Track{{Path: "cdda:/dev/sr0:100:200", Title: "Track 02", Number: 2}}
	out := cdAnnotate(context.Background(), toc, tracks)
	if out[0].Title != "The Sound of Violence" {
		t.Fatalf("mapped by position, not by the disc's track number: %+v", out[0])
	}
	_ = fmt.Sprint
}

// Against the real MusicBrainz and Cover Art Archive (network, read-only).
// Off unless HIFI_LIVE_CD=1.
func TestCDLiveMusicBrainz(t *testing.T) {
	if os.Getenv("HIFI_LIVE_CD") == "" {
		t.Skip("HIFI_LIVE_CD not set")
	}
	toc := exampleTOC()
	cdCacheDir = filepath.Join(t.TempDir(), "cd-cache")
	tracks := make([]Track, 15)
	for i := range tracks {
		tracks[i] = Track{Path: fmt.Sprintf("cdda:/dev/sr0:%d:%d", i, i+1), Title: fmt.Sprintf("Track %02d", i+1), Album: "Audio CD", Number: i + 1}
	}
	out := cdAnnotate(context.Background(), toc, tracks)
	t.Logf("album %q by %q: first %q, last %q, cover %v", out[0].Album, out[0].Artist, out[0].Title, out[14].Title, out[0].Art != nil)
	if out[0].Album == "Audio CD" || out[0].Title == "Track 01" {
		t.Fatal("the disc was not found")
	}
}
