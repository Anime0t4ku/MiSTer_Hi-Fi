package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

const gbaSampleRate = 22050

// Synthesize before playback so CPU-heavy MP2K mixing cannot starve the
// realtime audio callback. The measured PCM count supplies the exact duration.
var gbaRenderPCM = renderGBAPCM
var gbaStartPreparedAudio = nativeAudioStartTrack

func prepareGBAAudio(ctx context.Context, t Track) (string, float64, error) {
	f, err := os.CreateTemp("", "misterhifi-gba-*.wav")
	if err != nil {
		return "", 0, err
	}
	name := f.Name()
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(name)
		}
	}()
	if _, err = f.Write(make([]byte, 44)); err != nil {
		return "", 0, err
	}
	frames, err := gbaRenderPCM(ctx, t, f)
	if err != nil {
		return "", 0, err
	}
	if err = ctx.Err(); err != nil {
		return "", 0, err
	}
	if frames <= 0 || frames > int64(gbaSampleRate*60*20) {
		return "", 0, errors.New("GBA song is empty or exceeds the 20-minute preparation limit")
	}
	header := gbaWAVHeader(frames)
	if _, err = f.WriteAt(header, 0); err != nil {
		return "", 0, err
	}
	if err = f.Close(); err != nil {
		return "", 0, err
	}
	ok = true
	return name, float64(frames) / gbaSampleRate, nil
}
func gbaWAVHeader(frames int64) []byte {
	h := make([]byte, 44)
	copy(h, "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(frames*4+36))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 2)
	binary.LittleEndian.PutUint32(h[24:], gbaSampleRate)
	binary.LittleEndian.PutUint32(h[28:], gbaSampleRate*4)
	binary.LittleEndian.PutUint16(h[32:], 4)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(frames*4))
	return h
}
func (p *Player) prepareGBATrack(ctx context.Context, t Track, idx int, stop chan struct{}, generation uint64, cfg Config) {
	// Bound preparation even for damaged ROMs or unexpectedly long sequences.
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	ctx = context.WithValue(ctx, gbaProgressKey{}, func(frames int64) {
		p.mu.Lock()
		if p.stop == stop && p.generation == generation {
			p.gbaPreparedFrames = frames
		}
		p.mu.Unlock()
	})
	log.Printf("GBA: preparing %s", t.Title)
	file, duration, err := prepareGBAAudio(ctx, t)
	if err != nil {
		log.Printf("GBA: preparation failed: %v", err)
	} else {
		log.Printf("GBA: prepared %.2f seconds of audio", duration)
	}
	if file != "" {
		defer os.Remove(file)
	}
	p.opMu.Lock()
	defer p.opMu.Unlock()
	p.mu.Lock()
	if p.stop != stop || p.generation != generation {
		p.mu.Unlock()
		return
	}
	if err == nil {
		err = ctx.Err()
	}
	paused := p.paused
	p.mu.Unlock()
	if err == nil {
		local := t
		local.Path = file
		local.UseDirFD = false
		local.Duration = duration
		err = gbaStartPreparedAudio(local, cfg.EQ)
	}
	p.mu.Lock()
	p.loading = false
	if err != nil {
		p.playbackError = err.Error()
		p.stopped = true
		close(stop)
		p.stop = nil
		p.mu.Unlock()
		clearNowPlaying()
		return
	}
	t.Duration = duration
	t.SampleRate = gbaSampleRate
	t.BitDepth = 16
	t.BitRate = gbaSampleRate * 32 / 1000
	p.q.Tracks[idx] = t
	p.mu.Unlock()
	nativeAudioPause(paused)
	lastfmTrackStarted(cfg.LastFM, t.Artist, t.Title, t.Album, duration)
	lbTrackStarted(cfg.ListenBrainz, t.Artist, t.Title, t.Album, duration)
	// opMu is already held here; publishCurrent would acquire it again.
	p.publishCurrentUnlocked()
	go p.monitorPlayback(stop, generation)
}

type gbaProgressKey struct{}

func reportGBAProgress(ctx context.Context, frames int64) {
	if report, ok := ctx.Value(gbaProgressKey{}).(func(int64)); ok {
		report(frames)
	}
}
func (p *Player) preparationStatus() (string, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.q.Index >= 0 && p.q.Index < len(p.q.Tracks) && strings.HasPrefix(p.q.Tracks[p.q.Index].Path, "gba:") {
		seconds := p.gbaPreparedFrames / gbaSampleRate
		elapsed := 0
		if !p.gbaPreparingSince.IsZero() {
			elapsed = int(time.Since(p.gbaPreparingSince).Seconds())
		}
		return "PREPARING GBA...", fmt.Sprintf("AUDIO %02d:%02d | WAIT %dS", seconds/60, seconds%60, elapsed)
	}
	return "DOWNLOADING TRACK...", ""
}
