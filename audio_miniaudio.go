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
	"errors"
	"path/filepath"
	"strings"
	"unsafe"
)

func nativeAudioStartTrack(t Track, eq EQConfig) error {
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

func nativeAudioStartPCM(eq EQConfig) error {
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
func nativeAudioStop()      { C.mh_audio_stop() }

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
