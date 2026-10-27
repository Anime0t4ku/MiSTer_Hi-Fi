package main

import (
	"image/color"
	"testing"
)

func oledTestFB() *framebuffer {
	return &framebuffer{w: 640, h: 480, stride: 640 * 4, bpp: 4, back: make([]byte, 640*480*4)}
}
func oledPixel(fb *framebuffer, x, y int) color.RGBA {
	o := y*fb.stride + x*4
	return color.RGBA{fb.back[o+2], fb.back[o+1], fb.back[o], 255}
}
func TestOLEDMenuAndBadgeSurfaces(t *testing.T) {
	black := color.RGBA{0, 0, 0, 255}
	normal := color.RGBA{35, 37, 46, 255}
	cfg := defaultConfig()
	cfg.OLEDMode = true
	if oledSurface(&cfg, normal) != black || oledSurface(nil, normal) != normal {
		t.Fatal("surface theme mismatch")
	}
	fb := oledTestFB()
	p := newPlayer(Queue{Tracks: []Track{{Title: "Test"}}}, cfg, false)
	app := &App{fb: fb, cfg: &cfg, player: p}
	h := drawNowPlayingBar(app, true)
	y := fb.h - h - 42
	if oledPixel(fb, 34, y+4) != black {
		t.Fatal("Now Playing fill is not black")
	}
	drawInfoBadges(fb, 10, 10, 600, 3, Track{MediaFormat: "FLAC"}, &cfg)
	if oledPixel(fb, 12, 12) != black {
		t.Fatal("OLED badge fill is not black")
	}
	if oledPixel(fb, 10, 10) == black {
		t.Fatal("OLED badge outline missing")
	}
	cfg.OLEDMode = false
	drawInfoBadges(fb, 10, 10, 600, 3, Track{MediaFormat: "FLAC"}, &cfg)
	if oledPixel(fb, 12, 12) != (color.RGBA{255, 255, 255, 255}) {
		t.Fatal("normal badge appearance changed")
	}
}
func TestOLEDVisualizerUnlitSegmentsAndBackgrounds(t *testing.T) {
	black := color.RGBA{0, 0, 0, 255}
	old := ledPeaks
	defer func() { ledPeaks = old }()
	ledPeaks = [32]float64{}
	for _, style := range []string{"led", "kitt", "kitt-original"} {
		fb := oledTestFB()
		fb.fill(black)
		drawVisualizer(fb, style, make([]float64, 10), 20, 20, 400, 200, 1, black)
		for i := 0; i < len(fb.back); i += 4 {
			if fb.back[i] != 0 || fb.back[i+1] != 0 || fb.back[i+2] != 0 {
				t.Fatalf("%s lights inactive pixels", style)
			}
		}
	}
	for _, style := range []string{"sunset", "grid"} {
		fb := oledTestFB()
		fb.fill(black)
		drawVisualizer(fb, style, make([]float64, 10), 20, 20, 400, 200, 1, black)
		if oledPixel(fb, 21, 21) != black {
			t.Fatalf("%s still fills the background", style)
		}
		fb.fill(black)
		drawVisualizer(fb, style, make([]float64, 10), 20, 20, 400, 200, 1, color.RGBA{7, 8, 11, 255})
		if oledPixel(fb, 21, 21) == black {
			t.Fatalf("%s normal gradient missing", style)
		}
	}
}
