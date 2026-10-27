package main

// Artwork for Online Radio: a per-station logo (radio.json "logo", URL or
// path relative to baseDir) and, when the stream announces a song through ICY
// StreamTitle, the song's cover looked up via the iTunes Search API. The logo
// is the fallback while no cover is found.

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

var radioArtClient = &http.Client{Timeout: 8 * time.Second}

var (
	radioCoverMu    sync.Mutex
	radioCoverCache = map[string]string{} // "artist|title" -> artwork URL ("" = no match)
)

// fetchRadioBytes reads a logo from a local path or http(s) URL, with a small
// on-disk cache so stations still show a logo when the internet is slow.
func fetchRadioBytes(src string) ([]byte, error) {
	if !isHTTPURL(src) {
		if !filepath.IsAbs(src) {
			src = filepath.Join(baseDir, src)
		}
		return os.ReadFile(src)
	}
	sum := sha1.Sum([]byte(src))
	cache := filepath.Join(baseDir, "radio-cache", hex.EncodeToString(sum[:8]))
	if b, err := os.ReadFile(cache); err == nil && len(b) > 0 {
		return b, nil
	}
	resp, err := radioArtClient.Get(src)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if os.MkdirAll(filepath.Dir(cache), 0o755) == nil {
		os.WriteFile(cache, b, 0o644)
	}
	return b, nil
}

func radioLogo(src string) image.Image {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil
	}
	b, err := fetchRadioBytes(src)
	if err != nil {
		return nil
	}
	im, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil
	}
	return im
}

func radioNorm(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func radioNameMatch(want, got string) bool {
	w, g := radioNorm(want), radioNorm(got)
	return w != "" && g != "" && (strings.Contains(g, w) || strings.Contains(w, g))
}

// radioCoverURL returns the iTunes artwork URL for artist/title, or "".
func radioCoverURL(artist, title string) string {
	key := radioNorm(artist) + "|" + radioNorm(title)
	radioCoverMu.Lock()
	if u, ok := radioCoverCache[key]; ok {
		radioCoverMu.Unlock()
		return u
	}
	radioCoverMu.Unlock()

	q := url.Values{"term": {artist + " " + title}, "media": {"music"}, "entity": {"song"}, "limit": {"15"}}
	resp, err := radioArtClient.Get("https://itunes.apple.com/search?" + q.Encode())
	if err != nil {
		return "" // not cached: may work next time
	}
	defer resp.Body.Close()
	var res struct {
		Results []struct {
			ArtistName, TrackName, ArtworkUrl100 string
		} `json:"results"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&res) != nil {
		return ""
	}
	// Prefer an exact title, then a title without live/remix/etc. decoration,
	// then any title that merely contains the wanted one.
	best, bestScore := "", 0
	for _, r := range res.Results {
		if r.ArtworkUrl100 == "" || !radioNameMatch(artist, r.ArtistName) || !radioNameMatch(title, r.TrackName) {
			continue
		}
		score := 1
		if radioNorm(r.TrackName) == radioNorm(title) {
			score = 3
		} else if l := strings.ToLower(r.TrackName); !strings.Contains(l, "live") && !strings.Contains(l, "remix") && !strings.Contains(l, "demo") {
			score = 2
		}
		if score > bestScore {
			best, bestScore = strings.Replace(r.ArtworkUrl100, "100x100bb", "600x600bb", 1), score
		}
	}
	found := best
	radioCoverMu.Lock()
	radioCoverCache[key] = found
	radioCoverMu.Unlock()
	return found
}

func radioCover(artist, title string) image.Image {
	u := radioCoverURL(artist, title)
	if u == "" {
		return nil
	}
	resp, err := radioArtClient.Get(u)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	im, _, err := image.Decode(resp.Body)
	if err != nil {
		return nil
	}
	return im
}
