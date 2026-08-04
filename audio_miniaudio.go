//go:build linux && cgo

package main

/*
#cgo CFLAGS: -std=c11 -O2
#cgo LDFLAGS: -lpthread -lm -ldl -latomic
#include <stdlib.h>
#include "audio_engine.h"
*/
import "C"

import (
	"errors"
	"unsafe"
)

func nativeAudioStartFile(path string, eq EQConfig) error {
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	enabled := C.int(0)
	if eq.Enabled {
		enabled = 1
	}
	r := C.mh_audio_start_file(cp, enabled, C.float(eq.Bass), C.float(eq.LowMid), C.float(eq.Mid), C.float(eq.HighMid), C.float(eq.Treble))
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

func nativeAudioQueueNextFile(path string) error {
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	if C.mh_audio_queue_next_file(cp) != 0 {
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
