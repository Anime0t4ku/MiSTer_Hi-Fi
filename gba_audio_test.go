package main

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGBAPreparedDurationAndCleanup(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	old := gbaRenderPCM
	defer func() { gbaRenderPCM = old }()
	gbaRenderPCM = func(ctx context.Context, t Track, w io.Writer) (int64, error) {
		frames := int64(gbaSampleRate * 2)
		_, err := w.Write(make([]byte, frames*4))
		return frames, err
	}
	file, duration, err := prepareGBAAudio(context.Background(), Track{})
	if err != nil || duration != 2 {
		t.Fatal(file, duration, err)
	}
	b, err := os.ReadFile(file)
	if err != nil || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" || binary.LittleEndian.Uint32(b[40:44]) != gbaSampleRate*2*4 {
		t.Fatal("invalid prepared WAV", err)
	}
	os.Remove(file)
	gbaRenderPCM = func(context.Context, Track, io.Writer) (int64, error) { return 0, errors.New("render failed") }
	if _, _, err = prepareGBAAudio(context.Background(), Track{}); err == nil {
		t.Fatal("failed preparation succeeded")
	}
	matches, _ := filepath.Glob(filepath.Join(tmp, "misterhifi-gba-*"))
	if len(matches) != 0 {
		t.Fatal("failed preparation leaked files", matches)
	}
}

func TestGBAPreparationProgressStatus(t *testing.T) {
	cfg := defaultConfig()
	p := newPlayer(Queue{Tracks: []Track{{Path: "gba:1:test.gba"}}}, cfg, false)
	p.mu.Lock()
	p.loading = true
	p.gbaPreparingSince = time.Now().Add(-2 * time.Second)
	p.mu.Unlock()
	ctx := context.WithValue(context.Background(), gbaProgressKey{}, func(frames int64) { p.mu.Lock(); p.gbaPreparedFrames = frames; p.mu.Unlock() })
	reportGBAProgress(ctx, int64(gbaSampleRate*65))
	title, detail := p.preparationStatus()
	if title != "PREPARING GBA..." || !strings.Contains(detail, "AUDIO 01:05") || !strings.Contains(detail, "WAIT") {
		t.Fatal(title, detail)
	}
	p.mu.Lock()
	p.q.Tracks[0].Path = "plex:123"
	p.mu.Unlock()
	title, detail = p.preparationStatus()
	if title != "DOWNLOADING TRACK..." || detail != "" {
		t.Fatal(title, detail)
	}
}
