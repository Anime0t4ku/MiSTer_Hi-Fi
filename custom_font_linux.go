//go:build linux && cgo

package main

/*
#cgo CFLAGS: -std=c11 -O2
#cgo LDFLAGS: -lm
#include <stdlib.h>
#include "custom_font.h"
*/
import "C"

import (
	"sync"
	"unsafe"
)

type customGlyphKey struct {
	ch rune
	px int
}

type customGlyphBitmap struct {
	pixels     []byte
	w, h       int
	xoff, yoff int
	advance    int
}

var customFontMu sync.RWMutex
var customFontEnabled bool
var customGlyphCache sync.Map
var customAdvanceCache sync.Map

func clearCustomFontCaches() {
	customGlyphCache = sync.Map{}
	customAdvanceCache = sync.Map{}
}

func customFontValid(path string) bool {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	return C.mh_font_probe(p) != 0
}

func setCustomFont(path string) bool {
	customFontMu.Lock()
	defer customFontMu.Unlock()
	clearCustomFontCaches()
	if path == "" {
		C.mh_font_shutdown()
		customFontEnabled = false
		return true
	}
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	if C.mh_font_init(p) != 0 {
		customFontEnabled = false
		return false
	}
	customFontEnabled = true
	return true
}

func customFontGlyph(ch rune, px int) (customGlyphBitmap, bool) {
	if px <= 0 {
		return customGlyphBitmap{}, false
	}
	customFontMu.RLock()
	defer customFontMu.RUnlock()
	if !customFontEnabled {
		return customGlyphBitmap{}, false
	}
	key := customGlyphKey{ch: ch, px: px}
	if cached, ok := customGlyphCache.Load(key); ok {
		return cached.(customGlyphBitmap), true
	}
	if C.mh_font_has_glyph(C.int(ch)) == 0 {
		return customGlyphBitmap{}, false
	}
	var w, h, xoff, yoff, advance C.int
	bitmap := C.mh_font_render(C.int(ch), C.int(px), &w, &h, &xoff, &yoff, &advance)
	if bitmap == nil {
		return customGlyphBitmap{}, false
	}
	defer C.mh_font_free_bitmap(bitmap)
	count := int(w) * int(h)
	pixels := C.GoBytes(unsafe.Pointer(bitmap), C.int(count))
	g := customGlyphBitmap{pixels: pixels, w: int(w), h: int(h), xoff: int(xoff), yoff: int(yoff), advance: int(advance)}
	if g.advance <= 0 {
		g.advance = px
	}
	customGlyphCache.Store(key, g)
	customAdvanceCache.Store(key, g.advance)
	return g, true
}

func customFontAdvance(ch rune, px int) (int, bool) {
	if px <= 0 {
		return 0, false
	}
	customFontMu.RLock()
	defer customFontMu.RUnlock()
	if !customFontEnabled {
		return 0, false
	}
	key := customGlyphKey{ch: ch, px: px}
	if cached, ok := customAdvanceCache.Load(key); ok {
		return cached.(int), true
	}
	if C.mh_font_has_glyph(C.int(ch)) == 0 {
		return 0, false
	}
	adv := int(C.mh_font_advance(C.int(ch), C.int(px)))
	if adv <= 0 {
		return 0, false
	}
	customAdvanceCache.Store(key, adv)
	return adv, true
}
