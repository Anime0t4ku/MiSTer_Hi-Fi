package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultWebRemotePort = 8182

type webRemote struct {
	app     *App
	srv     *http.Server
	ln      net.Listener
	done    chan struct{}
	once    sync.Once
	opMu    sync.Mutex
	wsMu    sync.Mutex
	clients map[net.Conn]struct{}
	artMu   sync.Mutex
	artKey  string
	artData []byte
}

type webState struct {
	Connected   bool    `json:"connected"`
	Version     string  `json:"version"`
	State       string  `json:"state"`
	Title       string  `json:"title"`
	Artist      string  `json:"artist"`
	Album       string  `json:"album"`
	Format      string  `json:"format"`
	Position    float64 `json:"position"`
	Duration    float64 `json:"duration"`
	QueueIndex  int     `json:"queue_index"`
	QueueLength int     `json:"queue_length"`
	Shuffle     bool    `json:"shuffle"`
	Loop        bool    `json:"loop"`
	HasArt      bool    `json:"has_art"`
	ArtKey      string  `json:"art_key"`
}

type webEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

type webSource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func preferredLANIPv4() string {
	// Dialing UDP does not send application data, but lets the kernel tell us
	// which local address it would use for normal network traffic.
	if c, err := net.Dial("udp", "1.1.1.1:53"); err == nil {
		defer c.Close()
		if a, ok := c.LocalAddr().(*net.UDPAddr); ok && a.IP != nil && !a.IP.IsLoopback() {
			if ip := a.IP.To4(); ip != nil {
				return ip.String()
			}
		}
	}
	// Fallback for networks without a default route: only accept an active,
	// non-loopback IPv4 interface. No usable address means no Web Remote.
	ifs, _ := net.Interfaces()
	for _, iface := range ifs {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch a := addr.(type) {
			case *net.IPNet:
				ip = a.IP
			case *net.IPAddr:
				ip = a.IP
			}
			if ip4 := ip.To4(); ip4 != nil && !ip4.IsLoopback() && !ip4.IsUnspecified() {
				return ip4.String()
			}
		}
	}
	return ""
}

func resizeForWeb(src image.Image, maxDim int) image.Image {
	if src == nil {
		return nil
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || (w <= maxDim && h <= maxDim) {
		return src
	}
	nw, nh := w, h
	if w >= h {
		nw = maxDim
		nh = max(1, h*maxDim/w)
	} else {
		nh = maxDim
		nw = max(1, w*maxDim/h)
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := b.Min.Y + y*h/nh
		for x := 0; x < nw; x++ {
			sx := b.Min.X + x*w/nw
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func (w *webRemote) webArt(key string, art image.Image) []byte {
	w.artMu.Lock()
	if key != "" && key == w.artKey && len(w.artData) > 0 {
		data := w.artData
		w.artMu.Unlock()
		return data
	}
	w.artMu.Unlock()
	if art == nil {
		return nil
	}
	var buf bytes.Buffer
	// Web artwork is intentionally capped and JPEG encoded. Sending a huge
	// embedded PNG made first-load artwork noticeably slow on MiSTer hardware.
	if err := jpeg.Encode(&buf, resizeForWeb(art, 640), &jpeg.Options{Quality: 86}); err != nil {
		return nil
	}
	data := buf.Bytes()
	w.artMu.Lock()
	w.artKey, w.artData = key, data
	w.artMu.Unlock()
	return data
}

func startWebRemote(app *App) *webRemote {
	if app == nil || app.cfg == nil || !app.cfg.WebRemoteEnabled {
		return nil
	}
	ip := preferredLANIPv4()
	if ip == "" {
		fmt.Fprintln(os.Stderr, "MiSTer Hi-Fi Web Remote: no active network connection; not starting")
		return nil
	}
	port := app.cfg.WebRemotePort
	if port <= 0 || port > 65535 {
		port = defaultWebRemotePort
	}
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "MiSTer Hi-Fi Web Remote: port %d unavailable: %v\n", port, err)
		return nil
	}
	w := &webRemote{app: app, ln: ln, done: make(chan struct{}), clients: make(map[net.Conn]struct{})}
	app.webRemoteAddr = fmt.Sprintf("%s:%d", ip, port)
	mux := http.NewServeMux()
	mux.HandleFunc("/", w.handleIndex)
	mux.HandleFunc("/api/state", w.handleState)
	mux.HandleFunc("/api/sources", w.handleSources)
	mux.HandleFunc("/api/browse", w.handleBrowse)
	mux.HandleFunc("/api/play", w.handlePlay)
	mux.HandleFunc("/api/control", w.handleControl)
	mux.HandleFunc("/api/seek", w.handleSeek)
	mux.HandleFunc("/api/art", w.handleArt)
	mux.HandleFunc("/ws", w.handleWS)
	w.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := w.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "MiSTer Hi-Fi Web Remote:", err)
		}
	}()
	go w.broadcastLoop()
	fmt.Fprintf(os.Stderr, "MiSTer Hi-Fi Web Remote: http://%s\n", app.webRemoteAddr)
	return w
}

