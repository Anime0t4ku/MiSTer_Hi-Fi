package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const version = "1.1.0"
const baseDir = "/media/fat/Scripts/.config/MiSTerHiFi"
const socketPath = "/tmp/misterhifi.sock"
const smbMountRoot = "/tmp/misterhifi-mnt"

var supported = map[string]bool{".mp3": true, ".wav": true, ".flac": true, ".m3u": true, ".m3u8": true}

var swapABInput atomic.Bool
var swapXYInput atomic.Bool
var screenSaverSeconds atomic.Int64
var screenSaverActive atomic.Bool

type Config struct {
	EQ                 EQConfig `json:"eq"`
	Visualizer         string   `json:"visualizer"`
	OLEDMode           bool     `json:"oled_mode"`
	HideAlbumArt       bool     `json:"hide_album_art"`
	AutoHideMissingArt bool     `json:"auto_hide_missing_art"`
	ShowClock          bool     `json:"show_clock"`
	ConfirmOnExit      bool     `json:"confirm_on_exit"`
	ScreenSaverSeconds int      `json:"screensaver_seconds"`
	GaplessPlayback    bool     `json:"gapless_playback"`
	SwapAB             bool     `json:"swap_ab"`
	SwapXY             bool     `json:"swap_xy"`
	CustomFont         string   `json:"custom_font"`
}
type EQConfig struct {
	Enabled                            bool `json:"enabled"`
	Bass, LowMid, Mid, HighMid, Treble float64
}
type SMBConfig struct {
	Shares []SMBShare `json:"shares"`
}
type SMBShare struct {
	Name, Server, Share, Path, Username, Password string
	Guest                                         bool
}
type Track struct {
	Path, Title, Artist, Album    string
	BaseName                      string
	DirFD                         int
	UseDirFD                      bool
	Duration                      float64
	MediaFormat                   string
	BitDepth, SampleRate, BitRate int
	Art                           image.Image
}
type Queue struct {
	Tracks          []Track
	Index           int
	Repeat, Shuffle bool
	DirFD           int
	UseDirFD        bool
}

type fbVar struct {
	Xres, Yres, XresVirtual, YresVirtual, Xoffset, Yoffset, BitsPerPixel, Grayscale                                                                                                                                                                                                                         uint32
	RedOffset, RedLength, RedMsb, GreenOffset, GreenLength, GreenMsb, BlueOffset, BlueLength, BlueMsb, TranspOffset, TranspLength, TranspMsb, Nonstd, Activate, Height, Width, AccelFlags, Pixclock, LeftMargin, RightMargin, UpperMargin, LowerMargin, HsyncLen, VsyncLen, Sync, Vmode, Rotate, Colorspace uint32
	Reserved                                                                                                                                                                                                                                                                                                [4]uint32
}
type framebuffer struct {
	f                 *os.File
	data              []byte
	back              []byte
	w, h, stride, bpp int
}

type inputEvent struct {
	Time       syscall.Timeval
	Type, Code uint16
	Value      int32
}
type action int

const (
	actNone action = iota
	actLeft
	actRight
	actUp
	actDown
	actConfirm
	actBack
	actPrev
	actNext
	actPlayPause
	actStop
	actNowPlaying
	actSources
	actWake
)
const (
	evKey    = 1
	evAbs    = 3
	keyEsc   = 1
	keyEnter = 28
	keyUp    = 103
	keyLeft  = 105
	keyRight = 106
	keyDown  = 108
	keyBack  = 158
	btnSouth = 304
	btnEast  = 305
	btnTL    = 310
	btnTR    = 311
	btnNorth = 307
	btnWest  = 308
	btnStart = 315
	btnMode  = 316
	keyHome  = 102
	absHatX  = 16
	absHatY  = 17
)

var font = map[rune][7]byte{
	'A': {14, 17, 17, 31, 17, 17, 17}, 'B': {30, 17, 17, 30, 17, 17, 30}, 'C': {14, 17, 16, 16, 16, 17, 14}, 'D': {30, 17, 17, 17, 17, 17, 30}, 'E': {31, 16, 16, 30, 16, 16, 31}, 'F': {31, 16, 16, 30, 16, 16, 16}, 'G': {14, 17, 16, 23, 17, 17, 15}, 'H': {17, 17, 17, 31, 17, 17, 17}, 'I': {14, 4, 4, 4, 4, 4, 14}, 'J': {7, 2, 2, 2, 18, 18, 12}, 'K': {17, 18, 20, 24, 20, 18, 17}, 'L': {16, 16, 16, 16, 16, 16, 31}, 'M': {17, 27, 21, 21, 17, 17, 17}, 'N': {17, 25, 21, 19, 17, 17, 17}, 'O': {14, 17, 17, 17, 17, 17, 14}, 'P': {30, 17, 17, 30, 16, 16, 16}, 'Q': {14, 17, 17, 17, 21, 18, 13}, 'R': {30, 17, 17, 30, 20, 18, 17}, 'S': {15, 16, 16, 14, 1, 1, 30}, 'T': {31, 4, 4, 4, 4, 4, 4}, 'U': {17, 17, 17, 17, 17, 17, 14}, 'V': {17, 17, 17, 17, 17, 10, 4}, 'W': {17, 17, 17, 21, 21, 21, 10}, 'X': {17, 17, 10, 4, 10, 17, 17}, 'Y': {17, 17, 10, 4, 4, 4, 4}, 'Z': {31, 1, 2, 4, 8, 16, 31},
	'0': {14, 17, 19, 21, 25, 17, 14}, '1': {4, 12, 4, 4, 4, 4, 14}, '2': {14, 17, 1, 2, 4, 8, 31}, '3': {30, 1, 1, 14, 1, 1, 30}, '4': {2, 6, 10, 18, 31, 2, 2}, '5': {31, 16, 16, 30, 1, 1, 30}, '6': {14, 16, 16, 30, 17, 17, 14}, '7': {31, 1, 2, 4, 8, 8, 8}, '8': {14, 17, 17, 14, 17, 17, 14}, '9': {14, 17, 17, 15, 1, 1, 14},
	'-': {0, 0, 0, 31, 0, 0, 0}, '_': {0, 0, 0, 0, 0, 0, 31}, '.': {0, 0, 0, 0, 0, 12, 12}, '/': {1, 2, 2, 4, 8, 8, 16}, ':': {0, 12, 12, 0, 12, 12, 0}, ' ': {0, 0, 0, 0, 0, 0, 0}, '>': {16, 8, 4, 2, 4, 8, 16}, '<': {1, 2, 4, 8, 4, 2, 1}, '+': {0, 4, 4, 31, 4, 4, 0}, '=': {0, 31, 0, 31, 0, 0, 0}, '%': {17, 2, 4, 8, 17, 0, 0}, '(': {2, 4, 8, 8, 8, 4, 2}, ')': {8, 4, 2, 2, 2, 4, 8}, '[': {14, 8, 8, 8, 8, 8, 14}, ']': {14, 2, 2, 2, 2, 2, 14},
}

func openFB() (*framebuffer, error) {
	f, e := os.OpenFile("/dev/fb0", os.O_RDWR, 0)
	if e != nil {
		return nil, e
	}
	var v fbVar
	_, _, er := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), 0x4600, uintptr(unsafe.Pointer(&v)))
	if er != 0 {
		f.Close()
		return nil, er
	}
	bpp := int(v.BitsPerPixel / 8)
	if bpp != 2 && bpp != 4 {
		f.Close()
		return nil, fmt.Errorf("unsupported framebuffer depth")
	}
	stride := int(v.XresVirtual) * bpp
	data, e := syscall.Mmap(int(f.Fd()), 0, stride*int(v.YresVirtual), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if e != nil {
		f.Close()
		return nil, e
	}
	back := make([]byte, stride*int(v.Yres))
	copy(back, data[:len(back)])
	return &framebuffer{f: f, data: data, back: back, w: int(v.Xres), h: int(v.Yres), stride: stride, bpp: bpp}, nil
}
func (fb *framebuffer) close() {
	if fb == nil {
		return
	}
	if fb.data != nil {
		_ = syscall.Munmap(fb.data)
	}
	if fb.f != nil {
		_ = fb.f.Close()
	}
}
func (fb *framebuffer) put(x, y int, c color.RGBA) {
	if x < 0 || y < 0 || x >= fb.w || y >= fb.h {
		return
	}
	o := y*fb.stride + x*fb.bpp
	if fb.bpp == 4 {
		fb.back[o] = c.B
		fb.back[o+1] = c.G
		fb.back[o+2] = c.R
		fb.back[o+3] = 255
	} else {
		v := uint16((uint16(c.R>>3) << 11) | (uint16(c.G>>2) << 5) | uint16(c.B>>3))
		fb.back[o] = byte(v)
		fb.back[o+1] = byte(v >> 8)
	}
}
func (fb *framebuffer) rect(x, y, w, h int, c color.RGBA) {
	if fb == nil || w <= 0 || h <= 0 {
		return
	}
	if x < 0 {
		w += x
		x = 0
	}
	if y < 0 {
		h += y
		y = 0
	}
	if x+w > fb.w {
		w = fb.w - x
	}
	if y+h > fb.h {
		h = fb.h - y
	}
	if w <= 0 || h <= 0 {
		return
	}
	row := make([]byte, w*fb.bpp)
	if fb.bpp == 4 {
		for i := 0; i < len(row); i += 4 {
			row[i] = c.B
			row[i+1] = c.G
			row[i+2] = c.R
			row[i+3] = 255
		}
	} else {
		v := uint16((uint16(c.R>>3) << 11) | (uint16(c.G>>2) << 5) | uint16(c.B>>3))
		lo, hi := byte(v), byte(v>>8)
		for i := 0; i < len(row); i += 2 {
			row[i] = lo
			row[i+1] = hi
		}
	}
	start := y*fb.stride + x*fb.bpp
	for yy := 0; yy < h; yy++ {
		o := start + yy*fb.stride
		copy(fb.back[o:o+len(row)], row)
	}
}
func (fb *framebuffer) border(x, y, w, h, t int, c color.RGBA) {
	fb.rect(x, y, w, t, c)
	fb.rect(x, y+h-t, w, t, c)
	fb.rect(x, y, t, h, c)
	fb.rect(x+w-t, y, t, h, c)
}
func (fb *framebuffer) fill(c color.RGBA) { fb.rect(0, 0, fb.w, fb.h, c) }
func (fb *framebuffer) present() {
	if screenSaverActive.Load() {
		return
	}
	if fb == nil || len(fb.back) == 0 || len(fb.data) == 0 {
		return
	}
	n := fb.stride * fb.h
	if n > len(fb.back) {
		n = len(fb.back)
	}
	if n > len(fb.data) {
		n = len(fb.data)
	}
	copy(fb.data[:n], fb.back[:n])
}
func (fb *framebuffer) presentRegion(x, y, w, h int) {
	if screenSaverActive.Load() {
		return
	}
	if fb == nil || w <= 0 || h <= 0 {
		return
	}
	if x < 0 {
		w += x
		x = 0
	}
	if y < 0 {
		h += y
		y = 0
	}
	if x+w > fb.w {
		w = fb.w - x
	}
	if y+h > fb.h {
		h = fb.h - y
	}
	if w <= 0 || h <= 0 {
		return
	}
	n := w * fb.bpp
	for yy := y; yy < y+h; yy++ {
		o := yy*fb.stride + x*fb.bpp
		copy(fb.data[o:o+n], fb.back[o:o+n])
	}
}
func customFallbackPixelHeight(s int) int {
	if s < 1 {
		s = 1
	}
	px := (7*s*175 + 50) / 100
	if px < 1 {
		px = 1
	}
	return px
}

func (fb *framebuffer) text(x, y, s int, str string, c color.RGBA) {
	cx := x
	for _, ch := range strings.ToUpper(str) {
		if g, ok := font[ch]; ok {
			for gy, row := range g {
				for gx := 0; gx < 5; gx++ {
					if row&(1<<(4-gx)) != 0 {
						fb.rect(cx+gx*s, y+gy*s, s, s, c)
					}
				}
			}
			cx += 6 * s
			continue
		}
		fallbackPx := customFallbackPixelHeight(s)
		if g, ok := customFontGlyph(ch, fallbackPx); ok {
			baseline := y + 7*s - (fallbackPx-7*s)/2 + 2*s
			for gy := 0; gy < g.h; gy++ {
				for gx := 0; gx < g.w; gx++ {
					if g.pixels[gy*g.w+gx] >= 96 {
						fb.put(cx+g.xoff+gx, baseline+g.yoff+gy, c)
					}
				}
			}
			cx += g.advance
			continue
		}
		g := font['?']
		for gy, row := range g {
			for gx := 0; gx < 5; gx++ {
				if row&(1<<(4-gx)) != 0 {
					fb.rect(cx+gx*s, y+gy*s, s, s, c)
				}
			}
		}
		cx += 6 * s
	}
}
func tw(s int, str string) int {
	w := 0
	for _, ch := range strings.ToUpper(str) {
		if _, ok := font[ch]; ok {
			w += 6 * s
			continue
		}
		if adv, ok := customFontAdvance(ch, customFallbackPixelHeight(s)); ok {
			w += adv
		} else {
			w += 6 * s
		}
	}
	return w
}
func loadImg(p string) image.Image {
	f, e := os.Open(p)
	if e != nil {
		return nil
	}
	defer f.Close()
	im, _, e := image.Decode(f)
	if e != nil {
		return nil
	}
	return im
}
func sample(im image.Image, x, y int) color.RGBA {
	r, g, b, a := im.At(x, y).RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}
func (fb *framebuffer) drawImage(im image.Image, dx, dy, dw, dh int) {
	if im == nil {
		return
	}
	b := im.Bounds()
	sw, sh := b.Dx(), b.Dy()
	scale := math.Min(float64(dw)/float64(sw), float64(dh)/float64(sh))
	w, h := int(float64(sw)*scale), int(float64(sh)*scale)
	ox, oy := dx+(dw-w)/2, dy+(dh-h)/2
	for y := 0; y < h; y++ {
		sy := b.Min.Y + y*sh/h
		for x := 0; x < w; x++ {
			sx := b.Min.X + x*sw/w
			fb.put(ox+x, oy+y, sample(im, sx, sy))
		}
	}
}

func screenSaverInputLoop(fb *framebuffer, raw <-chan action, out chan<- action, done <-chan struct{}) {
	lastActivity := time.Now()
	sleeping := false
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case a := <-raw:
			lastActivity = time.Now()
			if sleeping {
				sleeping = false
				screenSaverActive.Store(false)
				select {
				case out <- actWake:
				case <-done:
					return
				}
				continue
			}
			select {
			case out <- a:
			case <-done:
				return
			}
		case now := <-tick.C:
			seconds := screenSaverSeconds.Load()
			if seconds <= 0 {
				continue
			}
			if !sleeping && now.Sub(lastActivity) >= time.Duration(seconds)*time.Second {
				fb.fill(color.RGBA{0, 0, 0, 255})
				fb.present()
				screenSaverActive.Store(true)
				sleeping = true
			}
		}
	}
}

