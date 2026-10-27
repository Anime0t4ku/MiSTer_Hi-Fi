package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Plex as a media server (mediaserver.go): the Plex-specific half. Playback is
// reported to Plex (timeline + scrobble), so Plex keeps the listening history
// and any scrobbler that follows Plex sessions (e.g. multi-scrobbler) sees it.

const plexClientID = "misterhifi-mister-fpga"

var plexConfigPath = filepath.Join(baseDir, "plex.json")

type PlexConfig struct {
	Server     string `json:"server"`
	Token      string `json:"token"`
	Library    string `json:"library"`
	Insecure   bool   `json:"insecure"`
	Scrobbling string `json:"scrobbling,omitempty"` // plex (default), lastfm, or none
	MaxTrackMB int64  `json:"max_track_mb,omitempty"`
}

type plexMeta struct {
	RatingKey        string `json:"ratingKey"`
	Key              string `json:"key"`
	Type             string `json:"type"`
	Title            string `json:"title"`
	ParentTitle      string `json:"parentTitle"`
	GrandparentTitle string `json:"grandparentTitle"`
	Thumb            string `json:"thumb"`
	ParentThumb      string `json:"parentThumb"`
	Index            int    `json:"index"`
	Duration         int64  `json:"duration"`
	Media            []struct {
		AudioCodec string `json:"audioCodec"`
		Bitrate    int    `json:"bitrate"`
		Container  string `json:"container"`
		Part       []struct {
			Key       string `json:"key"`
			Container string `json:"container"`
			Stream    []struct {
				StreamType   int `json:"streamType"`
				BitDepth     int `json:"bitDepth"`
				SamplingRate int `json:"samplingRate"`
			} `json:"Stream"`
		} `json:"Part"`
	} `json:"Media"`
}

type plexContainer struct {
	MediaContainer struct {
		TotalSize int        `json:"totalSize"`
		Size      int        `json:"size"`
		Directory []plexMeta `json:"Directory"`
		Metadata  []plexMeta `json:"Metadata"`
	} `json:"MediaContainer"`
}

func loadPlex() (PlexConfig, error) {
	var c PlexConfig
	b, err := os.ReadFile(plexConfigPath)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	c.Server = strings.TrimRight(strings.TrimSpace(c.Server), "/")
	c.Token = strings.TrimSpace(c.Token)
	if !isHTTPURL(c.Server) || c.Token == "" {
		return c, errors.New("plex.json needs server (http://host:32400) and token")
	}
	if c.Library != "" && !plexID(c.Library) {
		return c, errors.New("Plex library must be a numeric section ID")
	}
	if c.Scrobbling == "" {
		c.Scrobbling = "plex"
	}
	if c.Scrobbling != "plex" && !isOwnScrobbleMode(c.Scrobbling) && c.Scrobbling != "none" {
		return c, errors.New("scrobbling must be plex, lastfm, listenbrainz, both, or none")
	}
	if c.MaxTrackMB == 0 {
		c.MaxTrackMB = 128
	}
	if c.MaxTrackMB < 1 || c.MaxTrackMB > 1024 {
		return c, errors.New("max_track_mb must be between 1 and 1024")
	}
	return c, nil
}

var plexInsecureTransport = func() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // opt-in for self-signed LAN servers
	return transport
}()

func (c PlexConfig) client(timeout time.Duration) *http.Client {
	cl := &http.Client{Timeout: timeout}
	if c.Insecure {
		cl.Transport = plexInsecureTransport
	}
	cl.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("plex: too many redirects")
		}
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			req.Header.Del("X-Plex-Token")
		}
		return nil
	}
	return cl
}

func (c PlexConfig) request(path string, q url.Values) (*http.Request, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return nil, errors.New("plex: invalid server path")
	}
	u, err := url.Parse(c.Server + path)
	if err != nil {
		return nil, err
	}
	values := u.Query()
	for key, items := range q {
		values[key] = items
	}
	u.RawQuery = values.Encode()
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Token", c.Token)
	req.Header.Set("X-Plex-Client-Identifier", deviceID("plex-client-id", plexClientID))
	req.Header.Set("X-Plex-Product", "MiSTer Hi-Fi")
	req.Header.Set("X-Plex-Version", version)
	req.Header.Set("X-Plex-Platform", "Linux")
	req.Header.Set("X-Plex-Device", "MiSTer FPGA")
	req.Header.Set("X-Plex-Device-Name", "MiSTer Hi-Fi")
	return req, nil
}

