package main

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// GAME BOY source: music from the ROMs of the Game Boy family. Every system is
// one entry of gameBoySystems, shown under a single GAME BOY item of the sources
// menu (a menu of its own only when there is more than one).

// gbaLibStamp is set by the build to the hash of the GBA sound library (go build
// does not see a change in a C++ library, and would not link again).
var gbaLibStamp string

type gameBoySystem struct {
	Label string
	Dirs  []string // folders under games/ (SD card, then every USB drive) that hold its ROMs
	Exts  []string
}

// Only the GBA has a sound engine here (MP2K, most GBA games); Game Boy and
// Game Boy Color ROMs join this list when their engine does.
var gameBoySystems = []gameBoySystem{
	{Label: "GAME BOY ADVANCE", Dirs: []string{"GBA"}, Exts: []string{".gba", ".zip"}},
}

func (s gameBoySystem) roots() []string {
	var out []string
	bases := append([]string{"/media/fat", "/media/fat/cifs"}, detectUSB()...)
	for _, b := range bases {
		for _, d := range s.Dirs {
			p := filepath.Join(b, "games", d)
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				out = append(out, p)
			}
		}
	}
	return out
}

type gameBoyROM struct{ Name, Path, Art string }

// gamelist.xml next to the ROMs (EmulationStation's format, which the Zaparoo
// Companion and scrapers write): a game's name and box art. A ROM's own entry
// only has its path and points at the game's entry with parentid.
type gamelistGame struct {
	ID       string `xml:"id,attr"`
	ParentID string `xml:"parentid,attr"`
	Path     string `xml:"path"`
	Name     string `xml:"name"`
	Boxart   string `xml:"boxart2d"`
	Image    string `xml:"image"`
}

type gamelistInfo struct{ Name, Art string }

// loadGamelist maps ROM paths to what the folder knows about them: its
// gamelist.xml first, then hifi-gba.json (tools/romm-gba-meta.py: names and
// covers from RomM) for the games the gamelist leaves out or has no cover for.
func loadGamelist(root string) map[string]gamelistInfo {
	out := loadGamelistXML(root)
	if b, err := os.ReadFile(filepath.Join(root, "hifi-gba.json")); err == nil {
		var meta struct {
			Files map[string]struct{ Name, Art string } `json:"files"`
		}
		if json.Unmarshal(b, &meta) == nil {
			if out == nil {
				out = map[string]gamelistInfo{}
			}
			for file, m := range meta.Files {
				key := filepath.Clean(gbaMetadataPath(root, file))
				info := out[key]
				if info.Name == "" {
					info.Name = m.Name
				}
				if info.Art == "" && m.Art != "" {
					info.Art = gbaMetadataPath(root, m.Art)
				}
				if info.Name != "" {
					out[key] = info
				}
			}
		}
	}
	return out
}

func loadGamelistXML(root string) map[string]gamelistInfo {
	b, err := os.ReadFile(filepath.Join(root, "gamelist.xml"))
	if err != nil {
		return nil
	}
	var gl struct {
		Games []gamelistGame `xml:"game"`
	}
	if xml.Unmarshal(b, &gl) != nil {
		return nil
	}
	byID := map[string]gamelistGame{}
	for _, g := range gl.Games {
		if g.ID != "" {
			byID[g.ID] = g
		}
	}
	out := map[string]gamelistInfo{}
	for _, g := range gl.Games {
		if g.Path == "" {
			continue
		}
		info := gamelistInfo{Name: g.Name, Art: g.Boxart}
		if info.Art == "" {
			info.Art = g.Image
		}
		if p, ok := byID[g.ParentID]; ok {
			info = gamelistInfo{Name: p.Name, Art: p.Boxart}
			if info.Art == "" {
				info.Art = p.Image
			}
		}
		if info.Name == "" {
			continue
		}
		if info.Art != "" {
			info.Art = gbaMetadataPath(root, info.Art)
		}
		out[filepath.Clean(gbaMetadataPath(root, g.Path))] = info
	}
	return out
}

