package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestListenBrainzSubmit(t *testing.T) {
	var got struct {
		ListenType string `json:"listen_type"`
		Payload    []struct {
			ListenedAt    int64 `json:"listened_at"`
			TrackMetadata struct {
				Artist string `json:"artist_name"`
				Track  string `json:"track_name"`
				Album  string `json:"release_name"`
			} `json:"track_metadata"`
		} `json:"payload"`
	}
	var auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		b, _ := io.ReadAll(r.Body)
		got.Payload = nil
		_ = json.Unmarshal(b, &got)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	cfg := ListenBrainzConfig{Enabled: true, Token: "tok", URL: srv.URL + "/"}
	when := time.Unix(1700000000, 0)
	if err := listenBrainzSubmit(cfg, "single", when, "Czarface", "Skit", "Frankie", 47.5); err != nil {
		t.Fatal(err)
	}
	if auth != "Token tok" || path != "/1/submit-listens" || got.ListenType != "single" || got.Payload[0].ListenedAt != 1700000000 ||
		got.Payload[0].TrackMetadata.Artist != "Czarface" || got.Payload[0].TrackMetadata.Album != "Frankie" {
		t.Fatalf("unexpected request: %q %q %+v", auth, path, got)
	}
	if err := listenBrainzSubmit(cfg, "playing_now", time.Time{}, "", "Song", "", 0); err != nil {
		t.Fatal(err)
	}
	if got.ListenType != "playing_now" || got.Payload[0].ListenedAt != 0 || got.Payload[0].TrackMetadata.Artist != "Unknown Artist" {
		t.Fatalf("unexpected playing_now: %+v", got)
	}
}

func TestListenBrainzErrorsAndRules(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"Invalid authorization token."}`, 401)
	}))
	defer srv.Close()
	if err := listenBrainzSubmit(ListenBrainzConfig{Enabled: true, Token: "bad", URL: srv.URL}, "single", time.Now(), "a", "b", "", 0); err == nil {
		t.Fatal("an HTTP 401 must be an error")
	}
	if (ListenBrainzConfig{Enabled: true}).ready() || (ListenBrainzConfig{Token: "x"}).ready() {
		t.Fatal("needs both enabled and a token")
	}
	if listenBrainzMinPlayed(0) != 30 || listenBrainzMinPlayed(200) != 100 || listenBrainzMinPlayed(900) != 240 {
		t.Fatal("wrong minimum play time")
	}
	if (ListenBrainzConfig{}).root() != listenBrainzDefaultURL {
		t.Fatal("default URL")
	}
	for _, m := range []string{"lastfm", "listenbrainz", "both"} {
		if !isOwnScrobbleMode(m) {
			t.Fatal(m)
		}
	}
	if isOwnScrobbleMode("server") || isOwnScrobbleMode("none") {
		t.Fatal("server and none are not Hi-Fi's own")
	}
}
