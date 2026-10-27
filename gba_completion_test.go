//go:build !cgo

package main

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestGBAPreparationCompletesWithoutRelockingPlayer(t *testing.T) {
	exportTestPaths(t)
	oldRender, oldStart := gbaRenderPCM, gbaStartPreparedAudio
	defer func() { gbaRenderPCM, gbaStartPreparedAudio = oldRender, oldStart }()
	gbaRenderPCM = func(_ context.Context, _ Track, w io.Writer) (int64, error) {
		_, err := w.Write(make([]byte, 4*gbaSampleRate))
		return gbaSampleRate, err
	}
	var preparedPath string
	gbaStartPreparedAudio = func(track Track, _ EQConfig) error { preparedPath = track.Path; return nil }
	cfg := defaultConfig()
	p := newPlayer(Queue{Tracks: []Track{{Path: "gba:0:test.gba", Title: "Test"}}}, cfg, false)
	stop := make(chan struct{})
	p.stop = stop
	p.generation = 1
	p.loading = true
	p.stopped = false
	done := make(chan struct{})
	go func() { p.prepareGBATrack(context.Background(), p.q.Tracks[0], 0, stop, 1, cfg); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("preparation deadlocked after starting audio")
	}
	p.opMu.Lock()
	p.opMu.Unlock()
	if p.isLoading() {
		t.Fatal("still loading")
	}
	if p.durationNow() != time.Second {
		t.Fatal("duration was not committed")
	}
	if _, err := os.Stat(preparedPath); !os.IsNotExist(err) {
		t.Fatal("temporary audio was not removed")
	}
	p.stopPlaybackRaw()
}
func TestPlayerStatusFitsNarrowPanels(t *testing.T) {
	for _, size := range [][2]int{{180, 24}, {300, 36}, {540, 48}} {
		lines := playerStatusLines("PREPARING GBA...", "AUDIO 19:59 | WAIT 179S", size[0], size[1], 3)
		if len(lines) != 2 {
			t.Fatal("missing loading counters", size, lines)
		}
		for _, line := range lines {
			if tw(line.Scale, line.Text) > size[0] || line.Y+7*line.Scale > size[1] {
				t.Fatal("cropped loading counter", size, line)
			}
		}
		if !strings.Contains(lines[1].Text, "179S") {
			t.Fatal("counter truncated", lines)
		}
	}
}