func (c PlexConfig) list(path string, q url.Values, max int) ([]plexMeta, error) {
	return c.listContext(context.Background(), path, q, max)
}
func (c PlexConfig) listContext(ctx context.Context, path string, q url.Values, max int) ([]plexMeta, error) {
	var out []plexMeta
	const page = 300
	start := 0
	for {
		qq := url.Values{}
		for k, v := range q {
			qq[k] = v
		}
		qq.Set("X-Plex-Container-Start", fmt.Sprint(start))
		qq.Set("X-Plex-Container-Size", fmt.Sprint(page))
		req, err := c.request(path, qq)
		if err != nil {
			return nil, err
		}
		resp, err := c.client(20 * time.Second).Do(req.WithContext(ctx))
		if err != nil {
			return nil, err
		}
		var pc plexContainer
		err = json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&pc)
		resp.Body.Close()
		if resp.StatusCode == 401 {
			return nil, errors.New("plex: token rejected")
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("plex: server returned HTTP %d", resp.StatusCode)
		}
		if err != nil {
			return nil, fmt.Errorf("plex: invalid server response: %w", err)
		}
		mc := pc.MediaContainer
		got := append(append([]plexMeta{}, mc.Directory...), mc.Metadata...)
		out = append(out, got...)
		start += len(got)
		if len(got) == 0 || (mc.TotalSize > 0 && start >= mc.TotalSize) || (mc.TotalSize == 0 && len(got) < page) || (max > 0 && len(out) >= max) {
			break
		}
	}
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out, nil
}

func (c PlexConfig) musicSection() (string, error) {
	if c.Library != "" {
		return c.Library, nil
	}
	secs, err := c.list("/library/sections", nil, 0)
	if err != nil {
		return "", err
	}
	for _, s := range secs {
		if s.Type == "artist" {
			return s.Key, nil
		}
	}
	return "", errors.New("no music library on Plex server")
}

func plexTrack(m plexMeta) Track {
	t := Track{
		Path:     "plex:" + m.RatingKey,
		SrvKey:   m.RatingKey,
		Title:    m.Title,
		Artist:   m.GrandparentTitle,
		Album:    m.ParentTitle,
		Number:   m.Index,
		Duration: float64(m.Duration) / 1000,
		DirFD:    -1,
	}
	if len(m.Media) > 0 {
		md := m.Media[0]
		t.BitRate = md.Bitrate
		if len(md.Part) > 0 {
			t.SrvPart = md.Part[0].Key
			t.SrvExt = srvExtFromContainer(md.Part[0].Container)
			for _, s := range md.Part[0].Stream {
				if s.StreamType == 2 {
					t.BitDepth = s.BitDepth
					t.SampleRate = s.SamplingRate
				}
			}
		}
	}
	t.SrvThumb = m.ParentThumb
	if t.SrvThumb == "" {
		t.SrvThumb = m.Thumb
	}
	return t
}

// ---- the media server ----

func plexItems(metas []plexMeta) []mediaItem {
	out := make([]mediaItem, 0, len(metas))
	for _, m := range metas {
		if !plexID(m.RatingKey) {
			continue
		}
		out = append(out, mediaItem{ID: m.RatingKey, Title: m.Title, Artist: m.ParentTitle})
	}
	return out
}

func plexTracks(metas []plexMeta) []Track {
	out := make([]Track, 0, len(metas))
	for _, m := range metas {
		if t := plexTrack(m); t.SrvPart != "" {
			out = append(out, t)
		}
	}
	return out
}

// plexServer is the mediaServer for Plex (config: plex.json).
type plexServer struct{}

func (plexServer) ID() string    { return "plex" }
func (plexServer) Label() string { return "PLEX" }

func (plexServer) Configured() bool {
	_, err := os.Stat(plexConfigPath)
	return err == nil
}

func (plexServer) Connect(ctx context.Context) (mediaSession, error) {
	cfg, err := loadPlex()
	if err != nil {
		return nil, err
	}
	return &plexSession{cfg: cfg}, nil
}

// plexSession keeps the config it was made with (a snapshot).
type plexSession struct {
	cfg PlexConfig
}

