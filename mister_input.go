package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// MiSTer Main stores its system-wide controller definition as 32 little-endian
// uint32 values. The first entries are the virtual MiSTer gamepad controls.
const (
	misterMapEntries = 32
	misterKeyMax     = 0x2ff
	misterKeyEmu     = misterKeyMax + 1

	misterBtnRight  = 0
	misterBtnLeft   = 1
	misterBtnDown   = 2
	misterBtnUp     = 3
	misterBtnA      = 4
	misterBtnB      = 5
	misterBtnX      = 6
	misterBtnY      = 7
	misterBtnL      = 8
	misterBtnR      = 9
	misterBtnSelect = 10
	misterBtnStart  = 11
	misterBtnOSD1   = 21
	misterBtnOSD2   = 22
)

type inputAbsInfo struct {
	Value      int32
	Minimum    int32
	Maximum    int32
	Fuzz       int32
	Flat       int32
	Resolution int32
}

type misterControllerMap struct {
	values      [misterMapEntries]uint32
	axisInfo    map[uint16]inputAbsInfo
	axisState   map[uint16]int
	pressed     map[uint16]bool
	osdPressed1 bool
	osdPressed2 bool
	path        string
}

func readHexInputID(eventPath, field string) (uint16, error) {
	name := filepath.Base(eventPath)
	b, err := os.ReadFile(filepath.Join("/sys/class/input", name, "device/id", field))
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(b)), 16, 16)
	return uint16(v), err
}

func findMisterMapPath(eventPath string) string {
	vid, err := readHexInputID(eventPath, "vendor")
	if err != nil {
		return ""
	}
	pid, err := readHexInputID(eventPath, "product")
	if err != nil {
		return ""
	}

	dir := "/media/fat/config/inputs"
	exact := filepath.Join(dir, fmt.Sprintf("input_%04x_%04x_v3.map", vid, pid))
	if st, err := os.Stat(exact); err == nil && !st.IsDir() {
		return exact
	}

	// controller_unique_mapping and a few special devices can add an identifier
	// between VID/PID and the v3 suffix. Prefer the first matching Main map.
	patterns := []string{
		filepath.Join(dir, fmt.Sprintf("input_%04x_%04x_*_v3.map", vid, pid)),
		filepath.Join(dir, fmt.Sprintf("input_%04x_%04x*_v3.map", vid, pid)),
	}
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		if len(matches) != 0 {
			return matches[0]
		}
	}
	return ""
}

func loadMisterControllerMap(eventPath string) *misterControllerMap {
	path := findMisterMapPath(eventPath)
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) < misterMapEntries*4 {
		return nil
	}
	m := &misterControllerMap{
		axisInfo:  make(map[uint16]inputAbsInfo),
		axisState: make(map[uint16]int),
		pressed:   make(map[uint16]bool),
		path:      path,
	}
	for i := range m.values {
		m.values[i] = binary.LittleEndian.Uint32(b[i*4 : i*4+4])
	}
	return m
}

func eviocgabs(code uint16) uintptr {
	// _IOR('E', 0x40 + code, struct input_absinfo), struct size = 24 bytes.
	return uintptr(0x80184540 + uint32(code))
}

func (m *misterControllerMap) absInfoFor(f *os.File, code uint16) inputAbsInfo {
	if info, ok := m.axisInfo[code]; ok {
		return info
	}
	var info inputAbsInfo
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), eviocgabs(code), uintptr(unsafe.Pointer(&info)))
	if errno != 0 || info.Maximum <= info.Minimum {
		// Works for the normal -1/0/+1 hat case if querying metadata fails.
		info.Minimum = -1
		info.Maximum = 1
	}
	m.axisInfo[code] = info
	return info
}

