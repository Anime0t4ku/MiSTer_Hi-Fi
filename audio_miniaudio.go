//go:build linux && cgo

package main

/*
#cgo CFLAGS: -std=c11 -O2
#cgo LDFLAGS: -L${SRCDIR}/m4a_decoder/target/armv7-unknown-linux-gnueabihf/release -lmisterhifi_m4a -lpthread -lm -ldl -latomic
#include <stdlib.h>
#include "audio_engine.h"
#include "m4a_bridge.h"
*/
import "C"

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"
)

var nativeAudioControlMu sync.Mutex

func nativeAudioStartTrack(t Track, eq EQConfig) error {
	nativeAudioControlMu.Lock()
	defer nativeAudioControlMu.Unlock()
	f, err := openTrackFile(t)
	if err != nil {
		return err
	}
	defer f.Close()
	enabled := C.int(0)
	if eq.Enabled {
		enabled = 1
	}
	var r C.int
	if strings.EqualFold(filepath.Ext(t.Path), ".m4a") {
		r = C.mh_audio_start_m4a_fd(C.int(f.Fd()), enabled, C.float(eq.Bass), C.float(eq.LowMid), C.float(eq.Mid), C.float(eq.HighMid), C.float(eq.Treble))
	} else {
		r = C.mh_audio_start_fd(C.int(f.Fd()), enabled, C.float(eq.Bass), C.float(eq.LowMid), C.float(eq.Mid), C.float(eq.HighMid), C.float(eq.Treble))
	}
	if r != 0 {
		return errors.New(C.GoString(C.mh_audio_last_error()))
	}
	return nil
}

func nativeAudioStartURL(rawURL string, eq EQConfig) (func(), error) {
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("User-Agent", "MiSTer-Hi-Fi/"+version)
	req.Header.Set("Icy-MetaData", "0")
	req.Header.Set("Accept", "audio/*,*/*;q=0.8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("radio stream returned %s", resp.Status)
	}
	br := bufio.NewReaderSize(resp.Body, 16384)
	probe, _ := br.Peek(8192)
	encoding := radioEncodingHint(probe, resp.Header.Get("Content-Type"), rawURL)
	switch encoding {
	case 6:
		resp.Body.Close()
		cancel()
		return nil, errors.New("AAC/AAC+ radio streams are not supported")
	case 7:
		resp.Body.Close()
		cancel()
		return nil, errors.New("Opus radio streams are not supported")
	case 0:
		resp.Body.Close()
		cancel()
		return nil, errors.New("unsupported or unrecognized radio stream format")
	}
	if encoding == 2 {
		if off := firstMP3FrameOffset(probe); off > 0 {
			_, _ = br.Discard(off)
		}
	}

	r, w, err := os.Pipe()
	if err != nil {
		resp.Body.Close()
		cancel()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer w.Close()
		defer resp.Body.Close()
		_, _ = io.Copy(w, br)
	}()

	enabled := C.int(0)
	if eq.Enabled {
		enabled = 1
	}
	nativeAudioControlMu.Lock()
	rc := C.mh_audio_start_stream_fd(C.int(r.Fd()), C.int(encoding), enabled, C.float(eq.Bass), C.float(eq.LowMid), C.float(eq.Mid), C.float(eq.HighMid), C.float(eq.Treble))
	nativeAudioControlMu.Unlock()
	_ = r.Close()
	if rc != 0 {
		cancel()
		_ = w.Close()
		<-done
		return nil, errors.New(C.GoString(C.mh_audio_last_error()))
	}
	stop := func() {
		cancel()
		_ = w.Close()
	}
	return stop, nil
}

func firstMP3FrameOffset(b []byte) int {
	for i := 0; i+4 <= len(b); i++ {
		if b[i] != 0xff || (b[i+1]&0xe0) != 0xe0 {
			continue
		}
		version := (b[i+1] >> 3) & 0x03
		layer := (b[i+1] >> 1) & 0x03
		bitrate := (b[i+2] >> 4) & 0x0f
		sampleRate := (b[i+2] >> 2) & 0x03
		if version == 1 || layer == 0 || bitrate == 0 || bitrate == 0x0f || sampleRate == 3 {
			continue
		}
		return i
	}
	return -1
}

func radioEncodingHint(probe []byte, contentType, rawURL string) int {
	lowerProbe := bytes.ToLower(probe)
	ct := strings.ToLower(contentType)
	u := strings.ToLower(rawURL)

	if bytes.HasPrefix(probe, []byte("fLaC")) {
		return 1
	}
	if bytes.HasPrefix(probe, []byte("OggS")) {
		if bytes.Contains(probe, []byte("fLaC")) || bytes.Contains(probe, []byte("FLAC")) {
			return 5
		}
		if bytes.Contains(lowerProbe, []byte("vorbis")) {
			return 3
		}
		if bytes.Contains(probe, []byte("OpusHead")) {
			return 7
		}
	}
	if bytes.Contains(lowerProbe, []byte("vorbis")) {
		return 3
	}
	if bytes.Contains(probe, []byte("fLaC")) {
		return 1
	}
	if bytes.HasPrefix(probe, []byte("RIFF")) && len(probe) >= 12 && string(probe[8:12]) == "WAVE" {
		return 4
	}
	if looksLikeADTS(probe) {
		return 6
	}
	if bytes.HasPrefix(probe, []byte("ID3")) || firstMP3FrameOffset(probe) >= 0 {
		return 2
	}
	if strings.Contains(ct, "flac") || strings.Contains(u, ".flac") || strings.HasSuffix(u, "/flac") {
		return 1
	}
	if strings.Contains(ct, "aac") || strings.Contains(ct, "aacp") || strings.Contains(u, ".aac") {
		return 6
	}
	if strings.Contains(ct, "opus") || strings.Contains(u, ".opus") {
		return 7
	}
	if strings.Contains(ct, "mpeg") || strings.Contains(ct, "mp3") || strings.Contains(u, ".mp3") {
		return 2
	}
	if strings.Contains(ct, "vorbis") || strings.Contains(u, ".ogg") || strings.Contains(u, ".oga") {
		return 3
	}
	if strings.Contains(ct, "wav") || strings.Contains(u, ".wav") {
		return 4
	}
	return 0
}

