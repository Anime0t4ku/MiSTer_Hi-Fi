package main

import (
	"strings"
	"testing"
	"time"
)

func TestExperimentalDefaultsAndValidation(t *testing.T) {
	c := defaultConfig()
	if c.GBAMusicEnabled || c.FPSTarget != 30 {
		t.Fatal("experimental features must default off / 30 FPS")
	}
	for _, fps := range []int{30, 60} {
		next, err := patchedAppConfig(c, map[string]any{"fps_target": fps, "gba_music_enabled": true})
		if err != nil || next.FPSTarget != fps || !next.GBAMusicEnabled {
			t.Fatalf("valid settings: %+v %v", next, err)
		}
		if fpsInterval(fps) != time.Second/time.Duration(fps) {
			t.Fatal("wrong render interval")
		}
	}
	if _, err := patchedAppConfig(c, map[string]any{"fps_target": 45}); err == nil {
		t.Fatal("accepted unsupported FPS")
	}
	app := &App{cfg: &c}
	if err := app.startQueue(Queue{Tracks: []Track{{Path: "gba:1:test.gba"}}}, nil); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatal("disabled GBA queue accepted", err)
	}
}

func TestSettingsShoulderTabsAndExperimentalControls(t *testing.T) {
	old := appConfigPath
	appConfigPath = t.TempDir() + "/config.json"
	defer func() { appConfigPath = old }()
	cfg := defaultConfig()
	acts := make(chan action, 16)
	// R four times reaches Experimental, Down selects FPS, then GBA.
	for _, a := range []action{actNext, actNext, actNext, actNext, actDown, actConfirm, actDown, actConfirm, actPrev, actNext, actConfirm, actBack} {
		acts <- a
	}
	fb := &framebuffer{w: 1024, h: 768, stride: 1024 * 4, bpp: 4, back: make([]byte, 1024*768*4), data: make([]byte, 1024*768*4)}
	app := &App{cfg: &cfg, acts: acts, fb: fb}
	settingsUI(app)
	if cfg.FPSTarget != 60 {
		t.Fatalf("FPS selection failed: %d", cfg.FPSTarget)
	}
	if cfg.GBAMusicEnabled {
		t.Fatal("tab must remember selection: returning and confirming disables GBA")
	}
	loaded := loadConfig()
	if loaded.FPSTarget != 60 || loaded.GBAMusicEnabled {
		t.Fatal("settings did not persist")
	}
	if _, err := externalQueue("gba://test.gba"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatal("disabled external GBA accepted", err)
	}
}