func inputLoop(ch chan<- action, done <-chan struct{}) {
	files, _ := filepath.Glob("/dev/input/event*")
	var emitMu sync.Mutex
	last := map[action]time.Time{}
	lastFaceAt := time.Time{}
	emit := func(a action, face bool) {
		if a == actNone {
			return
		}
		now := time.Now()
		emitMu.Lock()
		if face {
			if !lastFaceAt.IsZero() && now.Sub(lastFaceAt) < 220*time.Millisecond {
				emitMu.Unlock()
				return
			}
			lastFaceAt = now
		}
		window := 140 * time.Millisecond
		if t, ok := last[a]; ok && now.Sub(t) < window {
			emitMu.Unlock()
			return
		}
		last[a] = now
		emitMu.Unlock()
		select {
		case ch <- a:
		default:
		}
	}
	for _, p := range files {
		f, e := os.Open(p)
		if e != nil {
			continue
		}
		const eviocgrab = 0x40044590
		_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(eviocgrab), uintptr(1))
		go func(f *os.File) {
			defer func() {
				_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(eviocgrab), uintptr(0))
				f.Close()
			}()
			var hx, hy int32
			pressed := map[uint16]bool{}
			for {
				select {
				case <-done:
					return
				default:
				}
				var ev inputEvent
				if binary.Read(f, binary.LittleEndian, &ev) != nil {
					return
				}
				a := actNone
				face := false
				if ev.Type == evKey {
					if ev.Value == 0 {
						pressed[ev.Code] = false
					} else if ev.Value == 1 && !pressed[ev.Code] {
						pressed[ev.Code] = true
						switch ev.Code {
						case keyLeft:
							a = actLeft
						case keyRight:
							a = actRight
						case keyUp:
							a = actUp
						case keyDown:
							a = actDown
						case keyEnter:
							a = actConfirm
						case keyEsc, keyBack:
							a = actBack
						case keyHome, btnMode:
							a = actSources
						case btnSouth:
							if swapABInput.Load() {
								a = actConfirm
							} else {
								a = actBack
							}
							face = true
						case btnEast:
							if swapABInput.Load() {
								a = actBack
							} else {
								a = actConfirm
							}
							face = true
						case btnTL:
							a = actPrev
						case btnTR:
							a = actNext
						case btnWest:
							if swapXYInput.Load() {
								a = actStop
							} else {
								a = actPlayPause
							}
							face = true
						case btnNorth:
							if swapXYInput.Load() {
								a = actPlayPause
							} else {
								a = actStop
							}
							face = true
						case btnStart:
							a = actNowPlaying
						}
					}
				}
				if ev.Type == evAbs {
					if ev.Code == absHatX && ev.Value != hx {
						hx = ev.Value
						if ev.Value < 0 {
							a = actLeft
						} else if ev.Value > 0 {
							a = actRight
						}
					}
					if ev.Code == absHatY && ev.Value != hy {
						hy = ev.Value
						if ev.Value < 0 {
							a = actUp
						} else if ev.Value > 0 {
							a = actDown
						}
					}
				}
				emit(a, face)
			}
		}(f)
	}
}

type termState struct {
	fd   uintptr
	orig syscall.Termios
	ok   bool
}

func quietTerm() *termState {
	fd := os.Stdin.Fd()
	var t syscall.Termios
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&t)))
	if e != 0 {
		return &termState{}
	}
	o := t
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON
	_, _, e = syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&t)))
	return &termState{fd: fd, orig: o, ok: e == 0}
}
func (t *termState) restore() {
	if t != nil && t.ok {
		syscall.Syscall(syscall.SYS_IOCTL, t.fd, uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&t.orig)))
	}
}

func defaultConfig() Config {
	return Config{Visualizer: "bars", ConfirmOnExit: true}
}
func loadConfig() Config {
	c := defaultConfig()
	_ = os.MkdirAll(baseDir, 0755)
	path := filepath.Join(baseDir, "config.json")
	b, e := os.ReadFile(path)
	if e != nil {
		saveConfig(c)
		return c
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		backup := path + ".invalid-" + time.Now().Format("20060102-150405")
		_ = os.Rename(path, backup)
		c = defaultConfig()
		saveConfig(c)
		return c
	}
	if err := json.Unmarshal(b, &c); err != nil {
		backup := path + ".invalid-" + time.Now().Format("20060102-150405")
		_ = os.Rename(path, backup)
		c = defaultConfig()
		saveConfig(c)
		return c
	}
	if _, ok := raw["confirm_on_exit"]; !ok {
		c.ConfirmOnExit = true
	}
	if c.Visualizer == "" {
		c.Visualizer = "bars"
	}
	saveConfig(c)
	return c
}
func saveConfig(c Config) {
	_ = os.MkdirAll(baseDir, 0755)
	b, _ := json.MarshalIndent(c, "", "  ")
	_ = os.WriteFile(filepath.Join(baseDir, "config.json"), b, 0644)
}

const customFontsDir = baseDir + "/fonts"

type customFontOption struct {
	Name string
	File string
}