func (w *webRemote) Close() {
	if w == nil {
		return
	}
	w.once.Do(func() {
		close(w.done)
		w.wsMu.Lock()
		for c := range w.clients {
			_ = writeWSFrame(c, 0x8, []byte{})
			_ = c.Close()
		}
		w.clients = make(map[net.Conn]struct{})
		w.wsMu.Unlock()
		if w.srv != nil {
			_ = w.srv.Close()
		}
		if w.ln != nil {
			_ = w.ln.Close()
		}
		if w.app != nil {
			w.app.webRemoteAddr = ""
		}
	})
}

func (w *webRemote) state() webState {
	s := webState{Connected: true, Version: version, State: "stopped"}
	p := w.app.player
	if p == nil {
		return s
	}
	t, _, idx, qlen, paused, rep, shuf, stopped := playerSnapshot(p)
	s.QueueIndex, s.QueueLength, s.Shuffle, s.Loop = idx, qlen, shuf, rep
	if !stopped {
		if paused {
			s.State = "paused"
		} else {
			s.State = "playing"
		}
	}
	s.Title = t.Title
	if s.Title == "" && t.Path != "" {
		s.Title = strings.TrimSuffix(filepath.Base(t.Path), filepath.Ext(t.Path))
	}
	s.Artist, s.Album, s.Format = t.Artist, t.Album, t.MediaFormat
	s.Position = p.elapsedNow().Seconds()
	s.Duration = p.durationNow().Seconds()
	if s.Position < 0 {
		s.Position = 0
	}
	if s.Duration > 0 && s.Position > s.Duration {
		s.Position = s.Duration
	}
	art := t.Art
	if w.app.cfg != nil && w.app.cfg.PrioritizeExternalArt && !strings.HasPrefix(t.Path, "cdda:") && !isHTTPURL(t.Path) {
		if x := p.externalArtworkForTrackCached(t); x != nil {
			art = x
		}
	}
	s.HasArt = art != nil
	if s.HasArt {
		s.ArtKey = fmt.Sprintf("%d-%s-%s", idx, t.Path, t.Title)
	}
	return s
}

func writeJSON(rw http.ResponseWriter, v any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(rw).Encode(v)
}

func (w *webRemote) handleIndex(rw http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(rw, r)
		return
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(rw, webRemoteHTML)
}

func (w *webRemote) handleState(rw http.ResponseWriter, r *http.Request) { writeJSON(rw, w.state()) }

func (w *webRemote) handleSources(rw http.ResponseWriter, r *http.Request) {
	out := []webSource{{ID: "sd", Name: "SD Card"}}
	for i := range detectUSB() {
		out = append(out, webSource{ID: fmt.Sprintf("usb:%d", i), Name: fmt.Sprintf("USB %d", i+1)})
	}
	for i, s := range loadSMB().Shares {
		out = append(out, webSource{ID: fmt.Sprintf("smb:%d", i), Name: smbShareName(s)})
	}
	if cfg, err := loadRadio(); err == nil && len(cfg.Stations) > 0 {
		out = append(out, webSource{ID: "radio", Name: "Online Radio"})
	}
	if len(detectOptical()) > 0 {
		out = append(out, webSource{ID: "disc", Name: "Physical Disc"})
	}
	writeJSON(rw, out)
}

