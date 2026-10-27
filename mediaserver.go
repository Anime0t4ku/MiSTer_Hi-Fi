package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Media servers.
//
// A media server is a music server Hi-Fi can browse and stream from (Plex,
// Jellyfin, ...). Everything that does not depend on which server it is lives
// in this file: the track cache, fetching a track before it plays, the
// playback reports and scrobbling, prefetching the next track, the shuffle
// plan, the <id>://album/<key> targets for the command line and NFC tags, and
// the menus. A server only implements mediaServer and mediaSession (plex.go,
// jellyfin.go) and is listed in mediaServers; hifi/docs/MEDIASERVERS.md has
// the steps.
//
// Tracks are fetched into a small temporary cache and played as local files,
// so seeking, tags and metadata reuse SD/SMB playback. Gapless is disabled for
// them. Every network call takes a context that ends when the track stops, so
// choosing something else cancels what was in flight, and the worker goroutines
// are counted (runSrvWorker) so nothing outlives the app.

// srvTmpDir holds the tracks fetched from media servers (current + next). A
// variable so tests can point it at a temporary folder.
var srvTmpDir = "/tmp/misterhifi-srv"

// mediaItem is something a server lists: an artist, an album or a playlist.
type mediaItem struct {
	ID     string
	Title  string
	Artist string // the artist of an album, when the server says
}

// mediaServer is a kind of server. It holds no state: its configuration is
// read from baseDir/<ID>.json when a session is made.
type mediaServer interface {
	// ID is a short lower-case word: it names the config file (<id>.json), the
	// command-line/NFC scheme (<id>://...), Track.SrvID and the "srv:<id>"
	// source of the web remote.
	ID() string
	// Label is what the sources menu shows, upper case.
	Label() string
	// Configured says whether there is a config for it, without contacting the
	// server. A broken config still counts: the menu then shows the error. A
	// server that is not configured does not show up anywhere.
	Configured() bool
	// Connect reads and checks the config and returns a session. The session
	// is immutable: it keeps the config it was made with, so tracks that are
	// queued keep playing from the same place if the config changes meanwhile.
	// It returns errNotSignedIn when the server needs a sign-in first.
	Connect(ctx context.Context) (mediaSession, error)
}

// mediaSession is a connection to one server. Tracks it returns must set
// SrvID, SrvKey and SrvPart (what Download needs) and may set SrvExt (file
// extension with the dot), SrvThumb, Number, Title, Artist, Album, Duration
// and the format fields; the generic code attaches the session itself.
type mediaSession interface {
	// ValidID says whether a string can be an item id of this server. Ids go
	// into URL paths, so a session accepts only what its ids look like.
	ValidID(id string) bool
	Artists(ctx context.Context) ([]mediaItem, error)
	ArtistAlbums(ctx context.Context, artistID string) ([]mediaItem, error)
	// Albums lists all albums, or the newest ones (at most 100) when recent.
	Albums(ctx context.Context, recent bool) ([]mediaItem, error)
	Playlists(ctx context.Context) ([]mediaItem, error)
	AlbumTracks(ctx context.Context, albumID string) ([]Track, error)
	PlaylistTracks(ctx context.Context, playlistID string) ([]Track, error)
	// FindAlbum returns the album that best matches a text like "artist album".
	FindAlbum(ctx context.Context, query string) (mediaItem, error)
	// Download opens the track's file; length is -1 when the server does not say.
	Download(ctx context.Context, t Track) (body io.ReadCloser, length int64, err error)
	// Art returns the track's cover, or nil.
	Art(ctx context.Context, t Track) image.Image
	// Report tells the server what is playing: state is "playing", "paused" or
	// "stopped". Best effort: the caller only logs an error.
	Report(ctx context.Context, t Track, state string, pos time.Duration) error
	// Played marks the track as listened to (the server's scrobble).
	Played(ctx context.Context, t Track) error
	// Scrobbling says who counts a listen: "server" (the server keeps the
	// history and may hand it on to a scrobbler), "lastfm" (Hi-Fi's own
	// Last.fm scrobbling) or "none".
	Scrobbling() string
	// MaxTrackBytes is the biggest track the cache takes.
	MaxTrackBytes() int64
	// CacheKey identifies server, account and media, so that switching
	// libraries or servers can never play an unrelated cached file.
	CacheKey(t Track) string
}

// mediaServerLogin is implemented by servers that can sign in from the screen
// (Jellyfin: Quick Connect; Emby signs in from the web remote). Connect returns errNotSignedIn when it is needed.
type mediaServerLogin interface {
	Login(app *App) error
}