func axisDigitalState(value int32, info inputAbsInfo) int {
	center := info.Minimum + (info.Maximum-info.Minimum)/2
	rangeSize := info.Maximum - info.Minimum
	if rangeSize <= 2 {
		if value < center {
			return -1
		}
		if value > center {
			return 1
		}
		return 0
	}
	// MiSTer turns analog axes into digital edge events. A quarter-range from
	// center keeps navigation deliberate while still supporting analog D-pads.
	threshold := rangeSize / 4
	if value <= center-threshold {
		return -1
	}
	if value >= center+threshold {
		return 1
	}
	return 0
}

func (m *misterControllerMap) actionForCode(code uint16) (action, bool) {
	match := func(slot int) bool {
		v := uint16(m.values[slot] & 0xffff)
		return v != 0 && v == code
	}

	switch {
	case match(misterBtnRight):
		return actRight, true
	case match(misterBtnLeft):
		return actLeft, true
	case match(misterBtnDown):
		return actDown, true
	case match(misterBtnUp):
		return actUp, true
	case match(misterBtnA):
		if swapABInput.Load() {
			return actBack, true
		}
		return actConfirm, true
	case match(misterBtnB):
		if swapABInput.Load() {
			return actConfirm, true
		}
		return actBack, true
	case match(misterBtnX):
		if swapXYInput.Load() {
			return actStop, true
		}
		return actPlayPause, true
	case match(misterBtnY):
		if swapXYInput.Load() {
			return actPlayPause, true
		}
		return actStop, true
	case match(misterBtnL):
		return actPrev, true
	case match(misterBtnR):
		return actNext, true
	case match(misterBtnStart):
		return actNowPlaying, true
	}
	return actNone, false
}

func (m *misterControllerMap) updateOSD(code uint16, down bool) (action, bool) {
	c1 := uint16(m.values[misterBtnOSD1] & 0xffff)
	c2 := uint16(m.values[misterBtnOSD2] & 0xffff)
	if c1 == 0 && c2 == 0 {
		return actNone, false
	}
	old := m.osdPressed1 && m.osdPressed2
	if c1 != 0 && code == c1 {
		m.osdPressed1 = down
	}
	if c2 != 0 && code == c2 {
		m.osdPressed2 = down
	}
	// Main mirrors a single OSD button into both slots.
	if c1 != 0 && c2 == 0 {
		m.osdPressed2 = m.osdPressed1
	}
	if c2 != 0 && c1 == 0 {
		m.osdPressed1 = m.osdPressed2
	}
	if !old && m.osdPressed1 && m.osdPressed2 {
		return actSources, true
	}
	return actNone, c1 == code || c2 == code
}

// process returns (action, handled, faceButton). handled means a MiSTer map is
// authoritative for this controller event, even when it produces no action.
func (m *misterControllerMap) process(f *os.File, ev inputEvent) (action, bool, bool) {
	if ev.Type == evKey {
		if ev.Code < 256 {
			return actNone, false, false // keep normal keyboard/media-key handling
		}
		down := ev.Value != 0
		wasDown := m.pressed[ev.Code]
		m.pressed[ev.Code] = down
		if a, osd := m.updateOSD(ev.Code, down); osd {
			if a != actNone {
				return a, true, false
			}
			return actNone, true, false
		}
		if !down || wasDown {
			return actNone, true, false
		}
		a, ok := m.actionForCode(ev.Code)
		face := ok && (a == actConfirm || a == actBack || a == actPlayPause || a == actStop)
		return a, true, face
	}

	if ev.Type == evAbs {
		info := m.absInfoFor(f, ev.Code)
		state := axisDigitalState(ev.Value, info)
		old := m.axisState[ev.Code]
		if state == old {
			return actNone, true, false
		}
		m.axisState[ev.Code] = state
		if state == 0 {
			return actNone, true, false
		}
		code := uint16(misterKeyEmu + int(ev.Code)*2)
		if state > 0 {
			code++
		}
		a, _ := m.actionForCode(code)
		return a, true, false
	}
	return actNone, false, false
}
