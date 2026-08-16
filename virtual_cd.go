package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const virtualCDSectorBytes = 2352

type VirtualDisc struct {
	Path   string
	Tracks []Track
}

type cueTrackDef struct {
	number      int
	mode        string
	file        string
	index0Frame int64
	indexFrame  int64
}

func cueMSFFrames(s string) (int64, error) {
	p := strings.Split(strings.TrimSpace(s), ":")
	if len(p) != 3 {
		return 0, errors.New("invalid CUE INDEX")
	}
	m, e1 := strconv.ParseInt(p[0], 10, 64)
	sec, e2 := strconv.ParseInt(p[1], 10, 64)
	f, e3 := strconv.ParseInt(p[2], 10, 64)
	if e1 != nil || e2 != nil || e3 != nil || m < 0 || sec < 0 || sec >= 60 || f < 0 || f >= 75 {
		return 0, errors.New("invalid CUE INDEX")
	}
	return (m*60+sec)*75 + f, nil
}

func parseCueFile(path string) ([]Track, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dir := filepath.Dir(path)
	var defs []cueTrackDef
	currentFile := ""
	currentTrack := -1
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4096), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "FILE ") {
			rest := strings.TrimSpace(line[5:])
			if strings.HasPrefix(rest, "\"") {
				if end := strings.Index(rest[1:], "\""); end >= 0 {
					currentFile = rest[1 : 1+end]
				}
			} else if fields := strings.Fields(rest); len(fields) > 0 {
				currentFile = fields[0]
			}
		} else if strings.HasPrefix(upper, "TRACK ") {
			fields := strings.Fields(line)
			if len(fields) >= 3 && currentFile != "" {
				n, _ := strconv.Atoi(fields[1])
				defs = append(defs, cueTrackDef{number: n, mode: strings.ToUpper(fields[2]), file: filepath.Join(dir, currentFile), index0Frame: -1, indexFrame: -1})
				currentTrack = len(defs) - 1
			}
		} else if strings.HasPrefix(upper, "INDEX 00 ") && currentTrack >= 0 {
			fr, e := cueMSFFrames(strings.TrimSpace(line[len("INDEX 00 "):]))
			if e == nil {
				defs[currentTrack].index0Frame = fr
			}
		} else if strings.HasPrefix(upper, "INDEX 01 ") && currentTrack >= 0 {
			fr, e := cueMSFFrames(strings.TrimSpace(line[len("INDEX 01 "):]))
			if e == nil {
				defs[currentTrack].indexFrame = fr
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(defs) == 0 {
		return nil, errors.New("CUE contains no tracks")
	}

	var out []Track
	for i, d := range defs {
		if d.indexFrame < 0 || d.mode != "AUDIO" {
			continue
		}
		st, err := os.Stat(d.file)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(d.file), err)
		}
		totalFrames := st.Size() / virtualCDSectorBytes
		end := totalFrames
		if i+1 < len(defs) && filepath.Clean(defs[i+1].file) == filepath.Clean(d.file) {
			next := defs[i+1].indexFrame
			if defs[i+1].index0Frame >= 0 {
				next = defs[i+1].index0Frame
			}
			if next >= 0 {
				end = next
			}
		}
		if end <= d.indexFrame {
			continue
		}
		enc := url.QueryEscape(d.file)
		vp := fmt.Sprintf("vcdcue:%s:%d:%d", enc, d.indexFrame, end)
		dur := float64(end-d.indexFrame) / 75.0
		out = append(out, Track{Path: vp, Title: fmt.Sprintf("Track %02d", d.number), Album: filepath.Base(path), Duration: dur, MediaFormat: "CDDA", BitDepth: 16, SampleRate: 44100, BitRate: 1411})
	}
	if len(out) == 0 {
		return nil, errors.New("disc contains no audio tracks")
	}
	return out, nil
}

func loadVirtualDisc(path string) (*VirtualDisc, error) {
	ext := strings.ToLower(filepath.Ext(path))
	var tracks []Track
	var err error
	switch ext {
	case ".cue":
		tracks, err = parseCueFile(path)
	case ".chd":
		tracks, err = nativeCHDTracks(path)
	default:
		return nil, errors.New("unsupported virtual CD format")
	}
	if err != nil {
		return nil, err
	}
	return &VirtualDisc{Path: path, Tracks: tracks}, nil
}