func (w *webRemote) sourceRoot(id string) (root, kind string, err error) {
	switch {
	case id == "sd":
		return "/media/fat", "sd", nil
	case strings.HasPrefix(id, "usb:"):
		i, e := strconv.Atoi(strings.TrimPrefix(id, "usb:"))
		usbs := detectUSB()
		if e != nil || i < 0 || i >= len(usbs) {
			return "", "", fmt.Errorf("USB source unavailable")
		}
		return usbs[i], "usb", nil
	case strings.HasPrefix(id, "smb:"):
		i, e := strconv.Atoi(strings.TrimPrefix(id, "smb:"))
		shares := loadSMB().Shares
		if e != nil || i < 0 || i >= len(shares) {
			return "", "", fmt.Errorf("SMB source unavailable")
		}
		root, e := mountShare(shares[i])
		return root, "smb", e
	}
	return "", "", fmt.Errorf("invalid source")
}

func safeWithin(root, path string) (string, error) {
	root = filepath.Clean(root)
	if path == "" {
		return root, nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path outside source")
	}
	return path, nil
}

func (w *webRemote) handleBrowse(rw http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("source")
	if id == "radio" {
		cfg, err := loadRadio()
		if err != nil {
			http.Error(rw, err.Error(), 500)
			return
		}
		out := make([]webEntry, 0, len(cfg.Stations))
		for i, s := range cfg.Stations {
			out = append(out, webEntry{Name: s.Name, Path: fmt.Sprintf("radio:%d", i)})
		}
		writeJSON(rw, map[string]any{"path": "", "entries": out})
		return
	}
	if id == "disc" {
		devs := detectOptical()
		if len(devs) == 0 {
			http.Error(rw, "no optical drive", 404)
			return
		}
		tracks, err := readCDTOC(devs[0])
		if err != nil {
			http.Error(rw, err.Error(), 500)
			return
		}
		out := make([]webEntry, 0, len(tracks))
		for i, t := range tracks {
			out = append(out, webEntry{Name: t.Title, Path: fmt.Sprintf("disc:%d", i)})
		}
		writeJSON(rw, map[string]any{"path": "", "entries": out})
		return
	}
	root, _, err := w.sourceRoot(id)
	if err != nil {
		http.Error(rw, err.Error(), 400)
		return
	}
	dir, err := safeWithin(root, r.URL.Query().Get("path"))
	if err != nil {
		http.Error(rw, err.Error(), 400)
		return
	}
	es, err := os.ReadDir(dir)
	if err != nil {
		http.Error(rw, err.Error(), 500)
		return
	}
	out := make([]webEntry, 0, len(es))
	for _, e := range es {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() || supported[strings.ToLower(filepath.Ext(name))] {
			out = append(out, webEntry{Name: name, Path: filepath.Join(dir, name), IsDir: e.IsDir()})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	writeJSON(rw, map[string]any{"path": dir, "root": root, "entries": out})
}

type webPlayRequest struct {
	Source string `json:"source"`
	Path   string `json:"path"`
}

func (w *webRemote) handlePlay(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", 405)
		return
	}
	var req webPlayRequest
	if json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req) != nil {
		http.Error(rw, "invalid request", 400)
		return
	}
	w.opMu.Lock()
	defer w.opMu.Unlock()
	var q Queue
	var origin *browseOrigin
	if req.Source == "radio" {
		i, e := strconv.Atoi(strings.TrimPrefix(req.Path, "radio:"))
		cfg, ce := loadRadio()
		if e != nil || ce != nil || i < 0 || i >= len(cfg.Stations) {
			http.Error(rw, "station unavailable", 400)
			return
		}
		s := cfg.Stations[i]
		q = Queue{Tracks: []Track{{Path: s.URL, Title: s.Name, Album: "Online Radio", MediaFormat: "STREAM", DirFD: -1}}}
		origin = &browseOrigin{Kind: "radio", Selected: s.Name}
	} else if req.Source == "disc" {
		i, e := strconv.Atoi(strings.TrimPrefix(req.Path, "disc:"))
		devs := detectOptical()
		if e != nil || len(devs) == 0 {
			http.Error(rw, "disc unavailable", 400)
			return
		}
		tracks, e := readCDTOC(devs[0])
		if e != nil || i < 0 || i >= len(tracks) {
			http.Error(rw, "track unavailable", 400)
			return
		}
		q = Queue{Tracks: tracks, Index: i}
		origin = &browseOrigin{Kind: "disc", Selected: tracks[i].Title}
	} else {
		root, kind, e := w.sourceRoot(req.Source)
		if e != nil {
			http.Error(rw, e.Error(), 400)
			return
		}
		path, e := safeWithin(root, req.Path)
		if e != nil {
			http.Error(rw, e.Error(), 400)
			return
		}
		st, e := os.Stat(path)
		if e != nil || st.IsDir() {
			http.Error(rw, "invalid track", 400)
			return
		}
		if !supported[strings.ToLower(filepath.Ext(path))] {
			http.Error(rw, "unsupported track", 400)
			return
		}
		q = buildQueue(path, false)
		origin = &browseOrigin{Root: root, Dir: filepath.Dir(path), Selected: filepath.Base(path), Kind: kind}
	}
	if err := w.app.startQueue(q, origin); err != nil {
		http.Error(rw, err.Error(), 500)
		return
	}
	select {
	case w.app.webNowPlaying <- struct{}{}:
	default:
	}
	writeJSON(rw, w.state())
}

