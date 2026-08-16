//go:build linux && cgo

package main

/*
#cgo CFLAGS: -std=c11 -O2 -I${SRCDIR}/third_party/libchdr/include
#cgo LDFLAGS: ${SRCDIR}/third_party/libchdr/build-arm/libchdr_bundle.o -lpthread -lm -ldl
#include <stdlib.h>
#include "virtual_cd_chd.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"unsafe"
)

type nativeCHDDisc struct{ p *C.mh_chd_disc }

func nativeCHDOpen(path string) (*nativeCHDDisc, error) {
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	var p *C.mh_chd_disc
	if C.mh_chd_open_disc(cp, &p) != 0 {
		return nil, errors.New(C.GoString(C.mh_chd_last_error()))
	}
	return &nativeCHDDisc{p: p}, nil
}
func (d *nativeCHDDisc) Close() {
	if d != nil && d.p != nil {
		C.mh_chd_close_disc(d.p)
		d.p = nil
	}
}
func (d *nativeCHDDisc) ReadAudioFrame(frame int64, out []byte) error {
	if d == nil || d.p == nil || len(out) < virtualCDSectorBytes {
		return errors.New("invalid CHD audio read")
	}
	if C.mh_chd_read_audio_frame(d.p, C.uint64_t(frame), unsafe.Pointer(&out[0])) != 0 {
		return errors.New(C.GoString(C.mh_chd_last_error()))
	}
	return nil
}
func nativeCHDTracks(path string) ([]Track, error) {
	d, err := nativeCHDOpen(path)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	n := int(C.mh_chd_get_track_count(d.p))
	out := make([]Track, 0, n)
	enc := url.QueryEscape(path)
	for i := 0; i < n; i++ {
		var inf C.mh_chd_track_info
		if C.mh_chd_get_track(d.p, C.int(i), &inf) != 0 {
			continue
		}
		if inf.is_audio == 0 {
			continue
		}
		frames := int64(inf.frames)
		if frames <= 0 {
			continue
		}
		start := int64(inf.start_frame)
		num := int(inf.track_number)
		vp := fmt.Sprintf("vcdchd:%s:%d:%d", enc, start, frames)
		out = append(out, Track{Path: vp, Title: fmt.Sprintf("Track %02d", num), Album: filepath.Base(path), Duration: float64(frames) / 75.0, MediaFormat: "CDDA", BitDepth: 16, SampleRate: 44100, BitRate: 1411})
	}
	if len(out) == 0 {
		return nil, errors.New("disc contains no audio tracks")
	}
	return out, nil
}
