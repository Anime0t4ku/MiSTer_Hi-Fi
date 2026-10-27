//go:build !cgo

package main

import (
	"errors"
	"testing"
)

func TestGBAUnsupportedGameReturnsToHighlightedFile(t *testing.T) {
	fakeGBAStorage(t, false)
	attempts := 0
	gbaWebTrackList = func(gameBoyROM) ([]Track, error) { attempts++; return nil, errors.New("no MP2K sound engine found") }
	cfg := defaultConfig()
	cfg.GBAMusicEnabled = true
	acts := make(chan action, 16)
	// Enter Folder, select Game, dismiss its error, then confirm again without
	// moving: the same game must still be selected. Finally leave both folders.
	for _, a := range []action{actConfirm, actDown, actConfirm, actConfirm, actConfirm, actBack, actBack, actBack} {
		acts <- a
	}
	app := &App{cfg: &cfg, acts: acts, fb: oledTestFB()}
	gameBoyUI(app)
	if attempts != 2 {
		t.Fatalf("unsupported game did not retain its selection: %d attempts", attempts)
	}
}
