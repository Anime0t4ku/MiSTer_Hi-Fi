package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Plex music source. Tracks are fetched from the Plex server into a small
// temporary cache and played as local files, so seeking, tags and metadata
// reuse SD/SMB playback. Gapless is disabled for Plex. Playback is reported to
// Plex (timeline + scrobble), so Plex keeps the listening history and any
// scrobbler that follows Plex sessions (e.g. multi-scrobbler) sees it.

var plexTmpDir = "/tmp/misterhifi-plex"

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
	if c.Scrobbling != "plex" && c.Scrobbling != "lastfm" && c.Scrobbling != "none" {
		return c, errors.New("scrobbling must be plex, lastfm, or none")
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
	req.Header.Set("X-Plex-Client-Identifier", plexDeviceID())
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
		PlexKey:  m.RatingKey,
		Title:    m.Title,
		Artist:   m.GrandparentTitle,
		Album:    m.ParentTitle,
		Duration: float64(m.Duration) / 1000,
		DirFD:    -1,
	}
	if len(m.Media) > 0 {
		md := m.Media[0]
		t.BitRate = md.Bitrate
		if len(md.Part) > 0 {
			t.PlexPart = md.Part[0].Key
			t.PlexExt = "." + strings.ToLower(md.Part[0].Container)
			for _, s := range md.Part[0].Stream {
				if s.StreamType == 2 {
					t.BitDepth = s.BitDepth
					t.SampleRate = s.SamplingRate
				}
			}
		}
	}
	t.PlexThumb = m.ParentThumb
	if t.PlexThumb == "" {
		t.PlexThumb = m.Thumb
	}
	return t
}

// All cache mutation is serialized. Context cancellation releases queued jobs;
// unique .part files are removed on every failure. Keys include the server and
// media part so switching libraries/servers cannot play an unrelated cached file.
var plexCacheGate = make(chan struct{}, 1)
var plexWorkers sync.WaitGroup

func runPlexWorker(fn func()) { plexWorkers.Add(1); go func() { defer plexWorkers.Done(); fn() }() }
func closePlexWorkers()       { plexWorkers.Wait(); _ = os.RemoveAll(plexTmpDir) }
func cacheLock(ctx context.Context) error {
	select {
	case plexCacheGate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func cacheUnlock() { <-plexCacheGate }
func plexCachePath(c PlexConfig, t Track) string {
	ext := strings.ToLower(t.PlexExt)
	if !supported[ext] {
		ext = ".bin"
	}
	sum := sha256.Sum256([]byte(c.Server + "\x00" + c.Token + "\x00" + t.PlexKey + "\x00" + t.PlexPart))
	return filepath.Join(plexTmpDir, fmt.Sprintf("%x%s", sum[:16], ext))
}
func plexEnsureLocal(ctx context.Context, cfg PlexConfig, t Track) (string, error) {
	if t.PlexPart == "" {
		return "", errors.New("plex: track has no media part")
	}
	if err := cacheLock(ctx); err != nil {
		return "", err
	}
	defer cacheUnlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(plexTmpDir, 0700); err != nil {
		return "", err
	}
	dst := plexCachePath(cfg, t)
	if st, err := os.Stat(dst); err == nil && st.Size() > 0 {
		return dst, nil
	}
	req, err := cfg.request(t.PlexPart, url.Values{"download": {"1"}})
	if err != nil {
		return "", err
	}
	req.Header.Del("Accept")
	resp, err := cfg.client(5 * time.Minute).Do(req.WithContext(ctx))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("plex: download failed (HTTP %d)", resp.StatusCode)
	}
	limit := cfg.MaxTrackMB
	if limit == 0 {
		limit = 128
	}
	limit *= 1 << 20
	if resp.ContentLength > limit {
		return "", fmt.Errorf("plex: track exceeds %d MB cache limit", limit>>20)
	}
	f, err := os.CreateTemp(plexTmpDir, "download-*.part")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, limit+1))
	closeErr := f.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if n == 0 || n > limit || (resp.ContentLength >= 0 && n != resp.ContentLength) {
		return "", errors.New("plex: empty, incomplete, or oversized download")
	}
	if err := os.Rename(f.Name(), dst); err != nil {
		return "", err
	}
	return dst, nil
}

