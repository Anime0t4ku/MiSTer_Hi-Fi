package main

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Audio CD metadata.
//
// The drive only gives the table of contents, so a disc shows up as "Track 01",
// "Track 02"... This looks the disc up by its MusicBrainz disc id (worked out
// from the table of contents) and fills in the artist, the album, the track
// titles and the cover (Cover Art Archive, then iTunes). Only the disc id
// travels to MusicBrainz, not what is on the disc. Results are kept per disc id
// in baseDir/cd-cache/, so a disc is looked up once; offline, or for a disc
// MusicBrainz does not know, the generic names stay.

var (
	musicBrainzURL = "https://musicbrainz.org/ws/2"
	coverArtURL    = "https://coverartarchive.org"
	cdCacheDir     = filepath.Join(baseDir, "cd-cache")
	cdLookupClient = &http.Client{Timeout: 8 * time.Second}
)

// a disc MusicBrainz does not know is not asked about again for a week
const cdNegativeTTL = 7 * 24 * time.Hour

// cdTOC is a disc's table of contents in frames (75 per second), the way
// MusicBrainz wants it: offsets[0] is the lead-out, offsets[n] the start of
// track n, both counted from the start of the disc (LBA + 150).
type cdTOC struct {
	First, Last int
	Offsets     [100]int
}

// musicBrainzDiscID is libdiscid's id: SHA-1 over the first and last track and
// the 100 offsets, in upper-case hex, as base64 with + / = replaced by . _ -.
func musicBrainzDiscID(t cdTOC) string {
	h := sha1.New()
	fmt.Fprintf(h, "%02X%02X", t.First, t.Last)
	for _, o := range t.Offsets {
		fmt.Fprintf(h, "%08X", o)
	}
	s := base64.StdEncoding.EncodeToString(h.Sum(nil))
	return strings.NewReplacer("+", ".", "/", "_", "=", "-").Replace(s)
}

// cdInfo is what is known about a disc.
type cdInfo struct {
	Found     bool      `json:"found"`
	At        time.Time `json:"at"`
	Album     string    `json:"album,omitempty"`
	Artist    string    `json:"artist,omitempty"`
	Year      string    `json:"year,omitempty"`
	Titles    []string  `json:"titles,omitempty"`  // by position on the disc, 1-based at index 0
	Artists   []string  `json:"artists,omitempty"` // per track, when it differs from the album's
	ReleaseID string    `json:"release_id,omitempty"`
	GroupID   string    `json:"group_id,omitempty"`
}

type mbCredit struct {
	Name   string `json:"name"`
	Artist struct {
		Name string `json:"name"`
	} `json:"artist"`
	JoinPhrase string `json:"joinphrase"`
}

func mbArtist(c []mbCredit) string {
	var b strings.Builder
	for _, x := range c {
		n := x.Name
		if n == "" {
			n = x.Artist.Name
		}
		b.WriteString(n + x.JoinPhrase)
	}
	return strings.TrimSpace(b.String())
}

// cdLookupMB asks MusicBrainz about a disc id.
func cdLookupMB(ctx context.Context, id string) (cdInfo, error) {
	q := url.Values{"inc": {"artists+recordings+release-groups"}, "fmt": {"json"}}
	req, err := http.NewRequestWithContext(ctx, "GET", musicBrainzURL+"/discid/"+url.PathEscape(id)+"?"+q.Encode(), nil)
	if err != nil {
		return cdInfo{}, err
	}
	// MusicBrainz asks every client to say who it is
	req.Header.Set("User-Agent", "MiSTerHi-Fi/"+version+" ( https://github.com/Anime0t4ku/MiSTer_Hi-Fi )")
	req.Header.Set("Accept", "application/json")
	resp, err := cdLookupClient.Do(req)
	if err != nil {
		return cdInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return cdInfo{Found: false, At: time.Now()}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return cdInfo{}, fmt.Errorf("musicbrainz: HTTP %d", resp.StatusCode) // busy or down: not cached
	}
	var res struct {
		Releases []struct {
			ID           string     `json:"id"`
			Title        string     `json:"title"`
			Date         string     `json:"date"`
			ArtistCredit []mbCredit `json:"artist-credit"`
			ReleaseGroup struct {
				ID string `json:"id"`
			} `json:"release-group"`
			Media []struct {
				Discs []struct {
					ID string `json:"id"`
				} `json:"discs"`
				Tracks []struct {
					Position     int        `json:"position"`
					Title        string     `json:"title"`
					ArtistCredit []mbCredit `json:"artist-credit"`
				} `json:"tracks"`
			} `json:"media"`
		} `json:"releases"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&res); err != nil {
		return cdInfo{}, fmt.Errorf("musicbrainz: invalid answer: %w", err)
	}
	// the first release with this disc on one of its media
	for _, r := range res.Releases {
		for _, m := range r.Media {
			match := false
			for _, d := range m.Discs {
				if d.ID == id {
					match = true
				}
			}
			if !match || len(m.Tracks) == 0 {
				continue
			}
			info := cdInfo{Found: true, At: time.Now(), Album: r.Title, Artist: mbArtist(r.ArtistCredit), ReleaseID: r.ID, GroupID: r.ReleaseGroup.ID}
			if len(r.Date) >= 4 {
				info.Year = r.Date[:4]
			}
			info.Titles = make([]string, len(m.Tracks)+1)
			info.Artists = make([]string, len(m.Tracks)+1)
			for _, t := range m.Tracks {
				if t.Position < 1 || t.Position >= len(info.Titles) {
					continue
				}
				info.Titles[t.Position] = t.Title
				if a := mbArtist(t.ArtistCredit); a != info.Artist {
					info.Artists[t.Position] = a
				}
			}
			return info, nil
		}
	}
	return cdInfo{Found: false, At: time.Now()}, nil
}

// cdCover fetches the cover: the release's front cover at the Cover Art
// Archive, then the release group's, then an iTunes album search.
func cdCover(ctx context.Context, info cdInfo) image.Image {
	get := func(u string) image.Image {
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return nil
		}
		req.Header.Set("User-Agent", "MiSTerHi-Fi/"+version+" ( https://github.com/Anime0t4ku/MiSTer_Hi-Fi )")
		resp, err := cdLookupClient.Do(req)
		if err != nil {
			return nil
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil
		}
		im, _, err := image.Decode(io.LimitReader(resp.Body, 8<<20))
		if err != nil {
			return nil
		}
		return im
	}
	if info.ReleaseID != "" {
		if im := get(coverArtURL + "/release/" + url.PathEscape(info.ReleaseID) + "/front-500"); im != nil {
			return im
		}
	}
	if info.GroupID != "" {
		if im := get(coverArtURL + "/release-group/" + url.PathEscape(info.GroupID) + "/front-500"); im != nil {
			return im
		}
	}
	if info.Artist != "" && info.Album != "" {
		if u := cdItunesAlbumArt(ctx, info.Artist, info.Album); u != "" {
			return get(u)
		}
	}
	return nil
}

var itunesSearchURL = "https://itunes.apple.com/search"

func cdItunesAlbumArt(ctx context.Context, artist, album string) string {
	q := url.Values{"term": {artist + " " + album}, "media": {"music"}, "entity": {"album"}, "limit": {"10"}}
	req, err := http.NewRequestWithContext(ctx, "GET", itunesSearchURL+"?"+q.Encode(), nil)
	if err != nil {
		return ""
	}
	resp, err := cdLookupClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var res struct {
		Results []struct {
			ArtistName, CollectionName, ArtworkUrl100 string
		} `json:"results"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&res) != nil {
		return ""
	}
	for _, r := range res.Results {
		if r.ArtworkUrl100 != "" && radioNameMatch(artist, r.ArtistName) && radioNameMatch(album, r.CollectionName) {
			return strings.Replace(r.ArtworkUrl100, "100x100bb", "600x600bb", 1)
		}
	}
	return ""
}

