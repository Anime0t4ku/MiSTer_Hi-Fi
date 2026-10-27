//go:build linux && cgo

package main

/*
#cgo CFLAGS: -I${SRCDIR}/gba
#cgo LDFLAGS: ${SRCDIR}/third_party/agbplay-src/build-arm/libagbcore.a -static-libgcc -Wl,-Bstatic -lstdc++ -Wl,-Bdynamic -lm
#include <stdlib.h>
#include "gba_player.h"
*/
import "C"

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unsafe"
)

// Music from GBA ROMs (gba/gba_player.h; the menus are in gameboy_ui.go).
// A song's track path is "gba:<song id>:<rom path>"; the songs are rendered by
// agbplay's core and prepared as WAV before the normal file engine plays it.

type gbaROM struct{ p *C.gba_rom }

// gbaOpen opens a .gba file, or the first .gba inside a .zip: that is unpacked
// to a temporary file, which can go as soon as the engine has read it.
func gbaOpen(path string) (*gbaROM, error) {
	if strings.EqualFold(filepath.Ext(path), ".zip") {
		tmp, err := gbaUnzip(path)
		if err != nil {
			return nil, err
		}
		defer os.Remove(tmp)
		path = tmp
	}
	return gbaOpenFile(path)
}

func gbaUnzip(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if !strings.EqualFold(filepath.Ext(f.Name), ".gba") {
			continue
		}
		in, err := f.Open()
		if err != nil {
			return "", err
		}
		defer in.Close()
		out, err := os.CreateTemp("", "hifi-gba-*.gba")
		if err != nil {
			return "", err
		}
		_, err = io.Copy(out, in)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(out.Name())
			return "", err
		}
		return out.Name(), nil
	}
	return "", errors.New("no .gba file in the zip")
}

func gbaOpenFile(path string) (*gbaROM, error) {
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	var errbuf [256]C.char
	r := C.gba_open(cp, &errbuf[0], C.int(len(errbuf)))
	if r == nil {
		return nil, errors.New(C.GoString(&errbuf[0]))
	}
	return &gbaROM{r}, nil
}

func (r *gbaROM) closeNow()     { C.gba_close(r.p) }
func (r *gbaROM) Title() string { return strings.TrimSpace(C.GoString(C.gba_title(r.p))) }
func (r *gbaROM) Code() string  { return C.GoString(C.gba_code(r.p)) }

// Songs are the engine's song ids that have music in them.
func (r *gbaROM) Songs() []int {
	n := int(C.gba_song_count(r.p))
	ids := make([]int, n)
	for i := range ids {
		ids[i] = int(C.gba_song_id(r.p, C.int(i)))
	}
	return ids
}

// Opening a ROM reads it and looks for the song table, a second on the MiSTer,
// so the last one stays open: the next song of the same game starts at once.
// A ROM is only read by its players, so several can use it; it is closed when
// it is no longer the cached one and nobody uses it.
var gbaCache struct {
	sync.Mutex
	path string
	rom  *gbaROM
	refs map[*gbaROM]int
}

// gbaAcquire returns the ROM at path, opened or from the cache; every call is
// paired with gbaRelease.
func gbaAcquire(path string) (*gbaROM, error) {
	gbaCache.Lock()
	defer gbaCache.Unlock()
	if gbaCache.refs == nil {
		gbaCache.refs = map[*gbaROM]int{}
	}
	if gbaCache.rom != nil && gbaCache.path == path {
		gbaCache.refs[gbaCache.rom]++
		return gbaCache.rom, nil
	}
	r, err := gbaOpen(path)
	if err != nil {
		return nil, err
	}
	if old := gbaCache.rom; old != nil && gbaCache.refs[old] == 0 {
		delete(gbaCache.refs, old)
		old.closeNow()
	}
	gbaCache.path, gbaCache.rom = path, r
	gbaCache.refs[r] = 1
	return r, nil
}

func gbaRelease(r *gbaROM) {
	gbaCache.Lock()
	defer gbaCache.Unlock()
	gbaCache.refs[r]--
	if gbaCache.refs[r] == 0 && gbaCache.rom != r {
		delete(gbaCache.refs, r)
		r.closeNow()
	}
}

func gbaTrackPath(song int, rom string) string { return "gba:" + strconv.Itoa(song) + ":" + rom }

func parseGBATrackPath(p string) (int, string, error) {
	x := strings.TrimPrefix(p, "gba:")
	i := strings.Index(x, ":")
	if i < 0 {
		return 0, "", errors.New("invalid GBA track")
	}
	id, err := strconv.Atoi(x[:i])
	if err != nil {
		return 0, "", errors.New("invalid GBA track")
	}
	return id, x[i+1:], nil
}

// renderGBAPCM prepares a bounded two-loop song without touching the live audio
// engine. Only this worker owns its player. Cancellation is checked each chunk.
func renderGBAPCM(ctx context.Context, t Track, out io.Writer) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	song, path, err := parseGBATrackPath(t.Path)
	if err != nil {
		return 0, err
	}
	rom, err := gbaAcquire(path)
	if err != nil {
		return 0, err
	}
	defer gbaRelease(rom)
	pl := C.gba_start(rom.p, C.int(song), gbaSampleRate, 2, 0)
	if pl == nil {
		return 0, errors.New("cannot start GBA song")
	}
	defer C.gba_stop(pl)
	const chunk = 4096
	floats := make([]float32, chunk*2)
	pcm := make([]byte, chunk*4)
	var total int64
	var reported int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n := int(C.gba_render(pl, (*C.float)(unsafe.Pointer(&floats[0])), chunk))
		for i := 0; i < n*2; i++ {
			v := int(floats[i] * 32767)
			if v > 32767 {
				v = 32767
			} else if v < -32768 {
				v = -32768
			}
			pcm[i*2] = byte(v)
			pcm[i*2+1] = byte(v >> 8)
		}
		if n > 0 {
			if _, err := out.Write(pcm[:n*4]); err != nil {
				return total, err
			}
			total += int64(n)
			if total-reported >= gbaSampleRate {
				reportGBAProgress(ctx, total)
				reported = total
			}
		}
		if n < chunk {
			reportGBAProgress(ctx, total)
			return total, nil
		}
		if total > gbaSampleRate*60*20 {
			return total, errors.New("GBA song exceeds the 20-minute preparation limit")
		}
	}
}
func gbaSongLabel(i int) string { return fmt.Sprintf("SONG %d", i+1) }
