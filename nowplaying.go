package main

// Atomic now-playing hand-off for MiSTer Monitor and other local consumers.
import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"sync"
	"time"
)

var nowPlayingFile = "/tmp/misterhifi-now.json"
var nowPlayingCover = "/tmp/misterhifi-cover.jpg"

const nowPlayingMax = 400

var nowPlayingState struct {
	sync.Mutex
	key, artKey string
}

type nowPlayingInfo struct {
	Artist   string  `json:"artist"`
	Album    string  `json:"album"`
	Title    string  `json:"title"`
	Cover    bool    `json:"cover"`
	State    string  `json:"state"`
	Position float64 `json:"position"`
	Duration float64 `json:"duration"`
}

func publishNowPlaying(t Track) { _ = publishNowPlayingState(t, "playing", 0, t.Duration) }
func publishNowPlayingState(t Track, state string, position, duration float64) error {
	nowPlayingState.Lock()
	defer nowPlayingState.Unlock()
	hasCover := t.Art != nil
	artKey := fmt.Sprintf("%s\x00%s\x00%s\x00%p", t.Path, t.Artist, t.Album, t.Art)
	if hasCover && nowPlayingState.artKey != artKey {
		if err := writeNowPlayingCover(t.Art); err != nil {
			return err
		}
	} else if !hasCover {
		if err := os.Remove(nowPlayingCover); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	info := nowPlayingInfo{Artist: t.Artist, Album: t.Album, Title: t.Title, Cover: hasCover, State: state, Position: position, Duration: duration}
	b, err := json.Marshal(info)
	if err != nil {
		return err
	}
	key := string(b)
	if key == nowPlayingState.key && artKey == nowPlayingState.artKey {
		return nil
	}
	tmp := nowPlayingFile + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, nowPlayingFile); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// Commit only after successful I/O, so a transient error is retried.
	nowPlayingState.key, nowPlayingState.artKey = key, artKey
	return nil
}
func clearNowPlaying() {
	nowPlayingState.Lock()
	defer nowPlayingState.Unlock()
	nowPlayingState.key, nowPlayingState.artKey = "", ""
	_ = os.Remove(nowPlayingFile)
	_ = os.Remove(nowPlayingCover)
	_ = os.Remove(nowPlayingFile + ".tmp")
	_ = os.Remove(nowPlayingCover + ".tmp")
}
func writeNowPlayingCover(src image.Image) error {
	img := scaleDownBilinear(src, nowPlayingMax)
	tmp := nowPlayingCover + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	err = jpeg.Encode(f, img, &jpeg.Options{Quality: 85})
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, nowPlayingCover)
}
func (p *Player) publishCurrent() {
	p.opMu.Lock()
	defer p.opMu.Unlock()
	p.publishCurrentUnlocked()
}
func (p *Player) publishCurrentUnlocked() {
	p.mu.Lock()
	if p.stopped || p.stop == nil || p.q.Index < 0 || p.q.Index >= len(p.q.Tracks) {
		p.mu.Unlock()
		return
	}
	t := p.q.Tracks[p.q.Index]
	paused, loading := p.paused, p.loading
	p.mu.Unlock()
	state := "playing"
	if paused {
		state = "paused"
	}
	if loading {
		state = "loading"
	}
	pos := 0.0
	if !loading {
		pos = p.elapsedNow().Seconds()
	}
	_ = publishNowPlayingState(t, state, pos, t.Duration)
}
func (p *Player) monitorNowPlaying(stop <-chan struct{}, generation uint64) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		p.opMu.Lock()
		p.mu.Lock()
		valid := p.stop == stop && p.generation == generation && !p.stopped
		p.mu.Unlock()
		if valid {
			p.publishCurrentUnlocked()
		}
		p.opMu.Unlock()
		if !valid {
			return
		}
		select {
		case <-stop:
			return
		case <-tick.C:
		}
	}
}

func scaleDownBilinear(src image.Image, max int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	nw, nh := w, h
	if w > max || h > max {
		if w >= h {
			nw, nh = max, h*max/w
		} else {
			nw, nh = w*max/h, max
		}
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	sx, sy := float64(w)/float64(nw), float64(h)/float64(nh)
	at := func(x, y int) (r, g, bl float64) {
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		if x >= w {
			x = w - 1
		}
		if y >= h {
			y = h - 1
		}
		cr, cg, cb, _ := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
		return float64(cr >> 8), float64(cg >> 8), float64(cb >> 8)
	}
	for y := 0; y < nh; y++ {
		fy := (float64(y)+0.5)*sy - 0.5
		y0 := int(fy)
		if fy < 0 {
			y0, fy = 0, 0
		}
		ty := fy - float64(y0)
		for x := 0; x < nw; x++ {
			fx := (float64(x)+0.5)*sx - 0.5
			x0 := int(fx)
			if fx < 0 {
				x0, fx = 0, 0
			}
			tx := fx - float64(x0)
			r00, g00, b00 := at(x0, y0)
			r10, g10, b10 := at(x0+1, y0)
			r01, g01, b01 := at(x0, y0+1)
			r11, g11, b11 := at(x0+1, y0+1)
			mix := func(a, bb, c, d float64) uint8 {
				top := a + (bb-a)*tx
				bot := c + (d-c)*tx
				return uint8(top + (bot-top)*ty + 0.5)
			}
			dst.SetRGBA(x, y, color.RGBA{mix(r00, r10, r01, r11), mix(g00, g10, g01, g11), mix(b00, b10, b01, b11), 255})
		}
	}
	return dst
}
