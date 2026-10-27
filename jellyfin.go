package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Jellyfin as a media server (mediaserver.go): the Jellyfin-specific half.
// Emby (emby.go) uses the same code: its API is the one Jellyfin forked from,
// and JellyfinConfig.emby / jellyfinServer.emby switch the few differences:
// the /emby path prefix, the X-Emby-* headers, no Quick Connect (a user name
// and password sign-in instead), the item queries under /Users/<id>/ and
// numeric item ids.
//
// Config baseDir/jellyfin.json:
//
//	{"server": "http://host:8096", "token": "...", "user_id": "...",
//	 "library": "", "insecure": false, "scrobbling": "server", "max_track_mb": 128}
//
// token and user_id come from signing in with Quick Connect (the JELLYFIN
// entry of the sources menu does it, or Settings in the web remote, or
// `mister_hifi --jellyfin-login http://host:8096`). A file with only "server"
// is enough to make the entry appear. library is the id of the music library
// (the first one by default). scrobbling says who counts a listen: "server"
// (Jellyfin keeps the history, the default), "lastfm" (Hi-Fi's own Last.fm
// scrobbling) or "none".
//
// Jellyfin 12 only accepts the token in the Authorization header
// (MediaBrowser Token="..."), not as ?api_key= or X-Emby-Token.

const jellyfinClientID = "misterhifi-jellyfin"
const embyClientID = "misterhifi-emby"

var jellyfinConfigPath = filepath.Join(baseDir, "jellyfin.json")
var embyConfigPath = filepath.Join(baseDir, "emby.json")

type JellyfinConfig struct {
	Server     string `json:"server"`
	Token      string `json:"token"`
	UserID     string `json:"user_id"`
	Library    string `json:"library"`
	Insecure   bool   `json:"insecure"`
	Scrobbling string `json:"scrobbling,omitempty"` // server (default), lastfm, or none
	MaxTrackMB int64  `json:"max_track_mb,omitempty"`

	emby bool // set by the loaders: this is emby.json, not jellyfin.json
}

func (c JellyfinConfig) path() string {
	if c.emby {
		return embyConfigPath
	}
	return jellyfinConfigPath
}

// name is the server's name in messages.
func (c JellyfinConfig) name() string {
	if c.emby {
		return "Emby"
	}
	return "Jellyfin"
}

// jellyfinID accepts what item ids look like: Jellyfin's 32 hex digits (or the
// same with dashes) and Emby's numbers. Ids go into URL paths and queries, so
// nothing but hex digits and dashes may.
func jellyfinID(s string) bool {
	if len(s) == 0 || len(s) > 36 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' || c == '-') {
			return false
		}
	}
	return true
}

func loadJellyfin() (JellyfinConfig, error) { return loadJF(false) }
func loadEmby() (JellyfinConfig, error)     { return loadJF(true) }

func loadJF(emby bool) (JellyfinConfig, error) {
	c := JellyfinConfig{emby: emby}
	b, err := os.ReadFile(c.path())
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	c.Server = strings.TrimRight(strings.TrimSpace(c.Server), "/")
	c.Token = strings.TrimSpace(c.Token)
	c.UserID = strings.TrimSpace(c.UserID)
	if !isHTTPURL(c.Server) {
		return c, errors.New(strings.ToLower(c.name()) + ".json needs server (http://host:8096)")
	}
	if c.Library != "" && !jellyfinID(c.Library) {
		return c, errors.New(c.name() + " library must be a library id")
	}
	if c.UserID != "" && !jellyfinID(c.UserID) {
		return c, errors.New("invalid " + c.name() + " user id")
	}
	if c.Scrobbling == "" || c.Scrobbling == "plex" {
		c.Scrobbling = "server"
	}
	if c.Scrobbling != "server" && !isOwnScrobbleMode(c.Scrobbling) && c.Scrobbling != "none" {
		return c, errors.New("scrobbling must be server, lastfm, listenbrainz, both, or none")
	}
	if c.MaxTrackMB == 0 {
		c.MaxTrackMB = 128
	}
	if c.MaxTrackMB < 1 || c.MaxTrackMB > 1024 {
		return c, errors.New("max_track_mb must be between 1 and 1024")
	}
	return c, nil
}