type webControlRequest struct {
	Action string `json:"action"`
}

func (w *webRemote) handleControl(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", 405)
		return
	}
	var req webControlRequest
	_ = json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req)
	w.opMu.Lock()
	defer w.opMu.Unlock()
	p := w.app.player
	switch req.Action {
	case "playpause":
		if p != nil {
			p.togglePause()
		}
	case "play":
		if p != nil {
			p.playOrResume()
		}
	case "pause":
		if p != nil {
			p.pause()
		}
	case "previous":
		if p != nil {
			p.prev()
		}
	case "next":
		if p != nil {
			p.next()
		}
	case "stop":
		w.app.stopAndUnload()
		if w.app.webStop != nil {
			select {
			case w.app.webStop <- struct{}{}:
			default:
			}
		}
	case "shuffle":
		w.app.handlePlaybackShortcut(actShuffle)
	case "loop":
		w.app.handlePlaybackShortcut(actLoop)
	default:
		http.Error(rw, "unknown action", 400)
		return
	}
	writeJSON(rw, w.state())
}

type webSeekRequest struct {
	Position float64 `json:"position"`
}

func (p *Player) seekTo(seconds float64) {
	if seconds < 0 {
		seconds = 0
	}
	current := p.elapsedNow().Seconds()
	p.seekBy(seconds - current)
}
func (w *webRemote) handleSeek(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", 405)
		return
	}
	var req webSeekRequest
	if json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req) != nil {
		http.Error(rw, "invalid request", 400)
		return
	}
	w.opMu.Lock()
	defer w.opMu.Unlock()
	if w.app.player != nil {
		w.app.player.seekTo(req.Position)
	}
	writeJSON(rw, w.state())
}

func (w *webRemote) handleArt(rw http.ResponseWriter, r *http.Request) {
	p := w.app.player
	if p == nil {
		http.NotFound(rw, r)
		return
	}
	t, _, _, _, _, _, _, _ := playerSnapshot(p)
	art := t.Art
	if w.app.cfg != nil && w.app.cfg.PrioritizeExternalArt && !strings.HasPrefix(t.Path, "cdda:") && !isHTTPURL(t.Path) {
		if x := p.externalArtworkForTrackCached(t); x != nil {
			art = x
		}
	}
	if art == nil {
		http.NotFound(rw, r)
		return
	}
	key := r.URL.Query().Get("k")
	if key == "" {
		key = fmt.Sprintf("%s-%s", t.Path, t.Title)
	}
	data := w.webArt(key, art)
	if len(data) == 0 {
		http.NotFound(rw, r)
		return
	}
	etag := strconv.Quote(key)
	rw.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		rw.WriteHeader(http.StatusNotModified)
		return
	}
	rw.Header().Set("Content-Type", "image/jpeg")
	rw.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	_, _ = rw.Write(data)
}

