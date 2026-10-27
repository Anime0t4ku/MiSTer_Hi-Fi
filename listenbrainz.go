package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ListenBrainz scrobbling (https://listenbrainz.readthedocs.io), next to Last.fm. It needs only the
// user token of the profile page (https://listenbrainz.org/profile/). The URL can point at another
// server that speaks the same API (a self-hosted ListenBrainz, Maloja, Koito...).
type ListenBrainzConfig struct {
	Enabled bool   `json:"enabled"`
	Token   string `json:"token"`
	URL     string `json:"url"` // empty: https://api.listenbrainz.org
}

const listenBrainzDefaultURL = "https://api.listenbrainz.org"

func (c ListenBrainzConfig) ready() bool { return c.Enabled && strings.TrimSpace(c.Token) != "" }

func (c ListenBrainzConfig) root() string {
	u := strings.TrimRight(strings.TrimSpace(c.URL), "/")
	if u == "" {
		u = listenBrainzDefaultURL
	}
	return u
}

var listenBrainzHTTPClient = &http.Client{Timeout: 10 * time.Second}

// listenBrainzSubmit sends one listen: type "single" with the time it started, or
// "playing_now" (listenedAt ignored).
func listenBrainzSubmit(cfg ListenBrainzConfig, listenType string, listenedAt time.Time, artist, title, album string, duration float64) error {
	meta := map[string]any{
		"artist_name": nonEmptyOr(artist, "Unknown Artist"),
		"track_name":  title,
		"additional_info": map[string]any{
			"media_player":              "MiSTer Hi-Fi",
			"submission_client":         "MiSTer Hi-Fi",
			"submission_client_version": version,
		},
	}
	if album != "" {
		meta["release_name"] = album
	}
	if duration > 0 {
		meta["additional_info"].(map[string]any)["duration"] = int(duration + 0.5)
	}
	item := map[string]any{"track_metadata": meta}
	if listenType == "single" {
		item["listened_at"] = listenedAt.Unix()
	}
	body, err := json.Marshal(map[string]any{"listen_type": listenType, "payload": []any{item}})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, cfg.root()+"/1/submit-listens", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+strings.TrimSpace(cfg.Token))
	req.Header.Set("Content-Type", "application/json")
	resp, err := listenBrainzHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

var (
	lbMu      sync.Mutex
	lbPending *pendingScrobble
)

// listenBrainzMinPlayed is how long a track must have played to count: half its length or
// four minutes, whichever comes first, 30 seconds when the length is unknown (radio).
func listenBrainzMinPlayed(durationHint float64) float64 {
	if durationHint > 30 {
		return min(durationHint/2, 240)
	}
	return 30
}

// lbTrackStarted is the ListenBrainz counterpart of lastfmTrackStarted: it submits what played
// before (if long enough) and sends "playing now" for the new track.
func lbTrackStarted(cfg ListenBrainzConfig, artist, title, album string, durationHint float64) {
	if title == "" {
		return
	}
	lbMu.Lock()
	prev := lbPending
	lbPending = &pendingScrobble{artist: artist, title: title, album: album, startedAt: time.Now(), durationHint: durationHint}
	lbMu.Unlock()
	if !cfg.ready() {
		return
	}
	if prev != nil {
		go lbSubmitIfQualifies(cfg, prev)
	}
	go func() {
		if err := listenBrainzSubmit(cfg, "playing_now", time.Time{}, artist, title, album, durationHint); err != nil {
			log.Printf("listenbrainz: now playing failed: %v", err)
		}
	}()
}

// lbTrackStopped gives the track that was playing its chance to be submitted.
func lbTrackStopped(cfg ListenBrainzConfig) {
	lbMu.Lock()
	prev := lbPending
	lbPending = nil
	lbMu.Unlock()
	if prev != nil && cfg.ready() {
		go lbSubmitIfQualifies(cfg, prev)
	}
}

func lbSubmitIfQualifies(cfg ListenBrainzConfig, s *pendingScrobble) {
	if time.Since(s.startedAt).Seconds() < listenBrainzMinPlayed(s.durationHint) {
		return
	}
	if err := listenBrainzSubmit(cfg, "single", s.startedAt, s.artist, s.title, s.album, s.durationHint); err != nil {
		log.Printf("listenbrainz: submit failed for %q: %v", s.title, err)
	}
}

// scrobbleListenBrainz is the media servers' report that a track was heard long enough.
func scrobbleListenBrainz(cfg ListenBrainzConfig, t Track, startedAt time.Time) error {
	if !cfg.ready() {
		return nil
	}
	return listenBrainzSubmit(cfg, "single", startedAt, t.Artist, t.Title, t.Album, t.Duration)
}

// isOwnScrobbleMode says whether a media server's scrobbling mode is one of Hi-Fi's own scrobblers
// (as against "server" / "plex": the server keeps the history, or "none").
func isOwnScrobbleMode(m string) bool {
	return m == "lastfm" || m == "listenbrainz" || m == "both"
}
