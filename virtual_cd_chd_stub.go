//go:build !linux || !cgo

package main

import "errors"

type nativeCHDDisc struct{}

func nativeCHDOpen(string) (*nativeCHDDisc, error) {
	return nil, errors.New("CHD playback requires the MiSTer CGO build")
}
func (d *nativeCHDDisc) Close() {}
func (d *nativeCHDDisc) ReadAudioFrame(int64, []byte) error {
	return errors.New("CHD playback requires the MiSTer CGO build")
}
func nativeCHDTracks(string) ([]Track, error) {
	return nil, errors.New("CHD playback requires the MiSTer CGO build")
}