func plexID(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (s *plexSession) ValidID(id string) bool { return plexID(id) }

func (s *plexSession) section(ctx context.Context) (string, error) {
	if s.cfg.Library != "" {
		return s.cfg.Library, nil
	}
	list, err := s.cfg.listContext(ctx, "/library/sections", nil, 0)
	if err != nil {
		return "", err
	}
	for _, sec := range list {
		if sec.Type == "artist" {
			return sec.Key, nil
		}
	}
	return "", errors.New("no music library on Plex server")
}

func (s *plexSession) Artists(ctx context.Context) ([]mediaItem, error) {
	sec, err := s.section(ctx)
	if err != nil {
		return nil, err
	}
	m, err := s.cfg.listContext(ctx, "/library/sections/"+sec+"/all", url.Values{"type": {"8"}, "sort": {"titleSort"}}, 0)
	return plexItems(m), err
}

func (s *plexSession) ArtistAlbums(ctx context.Context, id string) ([]mediaItem, error) {
	m, err := s.cfg.listContext(ctx, "/library/metadata/"+id+"/children", nil, 0)
	return plexItems(m), err
}

func (s *plexSession) Albums(ctx context.Context, recent bool) ([]mediaItem, error) {
	sec, err := s.section(ctx)
	if err != nil {
		return nil, err
	}
	q, max := url.Values{"type": {"9"}, "sort": {"artist.titleSort,titleSort"}}, 0
	if recent {
		q, max = url.Values{"type": {"9"}, "sort": {"addedAt:desc"}}, 100
	}
	m, err := s.cfg.listContext(ctx, "/library/sections/"+sec+"/all", q, max)
	return plexItems(m), err
}

func (s *plexSession) Playlists(ctx context.Context) ([]mediaItem, error) {
	m, err := s.cfg.listContext(ctx, "/playlists", url.Values{"playlistType": {"audio"}}, 0)
	return plexItems(m), err
}

func (s *plexSession) AlbumTracks(ctx context.Context, id string) ([]Track, error) {
	m, err := s.cfg.listContext(ctx, "/library/metadata/"+id+"/children", nil, 0)
	return plexTracks(m), err
}

func (s *plexSession) PlaylistTracks(ctx context.Context, id string) ([]Track, error) {
	m, err := s.cfg.listContext(ctx, "/playlists/"+id+"/items", nil, 0)
	return plexTracks(m), err
}

func (s *plexSession) FindAlbum(ctx context.Context, query string) (mediaItem, error) {
	sec, err := s.section(ctx)
	if err != nil {
		return mediaItem{}, err
	}
	albums, err := s.cfg.listContext(ctx, "/library/sections/"+sec+"/search", url.Values{"type": {"9"}, "query": {query}}, 1)
	if err != nil {
		return mediaItem{}, err
	}
	items := plexItems(albums)
	if len(items) == 0 {
		return mediaItem{}, errors.New("no album found")
	}
	return items[0], nil
}

func (s *plexSession) Download(ctx context.Context, t Track) (io.ReadCloser, int64, error) {
	req, err := s.cfg.request(t.SrvPart, url.Values{"download": {"1"}})
	if err != nil {
		return nil, 0, err
	}
	req.Header.Del("Accept")
	resp, err := s.cfg.client(5 * time.Minute).Do(req.WithContext(ctx))
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("plex: download failed (HTTP %d)", resp.StatusCode)
	}
	return resp.Body, resp.ContentLength, nil
}

func (s *plexSession) Art(ctx context.Context, t Track) image.Image {
	if t.SrvThumb == "" {
		return nil
	}
	req, err := s.cfg.request("/photo/:/transcode", url.Values{
		"url": {t.SrvThumb}, "width": {"600"}, "height": {"600"}, "minSize": {"1"},
	})
	if err != nil {
		return nil
	}
	req.Header.Del("Accept")
	resp, err := s.cfg.client(20 * time.Second).Do(req.WithContext(ctx))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	im, _, err := image.Decode(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil
	}
	return im
}

func (s *plexSession) send(ctx context.Context, path string, q url.Values) error {
	req, err := s.cfg.request(path, q)
	if err != nil {
		return err
	}
	resp, err := s.cfg.client(8 * time.Second).Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("plex: reporting returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func (s *plexSession) Report(ctx context.Context, t Track, state string, pos time.Duration) error {
	q := url.Values{"ratingKey": {t.SrvKey}, "key": {"/library/metadata/" + t.SrvKey}, "state": {state}, "time": {fmt.Sprint(pos.Milliseconds())}, "duration": {fmt.Sprint(int64(t.Duration * 1000))}}
	return s.send(ctx, "/:/timeline", q)
}

func (s *plexSession) Played(ctx context.Context, t Track) error {
	return s.send(ctx, "/:/scrobble", url.Values{"key": {t.SrvKey}, "identifier": {"com.plexapp.plugins.library"}})
}

// Scrobbling maps plex.json's "plex" (the server counts) onto the generic "server".
func (s *plexSession) Scrobbling() string {
	if s.cfg.Scrobbling == "plex" || s.cfg.Scrobbling == "" {
		return "server"
	}
	return s.cfg.Scrobbling
}

func (s *plexSession) MaxTrackBytes() int64 { return s.cfg.MaxTrackMB << 20 }

func (s *plexSession) CacheKey(t Track) string {
	return s.cfg.Server + "\x00" + s.cfg.Token + "\x00" + t.SrvKey + "\x00" + t.SrvPart
}