// ---- cache ----

func cdCachePaths(id string) (infoPath, coverPath string) {
	return filepath.Join(cdCacheDir, id+".json"), filepath.Join(cdCacheDir, id+".jpg")
}

func cdLoadCache(id string) (info cdInfo, cover image.Image, ok bool) {
	ip, cp := cdCachePaths(id)
	b, err := os.ReadFile(ip)
	if err != nil || json.Unmarshal(b, &info) != nil {
		return cdInfo{}, nil, false
	}
	if !info.Found && time.Since(info.At) > cdNegativeTTL {
		return cdInfo{}, nil, false // look again
	}
	if f, err := os.Open(cp); err == nil {
		cover, _ = jpeg.Decode(f)
		f.Close()
	}
	return info, cover, true
}

func cdSaveCache(id string, info cdInfo, cover image.Image) {
	if err := os.MkdirAll(cdCacheDir, 0755); err != nil {
		return
	}
	ip, cp := cdCachePaths(id)
	if cover != nil {
		if f, err := os.Create(cp + ".tmp"); err == nil {
			err = jpeg.Encode(f, scaleDownBilinear(cover, 500), &jpeg.Options{Quality: 85})
			f.Close()
			if err == nil {
				_ = os.Rename(cp+".tmp", cp)
			} else {
				_ = os.Remove(cp + ".tmp")
			}
		}
	}
	b, _ := json.Marshal(info)
	if os.WriteFile(ip+".tmp", b, 0644) == nil {
		_ = os.Rename(ip+".tmp", ip)
	}
}

// ---- putting it on the tracks ----

// cdAnnotate fills in what is known about the disc on its tracks (the audio
// tracks in disc order). Without a network or for an unknown disc the tracks
// are left as they are.
func cdAnnotate(ctx context.Context, toc cdTOC, tracks []Track) []Track {
	if toc.Last < toc.First || len(tracks) == 0 {
		return tracks
	}
	id := musicBrainzDiscID(toc)
	info, cover, ok := cdLoadCache(id)
	if !ok {
		var err error
		info, err = cdLookupMB(ctx, id)
		if err != nil {
			return tracks // offline or busy: try again next time
		}
		if info.Found {
			cover = cdCover(ctx, info)
		}
		cdSaveCache(id, info, cover)
	}
	if !info.Found {
		return tracks
	}
	out := append([]Track(nil), tracks...)
	for i := range out {
		pos := out[i].Number // the disc's own number (a data track may come first)
		if pos < 1 {
			pos = i + 1
		}
		if pos < len(info.Titles) && info.Titles[pos] != "" {
			out[i].Title = info.Titles[pos]
		}
		out[i].Album = info.Album
		out[i].Artist = info.Artist
		if pos < len(info.Artists) && info.Artists[pos] != "" {
			out[i].Artist = info.Artists[pos]
		}
		out[i].Number = pos
		out[i].Art = cover
	}
	return out
}

// readCDTOCWithMetadata is readCDTOC with the disc looked up (a few seconds at
// most, instant for a disc seen before).
func readCDTOCWithMetadata(dev string) ([]Track, error) {
	tracks, toc, err := readCDTOCInfo(dev)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	return cdAnnotate(ctx, toc, tracks), nil
}