func (w *webRemote) broadcastLoop() {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-w.done:
			return
		case <-tick.C:
			b, _ := json.Marshal(w.state())
			w.wsMu.Lock()
			for c := range w.clients {
				if writeWSFrame(c, 0x1, b) != nil {
					_ = c.Close()
					delete(w.clients, c)
				}
			}
			w.wsMu.Unlock()
		}
	}
}

func wsAccept(key string) string {
	h := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(h[:])
}
func (w *webRemote) handleWS(rw http.ResponseWriter, r *http.Request) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(rw, "websocket required", 400)
		return
	}
	hj, ok := rw.(http.Hijacker)
	if !ok {
		http.Error(rw, "websocket unavailable", 500)
		return
	}
	c, buf, err := hj.Hijack()
	if err != nil {
		return
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		_ = c.Close()
		return
	}
	_, _ = fmt.Fprintf(buf, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", wsAccept(key))
	_ = buf.Flush()
	b, _ := json.Marshal(w.state())
	if writeWSFrame(c, 0x1, b) != nil {
		_ = c.Close()
		return
	}
	w.wsMu.Lock()
	w.clients[c] = struct{}{}
	w.wsMu.Unlock()
	go w.readWS(c)
}
func (w *webRemote) readWS(c net.Conn) {
	defer func() { w.wsMu.Lock(); delete(w.clients, c); w.wsMu.Unlock(); _ = c.Close() }()
	br := bufio.NewReader(c)
	for {
		if _, err := br.ReadByte(); err != nil {
			return
		}
		b2, err := br.ReadByte()
		if err != nil {
			return
		}
		n := int(b2 & 0x7f)
		if n == 126 {
			a, _ := br.ReadByte()
			b, _ := br.ReadByte()
			n = int(a)<<8 | int(b)
		} else if n == 127 {
			return
		}
		masked := b2&0x80 != 0
		var mask [4]byte
		if masked {
			if _, err = io.ReadFull(br, mask[:]); err != nil {
				return
			}
		}
		payload := make([]byte, n)
		if _, err = io.ReadFull(br, payload); err != nil {
			return
		}
	}
}
func writeWSFrame(c net.Conn, opcode byte, payload []byte) error {
	if c == nil {
		return io.ErrClosedPipe
	}
	h := []byte{0x80 | opcode}
	n := len(payload)
	if n < 126 {
		h = append(h, byte(n))
	} else if n <= 65535 {
		h = append(h, 126, byte(n>>8), byte(n))
	} else {
		h = append(h, 127, 0, 0, 0, 0, byte(uint64(n)>>24), byte(uint64(n)>>16), byte(uint64(n)>>8), byte(n))
	}
	if _, e := c.Write(h); e != nil {
		return e
	}
	_, e := c.Write(payload)
	return e
}