// loadJellyfinRaw reads the file without checking it (the settings form shows
// whatever is there, so a mistake can be fixed).
func loadJellyfinRaw() (JellyfinConfig, error) { return loadJFRaw(false) }

func loadJFRaw(emby bool) (JellyfinConfig, error) {
	c := JellyfinConfig{emby: emby}
	b, err := os.ReadFile(c.path())
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(b, &c)
}

func saveJellyfin(c JellyfinConfig) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path()), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(c.path()), strings.ToLower(c.name())+"-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	_ = os.Chmod(f.Name(), 0600) // it holds a token (a no-op on the FAT card)
	return os.Rename(f.Name(), c.path())
}

var jellyfinInsecureTransport = func() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // opt-in for self-signed LAN servers
	return transport
}()

func (c JellyfinConfig) client(timeout time.Duration) *http.Client {
	cl := &http.Client{Timeout: timeout}
	if c.Insecure {
		cl.Transport = jellyfinInsecureTransport
	}
	cl.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New(c.tag() + ": too many redirects")
		}
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			req.Header.Del("Authorization")
			req.Header.Del("X-Emby-Token")
			req.Header.Del("X-Emby-Authorization")
		}
		return nil
	}
	return cl
}

func (c JellyfinConfig) tag() string { return strings.ToLower(c.name()) }

// authHeader is the Authorization header; the token is left out before signing in.
func (c JellyfinConfig) authHeader() string {
	h := fmt.Sprintf(`MediaBrowser Client="MiSTer Hi-Fi", Device="MiSTer FPGA", DeviceId="%s", Version="%s"`,
		c.deviceID(), version)
	if c.Token != "" && !c.emby {
		h += fmt.Sprintf(`, Token="%s"`, c.Token)
	}
	return h
}

func (c JellyfinConfig) deviceID() string {
	if c.emby {
		return deviceID("emby-client-id", embyClientID)
	}
	return deviceID("jellyfin-client-id", jellyfinClientID)
}

// setAuth puts the credentials on a request: Jellyfin 12 takes the token in
// the Authorization header alone; Emby takes X-Emby-Authorization (who is
// asking) and X-Emby-Token.
func (c JellyfinConfig) setAuth(req *http.Request) {
	if !c.emby {
		req.Header.Set("Authorization", c.authHeader())
		return
	}
	req.Header.Set("X-Emby-Authorization", c.authHeader())
	if c.Token != "" {
		req.Header.Set("X-Emby-Token", c.Token)
	}
}

func (c JellyfinConfig) request(ctx context.Context, method, path string, q url.Values, body any) (*http.Request, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return nil, errors.New(c.tag() + ": invalid server path")
	}
	if c.emby {
		path = "/emby" + path
	}
	u := c.Server + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	c.setAuth(req)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// do runs a request and decodes a JSON answer into out (when not nil).
func (c JellyfinConfig) do(req *http.Request, timeout time.Duration, out any) error {
	resp, err := c.client(timeout).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return errNotSignedIn
	}
	if resp.StatusCode/100 != 2 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return fmt.Errorf("%s: server returned HTTP %d", c.tag(), resp.StatusCode)
	}
	if out == nil {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out); err != nil {
		return fmt.Errorf("%s: invalid server response: %w", c.tag(), err)
	}
	return nil
}

type jfItem struct {
	ID             string                  `json:"Id"`
	Name           string                  `json:"Name"`
	Album          string                  `json:"Album"`
	AlbumID        string                  `json:"AlbumId"`
	Artists        []string                `json:"Artists"`
	AlbumArtist    string                  `json:"AlbumArtist"`
	AlbumArtists   []struct{ Name string } `json:"AlbumArtists"`
	IndexNumber    int                     `json:"IndexNumber"`
	RunTimeTicks   int64                   `json:"RunTimeTicks"`
	CollectionType string                  `json:"CollectionType"`
	MediaSources   []struct {
		Container    string `json:"Container"`
		Bitrate      int    `json:"Bitrate"`
		MediaStreams []struct {
			Type       string `json:"Type"`
			BitDepth   int    `json:"BitDepth"`
			SampleRate int    `json:"SampleRate"`
		} `json:"MediaStreams"`
	} `json:"MediaSources"`
}

