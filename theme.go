package main

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Accent colour and the visualizer styles. The accent replaces the white of the
// selection frames, the progress bar and the plain spectrum bars; the retro LED
// visualizer has its own colours (green, yellow, red).

const defaultAccentHex = "#F0F0F0"

type accentPreset struct {
	Name string
	Hex  string
}

// The presets of the settings screen; the last entry, "Custom", is whatever RGB the user picked.
var accentPresets = []accentPreset{
	{"White", "#F0F0F0"},
	{"Red", "#FF3B30"},
	{"Orange", "#FF9500"},
	{"Yellow", "#FFD60A"},
	{"Green", "#34C759"},
	{"Teal", "#30D5C8"},
	{"Cyan", "#32ADE6"},
	{"Blue", "#3D6BFF"},
	{"Purple", "#AF52DE"},
	{"Pink", "#FF5CA8"},
	{"Grey", "#A0A0A8"},
}

var accentValue atomic.Uint32 // 0xRRGGBB

func init() { accentValue.Store(0xF0F0F0) }

// parseHexColour reads "#RRGGBB" (or "RRGGBB").
func parseHexColour(s string) (color.RGBA, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return color.RGBA{}, false
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(strings.ToLower(s), "%02x%02x%02x", &r, &g, &b); err != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{r, g, b, 255}, true
}

func hexColour(c color.RGBA) string { return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B) }

// setAccent makes hex the accent of the whole UI; an invalid value is ignored and the default used.
func setAccent(hex string) string {
	c, ok := parseHexColour(hex)
	if !ok {
		c, _ = parseHexColour(defaultAccentHex)
	}
	accentValue.Store(uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B))
	return hexColour(c)
}

func accentColour() color.RGBA {
	v := accentValue.Load()
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
}

// accentLabel is the preset's name, or "CUSTOM #RRGGBB".
func accentLabel(hex string) string {
	for _, p := range accentPresets {
		if strings.EqualFold(p.Hex, hex) {
			return strings.ToUpper(p.Name)
		}
	}
	return "CUSTOM " + strings.ToUpper(hex)
}

// cycleAccent steps through the presets; from a custom colour it goes to the first or last preset.
func cycleAccent(hex string, dir int) string {
	n := len(accentPresets)
	for i, p := range accentPresets {
		if strings.EqualFold(p.Hex, hex) {
			return accentPresets[((i+dir)%n+n)%n].Hex
		}
	}
	if dir < 0 {
		return accentPresets[n-1].Hex
	}
	return accentPresets[0].Hex
}

// Visualizer styles: "bars" is the original plain look, "led" the retro one.
var visualizerStyles = []struct{ ID, Label string }{
	{"bars", "Spectrum bars"},
	{"led", "Retro LED"},
	{"kitt", "KITT voicebox"},
	{"kitt-original", "KITT voicebox (original)"},
	{"sunset", "Retro sunset"},
	{"grid", "Retro grid"},
	{"wave", "Colour wave"},
}

func visualizerLabel(id string) string {
	for _, s := range visualizerStyles {
		if s.ID == id {
			return strings.ToUpper(s.Label)
		}
	}
	return strings.ToUpper(visualizerStyles[0].Label)
}

func cycleVisualizer(id string, dir int) string {
	n := len(visualizerStyles)
	for i, s := range visualizerStyles {
		if s.ID == id {
			return visualizerStyles[((i+dir)%n+n)%n].ID
		}
	}
	return visualizerStyles[0].ID
}

func lerpColour(a, b color.RGBA, t float64) color.RGBA {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	f := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return color.RGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), 255}
}

// ledColour is the colour of segment s of n: green at the bottom, through
// yellow and orange, to red at the top (the 80s equalizer look).
func ledColour(s, n int) color.RGBA {
	t := float64(s) / float64(max(1, n-1))
	green := color.RGBA{60, 235, 20, 255}
	yellow := color.RGBA{255, 240, 20, 255}
	red := color.RGBA{255, 40, 20, 255}
	if t < 0.55 {
		return lerpColour(green, yellow, t/0.55)
	}
	return lerpColour(yellow, red, (t-0.55)/0.45)
}

