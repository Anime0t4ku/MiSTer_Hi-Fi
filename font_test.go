package main

import "testing"

// Song titles are full of punctuation: the built-in font must draw it, or the
// text shows gaps (and tw() under-counts the width of what is drawn).
func TestBuiltInFontHasCommonPunctuation(t *testing.T) {
	for _, r := range "ABCXYZ0189 -_.,:;!?'\"&()[]/+=%<>*#@$|~^\\" {
		if _, ok := font[r]; !ok {
			t.Errorf("no glyph for %q", r)
		}
	}
}

func TestTitleDoesNotRunIntoTheClock(t *testing.T) {
	fb := &framebuffer{w: 640, h: 480, stride: 1280, bpp: 2, back: make([]byte, 640*480*2)}
	cx, cy, _, ch := clockBox(fb)
	if cx <= 0 || cx >= fb.w || cy < 0 || ch <= 0 {
		t.Fatalf("clock box is odd: %d,%d %d", cx, cy, ch)
	}
}