const webRemoteHTML = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover"><title>MiSTer Hi-Fi</title><style>
:root{color-scheme:dark;--bg:#151515;--panel:#222;--panel2:#2c2c2c;--line:#444;--text:#f5f5f5;--muted:#aaa;--white:#fff}*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font-family:Arial,Helvetica,sans-serif}.top{height:62px;border-bottom:1px solid var(--line);display:flex;align-items:center;padding:0 22px;font-weight:700;letter-spacing:.08em;background:#1b1b1b}.dot{width:9px;height:9px;border-radius:50%;background:#777;margin-left:auto}.dot.on{background:#eee}.layout{display:grid;grid-template-columns:minmax(280px,1fr) minmax(340px,480px);min-height:calc(100vh - 62px)}.browser{padding:22px;border-right:1px solid var(--line)}.player{padding:28px;background:#191919;display:flex;flex-direction:column;align-items:center}.sources{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:16px}.chip,button{background:#303030;color:#fff;border:1px solid #4a4a4a;border-radius:8px;padding:10px 13px;cursor:pointer}.chip.active{background:#efefef;color:#111}.path{color:var(--muted);font-size:13px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;margin:10px 0}.list{border:1px solid var(--line);border-radius:10px;overflow:hidden}.entry{width:100%;display:flex;text-align:left;border:0;border-bottom:1px solid #383838;border-radius:0;background:#242424;padding:13px 15px}.entry:last-child{border-bottom:0}.entry:hover{background:#303030}.entry .kind{color:#999;margin-left:auto}.art{width:min(78vw,360px);aspect-ratio:1;border-radius:12px;background:#2b2b2b;object-fit:cover;box-shadow:0 12px 35px #0008}.meta{width:100%;margin-top:22px;text-align:center}.title{font-size:24px;font-weight:700}.artist,.album{color:#bbb;margin-top:6px}.progress{width:100%;margin-top:26px}.times{display:flex;justify-content:space-between;color:#aaa;font-size:12px;margin-top:6px}input[type=range]{width:100%;accent-color:#fff}.controls{display:flex;align-items:center;gap:12px;margin-top:22px}.controls button{width:48px;height:48px;border-radius:50%;font-size:17px;display:flex;align-items:center;justify-content:center}.controls button svg{width:22px;height:22px;display:block;fill:currentColor}.controls .main{width:62px;height:62px;background:#eee;color:#111;font-size:22px}.toggles{display:flex;gap:10px;margin-top:18px}.toggles button.active{background:#eee;color:#111}.status{margin-top:auto;padding-top:22px;color:#888;font-size:12px}.empty{padding:22px;color:#999}.mobile-tabs{display:none}@media(max-width:760px){.layout{display:block}.browser,.player{border:0;min-height:calc(100vh - 116px);padding:18px}.browser.hidden,.player.hidden{display:none}.mobile-tabs{position:fixed;bottom:0;left:0;right:0;height:54px;background:#1e1e1e;border-top:1px solid #444;display:flex}.mobile-tabs button{flex:1;border:0;border-radius:0}.art{width:min(72vw,330px)}body{padding-bottom:54px}}
</style></head><body><div class="top">MISTER HI-FI<span id="dot" class="dot"></span></div><div class="layout"><section id="browser" class="browser"><div class="sources" id="sources"></div><div class="path" id="path">Select a source</div><div class="list" id="list"><div class="empty">Choose a source to browse music.</div></div></section><section id="player" class="player"><img id="art" class="art" alt="Album art"><div class="meta"><div id="title" class="title">Nothing playing</div><div id="artist" class="artist"></div><div id="album" class="album"></div></div><div class="progress"><input id="seek" type="range" min="0" max="1000" value="0"><div class="times"><span id="pos">0:00</span><span id="dur">0:00</span></div></div><div class="controls"><button onclick="control('previous')" aria-label="Previous"><svg viewBox="0 0 24 24" aria-hidden="true"><rect x="3" y="4" width="2.5" height="16"/><path d="M19.5 4v16L7 12z"/></svg></button><button id="play" class="main" onclick="control('playpause')">▶</button><button onclick="control('next')" aria-label="Next"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4.5 4v16L17 12z"/><rect x="18.5" y="4" width="2.5" height="16"/></svg></button><button onclick="control('stop')">■</button></div><div class="toggles"><button id="shuffle" onclick="control('shuffle')">Shuffle</button><button id="loop" onclick="control('loop')">Loop</button></div><div class="status" id="status">Connecting…</div></section></div><div class="mobile-tabs"><button onclick="tab('player')">Now Playing</button><button onclick="tab('browser')">Browse</button></div><script>
let currentSource='', currentPath='', rootPath='', state=null, dragging=false, ws=null, artKey=''; const $=id=>document.getElementById(id); function fmt(v){v=Math.max(0,Math.floor(v||0));return Math.floor(v/60)+':'+String(v%60).padStart(2,'0')} function tab(x){$('browser').classList.toggle('hidden',x!=='browser');$('player').classList.toggle('hidden',x!=='player')}
async function api(path,opt){const r=await fetch(path,opt);if(!r.ok)throw new Error(await r.text());return r.headers.get('content-type')?.includes('json')?r.json():r.text()} async function sources(){const a=await api('/api/sources');$('sources').innerHTML='';a.forEach(s=>{let b=document.createElement('button');b.className='chip';b.textContent=s.name;b.onclick=()=>openSource(s.id,b);$('sources').appendChild(b)})}
async function openSource(id,b){currentSource=id;currentPath='';document.querySelectorAll('.chip').forEach(x=>x.classList.remove('active'));b?.classList.add('active');await browse('')} async function browse(path){try{const d=await api('/api/browse?source='+encodeURIComponent(currentSource)+'&path='+encodeURIComponent(path||''));currentPath=d.path||'';rootPath=d.root||'';$('path').textContent=currentPath||currentSource;const l=$('list');l.innerHTML='';if(currentPath&&rootPath&&currentPath!==rootPath){let up=document.createElement('button');up.className='entry';up.innerHTML='<span>..</span><span class="kind">Folder</span>';up.onclick=()=>browse(currentPath.substring(0,currentPath.lastIndexOf('/'))||rootPath);l.appendChild(up)}d.entries.forEach(e=>{let x=document.createElement('button');x.className='entry';x.innerHTML='<span>'+escapeHtml(e.name)+'</span><span class="kind">'+(e.is_dir?'Folder':'Track')+'</span>';x.onclick=()=>e.is_dir?browse(e.path):play(e.path);l.appendChild(x)});if(!d.entries.length){let empty=document.createElement('div');empty.className='empty';empty.textContent='No playable items found.';l.appendChild(empty)}}catch(e){$('list').innerHTML='<div class="empty">'+escapeHtml(String(e.message||e))+'</div>'}}
async function play(path){await api('/api/play',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({source:currentSource,path})});if(innerWidth<=760)tab('player')} async function control(action){await api('/api/control',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action})})}
function render(s){state=s;$('dot').classList.toggle('on',s.connected);$('status').textContent=s.connected?'Connected to MiSTer Hi-Fi':'Disconnected from MiSTer Hi-Fi';$('title').textContent=s.title||'Nothing playing';$('artist').textContent=s.artist||'';$('album').textContent=s.album||'';$('play').textContent=s.state==='playing'?'❚❚':'▶';$('shuffle').classList.toggle('active',s.shuffle);$('loop').classList.toggle('active',s.loop);if(!dragging){$('seek').value=s.duration>0?Math.round(1000*s.position/s.duration):0}$('pos').textContent=fmt(s.position);$('dur').textContent=fmt(s.duration);if(s.has_art&&s.art_key!==artKey){artKey=s.art_key;$('art').src='/api/art?k='+encodeURIComponent(artKey)}else if(!s.has_art){artKey='';$('art').removeAttribute('src')}}
function connect(){const proto=location.protocol==='https:'?'wss':'ws';ws=new WebSocket(proto+'://'+location.host+'/ws');ws.onmessage=e=>{try{render(JSON.parse(e.data))}catch{}};ws.onopen=()=>{$('dot').classList.add('on')};ws.onclose=()=>{$('dot').classList.remove('on');$('status').textContent='Disconnected from MiSTer Hi-Fi';setTimeout(connect,1500)};ws.onerror=()=>ws.close()}
$('seek').addEventListener('pointerdown',()=>dragging=true);$('seek').addEventListener('input',()=>{if(state&&state.duration>0){let p=state.duration*$('seek').value/1000;$('pos').textContent=fmt(p)}});$('seek').addEventListener('change',async()=>{if(state&&state.duration>0){let p=state.duration*$('seek').value/1000;await api('/api/seek',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({position:p})})}dragging=false});$('seek').addEventListener('pointerup',()=>dragging=false);function escapeHtml(s){return String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))} sources();connect();if(innerWidth<=760)tab('player');
</script></body></html>`