func parseVirtualTrackPath(raw, prefix string) (string, int64, int64, error) {
	x := strings.TrimPrefix(raw, prefix)
	p := strings.Split(x, ":")
	if len(p) != 3 {
		return "", 0, 0, errors.New("invalid virtual CD track")
	}
	path, err := url.QueryUnescape(p[0])
	if err != nil {
		return "", 0, 0, err
	}
	a, err := strconv.ParseInt(p[1], 10, 64)
	if err != nil {
		return "", 0, 0, err
	}
	b, err := strconv.ParseInt(p[2], 10, 64)
	if err != nil {
		return "", 0, 0, err
	}
	return path, a, b, nil
}

func (p *Player) playVirtualCDTrack(t Track, stop <-chan struct{}) error {
	p.mu.Lock()
	offset := p.basePosition
	p.mu.Unlock()
	if err := nativeAudioStartPCM(p.cfg.EQ); err != nil {
		return err
	}
	if strings.HasPrefix(t.Path, "vcdcue:") {
		path, start, end, err := parseVirtualTrackPath(t.Path, "vcdcue:")
		if err != nil {
			nativeAudioFinishPCM()
			return err
		}
		start += int64(offset * 75)
		f, err := os.Open(path)
		if err != nil {
			nativeAudioFinishPCM()
			return err
		}
		go func() {
			defer f.Close()
			defer nativeAudioFinishPCM()
			buf := make([]byte, virtualCDSectorBytes*16)
			for frame := start; frame < end; {
				select {
				case <-stop:
					return
				default:
				}
				count := int64(16)
				if frame+count > end {
					count = end - frame
				}
				n := int(count) * virtualCDSectorBytes
				off := frame * virtualCDSectorBytes
				got, e := f.ReadAt(buf[:n], off)
				if got > 0 {
					if nativeAudioWritePCM(buf[:got]) != nil {
						return
					}
				}
				if e != nil && e != io.EOF {
					return
				}
				frame += int64(got / virtualCDSectorBytes)
				if got == 0 {
					return
				}
			}
		}()
		return nil
	}
	path, start, count, err := parseVirtualTrackPath(t.Path, "vcdchd:")
	if err != nil {
		nativeAudioFinishPCM()
		return err
	}
	start += int64(offset * 75)
	count -= int64(offset * 75)
	if count < 1 {
		count = 1
	}
	d, err := nativeCHDOpen(path)
	if err != nil {
		nativeAudioFinishPCM()
		return err
	}
	go func() {
		defer d.Close()
		defer nativeAudioFinishPCM()
		buf := make([]byte, virtualCDSectorBytes*8)
		for frame, left := start, count; left > 0; {
			select {
			case <-stop:
				return
			default:
			}
			nframes := int64(8)
			if nframes > left {
				nframes = left
			}
			n := 0
			for j := int64(0); j < nframes; j++ {
				if err := d.ReadAudioFrame(frame+j, buf[n:n+virtualCDSectorBytes]); err != nil {
					return
				}
				n += virtualCDSectorBytes
			}
			if nativeAudioWritePCM(buf[:n]) != nil {
				return
			}
			frame += nframes
			left -= nframes
		}
	}()
	return nil
}

func browseVirtualCD(app *App, root string) (string, bool) {
	settleInput(app.acts, 180000000)
	stack, pathStack, err := openDirChain(root, root)
	if err != nil {
		return "", false
	}
	defer func() {
		for _, f := range stack {
			_ = f.Close()
		}
	}()
	for {
		cur := stack[len(stack)-1]
		dir := pathStack[len(pathStack)-1]
		_, _ = cur.Seek(0, io.SeekStart)
		es, readErr := cur.ReadDir(-1)
		if readErr != nil {
			return "", false
		}
		entries := make([]browseEntry, 0, len(es))
		for _, x := range es {
			name := x.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			ext := strings.ToLower(filepath.Ext(name))
			if x.IsDir() || ext == ".chd" || ext == ".cue" {
				entries = append(entries, browseEntry{Name: name, Path: filepath.Join(dir, name), IsDir: x.IsDir()})
			}
		}
		sort.Slice(entries, func(i, j int) bool { return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name) })
		items := []string{"[..]"}
		for _, entry := range entries {
			items = append(items, entry.Name)
		}
		i, ok := menuWithEntryCounter(app, "SELECT VIRTUAL CD: "+short(dir, 26), items, 0, true)
		if !ok {
			if app.jumpSources || len(stack) == 1 {
				return "", false
			}
			_ = stack[len(stack)-1].Close()
			stack = stack[:len(stack)-1]
			pathStack = pathStack[:len(pathStack)-1]
			continue
		}
		if i == 0 {
			if len(stack) == 1 {
				return "", false
			}
			_ = stack[len(stack)-1].Close()
			stack = stack[:len(stack)-1]
			pathStack = pathStack[:len(pathStack)-1]
			continue
		}
		entry := entries[i-1]
		if entry.IsDir {
			child, er := openDirChild(cur, entry.Name)
			if er != nil {
				message(app, "BROWSE ERROR", er.Error())
				continue
			}
			stack = append(stack, child)
			pathStack = append(pathStack, entry.Path)
			continue
		}
		return entry.Path, true
	}
}