func looksLikeADTS(b []byte) bool {
	for i := 0; i+7 <= len(b); i++ {
		if b[i] != 0xff || (b[i+1]&0xf6) != 0xf0 {
			continue
		}
		sampleRateIndex := (b[i+2] >> 2) & 0x0f
		if sampleRateIndex == 0x0f {
			continue
		}
		frameLength := int(b[i+3]&0x03)<<11 | int(b[i+4])<<3 | int((b[i+5]>>5)&0x07)
		if frameLength >= 7 {
			return true
		}
	}
	return false
}

func nativeAudioStartPCM(eq EQConfig) error {
	nativeAudioControlMu.Lock()
	defer nativeAudioControlMu.Unlock()
	enabled := C.int(0)
	if eq.Enabled {
		enabled = 1
	}
	if C.mh_audio_start_pcm(enabled, C.float(eq.Bass), C.float(eq.LowMid), C.float(eq.Mid), C.float(eq.HighMid), C.float(eq.Treble)) != 0 {
		return errors.New(C.GoString(C.mh_audio_last_error()))
	}
	return nil
}

func nativeAudioQueueNextTrack(t Track) error {
	nativeAudioControlMu.Lock()
	defer nativeAudioControlMu.Unlock()
	f, err := openTrackFile(t)
	if err != nil {
		return err
	}
	defer f.Close()
	if C.mh_audio_queue_next_fd(C.int(f.Fd())) != 0 {
		return errors.New("unable to queue gapless track")
	}
	return nil
}

func nativeAudioMarkPCMTransition(nextDuration float64) error {
	nativeAudioControlMu.Lock()
	defer nativeAudioControlMu.Unlock()
	if C.mh_audio_mark_pcm_transition(C.double(nextDuration)) != 0 {
		return errors.New("unable to queue gapless CD transition")
	}
	return nil
}

func nativeAudioTakeTransition() bool { return C.mh_audio_take_transition() != 0 }

func nativeAudioWritePCM(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	if C.mh_audio_write_pcm(unsafe.Pointer(&b[0]), C.size_t(len(b))) != 0 {
		return errors.New(C.GoString(C.mh_audio_last_error()))
	}
	return nil
}

func nativeAudioFinishPCM() { C.mh_audio_finish_pcm() }
func nativeAudioStop() {
	nativeAudioControlMu.Lock()
	defer nativeAudioControlMu.Unlock()
	C.mh_audio_stop()
}

func nativeAudioSetEQ(eq EQConfig) {
	enabled := C.int(0)
	if eq.Enabled {
		enabled = 1
	}
	C.mh_audio_set_eq(enabled, C.float(eq.Bass), C.float(eq.LowMid), C.float(eq.Mid), C.float(eq.HighMid), C.float(eq.Treble))
}

func nativeAudioPause(paused bool) {
	v := C.int(0)
	if paused {
		v = 1
	}
	C.mh_audio_pause(v)
}

func nativeAudioPosition() float64 { return float64(C.mh_audio_position()) }
func nativeAudioDuration() float64 { return float64(C.mh_audio_duration()) }
func nativeAudioSeek(seconds float64) error {
	nativeAudioControlMu.Lock()
	defer nativeAudioControlMu.Unlock()
	if C.mh_audio_seek(C.double(seconds)) != 0 {
		return errors.New("unable to seek audio")
	}
	return nil
}
func nativeAudioEnded() bool { return C.mh_audio_ended() != 0 }

func nativeAudioLevels() [10]float64 {
	var out [10]float64
	var vals [10]C.float
	C.mh_audio_levels(&vals[0])
	for i := 0; i < 10; i++ {
		out[i] = float64(vals[i])
	}
	return out
}

func nativeM4AProbeTrack(t Track) (string, int, int, float64, error) {
	f, err := openTrackFile(t)
	if err != nil {
		return "", 0, 0, 0, err
	}
	defer f.Close()
	var codec, rate, bits C.int
	var duration C.double
	if C.mh_m4a_probe_fd(C.int(f.Fd()), &codec, &rate, &bits, &duration) != 0 {
		return "", 0, 0, 0, errors.New("unable to read M4A stream information")
	}
	name := "M4A"
	if codec == 1 {
		name = "AAC"
	} else if codec == 2 {
		name = "ALAC"
	}
	return name, int(rate), int(bits), float64(duration), nil
}