func (it jfItem) albumArtist() string {
	if it.AlbumArtist != "" {
		return it.AlbumArtist
	}
	if len(it.AlbumArtists) > 0 {
		return it.AlbumArtists[0].Name
	}
	if len(it.Artists) > 0 {
		return it.Artists[0]
	}
	return ""
}

type jfList struct {
	Items            []jfItem `json:"Items"`
	TotalRecordCount int      `json:"TotalRecordCount"`
}

// list pages through an endpoint that answers {Items, TotalRecordCount}.
func (c JellyfinConfig) list(ctx context.Context, path string, q url.Values, max int) ([]jfItem, error) {
	var out []jfItem
	const page = 300
	start := 0
	for {
		qq := url.Values{}
		for k, v := range q {
			qq[k] = v
		}
		qq.Set("startIndex", fmt.Sprint(start))
		limit := page
		if max > 0 && max-len(out) < limit {
			limit = max - len(out)
		}
		qq.Set("limit", fmt.Sprint(limit))
		req, err := c.request(ctx, "GET", path, qq, nil)
		if err != nil {
			return nil, err
		}
		var l jfList
		if err := c.do(req, 30*time.Second, &l); err != nil {
			return nil, err
		}
		out = append(out, l.Items...)
		start += len(l.Items)
		if len(l.Items) == 0 || start >= l.TotalRecordCount || (max > 0 && len(out) >= max) {
			break
		}
	}
	return out, nil
}

// ---- the media server ----

// jellyfinServer is the mediaServer for Jellyfin (config: jellyfin.json), or
// for Emby (config: emby.json) when emby is set.
type jellyfinServer struct{ emby bool }

func (s jellyfinServer) ID() string {
	if s.emby {
		return "emby"
	}
	return "jellyfin"
}

func (s jellyfinServer) Label() string { return strings.ToUpper(s.ID()) }

func (s jellyfinServer) Configured() bool {
	_, err := os.Stat(JellyfinConfig{emby: s.emby}.path())
	return err == nil
}

// viewsPath is the endpoint that lists a user's libraries.
func (c JellyfinConfig) viewsPath() string {
	if c.emby {
		return "/Users/" + c.UserID + "/Views"
	}
	return "/UserViews"
}

func (s jellyfinServer) Connect(ctx context.Context) (mediaSession, error) {
	c, err := loadJF(s.emby)
	if err != nil {
		return nil, err
	}
	if c.Token == "" || c.UserID == "" {
		return nil, errNotSignedIn
	}
	lib := c.Library
	if lib == "" {
		views, err := c.list(ctx, c.viewsPath(), url.Values{"userId": {c.UserID}}, 0)
		if err != nil {
			return nil, err
		}
		for _, v := range views {
			if v.CollectionType == "music" && jellyfinID(v.ID) {
				lib = v.ID
				break
			}
		}
		if lib == "" {
			return nil, errors.New("no music library on " + c.name())
		}
	}
	return &jellyfinSession{cfg: c, library: lib}, nil
}

// jellyfinSession keeps the config it was made with (a snapshot).
type jellyfinSession struct {
	cfg         JellyfinConfig
	library     string
	mu          sync.Mutex
	started     string // ItemId of the track whose playback start was reported
	playSession string // the id of that playback (Emby asks for it)
}

func (s *jellyfinSession) ValidID(id string) bool { return jellyfinID(id) }

// items lists an endpoint's items; "/Items" is Emby's per-user form when asked
// for under Emby (Emby documents the item queries under /Users/<id>/).
func (s *jellyfinSession) items(ctx context.Context, path string, extra url.Values, max int) ([]jfItem, error) {
	if s.cfg.emby && path == "/Items" {
		path = "/Users/" + s.cfg.UserID + "/Items"
	}
	q := url.Values{"userId": {s.cfg.UserID}}
	for k, v := range extra {
		q[k] = v
	}
	return s.cfg.list(ctx, path, q, max)
}

func jellyfinMediaItems(items []jfItem, withArtist bool) []mediaItem {
	out := make([]mediaItem, 0, len(items))
	for _, it := range items {
		if !jellyfinID(it.ID) {
			continue
		}
		m := mediaItem{ID: it.ID, Title: it.Name}
		if withArtist {
			m.Artist = it.albumArtist()
		}
		out = append(out, m)
	}
	return out
}

