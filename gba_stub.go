//go:build !linux || !cgo

package main

import (
	"context"
	"errors"
	"io"
)

type gbaROM struct{}

func gbaOpen(string) (*gbaROM, error) {
	return nil, errors.New("GBA playback requires the MiSTer CGO build")
}
func gbaAcquire(string) (*gbaROM, error) {
	return nil, errors.New("GBA playback requires the MiSTer CGO build")
}
func gbaRelease(*gbaROM)        {}
func (r *gbaROM) Title() string { return "" }
func (r *gbaROM) Code() string  { return "" }
func (r *gbaROM) Songs() []int  { return nil }

func gbaTrackPath(song int, rom string) string { return "" }
func gbaSongLabel(i int) string                { return "" }

func renderGBAPCM(context.Context, Track, io.Writer) (int64, error) {
	return 0, errors.New("GBA playback requires the MiSTer CGO build")
}