func playVirtualDisc(app *App, index int) {
	if app.virtualCD == nil || len(app.virtualCD.Tracks) == 0 {
		return
	}
	if index < 0 || index >= len(app.virtualCD.Tracks) {
		index = 0
	}
	q := Queue{Tracks: append([]Track(nil), app.virtualCD.Tracks...), Index: index}
	origin := &browseOrigin{Kind: "virtualcd", Selected: app.virtualCD.Tracks[index].Title}
	if err := app.startQueue(q, origin); err != nil {
		message(app, "PLAYBACK ERROR", err.Error())
		return
	}
	playerUI(app)
}

func browseMountedVirtualDisc(app *App) {
	if app.virtualCD == nil {
		return
	}
	names := make([]string, len(app.virtualCD.Tracks))
	for i, t := range app.virtualCD.Tracks {
		names[i] = t.Title
	}
	initial := 0
	for {
		i, ok := menu(app, "VIRTUAL CD - "+short(filepath.Base(app.virtualCD.Path), 24), names, initial)
		if !ok {
			return
		}
		playVirtualDisc(app, i)
		if app.jumpSources {
			return
		}
		initial = i
	}
}

func selectVirtualCDSource(app *App) (string, bool) {
	items := []string{"SD CARD"}
	types := []string{"sd"}
	usbs := detectUSB()
	if len(usbs) > 0 {
		items = append(items, "USB")
		types = append(types, "usb")
	}
	if smbAvailable() {
		items = append(items, "SMB")
		types = append(types, "smb")
	}

	i, ok := menu(app, "VIRTUAL CD - SELECT SOURCE", items, 0)
	if !ok {
		return "", false
	}
	switch types[i] {
	case "sd":
		return "/media/fat", true
	case "usb":
		root := usbs[0]
		if len(usbs) > 1 {
			settleInput(app.acts, 180*time.Millisecond)
			j, ok := menu(app, "VIRTUAL CD - USB", usbs, 0)
			if !ok {
				return "", false
			}
			root = usbs[j]
		}
		return root, true
	case "smb":
		cfg := loadSMB()
		idx := 0
		if len(cfg.Shares) > 1 {
			names := make([]string, len(cfg.Shares))
			for i, share := range cfg.Shares {
				names[i] = share.Name
				if names[i] == "" {
					names[i] = share.Server + "/" + share.Share
				}
			}
			settleInput(app.acts, 180*time.Millisecond)
			j, ok := menu(app, "VIRTUAL CD - SMB", names, 0)
			if !ok {
				return "", false
			}
			idx = j
		}
		root, err := mountShare(cfg.Shares[idx])
		if err != nil {
			message(app, "SMB ERROR", strings.ToUpper(err.Error()))
			return "", false
		}
		return root, true
	}
	return "", false
}

func virtualCDUI(app *App) {
	for {
		if app.virtualCD == nil {
			i, ok := menu(app, "VIRTUAL CD", []string{"SELECT DISC"}, 0)
			if !ok {
				return
			}
			_ = i
			root, ok := selectVirtualCDSource(app)
			if !ok {
				continue
			}
			path, ok := browseVirtualCD(app, root)
			if !ok {
				continue
			}
			d, err := loadVirtualDisc(path)
			if err != nil {
				message(app, "VIRTUAL CD", strings.ToUpper(err.Error()))
				continue
			}
			app.virtualCD = d
			continue
		}
		name := short(filepath.Base(app.virtualCD.Path), 30)
		i, ok := menu(app, "VIRTUAL CD - "+name, []string{"PLAY DISC", "BROWSE DISC", "UNMOUNT DISC"}, 0)
		if !ok {
			return
		}
		switch i {
		case 0:
			playVirtualDisc(app, 0)
			if app.jumpSources {
				return
			}
		case 1:
			browseMountedVirtualDisc(app)
			if app.jumpSources {
				return
			}
		case 2:
			if app.player != nil && app.origin != nil && app.origin.Kind == "virtualcd" {
				app.stopAndUnload()
			}
			app.virtualCD = nil
		}
	}
}
