//go:build !cgo

package main

import "errors"

func nativeAudioStartTrack(Track, EQConfig) error {
	return errors.New("MiSTer Hi-Fi audio engine requires a CGO build")
}
func nativeAudioStartPCM(EQConfig) error {
	return errors.New("MiSTer Hi-Fi audio engine requires a CGO build")
}
func nativeAudioQueueNextTrack(Track) error      { return nil }
func nativeAudioMarkPCMTransition(float64) error { return nil }
func nativeAudioTakeTransition() bool            { return false }
func nativeAudioWritePCM([]byte) error {
	return errors.New("MiSTer Hi-Fi audio engine requires a CGO build")
}
func nativeAudioFinishPCM()          {}
func nativeAudioStop()               {}
func nativeAudioPause(bool)          {}
func nativeAudioSetEQ(EQConfig)      {}
func nativeAudioPosition() float64   { return 0 }
func nativeAudioDuration() float64   { return 0 }
func nativeAudioSeek(float64) error  { return nil }
func nativeAudioEnded() bool         { return false }
func nativeAudioLevels() [10]float64 { return [10]float64{} }

func nativeM4AProbeTrack(t Track) (string, int, int, float64, error) {
	return "M4A", 0, 0, 0, errors.New("M4A decoding unavailable")
}