var ledPeaks [32]float64 // peak-hold per bar

// drawVisualizer draws the level bars (levels 0..1) in the style asked for,
// inside the box x, y, w, h. The caller has cleared the box.
func drawVisualizer(fb *framebuffer, style string, lv []float64, x, y, w, h int, scale float64, bg color.RGBA) {
	box := vizBox{fb, x, y, w, h}
	switch style {
	case "kitt":
		drawKITTColumns(box, lv, bg)
		return
	case "kitt-original":
		drawKITT(box, lv, bg)
		return
	case "sunset":
		drawSunset(box, lv, bg)
		return
	case "grid":
		drawGrid(box, lv, bg)
		return
	case "wave":
		drawWave(box, lv, bg)
		return
	}
	if style != "led" {
		barGap := scaledPx(scale, 12)
		bw := max(2, (w-barGap*9)/10)
		col := accentColour()
		for i, v := range lv {
			bh := clampInt(int(v*float64(h)), 0, h)
			fb.rect(x+i*(bw+barGap), y+h-bh, bw, bh, col)
		}
		return
	}
	// Segmented bars: lit segments, the unlit ones faintly visible and a
	// slowly falling peak segment on top, as on a 1980s hi-fi tower.
	n := len(lv)
	if n == 0 {
		return
	}
	gap := max(2, scaledPx(scale, 6))
	segGap := max(1, scaledPx(scale, 2))
	segs := 14
	bw := max(2, (w-gap*(n-1))/n)
	segH := max(2, (h-segGap*(segs-1))/segs)
	for i, v := range lv {
		lit := clampInt(int(v*float64(segs)+0.5), 0, segs)
		bx := x + i*(bw+gap)
		if i < len(ledPeaks) {
			if float64(lit) >= ledPeaks[i] {
				ledPeaks[i] = float64(lit)
			} else {
				ledPeaks[i] -= 0.12
			}
		}
		peak := -1
		if i < len(ledPeaks) {
			peak = int(ledPeaks[i]+0.5) - 1
		}
		for s := 0; s < segs; s++ {
			c := ledColour(s, segs)
			switch {
			case s < lit:
			case s == peak:
				c = lerpColour(bg, c, 0.8)
			default:
				c = inactiveVizColour(bg, c, 0.10)
			}
			fb.rect(bx, y+h-(s+1)*segH-s*segGap, bw, segH, c)
		}
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// accentEditor lets the user mix a colour with the red, green and blue values.
// Up/down choose the channel, left/right change it, confirm saves, back cancels.
func accentEditor(app *App, hex string) (string, bool) {
	fb, acts := app.fb, app.acts
	c, ok := parseHexColour(hex)
	if !ok {
		c, _ = parseHexColour(defaultAccentHex)
	}
	vals := [3]int{int(c.R), int(c.G), int(c.B)}
	names := [3]string{"RED", "GREEN", "BLUE"}
	sel := 0
	for {
		fb.fill(appBackground(app.cfg))
		drawTitle(fb, "ACCENT COLOUR")
		cur := color.RGBA{uint8(vals[0]), uint8(vals[1]), uint8(vals[2]), 255}
		ts := max(1, fb.h/240)
		y0 := max(80, fb.h/6)
		row := max(44, fb.h/9)
		for i := 0; i < 3; i++ {
			y := y0 + i*row
			if i == sel {
				fb.rect(45, y-6, fb.w-90, row-6, oledSurface(app.cfg, color.RGBA{35, 37, 46, 255}))
				fb.border(45, y-6, fb.w-90, row-6, 2, cur)
			}
			fb.text(65, y+4, ts, fmt.Sprintf("%s %d", names[i], vals[i]), color.RGBA{235, 235, 235, 255})
			sx, sw := 65+tw(ts, "GREEN 255")+30, fb.w-65-(65+tw(ts, "GREEN 255")+30)-30
			fb.rect(sx, y+row/2-10, sw, 6, oledSurface(app.cfg, color.RGBA{75, 75, 82, 255}))
			if app.cfg.OLEDMode {
				fb.border(sx, y+row/2-10, sw, 6, 1, color.RGBA{75, 75, 82, 255})
			}
			fb.rect(sx, y+row/2-10, sw*vals[i]/255, 6, cur)
			fb.rect(sx+sw*vals[i]/255-4, y+row/2-16, 8, 18, color.RGBA{240, 240, 240, 255})
		}
		sy := y0 + 3*row + 10
		fb.rect(65, sy, 120, row-10, cur)
		fb.text(205, sy+(row-10)/2-4*ts, ts, hexColour(cur), color.RGBA{225, 225, 230, 255})
		fb.text(65, fb.h-max(50, fb.h/10), max(1, ts-1), "UP/DOWN CHANNEL   LEFT/RIGHT ADJUST   A SAVE   B CANCEL", color.RGBA{160, 160, 165, 255})
		setAccent(hexColour(cur)) // live preview of the frames
		fb.present()
		var a action
		select {
		case a = <-acts:
		case <-app.external:
			continue
		}
		switch a {
		case actUp:
			sel = (sel + 2) % 3
		case actDown:
			sel = (sel + 1) % 3
		case actLeft:
			vals[sel] = clampInt(vals[sel]-8, 0, 255)
		case actRight:
			vals[sel] = clampInt(vals[sel]+8, 0, 255)
		case actConfirm:
			return hexColour(color.RGBA{uint8(vals[0]), uint8(vals[1]), uint8(vals[2]), 255}), true
		case actBack:
			setAccent(hex)
			return hex, false
		}
	}
}

// vizBox is the area a visualizer draws in; everything is clipped to it.
type vizBox struct {
	fb         *framebuffer
	x, y, w, h int
}

func (b vizBox) rect(x, y, w, h int, c color.RGBA) {
	if x < b.x {
		w -= b.x - x
		x = b.x
	}
	if y < b.y {
		h -= b.y - y
		y = b.y
	}
	if x+w > b.x+b.w {
		w = b.x + b.w - x
	}
	if y+h > b.y+b.h {
		h = b.y + b.h - y
	}
	if w > 0 && h > 0 {
		b.fb.rect(x, y, w, h, c)
	}
}

// line draws a line one row at a time, each row as wide as the line moves in
// it, so a flat line has no gaps; clipped to the box.
func (b vizBox) line(x0, y0, x1, y1, thick int, c color.RGBA) {
	if y1 < y0 {
		x0, y0, x1, y1 = x1, y1, x0, y0
	}
	at := func(y int) int {
		if y1 == y0 {
			return x0
		}
		return x0 + (x1-x0)*(y-y0)/(y1-y0)
	}
	for y := y0; y <= y1; y++ {
		xa, xb := at(y), at(y+1)
		if xb < xa {
			xa, xb = xb, xa
		}
		b.rect(xa-thick/2, y, xb-xa+thick, 1, c)
	}
}

func avgLevels(lv []float64) float64 {
	if len(lv) == 0 {
		return 0
	}
	t := 0.0
	for _, v := range lv {
		t += v
	}
	return t / float64(len(lv))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// drawKITT is the voicebox of the talking black car of the 1980s: three columns
// of segments that light up from the middle line outwards, up and down together.
// The middle column is the highest most of the time, the outer two follow lower.
func drawKITT(b vizBox, lv []float64, bg color.RGBA) {
	n := len(lv)
	if n < 2 {
		return
	}
	left, right := avgLevels(lv[:n/2])*1.5, avgLevels(lv[n/2:])*1.5
	mid := clamp01(math.Max(left, right)*1.25 + 0.05*math.Max(left, right))
	cols := [3]float64{clamp01(left * 0.8), mid, clamp01(right * 0.8)}
	half := 9
	segGap := max(1, b.h/90)
	segH := max(2, (b.h-segGap*2*half)/(2*half+1))
	colW := clampInt(b.w/5, 8, b.h*2/5)
	gap := max(3, colW/6)
	total := 3*colW + 2*gap
	x0 := b.x + (b.w-total)/2
	midY := b.y + b.h/2 - segH/2
	red := color.RGBA{255, 30, 20, 255}
	for c, v := range cols {
		lit := int(v*float64(half) + 0.5)
		bx := x0 + c*(colW+gap)
		for s := -half; s <= half; s++ {
			d := s
			if d < 0 {
				d = -d
			}
			col := inactiveVizColour(bg, red, 0.13)
			if d <= lit && v > 0.02 {
				t := float64(d) / float64(half) // 0 in the middle, 1 at the ends
				switch {
				case t < 0.35:
					col = lerpColour(color.RGBA{255, 250, 215, 255}, color.RGBA{255, 190, 40, 255}, t/0.35)
				case t < 0.7:
					col = lerpColour(color.RGBA{255, 190, 40, 255}, color.RGBA{255, 70, 25, 255}, (t-0.35)/0.35)
				default:
					col = red
				}
			}
			b.rect(bx, midY+s*(segH+segGap), colW, segH, col)
		}
	}
}

func vgradient(b vizBox, y0, y1 int, top, bottom color.RGBA) {
	if y1 <= y0 {
		return
	}
	for y := y0; y < y1; y++ {
		b.rect(b.x, y, b.w, 1, lerpColour(top, bottom, float64(y-y0)/float64(y1-y0)))
	}
}

// sun draws the striped sun of the 1980s with its centre on the horizon line,
// the part below the horizon hidden: yellow at the top, magenta lower, with
// dark bands that get thicker towards the bottom.
func sun(b vizBox, cx, horizon, r int) {
	top := color.RGBA{255, 225, 70, 255}
	bottom := color.RGBA{235, 40, 175, 255}
	for dy := -r; dy < 0; dy++ {
		t := float64(-dy) / float64(r) // 1 at the top of the sun, 0 at the horizon
		if t < 0.5 {
			k := int((0.5 - t) * 2 * float64(max(2, r/7))) // band thickness grows downwards
			period := max(4, r/6)
			if (-dy)%period < k {
				continue
			}
		}
		hw := int(math.Sqrt(float64(r*r - dy*dy)))
		b.rect(cx-hw, horizon+dy, 2*hw, 1, lerpColour(bottom, top, t))
	}
}

func hashBit(i int) bool { return (uint32(i)*2654435761)>>29 == 0 }

// drawSunset: a purple sky, a striped sun on the horizon and a small spectrum
// on either side of it, reflected faintly in the floor.
func drawSunset(b vizBox, lv []float64, bg color.RGBA) {
	n := len(lv)
	if n < 2 {
		return
	}
	horizon := b.y + b.h*62/100
	if !isOLEDBackground(bg) {
		vgradient(b, b.y, horizon, color.RGBA{18, 2, 40, 255}, color.RGBA{110, 14, 130, 255})
	}
	if !isOLEDBackground(bg) {
		vgradient(b, horizon, b.y+b.h, color.RGBA{30, 4, 55, 255}, color.RGBA{8, 2, 20, 255})
	}
	for i := 0; i < 60; i++ { // stars
		if hashBit(i * 7) {
			b.rect(b.x+(i*97)%b.w, b.y+(i*53)%max(1, horizon-b.y-4), 1, 1, color.RGBA{230, 210, 255, 255})
		}
	}
	r := min(b.h*50/100, b.w/6)
	cx := b.x + b.w/2
	sun(b, cx, horizon, r)
	// the sun's reflection: a few faint, shrinking streaks
	for k := 0; k < 5; k++ {
		hw := int(float64(r) * (0.75 - 0.12*float64(k)))
		b.rect(cx-hw, horizon+4+k*max(3, r/8), 2*hw, max(1, r/18), lerpColour(color.RGBA{30, 4, 55, 255}, color.RGBA{230, 90, 150, 255}, 0.45-0.08*float64(k)))
	}
	// spectrum left and right of the sun, rising from the horizon
	half := n / 2
	area := cx - r - b.x - b.w/25
	bw := max(2, area/(half*2))
	for side := 0; side < 2; side++ {
		for i := 0; i < half; i++ {
			bx, v := cx-r-b.w/25-(i+1)*bw*2, lv[i]
			if side == 1 {
				bx, v = cx+r+b.w/25+i*bw*2, lv[n-1-i]
			}
			bh := int(clamp01(v) * float64(horizon-b.y) * 0.8)
			for yy := 0; yy < bh; yy += 2 {
				t := float64(yy) / float64(max(1, horizon-b.y))
				b.rect(bx, horizon-yy-2, bw, 2, lerpColour(color.RGBA{255, 60, 200, 255}, color.RGBA{255, 225, 140, 255}, t*1.4))
			}
			rb := bh / 3
			b.rect(bx, horizon+1, bw, rb, color.RGBA{110, 25, 100, 255})
		}
	}
	b.rect(b.x, horizon, b.w, 2, color.RGBA{255, 130, 235, 255}) // the horizon's glow
	b.rect(b.x, horizon+2, b.w, 1, color.RGBA{160, 40, 150, 255})
}

var vizHist struct {
	sync.Mutex
	rows [8][10]float64
	at   time.Time
}

// pushHistory keeps the last levels, one row about every 110 ms (newest first).
func pushHistory(lv []float64) [8][10]float64 {
	vizHist.Lock()
	defer vizHist.Unlock()
	if time.Since(vizHist.at) > 110*time.Millisecond {
		copy(vizHist.rows[1:], vizHist.rows[:7])
		vizHist.at = time.Now()
	}
	for i := 0; i < len(lv) && i < 10; i++ {
		vizHist.rows[0][i] = lv[i]
	}
	return vizHist.rows
}

// drawGrid: a glowing floor in perspective running to the horizon, the sun on the
// horizon and the spectrum as bars standing in the lanes; the levels of a moment
// ago stand further back, so the music runs away towards the horizon.
func drawGrid(b vizBox, lv []float64, bg color.RGBA) {
	rows := pushHistory(lv)
	horizon := b.y + b.h*36/100
	if !isOLEDBackground(bg) {
		vgradient(b, b.y, horizon, color.RGBA{20, 2, 45, 255}, color.RGBA{120, 20, 140, 255})
	}
	if !isOLEDBackground(bg) {
		vgradient(b, horizon, b.y+b.h, color.RGBA{50, 4, 70, 255}, color.RGBA{14, 2, 30, 255})
	}
	cx := b.x + b.w/2
	sun(b, cx, horizon, b.h*28/100)
	line := color.RGBA{255, 50, 210, 255}
	floorH := b.y + b.h - horizon
	lane := b.w / 9
	for k := -7; k <= 7; k++ { // lines running to the vanishing point
		b.line(cx, horizon, cx+k*lane, b.y+b.h-1, max(1, b.h/110), lerpColour(color.RGBA{50, 4, 70, 255}, line, 0.9))
	}
	phase := float64(time.Now().UnixMilli()%1600) / 1600
	for j := 0; j < 8; j++ { // lines across, drifting towards the viewer
		u := (float64(j) + phase) / 8
		y := horizon + int(float64(floorH)*u*u)
		b.rect(b.x, y, b.w, max(1, b.h/110), lerpColour(color.RGBA{50, 4, 70, 255}, line, 0.35+0.65*u))
	}
	b.rect(b.x, horizon, b.w, 2, color.RGBA{255, 130, 235, 255})
	for r := 7; r >= 0; r-- { // bars, the farthest first
		u := 1 - float64(r)/8*0.85
		y := horizon + int(float64(floorH)*u*u)
		fade := 1 - float64(r)/9
		for i := 0; i < 10; i++ {
			lx := cx + int((float64(i)-4.5)*float64(lane)*u)
			bw := max(2, int(float64(lane)*u*0.62))
			bh := int(clamp01(rows[r][i]) * float64(b.h) * 0.6 * u)
			col := lerpColour(bg, lerpColour(color.RGBA{255, 40, 200, 255}, color.RGBA{255, 225, 80, 255}, 0.6), fade)
			b.rect(lx-bw/2, y-bh, bw, bh, col)
			b.rect(lx-bw/2, y-bh, bw, max(1, bh/12), lerpColour(bg, color.RGBA{255, 245, 200, 255}, fade))
		}
	}
}

// drawWave: a colourful sound wave, thin bars mirrored around a centre line,
// the colour running from blue through magenta to orange.
func drawWave(b vizBox, lv []float64, bg color.RGBA) {
	n := len(lv)
	if n < 2 {
		return
	}
	cols := clampInt(b.w/6, 16, 80)
	step := float64(b.w) / float64(cols)
	mid := b.y + b.h/2
	t := float64(time.Now().UnixMilli()%100000) / 1000
	stops := []color.RGBA{{40, 90, 255, 255}, {0, 220, 255, 255}, {230, 50, 220, 255}, {255, 150, 30, 255}}
	for i := 0; i < cols; i++ {
		f := float64(i) / float64(cols-1) * float64(n-1)
		k := int(f)
		if k >= n-1 {
			k = n - 2
		}
		fr := f - float64(k)
		fr = (1 - math.Cos(fr*math.Pi)) / 2
		v := lv[k]*(1-fr) + lv[k+1]*fr
		v *= 1.3 * (0.7 + 0.3*math.Abs(math.Sin(float64(i)*0.55+t*5)))
		bh := int(clamp01(v) * float64(b.h) / 2)
		pos := float64(i) / float64(cols-1) * float64(len(stops)-1)
		si := int(pos)
		if si >= len(stops)-1 {
			si = len(stops) - 2
		}
		c := lerpColour(stops[si], stops[si+1], pos-float64(si))
		x := b.x + int(float64(i)*step)
		w := max(1, int(step)-2)
		b.rect(x, mid-bh, w, 2*bh+1, c)
		b.rect(x, mid-bh-2, w, 2, lerpColour(bg, c, 0.35))
		b.rect(x, mid+bh+1, w, 2, lerpColour(bg, c, 0.35))
	}
	b.rect(b.x, mid, b.w, 1, color.RGBA{255, 255, 255, 255})
}

// drawKITTColumns is the first KITT voicebox: one column per band of the
// spectrum (ten), lit from the middle line outwards, up and down together.
func drawKITTColumns(b vizBox, lv []float64, bg color.RGBA) {
	n := len(lv)
	if n == 0 {
		return
	}
	gap := max(3, b.h/24)
	segGap := max(1, b.h/90)
	half := 7 // segments above and below the middle line
	bw := max(2, (b.w-gap*(n-1))/n)
	segH := max(2, (b.h-segGap*2*half)/(2*half+1))
	red := color.RGBA{255, 30, 20, 255}
	midY := b.y + b.h/2 - segH/2
	for i, v := range lv {
		lit := clampInt(int(v*float64(half)*1.6+0.5), 0, half) // the middle segment is on at any sound
		bx := b.x + i*(bw+gap)
		for s := -half; s <= half; s++ {
			d := s
			if d < 0 {
				d = -d
			}
			c := inactiveVizColour(bg, red, 0.10)
			if d <= lit && v > 0.01 {
				c = lerpColour(color.RGBA{150, 8, 8, 255}, color.RGBA{255, 90, 60, 255}, 1-float64(d)/float64(half+1))
			}
			b.rect(bx, midY+s*(segH+segGap), bw, segH, c)
		}
	}
}

// The visualizer receives playerBackground, which is pure black only in OLED mode.
func isOLEDBackground(bg color.RGBA) bool { return bg.R == 0 && bg.G == 0 && bg.B == 0 }
func inactiveVizColour(bg, lit color.RGBA, amount float64) color.RGBA {
	if isOLEDBackground(bg) {
		return bg
	}
	return lerpColour(bg, lit, amount)
}