var errNotSignedIn = errors.New("not signed in")

// mediaServers lists the kinds of server Hi-Fi knows.
var mediaServers = []mediaServer{plexServer{}, jellyfinServer{}, jellyfinServer{emby: true}}

func mediaServerByID(id string) mediaServer {
	for _, s := range mediaServers {
		if s.ID() == id {
			return s
		}
	}
	return nil
}

// configuredMediaServers are the servers that have a config file: only those
// show up in the sources menu and the web remote.
func configuredMediaServers() []mediaServer {
	var out []mediaServer
	for _, s := range mediaServers {
		if s.Configured() {
			out = append(out, s)
		}
	}
	return out
}

// isServerPath reports whether a track path is "<server id>:<key>" (as opposed
// to a file or URL), which means the track still has to be fetched.
func isServerPath(p string) bool {
	if isHTTPURL(p) {
		return false
	}
	id, _, ok := strings.Cut(p, ":")
	return ok && mediaServerByID(id) != nil
}

// trackSession is the session a server track plays through: the one it was
// listed with, or a new one when it came from somewhere else (a saved queue).
func trackSession(ctx context.Context, t Track) (mediaSession, error) {
	if t.Srv != nil {
		return t.Srv, nil
	}
	s := mediaServerByID(t.SrvID)
	if s == nil {
		return nil, errors.New("unknown media server: " + t.SrvID)
	}
	return s.Connect(ctx)
}

// attach marks listed tracks as coming through this session.
func attachSession(sess mediaSession, id string, tracks []Track) []Track {
	for i := range tracks {
		tracks[i].SrvID = id
		tracks[i].Srv = sess
		if tracks[i].Path == "" {
			tracks[i].Path = id + ":" + tracks[i].SrvKey
		}
		tracks[i].DirFD = -1
	}
	return tracks
}

// ---- track cache and workers ----

// All cache mutation is serialized. Context cancellation releases queued jobs;
// unique .part files are removed on every failure.
var srvCacheGate = make(chan struct{}, 1)
var srvWorkers sync.WaitGroup