func (s gameBoySystem) roms() []gameBoyROM {
	var out []gameBoyROM
	for _, root := range s.roots() {
		gl := loadGamelist(root)
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != root && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			for _, e := range s.Exts {
				if ext == e && !strings.HasPrefix(d.Name(), ".") && !strings.HasPrefix(d.Name(), "mister-boot") {
					r := gameBoyROM{Name: romDisplayName(d.Name()), Path: p}
					if info, ok := gl[filepath.Clean(p)]; ok {
						r.Name, r.Art = info.Name, info.Art
					}
					out = append(out, r)
				}
			}
			return nil
		})
	}
	// a .zip next to a .gba of the same name is the same game twice
	have := map[string]bool{}
	for _, r := range out {
		if strings.EqualFold(filepath.Ext(r.Path), ".gba") {
			have[strings.ToLower(strings.TrimSuffix(r.Path, filepath.Ext(r.Path)))] = true
		}
	}
	kept := out[:0]
	for _, r := range out {
		if strings.EqualFold(filepath.Ext(r.Path), ".zip") && have[strings.ToLower(strings.TrimSuffix(r.Path, filepath.Ext(r.Path)))] {
			continue
		}
		kept = append(kept, r)
	}
	out = kept
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

var romTags = regexp.MustCompile(`\s*[\(\[][^\)\]]*[\)\]]`)

// romDisplayName is a ROM's file name without extension and the (Europe) [!] tags.
func romDisplayName(file string) string {
	n := strings.TrimSuffix(file, filepath.Ext(file))
	if c := strings.TrimSpace(romTags.ReplaceAllString(n, "")); c != "" {
		return c
	}
	return n
}

// gameBoyAvailable: the item is in the sources menu only when there is a ROM folder.
func gameBoyAvailable() bool {
	for _, s := range gameBoySystems {
		if len(s.roots()) > 0 {
			return true
		}
	}
	return false
}

func gameBoyUI(app *App) {
	if !app.cfg.GBAMusicEnabled {
		return
	}
	folder := "/"
	returnFolder := "/"
	selection := map[string]int{}
	for {
		if app.jumpSources {
			return
		}
		page, err := gbaBrowse(folder)
		if err != nil {
			message(app, "GBA Music (Experimental)", err.Error())
			if folder == returnFolder {
				return
			}
			folder = returnFolder
			continue
		}
		folder = page.Path
		hasParent := page.Path != page.Root
		labels := []string{}
		if hasParent {
			labels = append(labels, "..")
		}
		for _, e := range page.Entries {
			name := e.Name
			if e.Kind == "Folder" {
				name += "/"
			}
			labels = append(labels, name)
		}
		if len(labels) == 0 {
			message(app, "GBA Music (Experimental)", "THIS FOLDER IS EMPTY")
			if hasParent {
				folder = page.Parent
				continue
			}
			return
		}
		i, ok := menu(app, "GBA Music: "+short(page.Path, 30), labels, selection[page.Path])
		if !ok {
			if hasParent {
				folder = page.Parent
				continue
			}
			return
		}
		selection[page.Path] = i
		if hasParent {
			if i == 0 {
				folder = page.Parent
				continue
			}
			i--
		}
		e := page.Entries[i]
		if e.Disabled {
			message(app, "GBA Music (Experimental)", "SELECT A .GBA OR .ZIP GAME")
			continue
		}
		if e.IsDir {
			returnFolder = page.Path
			folder = e.Path
			continue
		}
		q, err := gbaWebQueue(e.Path)
		if err == nil {
			err = app.startQueue(q, &browseOrigin{Kind: "gameboy", Selected: e.Name})
		}
		if err != nil {
			message(app, "PLAYBACK ERROR", err.Error())
			continue
		}
		playerUI(app)
	}
}

// gameBoyTracks opens the ROM and makes a track of every song of it, with the
// game's name and box art from the gamelist next to it, when it knows the game.
func gameBoyTracks(r gameBoyROM) ([]Track, error) {
	rom, err := gbaAcquire(r.Path)
	if err != nil {
		return nil, err
	}
	ids := rom.Songs()
	code := rom.Code()
	gbaRelease(rom)
	if len(ids) == 0 {
		return nil, errors.New("no music found")
	}
	var art image.Image
	if r.Art == "" {
		r.Art = gbaROMInfo(r.Path).Art
	}
	if r.Art != "" {
		if f, err := os.Open(r.Art); err == nil {
			if im, _, err := image.Decode(f); err == nil {
				art = scaleDownBilinear(im, 500)
			}
			f.Close()
		}
	}
	fmt.Fprintf(os.Stderr, "GBA: %s: %d songs, the first ones %v\n", r.Name, len(ids), ids[:min(len(ids), 6)])
	names := loadGBASongNames(filepath.Dir(r.Path), code)
	tracks := make([]Track, len(ids))
	for i, id := range ids {
		title := gbaSongLabel(i)
		if n := names[id]; n != "" {
			title = n
		}
		tracks[i] = Track{Path: gbaTrackPath(id, r.Path), Title: title, Artist: r.Name, Album: r.Name, Number: i + 1, MediaFormat: "GBA", DirFD: -1, Art: art}
	}
	return tracks, nil
}