func scanCustomFonts() []customFontOption {
	_ = os.MkdirAll(customFontsDir, 0755)
	es, err := os.ReadDir(customFontsDir)
	if err != nil {
		return nil
	}
	var out []customFontOption
	for _, e := range es {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".ttf" && ext != ".otf" {
			continue
		}
		p := filepath.Join(customFontsDir, e.Name())
		if !customFontValid(p) {
			continue
		}
		name := strings.TrimSpace(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		if name == "" {
			name = e.Name()
		}
		out = append(out, customFontOption{Name: name, File: e.Name()})
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func applyCustomFont(cfg *Config, fonts []customFontOption) {
	if cfg == nil || cfg.CustomFont == "" {
		setCustomFont("")
		return
	}
	for _, f := range fonts {
		if f.File == cfg.CustomFont {
			if setCustomFont(filepath.Join(customFontsDir, f.File)) {
				return
			}
			break
		}
	}
	cfg.CustomFont = ""
	setCustomFont("")
}

func customFontLabel(cfg *Config, fonts []customFontOption) string {
	if cfg == nil || cfg.CustomFont == "" {
		return "OFF"
	}
	for _, f := range fonts {
		if f.File == cfg.CustomFont {
			return strings.ToUpper(f.Name)
		}
	}
	return "OFF"
}

func cycleCustomFont(cfg *Config, fonts []customFontOption, dir int) {
	if cfg == nil || len(fonts) == 0 {
		return
	}
	idx := 0
	if cfg.CustomFont != "" {
		for i, f := range fonts {
			if f.File == cfg.CustomFont {
				idx = i + 1
				break
			}
		}
	}
	total := len(fonts) + 1
	idx = (idx + dir + total) % total
	if idx == 0 {
		cfg.CustomFont = ""
		setCustomFont("")
		return
	}
	cfg.CustomFont = fonts[idx-1].File
	if !setCustomFont(filepath.Join(customFontsDir, cfg.CustomFont)) {
		cfg.CustomFont = ""
		setCustomFont("")
	}
}
func loadSMB() SMBConfig {
	var c SMBConfig
	b, e := os.ReadFile(filepath.Join(baseDir, "smb.json"))
	if e == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}
func smbAvailable() bool { c := loadSMB(); return len(c.Shares) > 0 }
func sanitize(s string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", " ", "_")
	return r.Replace(s)
}
func isMounted(path string) bool {
	b, e := os.ReadFile("/proc/mounts")
	if e != nil {
		return false
	}
	needle := " " + path + " "
	for _, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

func decodeMountField(s string) string {
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(s)
}

func mountedSource(path string) string {
	b, e := os.ReadFile("/proc/mounts")
	if e != nil {
		return ""
	}
	clean := filepath.Clean(path)
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if filepath.Clean(decodeMountField(fields[1])) == clean {
			return decodeMountField(fields[0])
		}
	}
	return ""
}

func sameSMBSource(got, server, share string) bool {
	got = strings.ReplaceAll(strings.TrimSpace(got), `\`, "/")
	want := "//" + strings.Trim(server, "/") + "/" + strings.Trim(share, "/")
	return strings.EqualFold(got, want)
}

var smbMountMu sync.Mutex
var smbManagedMounts = map[string]struct{}{}

func registerSMBMount(path string) {
	smbMountMu.Lock()
	smbManagedMounts[path] = struct{}{}
	smbMountMu.Unlock()
}

func cleanupSMBMounts() {
	smbMountMu.Lock()
	paths := make([]string, 0, len(smbManagedMounts))
	for path := range smbManagedMounts {
		paths = append(paths, path)
	}
	smbMountMu.Unlock()
	for _, path := range paths {
		if isMounted(path) {
			_ = exec.Command("umount", path).Run()
		}
		if !isMounted(path) {
			_ = os.Remove(path)
		}
	}
	_ = os.Remove(smbMountRoot)
}

func mountShare(s SMBShare) (string, error) {
	name := s.Name
	if name == "" {
		name = s.Server + "_" + s.Share
	}
	mountRoot := filepath.Join(smbMountRoot, sanitize(name))
	_ = os.MkdirAll(mountRoot, 0755)
	if !isMounted(mountRoot) {
		src := "//" + s.Server + "/" + s.Share
		auth := []string{"ro", "iocharset=utf8"}
		if s.Guest {
			auth = append(auth, "guest")
		} else {
			auth = append(auth, "username="+s.Username, "password="+s.Password)
		}
		attempts := [][]string{
			append(append([]string{}, auth...), "vers=3.0"),
			append(append([]string{}, auth...), "vers=2.1"),
			append(append([]string{}, auth...), "vers=2.0"),
			append([]string{}, auth...),
			append(append([]string{}, auth...), "vers=1.0"),
		}
		sources := []string{src}
		if strings.Contains(s.Share, " ") {
			escaped := "//" + s.Server + "/" + strings.ReplaceAll(s.Share, " ", `\040`)
			encoded := "//" + s.Server + "/" + strings.ReplaceAll(s.Share, " ", "%20")
			if escaped != src {
				sources = append(sources, escaped)
			}
			if encoded != src && encoded != escaped {
				sources = append(sources, encoded)
			}
		}
		var last string
		for _, mountSrc := range sources {
			for _, opts := range attempts {
				cmd := exec.Command("mount", "-t", "cifs", mountSrc, mountRoot, "-o", strings.Join(opts, ","))
				out, err := cmd.CombinedOutput()
				if err == nil || isMounted(mountRoot) {
					if isMounted(mountRoot) && sameSMBSource(mountedSource(mountRoot), s.Server, s.Share) {
						last = ""
						break
					}
					if isMounted(mountRoot) {
						got := mountedSource(mountRoot)
						_ = exec.Command("umount", mountRoot).Run()
						last = "mounted unexpected share: " + got
						continue
					}
				}
				last = strings.TrimSpace(string(out))
				if last == "" {
					last = err.Error()
				}
			}
			if isMounted(mountRoot) && sameSMBSource(mountedSource(mountRoot), s.Server, s.Share) {
				break
			}
		}
		if !isMounted(mountRoot) || !sameSMBSource(mountedSource(mountRoot), s.Server, s.Share) {
			if isMounted(mountRoot) {
				_ = exec.Command("umount", mountRoot).Run()
			}
			return "", fmt.Errorf("unable to mount //%s/%s: %s", s.Server, s.Share, last)
		}
	}
	registerSMBMount(mountRoot)
	m := mountRoot
	if s.Path != "" {
		m = filepath.Join(mountRoot, filepath.FromSlash(s.Path))
		if st, e := os.Stat(m); e != nil || !st.IsDir() {
			return "", fmt.Errorf("SMB path not found: %s", s.Path)
		}
	}
	return m, nil
}

func audioFiles(dir string) []string {
	es, e := os.ReadDir(dir)
	if e != nil {
		return nil
	}
	var out []string
	for _, x := range es {
		if x.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(x.Name()))
		if ext == ".mp3" || ext == ".wav" || ext == ".flac" {
			out = append(out, filepath.Join(dir, x.Name()))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(filepath.Base(out[i])) < strings.ToLower(filepath.Base(out[j]))
	})
	return out
}
func basicTrack(p string) Track {
	return Track{Path: p, Title: strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)), DirFD: -1}
}

func basicTrackAt(dirFD int, dir, name string) Track {
	p := filepath.Join(dir, name)
	return Track{Path: p, BaseName: name, DirFD: dirFD, UseDirFD: true, Title: strings.TrimSuffix(name, filepath.Ext(name))}
}

func openTrackFile(t Track) (*os.File, error) {
	if t.UseDirFD && t.DirFD >= 0 && t.BaseName != "" {
		fd, err := syscall.Openat(t.DirFD, t.BaseName, syscall.O_RDONLY, 0)
		if err != nil {
			return nil, err
		}
		return os.NewFile(uintptr(fd), t.BaseName), nil
	}
	return os.Open(t.Path)
}
func folderArtwork(dir string) image.Image {
	es, e := os.ReadDir(dir)
	if e != nil {
		return nil
	}
	byName := make(map[string]string, len(es))
	for _, x := range es {
		if x.IsDir() {
			continue
		}
		byName[strings.ToLower(x.Name())] = x.Name()
	}
	for _, n := range []string{
		"cover.jpg", "cover.jpeg", "cover.png",
		"folder.jpg", "folder.jpeg", "folder.png",
		"front.jpg", "front.jpeg", "front.png",
	} {
		if actual, ok := byName[n]; ok {
			if im := loadImg(filepath.Join(dir, actual)); im != nil {
				return im
			}
		}
	}
	return nil
}

func trackFromPath(p string) Track {
	t := basicTrack(p)
	readBasicTags(&t)
	if t.Art == nil {
		t.Art = folderArtwork(filepath.Dir(p))
	}
	return t
}

func trackFromTrack(src Track) Track {
	t := src
	readBasicTags(&t)
	if t.Art == nil && !t.UseDirFD {
		t.Art = folderArtwork(filepath.Dir(t.Path))
	}
	return t
}
func readBasicTags(t *Track) {
	ext := strings.ToLower(filepath.Ext(t.Path))
	switch ext {
	case ".mp3":
		t.MediaFormat = "MP3"
		readMP3Info(t)
		readID3(t)
	case ".flac":
		t.MediaFormat = "FLAC"
		readFLAC(t)
	case ".wav":
		t.MediaFormat = "WAV"
		readWAVInfo(t)
	}
}
func syncSafeSize(b []byte) int {
	if len(b) < 4 {
		return 0
	}
	return int(b[0]&0x7f)<<21 | int(b[1]&0x7f)<<14 | int(b[2]&0x7f)<<7 | int(b[3]&0x7f)
}

func readMP3Info(t *Track) {
	f, e := openTrackFile(*t)
	if e != nil {
		return
	}
	defer f.Close()
	var offset int64
	h := make([]byte, 10)
	if _, e = io.ReadFull(f, h); e == nil && string(h[:3]) == "ID3" {
		offset = int64(10 + syncSafeSize(h[6:10]))
		if h[5]&0x10 != 0 {
			offset += 10
		}
	}
	if _, e = f.Seek(offset, io.SeekStart); e != nil {
		return
	}
	buf := make([]byte, 64*1024)
	n, _ := f.Read(buf)
	buf = buf[:n]
	bitratesMPEG1L3 := [...]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	bitratesMPEG2L3 := [...]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
	sampleRates := [...]int{44100, 48000, 32000}
	for i := 0; i+4 <= len(buf); i++ {
		v := binary.BigEndian.Uint32(buf[i : i+4])
		if v&0xffe00000 != 0xffe00000 {
			continue
		}
		versionID := int((v >> 19) & 0x3)
		layer := int((v >> 17) & 0x3)
		bitrateIndex := int((v >> 12) & 0xf)
		sampleIndex := int((v >> 10) & 0x3)
		if versionID == 1 || layer != 1 || bitrateIndex == 0 || bitrateIndex == 15 || sampleIndex == 3 {
			continue
		}
		rate := sampleRates[sampleIndex]
		switch versionID {
		case 2:
			rate /= 2
		case 0:
			rate /= 4
		}
		t.SampleRate = rate
		if versionID == 3 {
			t.BitRate = bitratesMPEG1L3[bitrateIndex]
		} else {
			t.BitRate = bitratesMPEG2L3[bitrateIndex]
		}
		return
	}
}

func readWAVInfo(t *Track) {
	f, e := openTrackFile(*t)
	if e != nil {
		return
	}
	defer f.Close()
	h := make([]byte, 12)
	if _, e = io.ReadFull(f, h); e != nil || string(h[:4]) != "RIFF" || string(h[8:12]) != "WAVE" {
		return
	}
	var channels, byteRate, dataSize int
	for {
		ch := make([]byte, 8)
		if _, e = io.ReadFull(f, ch); e != nil {
			break
		}
		n := int(binary.LittleEndian.Uint32(ch[4:8]))
		if n < 0 {
			break
		}
		switch string(ch[:4]) {
		case "fmt ":
			b := make([]byte, n)
			if _, e = io.ReadFull(f, b); e != nil {
				return
			}
			if len(b) >= 16 {
				channels = int(binary.LittleEndian.Uint16(b[2:4]))
				t.SampleRate = int(binary.LittleEndian.Uint32(b[4:8]))
				byteRate = int(binary.LittleEndian.Uint32(b[8:12]))
				t.BitDepth = int(binary.LittleEndian.Uint16(b[14:16]))
			}
		case "data":
			dataSize = n
			if _, e = f.Seek(int64(n+(n&1)), io.SeekCurrent); e != nil {
				return
			}
		default:
			if _, e = f.Seek(int64(n+(n&1)), io.SeekCurrent); e != nil {
				return
			}
		}
		if t.SampleRate > 0 && t.BitDepth > 0 && dataSize > 0 {
			break
		}
	}
	if byteRate > 0 {
		t.BitRate = byteRate * 8 / 1000
		if dataSize > 0 {
			t.Duration = float64(dataSize) / float64(byteRate)
		}
	} else if channels > 0 && t.SampleRate > 0 && t.BitDepth > 0 {
		t.BitRate = channels * t.SampleRate * t.BitDepth / 1000
	}
}

func readID3(t *Track) {
	f, e := openTrackFile(*t)
	if e != nil {
		return
	}
	defer f.Close()
	h := make([]byte, 10)
	if _, e = io.ReadFull(f, h); e != nil || string(h[:3]) != "ID3" {
		return
	}
	sz := int(h[6]&0x7f)<<21 | int(h[7]&0x7f)<<14 | int(h[8]&0x7f)<<7 | int(h[9]&0x7f)
	data := make([]byte, sz)
	_, _ = io.ReadFull(f, data)
	for i := 0; i+10 <= len(data); {
		id := string(data[i : i+4])
		n := int(binary.BigEndian.Uint32(data[i+4 : i+8]))
		if n <= 0 || i+10+n > len(data) {
			break
		}
		v := data[i+10 : i+10+n]
		txt := ""
		if len(v) > 1 {
			txt = strings.Trim(string(v[1:]), "\x00 ")
		}
		switch id {
		case "TIT2":
			if txt != "" {
				t.Title = txt
			}
		case "TPE1":
			t.Artist = txt
		case "TALB":
			t.Album = txt
		case "APIC":
			if t.Art == nil {
				if j := bytes.Index(v, []byte{0xff, 0xd8}); j >= 0 {
					im, _, e := image.Decode(bytes.NewReader(v[j:]))
					if e == nil {
						t.Art = im
					}
				}
				if j := bytes.Index(v, []byte{0x89, 'P', 'N', 'G'}); j >= 0 {
					im, _, e := image.Decode(bytes.NewReader(v[j:]))
					if e == nil {
						t.Art = im
					}
				}
			}
		}
		i += 10 + n
	}
}
func readFLAC(t *Track) {
	f, e := openTrackFile(*t)
	if e != nil {
		return
	}
	defer f.Close()
	sig := make([]byte, 4)
	if _, e = io.ReadFull(f, sig); e != nil || string(sig) != "fLaC" {
		return
	}
	for {
		h := make([]byte, 4)
		if _, e = io.ReadFull(f, h); e != nil {
			return
		}
		last := h[0]&0x80 != 0
		typ := h[0] & 0x7f
		n := int(h[1])<<16 | int(h[2])<<8 | int(h[3])
		b := make([]byte, n)
		if _, e = io.ReadFull(f, b); e != nil {
			return
		}
		if typ == 0 && len(b) >= 34 {
			x := binary.BigEndian.Uint64(b[10:18])
			t.SampleRate = int((x >> 44) & 0xfffff)
			t.BitDepth = int((x>>36)&0x1f) + 1
			totalSamples := x & 0xfffffffff
			if t.SampleRate > 0 && totalSamples > 0 {
				t.Duration = float64(totalSamples) / float64(t.SampleRate)
				if st, err := f.Stat(); err == nil && t.Duration > 0 {
					t.BitRate = int((float64(st.Size()) * 8 / t.Duration / 1000) + 0.5)
				}
			}
		}
		if typ == 4 {
			parseVorbis(t, b)
		}
		if typ == 6 && t.Art == nil {
			parseFlacPicture(t, b)
		}
		if last {
			return
		}
	}
}
func parseVorbis(t *Track, b []byte) {
	if len(b) < 8 {
		return
	}
	o := 0
	rd := func() (string, bool) {
		if o+4 > len(b) {
			return "", false
		}
		n := int(binary.LittleEndian.Uint32(b[o : o+4]))
		o += 4
		if o+n > len(b) {
			return "", false
		}
		s := string(b[o : o+n])
		o += n
		return s, true
	}
	_, ok := rd()
	if !ok || o+4 > len(b) {
		return
	}
	cnt := int(binary.LittleEndian.Uint32(b[o : o+4]))
	o += 4
	for i := 0; i < cnt; i++ {
		s, ok := rd()
		if !ok {
			return
		}
		p := strings.SplitN(s, "=", 2)
		if len(p) != 2 {
			continue
		}
		switch strings.ToUpper(p[0]) {
		case "TITLE":
			t.Title = p[1]
		case "ARTIST":
			t.Artist = p[1]
		case "ALBUM":
			t.Album = p[1]
		}
	}
}
func parseFlacPicture(t *Track, b []byte) {
	o := 0
	u32 := func() (uint32, bool) {
		if o+4 > len(b) {
			return 0, false
		}
		v := binary.BigEndian.Uint32(b[o : o+4])
		o += 4
		return v, true
	}
	_, ok := u32()
	if !ok {
		return
	}
	ml, _ := u32()
	o += int(ml)
	dl, _ := u32()
	o += int(dl)
	o += 16
	sz, _ := u32()
	if o+int(sz) > len(b) {
		return
	}
	im, _, e := image.Decode(bytes.NewReader(b[o : o+int(sz)]))
	if e == nil {
		t.Art = im
	}
}
func parseM3U(p string) []string {
	f, e := os.Open(p)
	if e != nil {
		return nil
	}
	defer f.Close()
	var out []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		x := strings.TrimSpace(s.Text())
		if x == "" || strings.HasPrefix(x, "#") {
			continue
		}
		if !filepath.IsAbs(x) {
			x = filepath.Join(filepath.Dir(p), x)
		}
		if supported[strings.ToLower(filepath.Ext(x))] && strings.ToLower(filepath.Ext(x)) != ".m3u" && strings.ToLower(filepath.Ext(x)) != ".m3u8" {
			out = append(out, x)
		}
	}
	return out
}
func buildQueue(path string, external bool) Queue {
	var ps []string
	idx := 0
	if external {
		if st, err := os.Stat(path); err == nil && st.IsDir() {
			ps = audioFiles(path)
		} else {
			ps = []string{path}
		}
	} else {
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".m3u" || ext == ".m3u8" {
			ps = parseM3U(path)
		} else {
			ps = audioFiles(filepath.Dir(path))
			for i, p := range ps {
				if p == path {
					idx = i
				}
			}
		}
	}
	q := Queue{Index: idx}
	for _, p := range ps {
		q.Tracks = append(q.Tracks, basicTrack(p))
	}
	return q
}

func buildQueueFromChoice(choice browseChoice) Queue {
	if !choice.UseDirFD || choice.DirFD < 0 {
		return buildQueue(choice.Path, false)
	}
	ext := strings.ToLower(filepath.Ext(choice.Name))
	if ext == ".m3u" || ext == ".m3u8" {
		_ = syscall.Close(choice.DirFD)
		return buildQueue(choice.Path, false)
	}
	queueFD, err := syscall.Openat(choice.DirFD, ".", syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		_ = syscall.Close(choice.DirFD)
		return Queue{}
	}
	df := os.NewFile(uintptr(queueFD), choice.Dir)
	entries, err := df.ReadDir(-1)
	_ = df.Close()
	if err != nil {
		_ = syscall.Close(choice.DirFD)
		return Queue{}
	}
	names := make([]string, 0, len(entries))
	for _, x := range entries {
		if x.IsDir() {
			continue
		}
		e := strings.ToLower(filepath.Ext(x.Name()))
		if e == ".mp3" || e == ".wav" || e == ".flac" {
			names = append(names, x.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	q := Queue{DirFD: choice.DirFD, UseDirFD: true}
	for i, name := range names {
		if name == choice.Name {
			q.Index = i
		}
		q.Tracks = append(q.Tracks, basicTrackAt(choice.DirFD, choice.Dir, name))
	}
	return q
}

func smbShareName(s SMBShare) string {
	if strings.TrimSpace(s.Name) != "" {
		return strings.TrimSpace(s.Name)
	}
	return strings.TrimSpace(s.Server + "/" + s.Share)
}

func resolveSMBExternal(raw string) (string, error) {
	rest := strings.TrimPrefix(raw, "smb://")
	parts := strings.SplitN(rest, "/", 2)
	shareName, e := url.PathUnescape(parts[0])
	if e != nil {
		return "", e
	}
	shareName = strings.TrimSpace(shareName)
	if shareName == "" {
		return "", errors.New("SMB share name is missing")
	}
	cfg := loadSMB()
	idx := -1
	for i, sh := range cfg.Shares {
		if strings.EqualFold(smbShareName(sh), shareName) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", fmt.Errorf("SMB share not configured: %s", shareName)
	}
	root, e := mountShare(cfg.Shares[idx])
	if e != nil {
		return "", e
	}
	if len(parts) == 1 || strings.TrimSpace(parts[1]) == "" {
		return root, nil
	}
	rel, e := url.PathUnescape(parts[1])
	if e != nil {
		return "", e
	}
	rel = strings.TrimPrefix(filepath.Clean("/"+filepath.FromSlash(rel)), string(filepath.Separator))
	target := filepath.Join(root, rel)
	cleanRoot := filepath.Clean(root)
	cleanTarget := filepath.Clean(target)
	if cleanTarget != cleanRoot && !strings.HasPrefix(cleanTarget, cleanRoot+string(filepath.Separator)) {
		return "", errors.New("SMB path escapes configured share")
	}
	return cleanTarget, nil
}

func normalizeExternalTarget(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 2 {
		if (raw[0] == '"' && raw[len(raw)-1] == '"') || (raw[0] == '\'' && raw[len(raw)-1] == '\'') {
			raw = strings.TrimSpace(raw[1 : len(raw)-1])
		}
	}
	return raw
}

func externalArg(args []string) string {
	return normalizeExternalTarget(strings.Join(args, " "))
}

func resolveExternalTarget(raw string) (string, error) {
	raw = normalizeExternalTarget(raw)
	if strings.HasPrefix(strings.ToLower(raw), "smb://") {
		return resolveSMBExternal(raw)
	}
	if raw == "" {
		return "", errors.New("empty playback target")
	}
	return raw, nil
}

func externalQueue(raw string) (Queue, error) {
	path, e := resolveExternalTarget(raw)
	if e != nil {
		return Queue{}, e
	}
	st, e := os.Stat(path)
	if e != nil {
		return Queue{}, errors.New("file or folder not found")
	}
	if st.IsDir() {
		q := buildQueue(path, true)
		if len(q.Tracks) == 0 {
			return Queue{}, errors.New("no supported audio files in folder")
		}
		return q, nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	if !supported[ext] {
		return Queue{}, errors.New("unsupported file type")
	}
	q := buildQueue(path, true)
	if len(q.Tracks) == 0 {
		if ext == ".m3u" || ext == ".m3u8" {
			return Queue{}, errors.New("playlist contains no supported tracks")
		}
		return Queue{}, errors.New("unable to build playback queue")
	}
	return q, nil
}

func sendExternal(raw string) error {
	c, e := net.DialTimeout("unix", socketPath, 350*time.Millisecond)
	if e != nil {
		return e
	}
	defer c.Close()
	_, e = io.WriteString(c, raw)
	return e
}

func externalListener(ch chan<- string) (net.Listener, error) {
	_ = os.Remove(socketPath)
	ln, e := net.Listen("unix", socketPath)
	if e != nil {
		return nil, e
	}
	_ = os.Chmod(socketPath, 0666)
	go func() {
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				b, _ := io.ReadAll(io.LimitReader(conn, 64*1024))
				v := strings.TrimSpace(string(b))
				if v != "" {
					ch <- v
				}
			}(c)
		}
	}()
	return ln, nil
}

type Player struct {
	mu                 sync.Mutex
	q                  Queue
	paused             bool
	stopped            bool
	stop               chan struct{}
	levels             [10]float64
	basePosition       float64
	cfg                Config
	gaplessQueuedIndex int
}

func newPlayer(q Queue, cfg Config) *Player {
	return &Player{q: q, cfg: cfg, stopped: true, gaplessQueuedIndex: -1}
}
func (p *Player) loadCurrentMetadata(index int, src Track) {
	if strings.HasPrefix(src.Path, "cdda:") {
		return
	}
	go func() {
		t := trackFromTrack(src)
		p.mu.Lock()
		if index >= 0 && index < len(p.q.Tracks) && p.q.Tracks[index].Path == src.Path {
			p.q.Tracks[index] = t
		}
		p.mu.Unlock()
	}()
}
func (p *Player) current() *Track {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.q.Index < 0 || p.q.Index >= len(p.q.Tracks) {
		return nil
	}
	t := p.q.Tracks[p.q.Index]
	return &t
}
func gaplessTrackSupported(t Track) bool {
	if strings.HasPrefix(t.Path, "cdda:") {
		return true
	}
	switch strings.ToLower(filepath.Ext(t.Path)) {
	case ".flac", ".wav":
		return true
	}
	return false
}

func (p *Player) nextQueueIndexLocked(from int) int {
	if len(p.q.Tracks) == 0 || from < 0 || from >= len(p.q.Tracks) {
		return -1
	}
	if p.q.Repeat {
		return from
	}
	if p.q.Shuffle {
		return -1
	}
	if from+1 < len(p.q.Tracks) {
		return from + 1
	}
	return -1
}

func (p *Player) prepareGaplessNextFile() {
	p.mu.Lock()
	if !p.cfg.GaplessPlayback || p.stopped || p.q.Index < 0 || p.q.Index >= len(p.q.Tracks) {
		p.gaplessQueuedIndex = -1
		p.mu.Unlock()
		return
	}
	current := p.q.Tracks[p.q.Index]
	nextIndex := p.nextQueueIndexLocked(p.q.Index)
	if nextIndex < 0 || nextIndex >= len(p.q.Tracks) {
		p.gaplessQueuedIndex = -1
		p.mu.Unlock()
		return
	}
	next := p.q.Tracks[nextIndex]
	if strings.HasPrefix(current.Path, "cdda:") || strings.HasPrefix(next.Path, "cdda:") ||
		!gaplessTrackSupported(current) || !gaplessTrackSupported(next) {
		p.gaplessQueuedIndex = -1
		p.mu.Unlock()
		return
	}
	p.gaplessQueuedIndex = nextIndex
	p.mu.Unlock()
	if err := nativeAudioQueueNextTrack(next); err != nil {
		p.mu.Lock()
		if p.gaplessQueuedIndex == nextIndex {
			p.gaplessQueuedIndex = -1
		}
		p.mu.Unlock()
	}
}

func (p *Player) handleGaplessTransition() {
	p.mu.Lock()
	nextIndex := p.gaplessQueuedIndex
	if nextIndex < 0 || nextIndex >= len(p.q.Tracks) {
		p.mu.Unlock()
		return
	}
	p.q.Index = nextIndex
	t := p.q.Tracks[nextIndex]
	p.gaplessQueuedIndex = -1
	p.basePosition = 0
	p.paused = false
	p.stopped = false
	p.mu.Unlock()
	if !strings.HasPrefix(t.Path, "cdda:") {
		p.loadCurrentMetadata(nextIndex, t)
		p.prepareGaplessNextFile()
	}
}

func (p *Player) playCurrent() error {
	p.stopPlaybackRaw()
	p.mu.Lock()
	if p.q.Index < 0 || p.q.Index >= len(p.q.Tracks) {
		p.mu.Unlock()
		return errors.New("empty queue")
	}
	idx := p.q.Index
	t := p.q.Tracks[idx]
	cfg := p.cfg
	stop := make(chan struct{})
	p.stop = stop
	p.paused = false
	p.stopped = false
	p.basePosition = 0
	p.gaplessQueuedIndex = -1
	p.mu.Unlock()
	var err error
	if strings.HasPrefix(t.Path, "cdda:") {
		err = p.playCDTrack(t, stop)
	} else {
		err = nativeAudioStartTrack(t, cfg.EQ)
		if err == nil {
			p.loadCurrentMetadata(idx, t)
		}
	}
	if err != nil {
		p.mu.Lock()
		if p.stop == stop {
			p.stop = nil
			p.stopped = true
		}
		p.mu.Unlock()
		return err
	}
	if !strings.HasPrefix(t.Path, "cdda:") {
		p.prepareGaplessNextFile()
	}
	go p.monitorPlayback(stop)
	return nil
}
func (p *Player) monitorPlayback(stop <-chan struct{}) {
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			lv := nativeAudioLevels()
			p.mu.Lock()
			p.levels = lv
			p.mu.Unlock()
			if nativeAudioTakeTransition() {
				p.handleGaplessTransition()
			}
			if nativeAudioEnded() {
				p.advance()
				return
			}
		}
	}
}
func parseCDDAPath(path string) (string, int32, int32, error) {
	x := strings.TrimPrefix(path, "cdda:")
	i2 := strings.LastIndex(x, ":")
	if i2 < 0 {
		return "", 0, 0, errors.New("invalid CD track")
	}
	end64, e := strconv.ParseInt(x[i2+1:], 10, 32)
	if e != nil {
		return "", 0, 0, e
	}
	x = x[:i2]
	i1 := strings.LastIndex(x, ":")
	if i1 < 0 {
		return "", 0, 0, errors.New("invalid CD track")
	}
	start64, e := strconv.ParseInt(x[i1+1:], 10, 32)
	if e != nil {
		return "", 0, 0, e
	}
	return x[:i1], int32(start64), int32(end64), nil
}
func (p *Player) playCDTrack(t Track, stop <-chan struct{}) error {
	dev, start, end, e := parseCDDAPath(t.Path)
	if e != nil {
		return e
	}
	p.mu.Lock()
	offset := p.basePosition
	startIndex := p.q.Index
	gapless := p.cfg.GaplessPlayback
	p.mu.Unlock()
	if offset > 0 {
		start += int32(offset * 75.0)
		if start >= end {
			start = end - 1
		}
	}
	f, e := os.Open(dev)
	if e != nil {
		return e
	}
	if e = nativeAudioStartPCM(p.cfg.EQ); e != nil {
		f.Close()
		return e
	}
	go func() {
		defer f.Close()
		currentIndex := startIndex
		currentDev := dev
		currentStart := start
		currentEnd := end
		buf := make([]byte, cdFrameBytes*16)
		for {
			lba := currentStart
			for lba < currentEnd {
				select {
				case <-stop:
					return
				default:
				}
				nf := int32(16)
				if lba+nf > currentEnd {
					nf = currentEnd - lba
				}
				n := int(nf) * cdFrameBytes
				r := cdReadAudio{Addr: lba, AddrFormat: cdromLBAMode, Frames: nf, Buf: uintptr(unsafe.Pointer(&buf[0]))}
				if err := cdIoctl(f.Fd(), cdromReadAudio, unsafe.Pointer(&r)); err != nil {
					nativeAudioFinishPCM()
					return
				}
				if err := nativeAudioWritePCM(buf[:n]); err != nil {
					nativeAudioFinishPCM()
					return
				}
				lba += nf
			}

			if !gapless {
				nativeAudioFinishPCM()
				return
			}

			p.mu.Lock()
			nextIndex := p.nextQueueIndexLocked(currentIndex)
			if nextIndex < 0 || nextIndex >= len(p.q.Tracks) || !gaplessTrackSupported(p.q.Tracks[nextIndex]) ||
				!strings.HasPrefix(p.q.Tracks[nextIndex].Path, "cdda:") {
				p.gaplessQueuedIndex = -1
				p.mu.Unlock()
				nativeAudioFinishPCM()
				return
			}
			nextTrack := p.q.Tracks[nextIndex]
			p.gaplessQueuedIndex = nextIndex
			p.mu.Unlock()

			nextDev, nextStart, nextEnd, err := parseCDDAPath(nextTrack.Path)
			if err != nil || nextDev != currentDev {
				p.mu.Lock()
				if p.gaplessQueuedIndex == nextIndex {
					p.gaplessQueuedIndex = -1
				}
				p.mu.Unlock()
				nativeAudioFinishPCM()
				return
			}
			if err := nativeAudioMarkPCMTransition(nextTrack.Duration); err != nil {
				p.mu.Lock()
				if p.gaplessQueuedIndex == nextIndex {
					p.gaplessQueuedIndex = -1
				}
				p.mu.Unlock()
				nativeAudioFinishPCM()
				return
			}
			currentIndex = nextIndex
			currentStart = nextStart
			currentEnd = nextEnd
		}
	}()
	return nil
}

func (p *Player) stopPlaybackRaw() {
	p.mu.Lock()
	if p.stop != nil {
		select {
		case <-p.stop:
		default:
			close(p.stop)
		}
	}
	p.stop = nil
	p.mu.Unlock()
	nativeAudioStop()
}
func (p *Player) stopAndReset() {
	p.stopPlaybackRaw()
	p.mu.Lock()
	p.paused = false
	p.stopped = true
	p.basePosition = 0
	p.levels = [10]float64{}
	p.gaplessQueuedIndex = -1
	p.mu.Unlock()
}
func (a *App) stopAndUnload() {
	if a.player == nil {
		return
	}
	if a.origin != nil {
		if t := a.player.current(); t != nil {
			if a.origin.Kind == "disc" {
				a.origin.Selected = t.Title
			} else if !strings.HasPrefix(t.Path, "cdda:") && filepath.Clean(filepath.Dir(t.Path)) == filepath.Clean(a.origin.Dir) {
				a.origin.Selected = filepath.Base(t.Path)
			}
		}
	}
	p := a.player
	a.player = nil
	p.stopAndReset()
	closeQueueDirFD(&p.q)
}
func (p *Player) advance() {
	p.mu.Lock()
	if len(p.q.Tracks) == 0 {
		p.mu.Unlock()
		return
	}
	if p.q.Repeat {
	} else if p.q.Shuffle && len(p.q.Tracks) > 1 {
		n := len(p.q.Tracks)
		next := int(time.Now().UnixNano() % int64(n-1))
		if next >= p.q.Index {
			next++
		}
		p.q.Index = next
	} else if p.q.Index+1 < len(p.q.Tracks) {
		p.q.Index++
	} else {
		p.stop = nil
		p.paused = false
		p.stopped = true
		p.levels = [10]float64{}
		p.mu.Unlock()
		nativeAudioStop()
		return
	}
	p.mu.Unlock()
	_ = p.playCurrent()
}
func (p *Player) prev() {
	p.mu.Lock()
	if len(p.q.Tracks) > 0 {
		p.q.Index = (p.q.Index - 1 + len(p.q.Tracks)) % len(p.q.Tracks)
	}
	p.mu.Unlock()
	_ = p.playCurrent()
}
func (p *Player) next() {
	p.mu.Lock()
	if len(p.q.Tracks) > 0 {
		if p.q.Shuffle && len(p.q.Tracks) > 1 {
			n := len(p.q.Tracks)
			next := int(time.Now().UnixNano() % int64(n-1))
			if next >= p.q.Index {
				next++
			}
			p.q.Index = next
		} else {
			p.q.Index = (p.q.Index + 1) % len(p.q.Tracks)
		}
	}
	p.mu.Unlock()
	_ = p.playCurrent()
}
func (p *Player) togglePause() {
	p.mu.Lock()
	stopped := p.stopped
	paused := p.paused
	p.mu.Unlock()
	if stopped {
		_ = p.playCurrent()
		return
	}
	paused = !paused
	p.mu.Lock()
	p.paused = paused
	p.mu.Unlock()
	nativeAudioPause(paused)
}
func (p *Player) seekBy(seconds float64) {
	p.mu.Lock()
	if p.stopped || p.q.Index < 0 || p.q.Index >= len(p.q.Tracks) {
		p.mu.Unlock()
		return
	}
	t := p.q.Tracks[p.q.Index]
	p.mu.Unlock()
	target := p.elapsedNow().Seconds() + seconds
	if target < 0 {
		target = 0
	}
	if t.Duration > 0 && target > t.Duration {
		target = t.Duration
	}
	if !strings.HasPrefix(t.Path, "cdda:") {
		_ = nativeAudioSeek(target)
		return
	}
	p.stopPlaybackRaw()
	stop := make(chan struct{})
	p.mu.Lock()
	p.stop = stop
	p.paused = false
	p.stopped = false
	p.basePosition = target
	p.mu.Unlock()
	if err := p.playCDTrack(t, stop); err != nil {
		p.stopAndReset()
		return
	}
	go p.monitorPlayback(stop)
}
func (p *Player) elapsedNow() time.Duration {
	p.mu.Lock()
	base := p.basePosition
	p.mu.Unlock()
	return time.Duration((base + nativeAudioPosition()) * float64(time.Second))
}

func (p *Player) durationNow() time.Duration {
	p.mu.Lock()
	if p.q.Index >= 0 && p.q.Index < len(p.q.Tracks) && p.q.Tracks[p.q.Index].Duration > 0 {
		d := p.q.Tracks[p.q.Index].Duration
		p.mu.Unlock()
		return time.Duration(d * float64(time.Second))
	}
	p.mu.Unlock()
	return time.Duration(nativeAudioDuration() * float64(time.Second))
}

type browseOrigin struct {
	Root, Dir, Selected string
	Kind                string
}

type App struct {
	fb          *framebuffer
	acts        <-chan action
	external    <-chan string
	cfg         *Config
	player      *Player
	origin      *browseOrigin
	jumpSources bool
}

func appBackground(cfg *Config) color.RGBA {
	if cfg != nil && cfg.OLEDMode {
		return color.RGBA{0, 0, 0, 255}
	}
	return color.RGBA{8, 9, 13, 255}
}

func playerBackground(cfg *Config) color.RGBA {
	if cfg != nil && cfg.OLEDMode {
		return color.RGBA{0, 0, 0, 255}
	}
	return color.RGBA{7, 8, 11, 255}
}

func effectiveHideAlbumArt(t Track, cfg *Config) bool {
	if cfg == nil {
		return false
	}
	return cfg.HideAlbumArt || (cfg.AutoHideMissingArt && t.Art == nil)
}

func clockText(cfg *Config) string {
	if cfg == nil || !cfg.ShowClock {
		return ""
	}
	return time.Now().Format("15:04")
}

func drawClock(fb *framebuffer, cfg *Config, bg color.RGBA) (int, int, int, int) {
	txt := clockText(cfg)
	scale := max(1, fb.h/360)
	margin := max(18, fb.w/64)
	pad := max(8, scale*4)
	h := scale*7 + pad*2
	w := tw(scale, "88:88") + pad*2
	x := fb.w - margin - w
	y := max(10, fb.h/54)
	fb.rect(x, y, w, h, bg)
	if txt != "" {
		fb.text(x+w-pad-tw(scale, txt), y+pad, scale, txt, color.RGBA{245, 245, 245, 255})
	}
	return x, y, w, h
}

func closeQueueDirFD(q *Queue) {
	if q == nil || !q.UseDirFD || q.DirFD < 0 {
		return
	}
	fd := q.DirFD
	q.DirFD = -1
	q.UseDirFD = false
	for i := range q.Tracks {
		if q.Tracks[i].UseDirFD && q.Tracks[i].DirFD == fd {
			q.Tracks[i].DirFD = -1
			q.Tracks[i].UseDirFD = false
		}
	}
	_ = syscall.Close(fd)
}

func (a *App) startQueue(q Queue, origin *browseOrigin) error {
	if a.player != nil {
		old := a.player
		a.player = nil
		a.origin = nil
		old.stopPlaybackRaw()
		closeQueueDirFD(&old.q)
	}
	p := newPlayer(q, *a.cfg)
	if err := p.playCurrent(); err != nil {
		closeQueueDirFD(&q)
		return err
	}
	a.player = p
	a.origin = origin
	return nil
}

func (a *App) startExternal(raw string) error {
	q, e := externalQueue(raw)
	if e != nil {
		return e
	}
	return a.startQueue(q, nil)
}

func (a *App) nowPlayingText() string {
	if a.player == nil {
		return ""
	}
	a.player.mu.Lock()
	defer a.player.mu.Unlock()
	if len(a.player.q.Tracks) == 0 || a.player.q.Index < 0 || a.player.q.Index >= len(a.player.q.Tracks) {
		return ""
	}
	prefix := "NOW PLAYING"
	if a.player.stopped {
		prefix = "LOADED"
	} else if a.player.paused {
		prefix = "PAUSED"
	}
	t := a.player.q.Tracks[a.player.q.Index]
	name := t.Title
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(t.Path), filepath.Ext(t.Path))
	}
	if t.Artist != "" {
		name += " - " + t.Artist
	}
	return prefix + ": " + name
}

func (a *App) handlePlaybackShortcut(act action) bool {
	if a.player == nil {
		return false
	}
	switch act {
	case actPrev:
		a.player.prev()
		return true
	case actNext:
		a.player.next()
		return true
	case actPlayPause:
		a.player.togglePause()
		return true
	case actStop:
		a.stopAndUnload()
		return true
	}
	return false
}

func drawTitle(fb *framebuffer, s string) {
	white := color.RGBA{245, 245, 245, 255}
	fb.text(30, 22, 3, s, white)
	fb.rect(30, 52, fb.w-60, 2, color.RGBA{70, 70, 78, 255})
}

func drawCircleOutline(fb *framebuffer, cx, cy, r, thick int, c color.RGBA) {
	if r < 2 {
		r = 2
	}
	steps := max(20, r*2)
	px := cx + r
	py := cy
	for i := 1; i <= steps; i++ {
		a := 2 * math.Pi * float64(i) / float64(steps)
		x := cx + int(math.Round(math.Cos(a)*float64(r)))
		y := cy + int(math.Round(math.Sin(a)*float64(r)))
		drawLine(fb, px, py, x, y, thick, c)
		px, py = x, y
	}
}

func drawFooterArrow(fb *framebuffer, cx, cy, size int, dir action, c color.RGBA) {
	t := max(1, size/8)
	h := size / 2
	switch dir {
	case actUp:
		drawLine(fb, cx, cy+h/2, cx, cy-h/2, t, c)
		drawLine(fb, cx, cy-h/2, cx-h/3, cy-h/6, t, c)
		drawLine(fb, cx, cy-h/2, cx+h/3, cy-h/6, t, c)
	case actDown:
		drawLine(fb, cx, cy-h/2, cx, cy+h/2, t, c)
		drawLine(fb, cx, cy+h/2, cx-h/3, cy+h/6, t, c)
		drawLine(fb, cx, cy+h/2, cx+h/3, cy+h/6, t, c)
	case actLeft:
		drawLine(fb, cx+h/2, cy, cx-h/2, cy, t, c)
		drawLine(fb, cx-h/2, cy, cx-h/6, cy-h/3, t, c)
		drawLine(fb, cx-h/2, cy, cx-h/6, cy+h/3, t, c)
	case actRight:
		drawLine(fb, cx-h/2, cy, cx+h/2, cy, t, c)
		drawLine(fb, cx+h/2, cy, cx+h/6, cy-h/3, t, c)
		drawLine(fb, cx+h/2, cy, cx+h/6, cy+h/3, t, c)
	}
}

func drawFooterButton(fb *framebuffer, cx, cy, size int, label string, c color.RGBA) {
	r := size / 2
	drawCircleOutline(fb, cx, cy, r, max(1, size/10), c)
	fs := max(1, size/12)
	fb.text(cx-tw(fs, label)/2, cy-7*fs/2, fs, label, c)
}

func drawFooterHome(fb *framebuffer, cx, cy, size int, c color.RGBA) {
	t := max(1, size/9)
	h := size / 2
	drawLine(fb, cx-h, cy, cx, cy-h, t, c)
	drawLine(fb, cx, cy-h, cx+h, cy, t, c)
	drawLine(fb, cx-h*3/4, cy, cx-h*3/4, cy+h, t, c)
	drawLine(fb, cx+h*3/4, cy, cx+h*3/4, cy+h, t, c)
	drawLine(fb, cx-h*3/4, cy+h, cx+h*3/4, cy+h, t, c)
}

func drawFooterPair(fb *framebuffer, x, cy, iconSize, textScale int, kind, label string, c color.RGBA) int {
	cx := x + iconSize/2
	switch kind {
	case "ud":
		drawFooterArrow(fb, cx, cy-iconSize/5, iconSize*3/4, actUp, c)
		drawFooterArrow(fb, cx, cy+iconSize/5, iconSize*3/4, actDown, c)
	case "lr":
		drawFooterArrow(fb, cx-iconSize/5, cy, iconSize*3/4, actLeft, c)
		drawFooterArrow(fb, cx+iconSize/5, cy, iconSize*3/4, actRight, c)
	case "a", "b":
		drawFooterButton(fb, cx, cy, iconSize, strings.ToUpper(kind), c)
	case "home":
		drawFooterHome(fb, cx, cy, iconSize, c)
	}
	tx := x + iconSize + max(5, iconSize/5)
	fb.text(tx, cy-7*textScale/2, textScale, label, c)
	return tx + tw(textScale, label) + max(14, iconSize*2/5)
}

func drawBrowserFooter(fb *framebuffer, jump bool) {
	c := color.RGBA{160, 160, 165, 255}
	ts := max(1, fb.h/540)
	icon := max(14, 14*ts)
	cy := fb.h - max(18, icon/2+7)
	x := 30
	x = drawFooterPair(fb, x, cy, icon, ts, "ud", "NAVIGATE", c)
	if jump {
		x = drawFooterPair(fb, x, cy, icon, ts, "lr", "JUMP 5", c)
	}
	x = drawFooterPair(fb, x, cy, icon, ts, "a", "OPEN", c)
	x = drawFooterPair(fb, x, cy, icon, ts, "b", "BACK", c)
	_ = drawFooterPair(fb, x, cy, icon, ts, "home", "SOURCES", c)
}

func drawEQFooter(fb *framebuffer) {
	c := color.RGBA{160, 160, 165, 255}
	ts := max(1, fb.h/540)
	icon := max(14, 14*ts)
	cy := fb.h - max(18, icon/2+7)
	x := 30
	x = drawFooterPair(fb, x, cy, icon, ts, "ud", "NAVIGATE", c)
	x = drawFooterPair(fb, x, cy, icon, ts, "lr", "ADJUST", c)
	x = drawFooterPair(fb, x, cy, icon, ts, "a", "SELECT", c)
	x = drawFooterPair(fb, x, cy, icon, ts, "b", "BACK", c)
	_ = drawFooterPair(fb, x, cy, icon, ts, "home", "SOURCES", c)
}

func drawMessageFooter(fb *framebuffer) {
	c := color.RGBA{160, 160, 165, 255}
	ts := max(1, fb.h/540)
	icon := max(14, 14*ts)
	cy := fb.h - max(18, icon/2+7)
	x := 40
	x = drawFooterPair(fb, x, cy, icon, ts, "a", "BACK", c)
	x = drawFooterPair(fb, x, cy, icon, ts, "b", "BACK", c)
	_ = drawFooterPair(fb, x, cy, icon, ts, "home", "SOURCES", c)
}

func drawNowPlayingBar(app *App, selected bool) int {
	if app.player == nil {
		return 0
	}
	fb := app.fb
	h := max(46, fb.h/15)
	y := fb.h - h - 42
	bg := color.RGBA{24, 26, 33, 255}
	fb.rect(30, y, fb.w-60, h, bg)
	if selected {
		fb.border(30, y, fb.w-60, h, 2, color.RGBA{245, 245, 245, 255})
	}
	scale := max(1, h/24)
	label := short(app.nowPlayingText(), max(20, (fb.w-100)/(6*scale)))
	fb.text(50, y+(h-7*scale)/2, scale, label, color.RGBA{225, 225, 230, 255})
	return h
}

func menu(app *App, title string, items []string, initial int) (int, bool) {
	fb, acts := app.fb, app.acts
	clockTick := time.NewTicker(30 * time.Second)
	defer clockTick.Stop()
	sel := initial
	if sel < 0 || sel >= len(items) {
		sel = 0
	}
	for {
		if app.jumpSources {
			return 0, false
		}
		hasBar := app.player != nil
		barSel := hasBar && sel == len(items)
		fb.fill(appBackground(app.cfg))
		drawTitle(fb, title)
		row := max(30, fb.h/10)
		reserve := 45
		if hasBar {
			reserve += max(46, fb.h/15) + 8
		}
		maxRows := (fb.h - 70 - reserve) / row
		if maxRows < 1 {
			maxRows = 1
		}
		itemSel := sel
		if itemSel >= len(items) {
			itemSel = len(items) - 1
		}
		if itemSel < 0 {
			itemSel = 0
		}
		first := 0
		if itemSel >= maxRows {
			first = itemSel - maxRows + 1
		}
		if first+maxRows > len(items) {
			first = len(items) - maxRows
			if first < 0 {
				first = 0
			}
		}
		y := 70
		for i := first; i < len(items) && i < first+maxRows; i++ {
			it := items[i]
			if it == "" {
				y += max(1, row/2)
				continue
			}
			if i == sel {
				fb.rect(45, y-5, fb.w-90, row-5, color.RGBA{35, 37, 46, 255})
				fb.border(45, y-5, fb.w-90, row-5, 2, color.RGBA{240, 240, 240, 255})
			}
			fb.text(65, y+6, max(1, row/22), it, color.RGBA{235, 235, 235, 255})
			y += row
		}
		if hasBar {
			drawNowPlayingBar(app, barSel)
		}
		drawBrowserFooter(fb, true)
		drawClock(fb, app.cfg, appBackground(app.cfg))
		fb.present()
		var a action
		select {
		case <-clockTick.C:
			continue
		case a = <-acts:
		case raw := <-app.external:
			if err := app.startExternal(raw); err == nil {
				playerUI(app)
				app.jumpSources = true
				return 0, false
			}
			continue
		}
		if a == actSources {
			app.jumpSources = true
			return 0, false
		}
		if a == actNowPlaying {
			if app.player != nil {
				playerUI(app)
				if app.jumpSources {
					return 0, false
				}
			}
			continue
		}
		if app.handlePlaybackShortcut(a) {
			continue
		}
		switch a {
		case actUp:
			if barSel {
				for i := len(items) - 1; i >= 0; i-- {
					if items[i] != "" {
						sel = i
						break
					}
				}
			} else {
				for i := sel - 1; i >= 0; i-- {
					if items[i] != "" {
						sel = i
						break
					}
				}
			}
		case actDown:
			moved := false
			for i := sel + 1; i < len(items); i++ {
				if items[i] != "" {
					sel = i
					moved = true
					break
				}
			}
			if !moved && hasBar && !barSel {
				sel = len(items)
			}
		case actLeft:
			if !barSel {
				sel -= 5
				if sel < 0 {
					sel = 0
				}
				for sel > 0 && items[sel] == "" {
					sel--
				}
			}
		case actRight:
			if !barSel {
				sel += 5
				if sel >= len(items) {
					sel = len(items) - 1
				}
				for sel < len(items)-1 && items[sel] == "" {
					sel++
				}
			}
		case actConfirm:
			if !barSel && sel >= 0 && sel < len(items) && items[sel] == "" {
				continue
			}
			if barSel {
				playerUI(app)
				if app.jumpSources {
					return 0, false
				}
				continue
			}
			return sel, true
		case actBack:
			return 0, false
		}
	}
}

func settleInput(acts <-chan action, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	for {
		select {
		case <-acts:
		case <-t.C:
			for {
				select {
				case <-acts:
				default:
					return
				}
			}
		}
	}
}

type browseChoice struct {
	Path, Dir, Name string
	DirFD           int
	UseDirFD        bool
}

type browseEntry struct {
	Name  string
	Path  string
	IsDir bool
}

func openDirChild(parent *os.File, name string) (*os.File, error) {
	fd, err := syscall.Openat(int(parent.Fd()), name, syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func openDirChain(root, target string) ([]*os.File, []string, error) {
	rootFile, err := os.Open(root)
	if err != nil {
		return nil, nil, err
	}
	files := []*os.File{rootFile}
	paths := []string{root}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return files, paths, nil
	}
	curPath := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		child, e := openDirChild(files[len(files)-1], part)
		if e != nil {
			for _, f := range files {
				_ = f.Close()
			}
			rootFile, e2 := os.Open(root)
			if e2 != nil {
				return nil, nil, e
			}
			return []*os.File{rootFile}, []string{root}, nil
		}
		files = append(files, child)
		curPath = filepath.Join(curPath, part)
		paths = append(paths, curPath)
	}
	return files, paths, nil
}

func browse(app *App, root, startDir, initialName string) (browseChoice, bool) {
	settleInput(app.acts, 180*time.Millisecond)
	if startDir == "" {
		startDir = root
	}
	stack, pathStack, err := openDirChain(root, startDir)
	if err != nil {
		return browseChoice{}, false
	}
	defer func() {
		for _, f := range stack {
			_ = f.Close()
		}
	}()
	focusName := initialName
	for {
		cur := stack[len(stack)-1]
		dir := pathStack[len(pathStack)-1]
		_, _ = cur.Seek(0, io.SeekStart)
		es, e := cur.ReadDir(-1)
		if e != nil {
			return browseChoice{}, false
		}
		entries := make([]browseEntry, 0, len(es))
		for _, x := range es {
			name := x.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			isDir := x.IsDir()
			if isDir || supported[strings.ToLower(filepath.Ext(name))] {
				entries = append(entries, browseEntry{Name: name, Path: filepath.Join(dir, name), IsDir: isDir})
			}
		}
		sort.Slice(entries, func(i, j int) bool { return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name) })
		items := make([]string, 1, len(entries)+1)
		items[0] = "[..]"
		for _, entry := range entries {
			items = append(items, entry.Name)
		}
		initial := 0
		if focusName != "" {
			for i, entry := range entries {
				if entry.Name == focusName {
					initial = i + 1
					break
				}
			}
		}
		focusName = ""
		i, ok := menu(app, "BROWSE: "+short(dir, 32), items, initial)
		if !ok {
			if app.jumpSources || len(stack) == 1 {
				return browseChoice{}, false
			}
			focusName = filepath.Base(dir)
			_ = stack[len(stack)-1].Close()
			stack = stack[:len(stack)-1]
			pathStack = pathStack[:len(pathStack)-1]
			continue
		}
		if i == 0 {
			if len(stack) == 1 {
				return browseChoice{}, false
			}
			focusName = filepath.Base(dir)
			_ = stack[len(stack)-1].Close()
			stack = stack[:len(stack)-1]
			pathStack = pathStack[:len(pathStack)-1]
			continue
		}
		entry := entries[i-1]
		if entry.IsDir {
			child, e := openDirChild(cur, entry.Name)
			if e != nil {
				message(app, "BROWSE ERROR", e.Error())
				continue
			}
			stack = append(stack, child)
			pathStack = append(pathStack, entry.Path)
			continue
		}
		dupFD, e := syscall.Dup(int(cur.Fd()))
		if e != nil {
			message(app, "BROWSE ERROR", e.Error())
			continue
		}
		return browseChoice{Path: entry.Path, Dir: dir, Name: entry.Name, DirFD: dupFD, UseDirFD: true}, true
	}
}

func short(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "..." + string(r[len(r)-n+3:])
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func playerScale(fb *framebuffer) float64 {
	sx := float64(fb.w) / 1920.0
	sy := float64(fb.h) / 1080.0
	s := math.Min(sx, sy)
	if s < 0.16 {
		s = 0.16
	}
	return s
}

func scaledPx(scale float64, v int) int {
	n := int(math.Round(float64(v) * scale))
	if n < 1 {
		return 1
	}
	return n
}

func scaledFont(scale float64, v int) int {
	n := int(math.Round(float64(v) * scale))
	if n < 1 {
		return 1
	}
	return n
}

type playerLayout struct {
	margin, top, artSize int
	rightX, rightW       int
	vizY, vizH           int
	timeY, barY, barH    int
	ctrlY                int
	scale                float64
}

func makePlayerLayout(fb *framebuffer, t Track, cfg *Config) playerLayout {
	scale := playerScale(fb)
	margin := scaledPx(scale, 60)
	top := scaledPx(scale, 115)
	bottomSafe := scaledPx(scale, 70)
	gapPanels := scaledPx(scale, 60)
	artSize := scaledPx(scale, 560)
	maxArtH := fb.h - top - bottomSafe - scaledPx(scale, 170)
	if artSize > maxArtH {
		artSize = maxArtH
	}
	maxArtW := fb.w * 36 / 100
	if artSize > maxArtW {
		artSize = maxArtW
	}
	if artSize < scaledPx(scale, 180) {
		artSize = scaledPx(scale, 180)
	}
	rightX := margin + artSize + gapPanels
	rightW := fb.w - rightX - margin
	if effectiveHideAlbumArt(t, cfg) {
		artSize = 0
		rightX = margin
		rightW = fb.w - margin*2
	} else if rightW < scaledPx(scale, 520) {
		rightX = fb.w * 38 / 100
		rightW = fb.w - rightX - margin
		artSize = rightX - margin - gapPanels
		if artSize > maxArtH {
			artSize = maxArtH
		}
	}
	metaGap := scaledPx(scale, 52)
	bodyGap := scaledPx(scale, 36)
	metaY := top + metaGap
	if t.Artist != "" {
		metaY += bodyGap
	}
	if t.Album != "" {
		metaY += bodyGap
	}
	metaY += bodyGap
	vizY := top + scaledPx(scale, 155)
	if metaY+scaledPx(scale, 22) > vizY {
		vizY = metaY + scaledPx(scale, 22)
	}
	controlsTop := fb.h - scaledPx(scale, 235)
	vizBottomLimit := controlsTop - scaledPx(scale, 105)
	vizH := scaledPx(scale, 280)
	if vizY+vizH > vizBottomLimit {
		vizH = vizBottomLimit - vizY
	}
	if vizH < scaledPx(scale, 85) {
		vizH = scaledPx(scale, 85)
	}
	timeY := vizY + vizH + scaledPx(scale, 24)
	barY := timeY + scaledPx(scale, 48)
	barH := scaledPx(scale, 5)
	ctrlY := fb.h - scaledPx(scale, 145)
	return playerLayout{margin: margin, top: top, artSize: artSize, rightX: rightX, rightW: rightW, vizY: vizY, vizH: vizH, timeY: timeY, barY: barY, barH: barH, ctrlY: ctrlY, scale: scale}
}

func playerSnapshot(p *Player) (Track, [10]float64, int, int, bool, bool, bool, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.q.Tracks) == 0 || p.q.Index < 0 || p.q.Index >= len(p.q.Tracks) {
		return Track{}, [10]float64{}, 0, 0, false, false, false, true
	}
	return p.q.Tracks[p.q.Index], p.levels, p.q.Index, len(p.q.Tracks), p.paused, p.q.Repeat, p.q.Shuffle, p.stopped
}

func drawLine(fb *framebuffer, x0, y0, x1, y1, thick int, c color.RGBA) {
	dx := int(math.Abs(float64(x1 - x0)))
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	dy := -int(math.Abs(float64(y1 - y0)))
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		fb.rect(x0-thick/2, y0-thick/2, thick, thick, c)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func drawPlayIcon(fb *framebuffer, cx, cy, size int, c color.RGBA) {
	h := size / 2
	for y := -h; y <= h; y++ {
		w := h - int(math.Abs(float64(y)))
		fb.rect(cx-h/3, cy+y, max(1, w), 2, c)
	}
}

func drawPauseIcon(fb *framebuffer, cx, cy, size int, c color.RGBA) {
	w := max(3, size/5)
	h := size
	gap := max(3, size/7)
	fb.rect(cx-gap/2-w, cy-h/2, w, h, c)
	fb.rect(cx+gap/2, cy-h/2, w, h, c)
}

func drawStopIcon(fb *framebuffer, cx, cy, size int, c color.RGBA) {
	s := size * 3 / 4
	fb.rect(cx-s/2, cy-s/2, s, s, c)
}

func drawPrevNextIcon(fb *framebuffer, cx, cy, size int, next bool, c color.RGBA) {
	barW := max(2, size/9)
	tri := size * 3 / 5
	if next {
		fb.rect(cx+size/3, cy-size/2, barW, size, c)
		for y := -tri / 2; y <= tri/2; y++ {
			w := tri/2 - int(math.Abs(float64(y)))
			fb.rect(cx-tri/3, cy+y, max(1, w), 2, c)
		}
	} else {
		fb.rect(cx-size/3-barW, cy-size/2, barW, size, c)
		for y := -tri / 2; y <= tri/2; y++ {
			w := tri/2 - int(math.Abs(float64(y)))
			fb.rect(cx+tri/3-w, cy+y, max(1, w), 2, c)
		}
	}
}

func drawShuffleIcon(fb *framebuffer, cx, cy, size int, c color.RGBA) {
	t := max(2, size/12)
	left := cx - size/2
	right := cx + size/2
	top := cy - size/3
	bot := cy + size/3
	drawLine(fb, left, top, cx-size/8, top, t, c)
	drawLine(fb, cx-size/8, top, cx+size/8, bot, t, c)
	drawLine(fb, cx+size/8, bot, right-size/8, bot, t, c)
	drawLine(fb, left, bot, cx-size/8, bot, t, c)
	drawLine(fb, cx-size/8, bot, cx+size/8, top, t, c)
	drawLine(fb, cx+size/8, top, right-size/8, top, t, c)
	drawLine(fb, right-size/8, top, right-size/4, top-size/8, t, c)
	drawLine(fb, right-size/8, top, right-size/4, top+size/8, t, c)
	drawLine(fb, right-size/8, bot, right-size/4, bot-size/8, t, c)
	drawLine(fb, right-size/8, bot, right-size/4, bot+size/8, t, c)
}

func drawRepeatIcon(fb *framebuffer, cx, cy, size int, c color.RGBA) {
	t := max(2, size/12)
	left := cx - size/2
	right := cx + size/2
	top := cy - size/3
	bot := cy + size/3
	drawLine(fb, left+size/8, top, right-size/8, top, t, c)
	drawLine(fb, right-size/8, top, right-size/4, top-size/8, t, c)
	drawLine(fb, right-size/8, top, right-size/4, top+size/8, t, c)
	drawLine(fb, right-size/8, bot, left+size/8, bot, t, c)
	drawLine(fb, left+size/8, bot, left+size/4, bot-size/8, t, c)
	drawLine(fb, left+size/8, bot, left+size/4, bot+size/8, t, c)
	drawLine(fb, right-size/8, top, right, cy, t, c)
	drawLine(fb, right, cy, right-size/8, bot, t, c)
	drawLine(fb, left+size/8, bot, left, cy, t, c)
	drawLine(fb, left, cy, left+size/8, top, t, c)
}

func drawEQIcon(fb *framebuffer, cx, cy, size int, c color.RGBA) {
	t := max(2, size/12)
	gap := size / 3
	for i, off := range []int{-gap, 0, gap} {
		x := cx + off
		drawLine(fb, x, cy-size/2, x, cy+size/2, t, c)
		knobY := cy
		if i == 0 {
			knobY = cy - size/5
		} else if i == 2 {
			knobY = cy + size/5
		}
		fb.rect(x-size/9, knobY-size/10, size*2/9, size/5, c)
	}
}

func formatSampleRate(rate int) string {
	if rate <= 0 {
		return ""
	}
	if rate%1000 == 0 {
		return fmt.Sprintf("%d kHz", rate/1000)
	}
	return fmt.Sprintf("%.1f kHz", float64(rate)/1000)
}

func audioInfoBadges(t Track) []string {
	out := make([]string, 0, 4)
	if t.MediaFormat != "" {
		out = append(out, t.MediaFormat)
	}
	if t.BitDepth > 0 {
		out = append(out, fmt.Sprintf("%d bit", t.BitDepth))
	}
	if t.SampleRate > 0 {
		out = append(out, formatSampleRate(t.SampleRate))
	}
	if t.BitRate > 0 {
		out = append(out, fmt.Sprintf("%d kbps", t.BitRate))
	}
	return out
}

func drawInfoBadges(fb *framebuffer, x, y, maxW, lineScale int, t Track, cfg *Config) {
	items := audioInfoBadges(t)
	if len(items) == 0 || maxW <= 0 {
		return
	}
	fontScale := max(1, lineScale-1)
	gap := max(2, lineScale*3)
	padX := max(3, lineScale*3)
	blockH := max(8, lineScale*7)
	textY := y + (blockH-fontScale*7)/2
	bgText := playerBackground(cfg)
	white := color.RGBA{255, 255, 255, 255}
	for _, item := range items {
		w := tw(fontScale, item) + padX*2
		if x+w > maxW {
			break
		}
		fb.rect(x, y, w, blockH, white)
		fb.text(x+padX, textY, fontScale, item, bgText)
		x += w + gap
	}
}

func drawPlayerStatic(fb *framebuffer, p *Player, sel int, cfg *Config) playerLayout {
	bg := playerBackground(cfg)
	fb.fill(bg)
	t, _, qidx, qlen, paused, rep, shuf, stopped := playerSnapshot(p)
	if qlen == 0 {
		return playerLayout{}
	}
	l := makePlayerLayout(fb, t, cfg)
	if !effectiveHideAlbumArt(t, cfg) {
		if t.Art != nil {
			fb.drawImage(t.Art, l.margin, l.top, l.artSize, l.artSize)
		} else {
			fb.rect(l.margin, l.top, l.artSize, l.artSize, color.RGBA{25, 27, 34, 255})
			noArtScale := scaledFont(l.scale, 2)
			label := "NO ALBUM ART"
			fb.text(l.margin+(l.artSize-tw(noArtScale, label))/2, l.top+l.artSize/2-scaledPx(l.scale, 8), noArtScale, label, color.RGBA{130, 130, 135, 255})
		}
	}
	titleScale := scaledFont(l.scale, 5)
	bodyScale := scaledFont(l.scale, 3)
	metaGap := scaledPx(l.scale, 52)
	bodyGap := scaledPx(l.scale, 36)
	maxTitleChars := max(12, l.rightW/(6*titleScale))
	fb.text(l.rightX, l.top, titleScale, short(t.Title, maxTitleChars), color.RGBA{250, 250, 250, 255})
	metaY := l.top + metaGap
	if t.Artist != "" {
		fb.text(l.rightX, metaY, bodyScale, short(t.Artist, max(12, l.rightW/(6*bodyScale))), color.RGBA{185, 185, 190, 255})
		metaY += bodyGap
	}
	if t.Album != "" {
		fb.text(l.rightX, metaY, bodyScale, short(t.Album, max(12, l.rightW/(6*bodyScale))), color.RGBA{150, 150, 155, 255})
		metaY += bodyGap
	}
	trackText := fmt.Sprintf("Track: %d of %d", qidx+1, qlen)
	trackColor := color.RGBA{150, 150, 155, 255}
	fb.text(l.rightX, metaY, bodyScale, trackText, trackColor)
	badgeX := l.rightX + tw(bodyScale, trackText) + scaledPx(l.scale, 18)
	drawInfoBadges(fb, badgeX, metaY, l.rightX+l.rightW, bodyScale, t, cfg)
	drawPlayerControls(fb, p, sel, l, paused, rep, shuf, stopped, qidx, qlen, cfg)
	drawClock(fb, cfg, bg)
	return l
}

func drawPlayerControls(fb *framebuffer, p *Player, sel int, l playerLayout, paused, rep, shuf, stopped bool, qidx, qlen int, cfg *Config) {
	bg := playerBackground(cfg)
	ctrlTop := l.ctrlY - scaledPx(l.scale, 22)
	ctrlH := fb.h - ctrlTop - scaledPx(l.scale, 55)
	ctrlX := l.margin
	ctrlW := fb.w - l.margin*2
	if ctrlH > 0 {
		fb.rect(ctrlX, ctrlTop, ctrlW, ctrlH, bg)
	}
	count := 7
	cellW := ctrlW / count
	boxH := scaledPx(l.scale, 76)
	iconSize := scaledPx(l.scale, 42)
	if iconSize > cellW*55/100 {
		iconSize = cellW * 55 / 100
	}
	for i := 0; i < count; i++ {
		x := ctrlX + i*cellW
		cx := x + cellW/2
		cy := l.ctrlY + boxH/2 - scaledPx(l.scale, 9)
		if sel == i+1 {
			fb.border(x+scaledPx(l.scale, 5), l.ctrlY-scaledPx(l.scale, 10), cellW-scaledPx(l.scale, 10), boxH, scaledPx(l.scale, 2), color.RGBA{255, 255, 255, 255})
		}
		c := color.RGBA{225, 225, 230, 255}
		switch i {
		case 0:
			drawPrevNextIcon(fb, cx, cy, iconSize, false, c)
		case 1:
			if paused || stopped {
				drawPlayIcon(fb, cx, cy, iconSize, c)
			} else {
				drawPauseIcon(fb, cx, cy, iconSize, c)
			}
		case 2:
			drawStopIcon(fb, cx, cy, iconSize, c)
		case 3:
			drawPrevNextIcon(fb, cx, cy, iconSize, true, c)
		case 4:
			drawShuffleIcon(fb, cx, cy, iconSize, c)
			if shuf {
				fb.rect(x+cellW/4, l.ctrlY+boxH-scaledPx(l.scale, 4), cellW/2, scaledPx(l.scale, 3), c)
			}
		case 5:
			drawRepeatIcon(fb, cx, cy, iconSize, c)
			if rep {
				fb.rect(x+cellW/4, l.ctrlY+boxH-scaledPx(l.scale, 4), cellW/2, scaledPx(l.scale, 3), c)
			}
		case 6:
			drawEQIcon(fb, cx, cy, iconSize, c)
		}
	}

}

func drawPlayerDynamic(fb *framebuffer, p *Player, l playerLayout, sel int, cfg *Config) {
	if l.rightW <= 0 {
		return
	}
	bg := playerBackground(cfg)
	_, lv, _, _, paused, _, _, stopped := playerSnapshot(p)
	vizPad := scaledPx(l.scale, 2)
	fb.rect(l.rightX-vizPad, l.vizY-vizPad, l.rightW+vizPad*2, l.vizH+vizPad*2, bg)
	barGap := scaledPx(l.scale, 12)
	bw := (l.rightW - barGap*9) / 10
	if bw < 2 {
		bw = 2
	}
	if !stopped {
		for i, v := range lv {
			h := int(v * float64(l.vizH))
			if h < 0 {
				h = 0
			}
			if h > l.vizH {
				h = l.vizH
			}
			fb.rect(l.rightX+i*(bw+barGap), l.vizY+l.vizH-h, bw, h, color.RGBA{225, 225, 230, 255})
		}
	}
	elapsed := p.elapsedNow()
	duration := p.durationNow()
	timeScale := scaledFont(l.scale, 3)
	timeAreaH := scaledPx(l.scale, 74)
	fb.rect(l.rightX, l.timeY, l.rightW, timeAreaH, bg)
	timeTxt := fmt.Sprintf("%02d:%02d", int(elapsed.Minutes()), int(elapsed.Seconds())%60)
	if duration > 0 {
		timeTxt += fmt.Sprintf(" / %02d:%02d", int(duration.Minutes()), int(duration.Seconds())%60)
	}
	if paused {
		timeTxt += "   PAUSED"
	}
	fb.text(l.rightX, l.timeY, timeScale, timeTxt, color.RGBA{185, 185, 190, 255})
	barBoxPad := scaledPx(l.scale, 8)
	barClearPad := barBoxPad + scaledPx(l.scale, 2)
	fb.rect(l.rightX-barClearPad, l.barY-barClearPad, l.rightW+barClearPad*2, l.barH+barClearPad*2, bg)
	if sel == 0 {
		fb.border(l.rightX-barBoxPad, l.barY-barBoxPad, l.rightW+barBoxPad*2, l.barH+barBoxPad*2, scaledPx(l.scale, 2), color.RGBA{255, 255, 255, 255})
	}
	fb.rect(l.rightX, l.barY, l.rightW, l.barH, color.RGBA{75, 75, 82, 255})
	fill := 0
	if duration > 0 {
		ratio := elapsed.Seconds() / duration.Seconds()
		if ratio < 0 {
			ratio = 0
		}
		if ratio > 1 {
			ratio = 1
		}
		fill = int(float64(l.rightW) * ratio)
	}
	fb.rect(l.rightX, l.barY, fill, l.barH, color.RGBA{240, 240, 240, 255})
}

func playerTrackKey(p *Player) string {
	t, _, idx, _, paused, rep, shuf, stopped := playerSnapshot(p)
	return fmt.Sprintf("%d|%s|%s|%s|%t|%t|%t|%t", idx, t.Title, t.Artist, t.Album, t.Art != nil, paused, rep || shuf, stopped)
}

func playerUI(app *App) {
	p := app.player
	if p == nil {
		return
	}
	fb, acts, cfg := app.fb, app.acts, app.cfg
	sel := 2
	l := drawPlayerStatic(fb, p, sel, cfg)
	drawPlayerDynamic(fb, p, l, sel, cfg)
	fb.present()
	lastTrackKey := playerTrackKey(p)
	lastClock := clockText(cfg)
	vizTick := time.NewTicker(33 * time.Millisecond)
	metaTick := time.NewTicker(250 * time.Millisecond)
	defer vizTick.Stop()
	defer metaTick.Stop()
	for {
		select {
		case raw := <-app.external:
			if err := app.startExternal(raw); err == nil {
				p = app.player
				sel = 2
				l = drawPlayerStatic(fb, p, sel, cfg)
				drawPlayerDynamic(fb, p, l, sel, cfg)
				fb.present()
				lastTrackKey = playerTrackKey(p)
			}
		case a := <-acts:
			redrawControls := false
			fullRedraw := false
			switch a {
			case actWake:
				fullRedraw = true
			case actUp:
				if sel != 0 {
					sel = 0
					redrawControls = true
				}
			case actDown:
				if sel == 0 {
					sel = 2
					redrawControls = true
				}
			case actLeft:
				if sel == 0 {
					p.seekBy(-10)
				} else if sel > 1 {
					sel--
					redrawControls = true
				}
			case actRight:
				if sel == 0 {
					p.seekBy(10)
				} else if sel < 7 {
					sel++
					redrawControls = true
				}
			case actPrev:
				p.prev()
				fullRedraw = true
			case actNext:
				p.next()
				fullRedraw = true
			case actPlayPause:
				p.togglePause()
				redrawControls = true
			case actStop:
				external := app.origin == nil
				app.stopAndUnload()
				if external {
					app.jumpSources = true
				}
				return
			case actNowPlaying:
			case actSources:
				app.jumpSources = true
				return
			case actBack:
				if app.origin == nil {
					app.jumpSources = true
				}
				return
			case actConfirm:
				switch sel {
				case 0:
				case 1:
					p.prev()
					fullRedraw = true
				case 2:
					p.togglePause()
					redrawControls = true
				case 3:
					external := app.origin == nil
					app.stopAndUnload()
					if external {
						app.jumpSources = true
					}
					return
				case 4:
					p.next()
					fullRedraw = true
				case 5:
					p.mu.Lock()
					p.q.Shuffle = !p.q.Shuffle
					p.mu.Unlock()
					redrawControls = true
				case 6:
					p.mu.Lock()
					p.q.Repeat = !p.q.Repeat
					p.mu.Unlock()
					redrawControls = true
				case 7:
					eqUI(app)
					if app.jumpSources {
						return
					}
					p.cfg = *cfg
					nativeAudioSetEQ(cfg.EQ)
					fullRedraw = true
				}
			}
			if fullRedraw {
				l = drawPlayerStatic(fb, p, sel, cfg)
				drawPlayerDynamic(fb, p, l, sel, cfg)
				fb.present()
				lastTrackKey = playerTrackKey(p)
			} else if redrawControls {
				_, _, qidx, qlen, paused, rep, shuf, stopped := playerSnapshot(p)
				drawPlayerControls(fb, p, sel, l, paused, rep, shuf, stopped, qidx, qlen, cfg)
				drawPlayerDynamic(fb, p, l, sel, cfg)
				fb.presentRegion(l.rightX-scaledPx(l.scale, 10), l.barY-scaledPx(l.scale, 12), l.rightW+scaledPx(l.scale, 20), l.barH+scaledPx(l.scale, 24))
				ctrlTop := l.ctrlY - scaledPx(l.scale, 22)
				fb.presentRegion(l.margin, ctrlTop, fb.w-l.margin*2, fb.h-ctrlTop)
			}
		case <-vizTick.C:
			drawPlayerDynamic(fb, p, l, sel, cfg)
			fb.presentRegion(l.rightX-scaledPx(l.scale, 10), l.vizY-scaledPx(l.scale, 2), l.rightW+scaledPx(l.scale, 20), l.barY+l.barH-l.vizY+scaledPx(l.scale, 12))
		case <-metaTick.C:
			k := playerTrackKey(p)
			if k != lastTrackKey {
				l = drawPlayerStatic(fb, p, sel, cfg)
				drawPlayerDynamic(fb, p, l, sel, cfg)
				fb.present()
				lastTrackKey = k
				lastClock = clockText(cfg)
				continue
			}
			ct := clockText(cfg)
			if ct != lastClock {
				x, y, w, h := drawClock(fb, cfg, playerBackground(cfg))
				fb.presentRegion(x, y, w, h)
				lastClock = ct
			}
		}
	}
}

func eqUI(app *App) {
	fb, acts, cfg := app.fb, app.acts, app.cfg
	clockTick := time.NewTicker(30 * time.Second)
	defer clockTick.Stop()
	names := []string{"ENABLED", "BASS 60HZ", "LOW-MID 250HZ", "MID 1KHZ", "HIGH-MID 4KHZ", "TREBLE 12KHZ", "RESET FLAT"}
	sel := 0
	for {
		fb.fill(appBackground(cfg))
		drawTitle(fb, "EQUALIZER")
		vals := []string{onoff(cfg.EQ.Enabled), fmt.Sprintf("%+.1F DB", cfg.EQ.Bass), fmt.Sprintf("%+.1F DB", cfg.EQ.LowMid), fmt.Sprintf("%+.1F DB", cfg.EQ.Mid), fmt.Sprintf("%+.1F DB", cfg.EQ.HighMid), fmt.Sprintf("%+.1F DB", cfg.EQ.Treble), ""}
		row := max(26, fb.h/11)
		y := 70
		for i, n := range names {
			if i == sel {
				fb.rect(40, y-5, fb.w-80, row-3, color.RGBA{34, 36, 45, 255})
				fb.border(40, y-5, fb.w-80, row-3, 2, color.RGBA{245, 245, 245, 255})
			}
			fb.text(60, y+5, max(1, row/24), n, color.RGBA{235, 235, 235, 255})
			if vals[i] != "" {
				fb.text(fb.w-60-tw(max(1, row/24), vals[i]), y+5, max(1, row/24), vals[i], color.RGBA{200, 200, 205, 255})
			}
			y += row
		}
		drawEQFooter(fb)
		drawClock(fb, cfg, appBackground(cfg))
		fb.present()
		var a action
		select {
		case <-clockTick.C:
			continue
		case a = <-acts:
		case raw := <-app.external:
			if err := app.startExternal(raw); err == nil {
				playerUI(app)
				app.jumpSources = true
				return
			}
			continue
		}
		switch a {
		case actUp:
			if sel > 0 {
				sel--
			}
		case actDown:
			if sel < len(names)-1 {
				sel++
			}
		case actSources:
			saveConfig(*cfg)
			app.jumpSources = true
			return
		case actBack:
			saveConfig(*cfg)
			return
		case actConfirm:
			if sel == 0 {
				cfg.EQ.Enabled = !cfg.EQ.Enabled
			} else if sel == 6 {
				cfg.EQ = EQConfig{Enabled: cfg.EQ.Enabled}
			}
		case actLeft:
			adjustEQ(&cfg.EQ, sel, -0.5)
		case actRight:
			adjustEQ(&cfg.EQ, sel, 0.5)
		}
		if app.player != nil {
			app.player.cfg = *cfg
			nativeAudioSetEQ(cfg.EQ)
		}
	}
}
func adjustEQ(e *EQConfig, i int, d float64) {
	v := func(x *float64) { *x = math.Max(-6, math.Min(6, *x+d)) }
	switch i {
	case 1:
		v(&e.Bass)
	case 2:
		v(&e.LowMid)
	case 3:
		v(&e.Mid)
	case 4:
		v(&e.HighMid)
	case 5:
		v(&e.Treble)
	}
}
func onoff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}
func message(app *App, title, msg string) {
	fb, acts := app.fb, app.acts
	clockTick := time.NewTicker(30 * time.Second)
	defer clockTick.Stop()
	for {
		fb.fill(appBackground(app.cfg))
		drawTitle(fb, title)
		fb.text(40, fb.h/2, 2, short(msg, max(20, fb.w/12)), color.RGBA{230, 230, 230, 255})
		drawMessageFooter(fb)
		drawClock(fb, app.cfg, appBackground(app.cfg))
		fb.present()
		var a action
		select {
		case <-clockTick.C:
			continue
		case a = <-acts:
		case raw := <-app.external:
			if err := app.startExternal(raw); err == nil {
				playerUI(app)
				app.jumpSources = true
				return
			}
			continue
		}
		if a == actSources {
			app.jumpSources = true
			return
		}
		if a == actBack || a == actConfirm {
			return
		}
	}
}
func detectUSB() []string {
	var out []string
	for i := 0; i < 8; i++ {
		p := fmt.Sprintf("/media/usb%d", i)
		if st, e := os.Stat(p); e == nil && st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}
func detectOptical() []string { m, _ := filepath.Glob("/dev/sr*"); sort.Strings(m); return m }

type cdTOCHeader struct{ First, Last uint8 }
type cdTOCEntry struct {
	Track    uint8
	AdrCtrl  uint8
	Format   uint8
	Pad      uint8
	Addr     int32
	DataMode uint8
	Pad2     [3]uint8
}
type cdReadAudio struct {
	Addr       int32
	AddrFormat uint8
	Pad        [3]uint8
	Frames     int32
	Buf        uintptr
}

const (
	cdromReadTOCHdr   = 0x5305
	cdromReadTOCEntry = 0x5306
	cdromReadAudio    = 0x530e
	cdromLBAMode      = 0x01
	cdromLeadout      = 0xaa
	cdFrameBytes      = 2352
)

func cdIoctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	if e != 0 {
		return e
	}
	return nil
}
func readCDTOC(dev string) ([]Track, error) {
	f, e := os.Open(dev)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	var h cdTOCHeader
	if e = cdIoctl(f.Fd(), cdromReadTOCHdr, unsafe.Pointer(&h)); e != nil {
		return nil, e
	}
	if h.First == 0 || h.Last < h.First {
		return nil, errors.New("no audio CD detected")
	}
	type ent struct {
		track uint8
		lba   int32
		ctrl  uint8
	}
	var es []ent
	for tr := h.First; tr <= h.Last; tr++ {
		x := cdTOCEntry{Track: tr, Format: cdromLBAMode}
		if e = cdIoctl(f.Fd(), cdromReadTOCEntry, unsafe.Pointer(&x)); e != nil {
			return nil, e
		}
		es = append(es, ent{track: tr, lba: x.Addr, ctrl: (x.AdrCtrl >> 4) & 0x0f})
	}
	lead := cdTOCEntry{Track: cdromLeadout, Format: cdromLBAMode}
	if e = cdIoctl(f.Fd(), cdromReadTOCEntry, unsafe.Pointer(&lead)); e != nil {
		return nil, e
	}
	var out []Track
	for i, x := range es {
		if x.ctrl&0x04 != 0 {
			continue
		}
		end := lead.Addr
		if i+1 < len(es) {
			end = es[i+1].lba
		}
		dur := float64(end-x.lba) / 75.0
		out = append(out, Track{Path: fmt.Sprintf("cdda:%s:%d:%d", dev, x.lba, end), Title: fmt.Sprintf("Track %02d", x.track), Album: "Audio CD", Duration: dur, MediaFormat: "CDDA", BitDepth: 16, SampleRate: 44100, BitRate: 1411})
	}
	if len(out) == 0 {
		return nil, errors.New("disc contains no audio tracks")
	}
	return out, nil
}
func currentReturnName(app *App, origin *browseOrigin) string {
	if origin == nil {
		return ""
	}
	name := origin.Selected
	if app.player == nil {
		return name
	}
	if ext := strings.ToLower(filepath.Ext(name)); ext == ".m3u" || ext == ".m3u8" {
		return name
	}
	if t := app.player.current(); t != nil && !strings.HasPrefix(t.Path, "cdda:") && filepath.Clean(filepath.Dir(t.Path)) == filepath.Clean(origin.Dir) {
		return filepath.Base(t.Path)
	}
	return name
}

func runBrowserSource(app *App, root, kind string) {
	startDir := root
	selected := ""
	for {
		choice, ok := browse(app, root, startDir, selected)
		if !ok {
			return
		}
		origin := &browseOrigin{Root: root, Dir: choice.Dir, Selected: choice.Name, Kind: kind}
		if err := app.startQueue(buildQueueFromChoice(choice), origin); err != nil {
			message(app, "PLAYBACK ERROR", err.Error())
			startDir, selected = choice.Dir, choice.Name
			continue
		}
		playerUI(app)
		if app.jumpSources {
			return
		}
		startDir = origin.Dir
		selected = currentReturnName(app, origin)
	}
}

var screenSaverOptions = []int{0, 30, 60, 120, 300, 600}

func screenSaverLabel(seconds int) string {
	switch seconds {
	case 30:
		return "30 SECONDS"
	case 60:
		return "1 MINUTE"
	case 120:
		return "2 MINUTES"
	case 300:
		return "5 MINUTES"
	case 600:
		return "10 MINUTES"
	default:
		return "OFF"
	}
}

func cycleScreenSaver(current, dir int) int {
	idx := 0
	for i, v := range screenSaverOptions {
		if v == current {
			idx = i
			break
		}
	}
	idx += dir
	if idx < 0 {
		idx = len(screenSaverOptions) - 1
	}
	if idx >= len(screenSaverOptions) {
		idx = 0
	}
	return screenSaverOptions[idx]
}

func confirmExitUI(app *App) bool {
	fb, acts := app.fb, app.acts
	for {
		bg := appBackground(app.cfg)
		fb.fill(bg)
		drawTitle(fb, "EXIT MISTER HI-FI?")
		scale := max(1, fb.h/180)
		msg := "A  EXIT     B  CANCEL"
		x := (fb.w - tw(scale, msg)) / 2
		y := fb.h/2 - scale*4
		fb.text(x, y, scale, msg, color.RGBA{240, 240, 240, 255})
		drawClock(fb, app.cfg, bg)
		fb.present()

		select {
		case a := <-acts:
			switch a {
			case actConfirm:
				return true
			case actBack, actSources, actWake:
				return false
			}
		case raw := <-app.external:
			if err := app.startExternal(raw); err == nil {
				playerUI(app)
				return false
			}
		}
	}
}

func settingsUI(app *App) {
	fb, acts, cfg := app.fb, app.acts, app.cfg
	clockTick := time.NewTicker(30 * time.Second)
	defer clockTick.Stop()
	sel := 0
	labels := []string{
		"OLED MODE",
		"SHOW ALBUM ART",
		"AUTO HIDE MISSING ART",
		"SHOW CLOCK",
		"CONFIRM ON EXIT",
		"SCREENSAVER",
		"GAPLESS PLAYBACK (EXPERIMENTAL)",
		"SWAP A/B",
		"SWAP X/Y",
		"CUSTOM FALLBACK FONT",
	}
	fonts := scanCustomFonts()
	applyCustomFont(cfg, fonts)
	fallbackFontHelp := "USED ONLY FOR CHARACTERS MISSING FROM THE BUILT-IN FONT"
	if len(fonts) == 0 {
		fallbackFontHelp = "ADD .TTF OR .OTF FONTS TO THE MISTER HI-FI FONTS FOLDER"
	}
	enabled := func(i int) bool {
		if i == 2 && cfg.HideAlbumArt {
			return false
		}
		if i == 9 && len(fonts) == 0 {
			return false
		}
		return true
	}
	move := func(from, dir int) int {
		i := from
		for {
			i += dir
			if i < 0 || i >= len(labels) {
				return from
			}
			if enabled(i) {
				return i
			}
		}
	}
	for {
		if app.jumpSources {
			return
		}
		fb.fill(appBackground(cfg))
		drawTitle(fb, "SETTINGS")
		y0 := max(66, fb.h/15)
		bottomReserve := max(82, fb.h/11)
		row := (fb.h - y0 - bottomReserve) / len(labels)
		if row < 34 {
			row = 34
		}
		values := []string{
			onoff(cfg.OLEDMode),
			onoff(!cfg.HideAlbumArt),
			onoff(cfg.AutoHideMissingArt),
			onoff(cfg.ShowClock),
			onoff(cfg.ConfirmOnExit),
			screenSaverLabel(cfg.ScreenSaverSeconds),
			onoff(cfg.GaplessPlayback),
			onoff(cfg.SwapAB),
			onoff(cfg.SwapXY),
			customFontLabel(cfg, fonts),
		}
		ts := max(1, row/22)
		for i := range labels {
			y := y0 + i*row
			rowEnabled := enabled(i)
			if sel == i && rowEnabled {
				fb.rect(45, y-4, fb.w-90, row-3, color.RGBA{35, 37, 46, 255})
				fb.border(45, y-4, fb.w-90, row-3, 2, color.RGBA{240, 240, 240, 255})
			}
			labelColor := color.RGBA{235, 235, 235, 255}
			valueColor := color.RGBA{200, 200, 205, 255}
			subColor := color.RGBA{125, 125, 132, 255}
			if !rowEnabled {
				labelColor = color.RGBA{90, 90, 96, 255}
				valueColor = color.RGBA{75, 75, 82, 255}
				subColor = color.RGBA{70, 70, 76, 255}
			}
			hasSub := i == 5 || i == 6 || i == 9
			labelY := y + 5
			if hasSub {
				labelY = y + 1
			}
			fb.text(65, labelY, ts, labels[i], labelColor)
			if i == 5 {
				subScale := max(1, ts-1)
				fb.text(65, labelY+ts*8, subScale, "TURNS THE DISPLAY COMPLETELY BLACK WHILE INACTIVE", subColor)
			}
			if i == 6 {
				subScale := max(1, ts-1)
				fb.text(65, labelY+ts*8, subScale, "FLAC / WAV / CDDA ONLY", subColor)
			}
			if i == 9 {
				subScale := max(1, ts-1)
				fb.text(65, labelY+ts*8, subScale, fallbackFontHelp, subColor)
			}
			fb.text(fb.w-65-tw(ts, values[i]), y+5, ts, values[i], valueColor)
		}
		drawBrowserFooter(fb, false)
		drawClock(fb, cfg, appBackground(cfg))
		fb.present()

		var a action
		select {
		case <-clockTick.C:
			continue
		case a = <-acts:
		case raw := <-app.external:
			if err := app.startExternal(raw); err == nil {
				playerUI(app)
				app.jumpSources = true
				return
			}
			continue
		}
		if a == actWake {
			continue
		}
		if app.handlePlaybackShortcut(a) {
			continue
		}
		switch a {
		case actSources:
			saveConfig(*cfg)
			app.jumpSources = true
			return
		case actBack:
			saveConfig(*cfg)
			return
		case actUp:
			sel = move(sel, -1)
		case actDown:
			sel = move(sel, 1)
		case actConfirm, actLeft, actRight:
			if !enabled(sel) {
				continue
			}
			switch sel {
			case 0:
				cfg.OLEDMode = !cfg.OLEDMode
			case 1:
				cfg.HideAlbumArt = !cfg.HideAlbumArt
			case 2:
				cfg.AutoHideMissingArt = !cfg.AutoHideMissingArt
			case 3:
				cfg.ShowClock = !cfg.ShowClock
			case 4:
				cfg.ConfirmOnExit = !cfg.ConfirmOnExit
			case 5:
				dir := 1
				if a == actLeft {
					dir = -1
				}
				cfg.ScreenSaverSeconds = cycleScreenSaver(cfg.ScreenSaverSeconds, dir)
				screenSaverSeconds.Store(int64(cfg.ScreenSaverSeconds))
			case 6:
				cfg.GaplessPlayback = !cfg.GaplessPlayback
				if app.player != nil {
					app.player.mu.Lock()
					app.player.cfg.GaplessPlayback = cfg.GaplessPlayback
					app.player.mu.Unlock()
					if cfg.GaplessPlayback {
						app.player.prepareGaplessNextFile()
					}
				}
			case 7:
				cfg.SwapAB = !cfg.SwapAB
				swapABInput.Store(cfg.SwapAB)
			case 8:
				cfg.SwapXY = !cfg.SwapXY
				swapXYInput.Store(cfg.SwapXY)
			case 9:
				dir := 1
				if a == actLeft {
					dir = -1
				}
				cycleCustomFont(cfg, fonts, dir)
			}
			saveConfig(*cfg)
		}
	}
}

func physicalDisc(app *App) {
	devs := detectOptical()
	if len(devs) == 0 {
		message(app, "PHYSICAL DISC", "NO OPTICAL DRIVE DETECTED")
		return
	}
	var tracks []Track
	var err error
	for _, d := range devs {
		tracks, err = readCDTOC(d)
		if err == nil && len(tracks) > 0 {
			break
		}
	}
	if len(tracks) == 0 {
		if err == nil {
			err = errors.New("no audio CD detected")
		}
		message(app, "PHYSICAL DISC", strings.ToUpper(err.Error()))
		return
	}
	names := make([]string, len(tracks))
	for i, t := range tracks {
		names[i] = t.Title
	}
	q := Queue{Tracks: append([]Track(nil), tracks...), Index: 0}
	origin := &browseOrigin{Kind: "disc", Selected: names[0]}
	if err := app.startQueue(q, origin); err != nil {
		message(app, "PLAYBACK ERROR", err.Error())
		return
	}
	playerUI(app)
	if app.jumpSources {
		return
	}
	initial := 0
	for {
		if app.player != nil {
			app.player.mu.Lock()
			if app.player.q.Index >= 0 && app.player.q.Index < len(names) {
				initial = app.player.q.Index
			}
			app.player.mu.Unlock()
		}
		i, ok := menu(app, "AUDIO CD", names, initial)
		if !ok {
			return
		}
		q := Queue{Tracks: append([]Track(nil), tracks...), Index: i}
		origin = &browseOrigin{Kind: "disc", Selected: names[i]}
		if err := app.startQueue(q, origin); err != nil {
			message(app, "PLAYBACK ERROR", err.Error())
			continue
		}
		playerUI(app)
		if app.jumpSources {
			return
		}
		initial = i
	}
}

func sourcesUI(app *App) {
	for {
		if app.jumpSources {
			app.jumpSources = false
		}
		items := []string{"SD CARD"}
		types := []string{"sd"}
		usbs := detectUSB()
		if len(usbs) > 0 {
			items = append(items, "USB")
			types = append(types, "usb")
		}
		if smbAvailable() {
			items = append(items, "SMB")
			types = append(types, "smb")
		}
		if len(detectOptical()) > 0 {
			items = append(items, "PHYSICAL DISC")
			types = append(types, "disc")
		}
		items = append(items, "", "SETTINGS")
		types = append(types, "separator", "settings")
		i, ok := menu(app, "MISTER HI-FI v"+version, items, 0)
		if !ok {
			if app.jumpSources {
				app.jumpSources = false
				continue
			}
			if app.cfg.ConfirmOnExit && !confirmExitUI(app) {
				continue
			}
			return
		}
		switch types[i] {
		case "sd":
			runBrowserSource(app, "/media/fat", "sd")
		case "usb":
			root := usbs[0]
			if len(usbs) > 1 {
				settleInput(app.acts, 180*time.Millisecond)
				j, ok := menu(app, "USB", usbs, 0)
				if !ok {
					if app.jumpSources {
						app.jumpSources = false
					}
					continue
				}
				root = usbs[j]
			}
			runBrowserSource(app, root, "usb")
		case "smb":
			s := loadSMB()
			idx := 0
			if len(s.Shares) > 1 {
				settleInput(app.acts, 180*time.Millisecond)
				names := make([]string, len(s.Shares))
				for i, x := range s.Shares {
					names[i] = x.Name
					if names[i] == "" {
						names[i] = x.Server + "/" + x.Share
					}
				}
				j, ok := menu(app, "SMB", names, 0)
				if !ok {
					if app.jumpSources {
						app.jumpSources = false
					}
					continue
				}
				idx = j
			}
			root, e := mountShare(s.Shares[idx])
			if e != nil {
				message(app, "SMB ERROR", e.Error())
				continue
			}
			runBrowserSource(app, root, "smb")
		case "disc":
			physicalDisc(app)
		case "settings":
			settingsUI(app)
		}
	}
}

func main() {
	_ = os.MkdirAll(smbMountRoot, 0755)
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("MiSTer Hi-Fi v" + version)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--send" {
		if len(os.Args) < 3 {
			os.Exit(2)
		}
		target := externalArg(os.Args[2:])
		if target == "" {
			os.Exit(2)
		}
		if e := sendExternal(target); e != nil {
			os.Exit(1)
		}
		return
	}
	var launchTarget string
	if len(os.Args) > 1 {
		launchTarget = externalArg(os.Args[1:])
		if launchTarget != "" && sendExternal(launchTarget) == nil {
			return
		}
	}
	external := make(chan string, 8)
	ln, e := externalListener(external)
	if e != nil {
		fmt.Fprintln(os.Stderr, "MiSTer Hi-Fi IPC:", e)
		os.Exit(1)
	}
	defer func() {
		_ = ln.Close()
		_ = os.Remove(socketPath)
	}()
	fb, e := openFB()
	if e != nil {
		fmt.Fprintln(os.Stderr, "MiSTer Hi-Fi:", e)
		os.Exit(1)
	}
	defer fb.close()
	term := quietTerm()
	defer term.restore()
	cfg := loadConfig()
	_ = os.MkdirAll(customFontsDir, 0755)
	startupFonts := scanCustomFonts()
	applyCustomFont(&cfg, startupFonts)
	saveConfig(cfg)
	swapABInput.Store(cfg.SwapAB)
	swapXYInput.Store(cfg.SwapXY)
	done := make(chan struct{})
	rawActs := make(chan action, 8)
	acts := make(chan action, 8)
	screenSaverSeconds.Store(int64(cfg.ScreenSaverSeconds))
	inputLoop(rawActs, done)
	go screenSaverInputLoop(fb, rawActs, acts, done)
	defer close(done)
	app := &App{fb: fb, acts: acts, external: external, cfg: &cfg}
	defer cleanupSMBMounts()
	defer func() {
		if app.player != nil {
			app.player.stopPlaybackRaw()
		}
	}()
	if launchTarget != "" {
		if err := app.startExternal(launchTarget); err != nil {
			message(app, "MISTER HI-FI", strings.ToUpper(err.Error()))
		} else {
			playerUI(app)
		}
	}
	sourcesUI(app)
}

var _ = strconv.Itoa