func (s *jellyfinSession) Artists(ctx context.Context) ([]mediaItem, error) {
	it, err := s.items(ctx, "/Artists/AlbumArtists", url.Values{
		"parentId": {s.library}, "sortBy": {"SortName"}, "sortOrder": {"Ascending"},
	}, 0)
	return jellyfinMediaItems(it, false), err
}

func (s *jellyfinSession) albumQuery(extra url.Values) url.Values {
	q := url.Values{
		"parentId": {s.library}, "includeItemTypes": {"MusicAlbum"}, "recursive": {"true"},
	}
	for k, v := range extra {
		q[k] = v
	}
	return q
}

func (s *jellyfinSession) ArtistAlbums(ctx context.Context, artistID string) ([]mediaItem, error) {
	it, err := s.items(ctx, "/Items", s.albumQuery(url.Values{
		"albumArtistIds": {artistID}, "sortBy": {"ProductionYear,SortName"},
	}), 0)
	return jellyfinMediaItems(it, false), err
}

func (s *jellyfinSession) Albums(ctx context.Context, recent bool) ([]mediaItem, error) {
	extra, max := url.Values{"sortBy": {"AlbumArtist,SortName"}}, 0
	if recent {
		extra, max = url.Values{"sortBy": {"DateCreated"}, "sortOrder": {"Descending"}}, 100
	}
	it, err := s.items(ctx, "/Items", s.albumQuery(extra), max)
	return jellyfinMediaItems(it, true), err
}

func (s *jellyfinSession) Playlists(ctx context.Context) ([]mediaItem, error) {
	q := url.Values{"includeItemTypes": {"Playlist"}, "recursive": {"true"}, "mediaTypes": {"Audio"}, "sortBy": {"SortName"}}
	if s.cfg.emby {
		// Emby takes mediaTypes over includeItemTypes and lists every audio item
		q.Del("mediaTypes")
	}
	it, err := s.items(ctx, "/Items", q, 0)
	return jellyfinMediaItems(it, false), err
}

func jellyfinTrack(it jfItem, id string) Track {
	t := Track{
		Path:     id + ":" + it.ID,
		SrvKey:   it.ID,
		SrvPart:  "/Items/" + it.ID + "/Download",
		Title:    it.Name,
		Album:    it.Album,
		Number:   it.IndexNumber,
		Duration: float64(it.RunTimeTicks) / 1e7,
		DirFD:    -1,
	}
	if len(it.Artists) > 0 {
		t.Artist = it.Artists[0]
	} else {
		t.Artist = it.albumArtist()
	}
	if len(it.MediaSources) > 0 {
		ms := it.MediaSources[0]
		t.BitRate = ms.Bitrate / 1000
		if ms.Container != "" {
			t.SrvExt = srvExtFromContainer(ms.Container)
		}
		for _, st := range ms.MediaStreams {
			if st.Type == "Audio" {
				t.BitDepth = st.BitDepth
				t.SampleRate = st.SampleRate
				break
			}
		}
	}
	// the cover is the album's
	t.SrvThumb = it.AlbumID
	if !jellyfinID(t.SrvThumb) {
		t.SrvThumb = it.ID
	}
	return t
}

func jellyfinTracks(items []jfItem, id string) []Track {
	out := make([]Track, 0, len(items))
	for _, it := range items {
		if jellyfinID(it.ID) {
			out = append(out, jellyfinTrack(it, id))
		}
	}
	return out
}

// srvID is the id of the media server this session talks to.
func (s *jellyfinSession) srvID() string { return jellyfinServer{emby: s.cfg.emby}.ID() }

func (s *jellyfinSession) AlbumTracks(ctx context.Context, albumID string) ([]Track, error) {
	it, err := s.items(ctx, "/Items", url.Values{
		"parentId": {albumID}, "includeItemTypes": {"Audio"}, "fields": {"MediaSources"},
		"sortBy": {"ParentIndexNumber,IndexNumber,SortName"},
	}, 0)
	return jellyfinTracks(it, s.srvID()), err
}

func (s *jellyfinSession) PlaylistTracks(ctx context.Context, id string) ([]Track, error) {
	it, err := s.items(ctx, "/Playlists/"+id+"/Items", url.Values{"fields": {"MediaSources"}}, 0)
	return jellyfinTracks(it, s.srvID()), err
}