func runSrvWorker(fn func()) { srvWorkers.Add(1); go func() { defer srvWorkers.Done(); fn() }() }
func closeSrvWorkers()       { srvWorkers.Wait(); _ = os.RemoveAll(srvTmpDir) }
func cacheLock(ctx context.Context) error {
	select {
	case srvCacheGate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func cacheUnlock() { <-srvCacheGate }

// srvExtFromContainer turns a server's container name into the file extension Hi-Fi picks its
// decoder by. The servers call an .m4a file "mp4" (Plex) or "mov,mp4,m4a,..." (ffprobe's list,
// which Jellyfin and Emby may pass on); all of them are the m4a decoder's (AAC and ALAC in MP4).
func srvExtFromContainer(container string) string {
	for _, c := range strings.Split(strings.ToLower(strings.TrimSpace(container)), ",") {
		switch strings.TrimSpace(c) {
		case "mp4", "m4a", "m4b", "mov":
			return ".m4a"
		}
	}
	first := strings.TrimSpace(strings.SplitN(strings.ToLower(container), ",", 2)[0])
	if first == "" {
		return ""
	}
	return "." + first
}

func srvCachePath(sess mediaSession, t Track) string {
	ext := strings.ToLower(t.SrvExt)
	if !supported[ext] {
		ext = ".bin"
	}
	sum := sha256.Sum256([]byte(sess.CacheKey(t)))
	return filepath.Join(srvTmpDir, fmt.Sprintf("%x%s", sum[:16], ext))
}

// srvEnsureLocal returns a local file for a server track, fetching it when it
// is not in the temporary cache yet.
func srvEnsureLocal(ctx context.Context, sess mediaSession, t Track) (string, error) {
	if t.SrvPart == "" {
		return "", errors.New(t.SrvID + ": track has no media")
	}
	if err := cacheLock(ctx); err != nil {
		return "", err
	}
	defer cacheUnlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(srvTmpDir, 0700); err != nil {
		return "", err
	}
	dst := srvCachePath(sess, t)
	if st, err := os.Stat(dst); err == nil && st.Size() > 0 {
		return dst, nil
	}
	body, length, err := sess.Download(ctx, t)
	if err != nil {
		return "", err
	}
	defer body.Close()
	limit := sess.MaxTrackBytes()
	if limit <= 0 {
		limit = 128 << 20
	}
	if length > limit {
		return "", fmt.Errorf("%s: track exceeds %d MB cache limit", t.SrvID, limit>>20)
	}
	f, err := os.CreateTemp(srvTmpDir, "download-*.part")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	n, copyErr := io.Copy(f, io.LimitReader(body, limit+1))
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
	if n == 0 || n > limit || (length >= 0 && n != length) {
		return "", errors.New(t.SrvID + ": empty, incomplete, or oversized download")
	}
	if err := os.Rename(f.Name(), dst); err != nil {
		return "", err
	}
	return dst, nil
}

// ---- playback reports ----

// Listen time follows wall time only while this generation is actually playing.
// Seeking changes the timeline position, never the time used to qualify a play.
func srvPlayedEnough(t Track, heard time.Duration) bool {
	threshold := 4 * time.Minute
	if t.Duration > 0 && time.Duration(t.Duration*float64(time.Second)/2) < threshold {
		threshold = time.Duration(t.Duration * float64(time.Second) / 2)
	}
	return heard >= threshold
}

// srvSession reports one track's playback until it stops: the timeline to the
// server, and the play itself (server or Last.fm) once enough was heard. The
// loop samples the player every second and never waits for the network; a
// second goroutine sends the newest snapshot.
func (p *Player) srvSession(t Track, stop <-chan struct{}, generation uint64) {
	ctx0, cancel0 := playbackContext(stop)
	sess, err := trackSession(ctx0, t)
	cancel0()
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
	lbCfg := p.cfg.ListenBrainz
	p.mu.Unlock()
	mode := sess.Scrobbling()
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
					reportErr = sess.Report(ctx, t, u.state, u.pos)
					lastState = u.state
					lastReport = time.Now()
				}
				if !scrobbled && srvPlayedEnough(t, u.heard) {
					switch mode {
					case "server", "":
						reportErr = sess.Played(ctx, t)
					case "lastfm":
						reportErr = scrobbleLastFM(lastfmCfg, t, startedAt)
					case "listenbrainz":
						reportErr = scrobbleListenBrainz(lbCfg, t, startedAt)
					case "both":
						reportErr = errors.Join(scrobbleLastFM(lastfmCfg, t, startedAt), scrobbleListenBrainz(lbCfg, t, startedAt))
					default:
						reportErr = nil
					}
					scrobbled = reportErr == nil
				}
				if reportErr != nil && ctx.Err() == nil && !warned {
					log.Printf("%s playback reporting: %v", t.SrvID, reportErr)
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
		if err := sess.Report(endCtx, t, "stopped", pos); err != nil {
			log.Printf("%s stopped report: %v", t.SrvID, err)
		}
	}()
	for {
		p.opMu.Lock()
		p.mu.Lock()
		// a gapless transition moves the index on while stop and generation stay
		valid := p.stop == stop && p.generation == generation && !p.stopped &&
			p.q.Index >= 0 && p.q.Index < len(p.q.Tracks) && p.q.Tracks[p.q.Index].SrvKey == t.SrvKey
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

func scrobbleLastFM(cfg LastFMConfig, t Track, startedAt time.Time) error {
	if !cfg.ready() {
		return nil
	}
	params := map[string]string{"api_key": cfg.APIKey, "sk": cfg.SessionKey, "track": t.Title, "artist": nonEmptyOr(t.Artist, "Unknown Artist"), "album": t.Album, "timestamp": strconv.FormatInt(startedAt.Unix(), 10)}
	_, err := lastfmCall("track.scrobble", params, cfg.APISecret, http.MethodPost)
	return err
}

// ---- next track, loading, prefetch ----

// Playback and prefetch share the shuffle choice; the plan is invalidated by
// changing repeat/shuffle settings or moving to another index.
type srvNextPlan struct {
	from, next           int
	shuffle, repeat, one bool
	valid                bool
}

// srvNextLocked is the index that plays after the current one (manual: the
// player pressed next). For server tracks the shuffle choice is kept in a plan
// so that the prefetched track is the one that really plays.
func (p *Player) srvNextLocked(manual bool) int {
	q := &p.q
	n := len(q.Tracks)
	if n == 0 {
		return -1
	}
	if !manual && q.Repeat && q.RepeatOne {
		return q.Index
	}
	if q.Shuffle && n > 1 {
		if q.Tracks[q.Index].SrvKey == "" {
			next := int(time.Now().UnixNano() % int64(n-1))
			if next >= q.Index {
				next++
			}
			return next
		}
		plan := &p.srvPlan
		if !plan.valid || plan.from != q.Index || plan.shuffle != q.Shuffle || plan.repeat != q.Repeat || plan.one != q.RepeatOne {
			next := int(time.Now().UnixNano() % int64(n-1))
			if next >= q.Index {
				next++
			}
			*plan = srvNextPlan{from: q.Index, next: next, shuffle: q.Shuffle, repeat: q.Repeat, one: q.RepeatOne, valid: true}
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

// playbackContext ends when the track stops (or is replaced).
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

// prepareSrvTrack fetches a track in a worker (the UI shows "downloading")
// and then starts it, unless the player moved on meanwhile.
func (p *Player) prepareSrvTrack(ctx context.Context, t Track, idx int, stop chan struct{}, generation uint64, cfg Config) {
	sess, err := trackSession(ctx, t)
	local := ""
	if err == nil {
		local, err = srvEnsureLocal(ctx, sess, t)
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
		log.Printf("%s playback: %v", t.SrvID, err)
		return
	}
	t.Path = local
	t.Srv = sess
	p.q.Tracks[idx] = t
	paused := p.paused
	p.mu.Unlock()
	if err := p.startPreparedTrackUnlocked(t, idx, stop, generation, cfg); err != nil {
		log.Printf("%s audio: %v", t.SrvID, err)
		return
	}
	nativeAudioPause(paused)
}

// srvArtwork fetches the cover from the server, for the current track when
// the file itself has none. It is small and runs before the prefetch of the
// next track (which can be tens of MB).
func (p *Player) srvArtwork(t Track, idx int, stop <-chan struct{}, generation uint64) {
	ctx, cancel := playbackContext(stop)
	defer cancel()
	sess, err := trackSession(ctx, t)
	if err != nil {
		return
	}
	art := sess.Art(ctx, t)
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

// srvPrefetch fetches the next queue entry while the current one plays and
// drops older cached files (keeps current + next).
func (p *Player) srvPrefetch(idx int, cur Track, stop <-chan struct{}, generation uint64) {
	ctx, cancel := playbackContext(stop)
	defer cancel()
	sess, err := trackSession(ctx, cur)
	if err != nil {
		return
	}
	p.mu.Lock()
	nextIndex := p.srvNextLocked(false)
	var next Track
	if nextIndex >= 0 {
		next = p.q.Tracks[nextIndex]
	}
	p.mu.Unlock()
	keep := map[string]bool{srvCachePath(sess, cur): true}
	var nextSess mediaSession
	if next.SrvKey != "" {
		if nextSess, err = trackSession(ctx, next); err == nil {
			keep[srvCachePath(nextSess, next)] = true
		}
	}
	if !p.srvTrimCache(ctx, keep, stop, generation) {
		return
	}
	if next.SrvKey != "" && nextSess != nil {
		if _, err := srvEnsureLocal(ctx, nextSess, next); err != nil {
			if ctx.Err() == nil {
				log.Printf("%s prefetch: %v", next.SrvID, err)
			}
			return
		}
		// the next track is on the card: queue it for a gapless transition
		p.opMu.Lock()
		p.mu.Lock()
		current := p.stop == stop && p.generation == generation && !p.stopped && p.q.Index == idx
		p.mu.Unlock()
		if current {
			p.prepareGaplessNextFile()
		}
		p.opMu.Unlock()
	}
}

func (p *Player) srvTrimCache(ctx context.Context, keep map[string]bool, stop <-chan struct{}, generation uint64) bool {
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
	entries, _ := os.ReadDir(srvTmpDir)
	for _, entry := range entries {
		path := filepath.Join(srvTmpDir, entry.Name())
		if !keep[path] {
			_ = os.Remove(path)
		}
	}
	return true
}

// ---- device id ----

var deviceIDMu sync.Mutex
var deviceIDCache = map[string]string{}

// deviceID is a stable id for this MiSTer towards a server, kept in
// baseDir/<file> (a per-boot fallback when the card is read-only).
func deviceID(file, prefix string) string {
	deviceIDMu.Lock()
	defer deviceIDMu.Unlock()
	path := filepath.Join(baseDir, file)
	if id := deviceIDCache[path]; id != "" {
		return id
	}
	if b, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(b)) != "" {
		deviceIDCache[path] = strings.TrimSpace(string(b))
		return deviceIDCache[path]
	}
	b, _ := os.ReadFile("/proc/sys/kernel/random/uuid")
	id := strings.TrimSpace(string(b))
	if id == "" {
		host, _ := os.Hostname()
		id = host + "-" + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	id = prefix + "-" + id
	_ = os.MkdirAll(baseDir, 0755)
	_ = os.WriteFile(path, []byte(id), 0600)
	deviceIDCache[path] = id
	return id
}
