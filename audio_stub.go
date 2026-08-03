//go:build !cgo

package main

import "errors"

func nativeAudioStartFile(string, EQConfig) error {
	return errors.New("MiSTer Hi-Fi audio engine requires a CGO build")
}
func nativeAudioStartPCM(EQConfig) error {
	return errors.New("MiSTer Hi-Fi audio engine requires a CGO build")
}
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