// FindAlbum finds the album that best matches a text like "artist album".
// Jellyfin's search only looks at one item's own name, and a library whose
// tags are incomplete may have no artist on its albums at all, so every word
// of the text is searched, among albums and among tracks (a track's name and
// artist tell which album it is on), and the albums are scored on how many
// of the words occur in what is known about them. The best score wins, the
// shortest title among equals ("X" before "X (Deluxe)").
func (s *jellyfinSession) FindAlbum(ctx context.Context, query string) (mediaItem, error) {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return mediaItem{}, errors.New("empty search")
	}
	terms := []string{query}
	byLength := append([]string{}, words...)
	sort.SliceStable(byLength, func(i, j int) bool { return len(byLength[i]) > len(byLength[j]) })
	terms = append(terms, byLength...)

	type cand struct {
		item mediaItem
		hay  string
	}
	cands := map[string]*cand{}
	add := func(id, title, artist, extra string) {
		if !jellyfinID(id) {
			return
		}
		c := cands[id]
		if c == nil {
			c = &cand{item: mediaItem{ID: id, Title: title, Artist: artist}}
			cands[id] = c
		}
		c.hay += " " + strings.ToLower(artist+" "+title+" "+extra)
	}
	seen := map[string]bool{}
	for _, term := range terms {
		if seen[term] {
			continue
		}
		seen[term] = true
		albums, err := s.items(ctx, "/Items", s.albumQuery(url.Values{"searchTerm": {term}}), 100)
		if err != nil {
			return mediaItem{}, err
		}
		for _, it := range albums {
			add(it.ID, it.Name, it.albumArtist(), "")
		}
		tracks, err := s.items(ctx, "/Items", url.Values{
			"parentId": {s.library}, "includeItemTypes": {"Audio"}, "recursive": {"true"}, "searchTerm": {term},
		}, 100)
		if err != nil {
			return mediaItem{}, err
		}
		for _, it := range tracks {
			if it.AlbumID != "" {
				add(it.AlbumID, it.Album, it.albumArtist(), it.Name+" "+strings.Join(it.Artists, " "))
			}
		}
	}
	var best *cand
	bestScore := 0
	for _, c := range cands {
		score := 0
		for _, w := range words {
			if strings.Contains(c.hay, w) {
				score++
			}
		}
		if score > bestScore || (score == bestScore && best != nil && len(c.item.Title) < len(best.item.Title)) {
			best, bestScore = c, score
		}
	}
	// at least most of the words must be there, or it is no match
	if best == nil || bestScore*3 < len(words)*2 {
		return mediaItem{}, errors.New("no album found")
	}
	return best.item, nil
}

func (s *jellyfinSession) Download(ctx context.Context, t Track) (io.ReadCloser, int64, error) {
	if !jellyfinID(t.SrvKey) {
		return nil, 0, errors.New(s.cfg.tag() + ": invalid item id")
	}
	req, err := s.cfg.request(ctx, "GET", "/Items/"+t.SrvKey+"/Download", nil, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Del("Accept")
	resp, err := s.cfg.client(5 * time.Minute).Do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("%s: download failed (HTTP %d)", s.cfg.tag(), resp.StatusCode)
	}
	return resp.Body, resp.ContentLength, nil
}