func plexArt(ctx context.Context, cfg PlexConfig, t Track) image.Image {
	if t.PlexThumb == "" {
		return nil
	}
	req, err := cfg.request("/photo/:/transcode", url.Values{
		"url": {t.PlexThumb}, "width": {"600"}, "height": {"600"}, "minSize": {"1"},
	})
	if err != nil {
		return nil
	}
	req.Header.Del("Accept")
	resp, err := cfg.client(20 * time.Second).Do(req.WithContext(ctx))
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

func plexReport(ctx context.Context, cfg PlexConfig, t Track, state string, pos time.Duration) error {
	q := url.Values{"ratingKey": {t.PlexKey}, "key": {"/library/metadata/" + t.PlexKey}, "state": {state}, "time": {fmt.Sprint(pos.Milliseconds())}, "duration": {fmt.Sprint(int64(t.Duration * 1000))}}
	return plexSend(ctx, cfg, "/:/timeline", q)
}
func plexSend(ctx context.Context, cfg PlexConfig, path string, q url.Values) error {
	req, err := cfg.request(path, q)
	if err != nil {
		return err
	}
	resp, err := cfg.client(8 * time.Second).Do(req.WithContext(ctx))
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
func plexScrobble(ctx context.Context, cfg PlexConfig, t Track) error {
	return plexSend(ctx, cfg, "/:/scrobble", url.Values{"key": {t.PlexKey}, "identifier": {"com.plexapp.plugins.library"}})
}

// Listen time follows wall time only while this generation is actually playing.
// Seeking changes the timeline position, never the time used to qualify a play.
func plexPlayedEnough(t Track, heard time.Duration) bool {
	threshold := 4 * time.Minute
	if t.Duration > 0 && time.Duration(t.Duration*float64(time.Second)/2) < threshold {
		threshold = time.Duration(t.Duration * float64(time.Second) / 2)
	}
	return heard >= threshold
}
func (p *Player) plexSession(t Track, stop <-chan struct{}, generation uint64) {
	cfg, err := trackPlexConfig(t)
	if err != nil {
		return
	}
	ctx, cancel := playbackContext(stop)
	defer cancel()
	startedAt := time.Now()
	type update struct {
		state      string
		pos, heard time.Duration
	}
	updates := make(chan update, 1)
	networkDone := make(chan struct{})
	p.mu.Lock()
	lastfmCfg := p.cfg.LastFM
	p.mu.Unlock()
	go func() {
		defer close(networkDone)
		lastState := ""
		lastReport := time.Time{}
		scrobbled, warned := false, false
		for {
			select {
			case <-ctx.Done():
				return
			case u := <-updates:
				if ctx.Err() != nil {
					return
				}
				var reportErr error
				if u.state != lastState || time.Since(lastReport) >= 10*time.Second {
					reportErr = plexReport(ctx, cfg, t, u.state, u.pos)
					lastState = u.state
					lastReport = time.Now()
				}
				if !scrobbled && plexPlayedEnough(t, u.heard) {
					switch cfg.Scrobbling {
					case "plex", "":
						reportErr = plexScrobble(ctx, cfg, t)
					case "lastfm":
						reportErr = scrobblePlexLastFM(lastfmCfg, t, startedAt)
					default:
						reportErr = nil
					}
					scrobbled = reportErr == nil
				}
				if reportErr != nil && ctx.Err() == nil && !warned {
					log.Printf("Plex playback reporting: %v", reportErr)
					warned = true
				}
			}
		}
	}()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var pos, heard time.Duration
	last := time.Now()
	wasPlaying := false
	defer func() {
		cancel()
		<-networkDone
		endCtx, endCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer endCancel()
		if err := plexReport(endCtx, cfg, t, "stopped", pos); err != nil {
			log.Printf("Plex stopped report: %v", err)
		}
	}()
	for {
		p.opMu.Lock()
		p.mu.Lock()
		valid := p.stop == stop && p.generation == generation && !p.stopped
		paused := p.paused
		p.mu.Unlock()
		if !valid {
			p.opMu.Unlock()
			return
		}
		pos = p.elapsedNow()
		p.opMu.Unlock()
		now := time.Now()
		if wasPlaying && !paused {
			heard += now.Sub(last)
		}
		last = now
		wasPlaying = !paused
		state := "playing"
		if paused {
			state = "paused"
		}
		u := update{state: state, pos: pos, heard: heard}
		// Keep the newest snapshot when reporting is slow, without delaying sampling.
		select {
		case updates <- u:
		default:
			select {
			case <-updates:
			default:
			}
			select {
			case updates <- u:
			default:
			}
		}
		select {
		case <-stop:
			return
		case <-tick.C:
		}
	}
}

// ---- UI ----

func plexUI(app *App) {
	cfg, err := loadPlex()
	if err != nil {
		if os.IsNotExist(err) {
			message(app, "PLEX", "CONFIGURE PLEX IN WEB REMOTE > PLEX SETTINGS")
		} else {
			message(app, "PLEX", strings.ToUpper(err.Error()))
		}
		return
	}
	sec, err := cfg.musicSection()
	if err != nil {
		message(app, "PLEX ERROR", strings.ToUpper(err.Error()))
		return
	}
	initial := 0
	for {
		i, ok := menu(app, "PLEX", []string{"ARTISTS", "ALBUMS", "RECENT ALBUMS", "PLAYLISTS"}, initial)
		if !ok {
			return
		}
		initial = i
		switch i {
		case 0:
			plexArtistsUI(app, cfg, sec)
		case 1:
			plexAllAlbumsUI(app, cfg, sec)
		case 2:
			plexRecentUI(app, cfg, sec)
		case 3:
			plexPlaylistsUI(app, cfg)
		}
		if app.jumpSources {
			return
		}
	}
}

func plexArtistsUI(app *App, cfg PlexConfig, sec string) {
	artists, err := cfg.list("/library/sections/"+sec+"/all", url.Values{"type": {"8"}, "sort": {"titleSort"}}, 0)
	if err != nil {
		message(app, "PLEX ERROR", strings.ToUpper(err.Error()))
		return
	}
	if len(artists) == 0 {
		message(app, "PLEX", "NO ARTISTS")
		return
	}
	names := make([]string, len(artists))
	for i, a := range artists {
		names[i] = a.Title
	}
	initial := 0
	for {
		i, ok := menuWithEntryCounter(app, "PLEX - ARTISTS", names, initial, true)
		if !ok {
			return
		}
		initial = i
		albums, err := cfg.list("/library/metadata/"+artists[i].RatingKey+"/children", nil, 0)
		if err != nil {
			message(app, "PLEX ERROR", strings.ToUpper(err.Error()))
			continue
		}
		plexAlbumsUI(app, cfg, artists[i].Title, albums, false)
		if app.jumpSources {
			return
		}
	}
}

func plexAllAlbumsUI(app *App, cfg PlexConfig, sec string) {
	albums, err := cfg.list("/library/sections/"+sec+"/all", url.Values{"type": {"9"}, "sort": {"artist.titleSort,titleSort"}}, 0)
	if err != nil {
		message(app, "PLEX ERROR", strings.ToUpper(err.Error()))
		return
	}
	plexAlbumsUI(app, cfg, "ALBUMS", albums, true)
}

func plexRecentUI(app *App, cfg PlexConfig, sec string) {
	albums, err := cfg.list("/library/sections/"+sec+"/all", url.Values{"type": {"9"}, "sort": {"addedAt:desc"}}, 100)
	if err != nil {
		message(app, "PLEX ERROR", strings.ToUpper(err.Error()))
		return
	}
	plexAlbumsUI(app, cfg, "RECENT ALBUMS", albums, true)
}

func plexAlbumsUI(app *App, cfg PlexConfig, title string, albums []plexMeta, withArtist bool) {
	if len(albums) == 0 {
		message(app, "PLEX", "NO ALBUMS")
		return
	}
	names := make([]string, len(albums))
	for i, a := range albums {
		names[i] = a.Title
		if withArtist && a.ParentTitle != "" {
			names[i] = a.ParentTitle + " - " + a.Title
		}
	}
	initial := 0
	for {
		i, ok := menuWithEntryCounter(app, "PLEX - "+title, names, initial, true)
		if !ok {
			return
		}
		initial = i
		tracks, err := cfg.list("/library/metadata/"+albums[i].RatingKey+"/children", nil, 0)
		if err != nil {
			message(app, "PLEX ERROR", strings.ToUpper(err.Error()))
			continue
		}
		plexTracksUI(app, cfg, albums[i].Title, tracks, false)
		if app.jumpSources {
			return
		}
	}
}

func plexPlaylistsUI(app *App, cfg PlexConfig) {
	lists, err := cfg.list("/playlists", url.Values{"playlistType": {"audio"}}, 0)
	if err != nil {
		message(app, "PLEX ERROR", strings.ToUpper(err.Error()))
		return
	}
	if len(lists) == 0 {
		message(app, "PLEX", "NO PLAYLISTS")
		return
	}
	names := make([]string, len(lists))
	for i, l := range lists {
		names[i] = l.Title
	}
	initial := 0
	for {
		i, ok := menuWithEntryCounter(app, "PLEX - PLAYLISTS", names, initial, true)
		if !ok {
			return
		}
		initial = i
		tracks, err := cfg.list("/playlists/"+lists[i].RatingKey+"/items", nil, 0)
		if err != nil {
			message(app, "PLEX ERROR", strings.ToUpper(err.Error()))
			continue
		}
		plexTracksUI(app, cfg, lists[i].Title, tracks, true)
		if app.jumpSources {
			return
		}
	}
}

func plexTracksUI(app *App, cfg PlexConfig, title string, metas []plexMeta, withArtist bool) {
	var tracks []Track
	var names []string
	for _, m := range metas {
		t := plexTrack(m)
		t.PlexSource = &cfg
		if t.PlexPart == "" {
			continue
		}
		tracks = append(tracks, t)
		name := m.Title
		if withArtist && m.GrandparentTitle != "" {
			name = m.GrandparentTitle + " - " + m.Title
		} else if m.Index > 0 {
			name = fmt.Sprintf("%02d  %s", m.Index, m.Title)
		}
		names = append(names, name)
	}
	if len(tracks) == 0 {
		message(app, "PLEX", "NO TRACKS")
		return
	}
	initial := 0
	for {
		i, ok := menuWithEntryCounter(app, "PLEX - "+title, names, initial, true)
		if !ok {
			return
		}
		initial = i
		q := Queue{Tracks: append([]Track{}, tracks...), Index: i}
		origin := &browseOrigin{Kind: "plex", Selected: names[i]}
		if err := app.startQueue(q, origin); err != nil {
			message(app, "PLEX ERROR", strings.ToUpper(err.Error()))
			continue
		}
		playerUI(app)
		if app.jumpSources {
			return
		}
		if t := app.player; t != nil {
			t.mu.Lock()
			if t.q.Index >= 0 && t.q.Index < len(names) && len(t.q.Tracks) == len(tracks) {
				// keep the cursor on the track that was playing when we left
				initial = t.q.Index
			}
			t.mu.Unlock()
		}
	}
}

// Playback and prefetch share the shuffle choice; the plan is invalidated by
// changing repeat/shuffle settings or moving to another index.
type plexNextPlan struct {
	from, next           int
	shuffle, repeat, one bool
	valid                bool
}

func (p *Player) plexNextLocked(manual bool) int {
	q := &p.q
	n := len(q.Tracks)
	if n == 0 {
		return -1
	}
	if !manual && q.Repeat && q.RepeatOne {
		return q.Index
	}
	if q.Shuffle && n > 1 {
		if q.Tracks[q.Index].PlexKey == "" {
			next := int(time.Now().UnixNano() % int64(n-1))
			if next >= q.Index {
				next++
			}
			return next
		}
		plan := &p.plexPlan
		if !plan.valid || plan.from != q.Index || plan.shuffle != q.Shuffle || plan.repeat != q.Repeat || plan.one != q.RepeatOne {
			next := int(time.Now().UnixNano() % int64(n-1))
			if next >= q.Index {
				next++
			}
			*plan = plexNextPlan{from: q.Index, next: next, shuffle: q.Shuffle, repeat: q.Repeat, one: q.RepeatOne, valid: true}
		}
		return plan.next
	}
	if q.Index+1 < n {
		return q.Index + 1
	}
	if q.Repeat || manual {
		return 0
	}
	return -1
}
func (p *Player) isLoading() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.loading }
func playbackContext(stop <-chan struct{}) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}
func (p *Player) preparePlexTrack(ctx context.Context, t Track, idx int, stop chan struct{}, generation uint64, cfg Config) {
	plexCfg, err := trackPlexConfig(t)
	local := ""
	if err == nil {
		local, err = plexEnsureLocal(ctx, plexCfg, t)
	}
	p.opMu.Lock()
	defer p.opMu.Unlock()
	p.mu.Lock()
	if p.stop != stop || p.generation != generation || ctx.Err() != nil {
		p.mu.Unlock()
		return
	}
	p.loading = false
	if err != nil {
		p.playbackError = err.Error()
		p.stopped = true
		close(stop)
		p.stop = nil
		p.mu.Unlock()
		clearNowPlaying()
		log.Printf("Plex playback: %v", err)
		return
	}
	t.Path = local
	t.PlexSource = &plexCfg
	p.q.Tracks[idx] = t
	paused := p.paused
	p.mu.Unlock()
	if err := p.startPreparedTrackUnlocked(t, idx, stop, generation, cfg); err != nil {
		log.Printf("Plex audio: %v", err)
		return
	}
	nativeAudioPause(paused)
}
func (p *Player) plexArtwork(t Track, idx int, stop <-chan struct{}, generation uint64) {
	ctx, cancel := playbackContext(stop)
	defer cancel()
	cfg, err := trackPlexConfig(t)
	if err != nil {
		return
	}
	art := plexArt(ctx, cfg, t)
	if art == nil || ctx.Err() != nil {
		return
	}
	p.mu.Lock()
	valid := p.stop == stop && p.generation == generation && idx == p.q.Index
	if valid && p.q.Tracks[idx].Art == nil {
		p.q.Tracks[idx].Art = art
	}
	p.mu.Unlock()
	if valid {
		p.publishCurrent()
	}
}
func (p *Player) plexPrefetch(idx int, cur Track, stop <-chan struct{}, generation uint64) {
	ctx, cancel := playbackContext(stop)
	defer cancel()
	cfg, err := trackPlexConfig(cur)
	if err != nil {
		return
	}
	p.mu.Lock()
	nextIndex := p.plexNextLocked(false)
	var next Track
	if nextIndex >= 0 {
		next = p.q.Tracks[nextIndex]
	}
	p.mu.Unlock()
	keep := map[string]bool{plexCachePath(cfg, cur): true}
	if next.PlexKey != "" {
		keep[plexCachePath(cfg, next)] = true
	}
	if !p.plexTrimCache(ctx, keep, stop, generation) {
		return
	}
	if next.PlexKey != "" {
		if _, err := plexEnsureLocal(ctx, cfg, next); err != nil && ctx.Err() == nil {
			log.Printf("Plex prefetch: %v", err)
		}
	}
}
func (p *Player) plexTrimCache(ctx context.Context, keep map[string]bool, stop <-chan struct{}, generation uint64) bool {
	if err := cacheLock(ctx); err != nil {
		return false
	}
	defer cacheUnlock()
	p.opMu.Lock()
	defer p.opMu.Unlock()
	p.mu.Lock()
	valid := p.stop == stop && p.generation == generation && !p.stopped
	p.mu.Unlock()
	if !valid || ctx.Err() != nil {
		return false
	}
	entries, _ := os.ReadDir(plexTmpDir)
	for _, entry := range entries {
		path := filepath.Join(plexTmpDir, entry.Name())
		if !keep[path] {
			_ = os.Remove(path)
		}
	}
	return true
}

