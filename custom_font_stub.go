//go:build !linux || !cgo

package main

type customGlyphBitmap struct {
	pixels     []byte
	w, h       int
	xoff, yoff int
	advance    int
}

func customFontValid(string) bool                         { return false }
func setCustomFont(string) bool                           { return true }
func customFontGlyph(rune, int) (customGlyphBitmap, bool) { return customGlyphBitmap{}, false }
func customFontAdvance(rune, int) (int, bool)             { return 0, false }