func (s *jellyfinSession) Art(ctx context.Context, t Track) image.Image {
	if !jellyfinID(t.SrvThumb) {
		return nil
	}
	req, err := s.cfg.request(ctx, "GET", "/Items/"+t.SrvThumb+"/Images/Primary", url.Values{"maxWidth": {"600"}}, nil)
	if err != nil {
		return nil
	}
	req.Header.Del("Accept")
	resp, err := s.cfg.client(20 * time.Second).Do(req)
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

func (s *jellyfinSession) post(ctx context.Context, path string, q url.Values, body any) error {
	req, err := s.cfg.request(ctx, "POST", path, q, body)
	if err != nil {
		return err
	}
	return s.cfg.do(req, 8*time.Second, nil)
}

func (s *jellyfinSession) Report(ctx context.Context, t Track, state string, pos time.Duration) error {
	ticks := pos.Nanoseconds() / 100
	body := map[string]any{"ItemId": t.SrvKey, "PositionTicks": ticks, "PlayMethod": "DirectPlay", "CanSeek": true}
	s.mu.Lock()
	first := s.started != t.SrvKey && state != "stopped"
	if first {
		// one id for the whole playback of the track; Emby refuses a start without it
		s.started, s.playSession = t.SrvKey, fmt.Sprintf("%x", time.Now().UnixNano())
	}
	if s.cfg.emby {
		body["PlaySessionId"] = s.playSession
	}
	s.mu.Unlock()
	if state == "stopped" {
		return s.post(ctx, "/Sessions/Playing/Stopped", nil, body)
	}
	body["IsPaused"] = state == "paused"
	if first {
		return s.post(ctx, "/Sessions/Playing", nil, body)
	}
	return s.post(ctx, "/Sessions/Playing/Progress", nil, body)
}

func (s *jellyfinSession) Played(ctx context.Context, t Track) error {
	if s.cfg.emby {
		return s.post(ctx, "/Users/"+s.cfg.UserID+"/PlayedItems/"+t.SrvKey, nil, nil)
	}
	// Jellyfin 10.9+; older servers use /Users/{id}/PlayedItems/{id}
	err := s.post(ctx, "/UserPlayedItems/"+t.SrvKey, url.Values{"userId": {s.cfg.UserID}}, nil)
	if err != nil && !errors.Is(err, errNotSignedIn) && ctx.Err() == nil {
		return s.post(ctx, "/Users/"+s.cfg.UserID+"/PlayedItems/"+t.SrvKey, nil, nil)
	}
	return err
}

func (s *jellyfinSession) Scrobbling() string   { return s.cfg.Scrobbling }
func (s *jellyfinSession) MaxTrackBytes() int64 { return s.cfg.MaxTrackMB << 20 }

func (s *jellyfinSession) CacheKey(t Track) string {
	return s.cfg.Server + "\x00" + s.cfg.UserID + "\x00" + t.SrvKey + "\x00" + t.SrvPart
}

// ---- Quick Connect sign-in ----

type jfQuickConnect struct {
	Secret        string `json:"Secret"`
	Code          string `json:"Code"`
	Authenticated bool   `json:"Authenticated"`
}

// quickConnectStart asks the server for a sign-in code. The user enters it
// in Jellyfin (profile, Quick Connect); quickConnectFinish then collects the token.
func (c JellyfinConfig) quickConnectStart(ctx context.Context) (jfQuickConnect, error) {
	var qc jfQuickConnect
	req, err := c.request(ctx, "POST", "/QuickConnect/Initiate", nil, nil)
	if err != nil {
		return qc, err
	}
	if err := c.do(req, 10*time.Second, &qc); err != nil {
		if errors.Is(err, errNotSignedIn) {
			return qc, errors.New("Quick Connect is switched off on the server")
		}
		return qc, err
	}
	if qc.Secret == "" || qc.Code == "" {
		return qc, errors.New("jellyfin: no Quick Connect code returned")
	}
	return qc, nil
}

// quickConnectApproved reports whether the code was entered in Jellyfin.
func (c JellyfinConfig) quickConnectApproved(ctx context.Context, secret string) (bool, error) {
	req, err := c.request(ctx, "GET", "/QuickConnect/Connect", url.Values{"secret": {secret}}, nil)
	if err != nil {
		return false, err
	}
	var qc jfQuickConnect
	if err := c.do(req, 10*time.Second, &qc); err != nil {
		return false, err
	}
	return qc.Authenticated, nil
}

// quickConnectFinish exchanges an approved code for a token and saves the config.
func (c JellyfinConfig) quickConnectFinish(ctx context.Context, secret string) (JellyfinConfig, error) {
	req, err := c.request(ctx, "POST", "/Users/AuthenticateWithQuickConnect", nil, map[string]string{"Secret": secret})
	if err != nil {
		return c, err
	}
	var res struct {
		AccessToken string `json:"AccessToken"`
		User        struct {
			ID string `json:"Id"`
		} `json:"User"`
	}
	if err := c.do(req, 10*time.Second, &res); err != nil {
		return c, err
	}
	if res.AccessToken == "" || !jellyfinID(res.User.ID) {
		return c, errors.New("jellyfin: sign-in returned no token")
	}
	c.Token, c.UserID = res.AccessToken, res.User.ID
	return c, saveJellyfin(c)
}

// Login signs in from the screen: it shows the Quick Connect code and waits
// until it was entered in Jellyfin (up to three minutes; Back cancels).
func (s jellyfinServer) Login(app *App) error {
	if s.emby {
		return errors.New("sign in via the web remote (settings, Emby)")
	}
	c, err := loadJellyfin()
	if err != nil {
		return err
	}
	c.Token, c.UserID = "", ""
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	qc, err := c.quickConnectStart(ctx)
	if err != nil {
		return err
	}
	fb := app.fb
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	draw := func() {
		fb.fill(appBackground(app.cfg))
		drawTitle(fb, "JELLYFIN SIGN IN")
		drawWebRemoteAddress(app)
		fb.text(40, fb.h/4, 2, "IN JELLYFIN OPEN YOUR PROFILE,", grey(230))
		fb.text(40, fb.h/4+40, 2, "QUICK CONNECT, AND ENTER:", grey(230))
		fb.text(40, fb.h/2, 6, qc.Code, grey(255))
		fb.text(40, fb.h*3/4, 1, "BACK CANCELS", grey(160))
		fb.present()
	}
	draw()
	for {
		select {
		case a := <-app.acts:
			if a == actBack || a == actSources {
				return errors.New("cancelled")
			}
		case <-ctx.Done():
			return errors.New("sign-in timed out")
		case <-tick.C:
			ok, err := c.quickConnectApproved(ctx, qc.Secret)
			if err != nil {
				return err
			}
			if ok {
				_, err := c.quickConnectFinish(ctx, qc.Secret)
				return err
			}
		}
	}
}

func grey(v uint8) color.RGBA { return color.RGBA{v, v, v, 255} }

// runJellyfinLoginCLI is `mister_hifi --jellyfin-login [http://host:8096]`.
func runJellyfinLoginCLI(args []string) {
	c, err := loadJellyfin()
	if len(args) > 0 {
		c.Server = strings.TrimRight(strings.TrimSpace(args[0]), "/")
	} else if err != nil {
		fmt.Fprintln(os.Stderr, "usage: mister_hifi --jellyfin-login http://host:8096")
		os.Exit(2)
	}
	if !isHTTPURL(c.Server) {
		fmt.Fprintln(os.Stderr, "the server must be an http(s) URL")
		os.Exit(2)
	}
	c.Token, c.UserID = "", ""
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	qc, err := c.quickConnectStart(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "jellyfin:", err)
		os.Exit(1)
	}
	fmt.Printf("In Jellyfin open your profile, Quick Connect, and enter the code: %s\n", qc.Code)
	for ctx.Err() == nil {
		ok, err := c.quickConnectApproved(ctx, qc.Secret)
		if err != nil {
			fmt.Fprintln(os.Stderr, "jellyfin:", err)
			os.Exit(1)
		}
		if ok {
			if _, err := c.quickConnectFinish(ctx, qc.Secret); err != nil {
				fmt.Fprintln(os.Stderr, "jellyfin:", err)
				os.Exit(1)
			}
			fmt.Println("Signed in. Saved to", jellyfinConfigPath)
			return
		}
		time.Sleep(2 * time.Second)
	}
	fmt.Fprintln(os.Stderr, "timed out")
	os.Exit(1)
}

// testJellyfinConnection checks the server answers as a Jellyfin and, when
// signed in, that there is a music library to use.
func testJellyfinConnection(ctx context.Context, cfg JellyfinConfig) error {
	req, err := cfg.request(ctx, "GET", "/System/Info/Public", nil, nil)
	if err != nil {
		return err
	}
	var info struct {
		ProductName string `json:"ProductName"`
	}
	if err := cfg.do(req, 10*time.Second, &info); err != nil && !errors.Is(err, errNotSignedIn) {
		return err
	}
	if cfg.Token == "" || cfg.UserID == "" {
		if cfg.emby {
			return errors.New("server reachable, but not signed in yet: use Sign in")
		}
		return errors.New("server reachable, but not signed in yet: use Sign in with Quick Connect")
	}
	if cfg.Library != "" {
		return nil
	}
	views, err := cfg.list(ctx, cfg.viewsPath(), url.Values{"userId": {cfg.UserID}}, 0)
	if err != nil {
		return err
	}
	for _, v := range views {
		if v.CollectionType == "music" {
			return nil
		}
	}
	return errors.New("server connected, but no music library was found")
}