var plexIDMu sync.Mutex
var plexIDCache string

func plexDeviceID() string {
	plexIDMu.Lock()
	defer plexIDMu.Unlock()
	if plexIDCache != "" {
		return plexIDCache
	}
	path := filepath.Join(baseDir, "plex-client-id")
	if b, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(b)) != "" {
		plexIDCache = strings.TrimSpace(string(b))
		return plexIDCache
	}
	// Kernel boot ID gives a per-machine fallback even if the SD card is read-only.
	b, _ := os.ReadFile("/proc/sys/kernel/random/uuid")
	id := strings.TrimSpace(string(b))
	if id == "" {
		host, _ := os.Hostname()
		id = host + "-" + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	id = plexClientID + "-" + id
	_ = os.MkdirAll(baseDir, 0755)
	_ = os.WriteFile(path, []byte(id), 0600)
	plexIDCache = id
	return id
}

func scrobblePlexLastFM(cfg LastFMConfig, t Track, startedAt time.Time) error {
	if !cfg.ready() {
		return nil
	}
	params := map[string]string{"api_key": cfg.APIKey, "sk": cfg.SessionKey, "track": t.Title, "artist": nonEmptyOr(t.Artist, "Unknown Artist"), "album": t.Album, "timestamp": strconv.FormatInt(startedAt.Unix(), 10)}
	_, err := lastfmCall("track.scrobble", params, cfg.APISecret, http.MethodPost)
	return err
}

func trackPlexConfig(t Track) (PlexConfig, error) {
	if t.PlexSource != nil {
		return *t.PlexSource, nil
	}
	return loadPlex()
}