// loadGBASongNames reads song names for a game from hifi-gba-songs.json in the ROM
// folder: {"<game code>": {"<song id>": "name"}} (tools/gba-song-names.py makes it
// from the decompilation projects' song lists; the game code is the ROM header's,
// e.g. BPEE). A game without an entry keeps "SONG n".
func loadGBASongNames(dir, code string) map[int]string {
	b, err := os.ReadFile(filepath.Join(dir, "hifi-gba-songs.json"))
	if err != nil || code == "" {
		return nil
	}
	var all map[string]map[string]string
	if json.Unmarshal(b, &all) != nil {
		return nil
	}
	out := map[int]string{}
	for k, v := range all[code] {
		if id, err := strconv.Atoi(k); err == nil && v != "" {
			out[id] = v
		}
	}
	return out
}

// gbaROMInfo is a ROM given by its path: named and with art from its gamelist.
func gbaMetadataPath(root, name string) string {
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}
	return filepath.Join(root, name)
}
func gbaROMInfo(file string) gameBoyROM {
	r := gameBoyROM{Name: romDisplayName(filepath.Base(file)), Path: file}
	limit := filepath.Dir(file)
	for _, g := range gbaStorageGroups() {
		for _, root := range g.Roots {
			if _, err := safeWithin(root.Dir, file); err == nil {
				limit = root.Dir
			}
		}
	}
	for dir := filepath.Dir(file); ; dir = filepath.Dir(dir) {
		if info, ok := loadGamelist(dir)[filepath.Clean(file)]; ok {
			if info.Name != "" {
				r.Name = info.Name
			}
			if info.Art != "" {
				if st, err := os.Stat(info.Art); err == nil && !st.IsDir() {
					r.Art = info.Art
					break
				}
			}
		}
		if dir == limit || filepath.Dir(dir) == dir {
			break
		}
	}
	if r.Art == "" {
		dir := filepath.Dir(file)
		stem := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		for _, sub := range []string{"", "images", "media", "boxart", "covers", ".boxart"} {
			for _, suffix := range []string{"", "-image", "-boxart", "-cover"} {
				for _, ext := range []string{".png", ".jpg", ".jpeg"} {
					candidate := filepath.Join(dir, sub, stem+suffix+ext)
					if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
						r.Art = candidate
						return r
					}
				}
			}
		}
		for _, name := range folderArtworkNames {
			candidate := filepath.Join(dir, name)
			if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
				r.Art = candidate
				break
			}
		}
	}
	return r
}

// gbaExternalQueue handles the target "gba://<ROM path>[#<song number>]" (a
// Zaparoo tag, or `HiFi gba://...` from a shell): all the songs of the ROM, from
// the song given (1 is the first one).
func gbaExternalQueue(raw string) (Queue, bool, error) {
	rest, ok := strings.CutPrefix(raw, "gba://")
	if !ok {
		return Queue{}, false, nil
	}
	path, frag, _ := strings.Cut(rest, "#")
	tracks, err := gameBoyTracks(gbaROMInfo(path))
	if err != nil {
		return Queue{}, true, err
	}
	n, _ := strconv.Atoi(frag)
	if n < 1 || n > len(tracks) {
		n = 1
	}
	return Queue{Tracks: tracks, Index: n - 1, Repeat: true}, true, nil
}

func gameBoySongs(app *App, r gameBoyROM) {
	tracks, err := gameBoyTracks(r)
	if err != nil {
		message(app, "GBA Music (Experimental)", strings.ToUpper(err.Error()))
		return
	}
	title := r.Name
	names := make([]string, len(tracks))
	for i, t := range tracks {
		names[i] = t.Title
	}
	initial := 0
	for {
		settleInput(app.acts, 180*time.Millisecond)
		i, ok := menuWithEntryCounter(app, short(title, 30), names, initial, true)
		if !ok {
			return
		}
		q := Queue{Tracks: append([]Track(nil), tracks...), Index: i, Repeat: true}
		origin := &browseOrigin{Kind: "gameboy", Selected: names[i]}
		if err := app.startQueue(q, origin); err != nil {
			message(app, "PLAYBACK ERROR", err.Error())
			continue
		}
		playerUI(app)
		if app.jumpSources {
			return
		}
		initial = i
	}
}
