package main

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type gbaStorageRoot struct{ ID, Label, Dir string }
type gbaStorageGroup struct {
	ID, Label string
	Roots     []gbaStorageRoot
}

var gbaStorageGroups = func() []gbaStorageGroup {
	groups := []gbaStorageGroup{}
	for _, v := range []struct{ id, label, base string }{{"cifs", "CIFS", "/media/fat/cifs"}, {"sd", "SD CARD", "/media/fat"}} {
		dir := filepath.Join(v.base, "games", "GBA")
		if st, e := os.Stat(dir); e == nil && st.IsDir() {
			groups = append(groups, gbaStorageGroup{v.id, v.label, []gbaStorageRoot{{v.id, v.label, dir}}})
		}
	}
	usb := gbaStorageGroup{ID: "usb", Label: "USB"}
	for _, base := range detectUSB() {
		dir := filepath.Join(base, "games", "GBA")
		if st, e := os.Stat(dir); e == nil && st.IsDir() {
			id := filepath.Base(base)
			usb.Roots = append(usb.Roots, gbaStorageRoot{id, strings.ToUpper(id), dir})
		}
	}
	if len(usb.Roots) > 0 {
		groups = append(groups, usb)
	}
	return groups
}
var gbaWebTrackList = gameBoyTracks

func gbaROMFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".gba" || ext == ".zip"
}
func gbaResolveBrowser(folder string) (gbaStorageRoot, string, string, error) {
	if !strings.HasPrefix(folder, "/") || path.Clean(folder) != folder {
		return gbaStorageRoot{}, "", "", errors.New("invalid GBA path")
	}
	parts := strings.Split(strings.TrimPrefix(folder, "/"), "/")
	for _, group := range gbaStorageGroups() {
		if parts[0] != group.ID {
			continue
		}
		root := group.Roots[0]
		prefix := "/" + group.ID
		start := 1
		if len(group.Roots) > 1 {
			if len(parts) < 2 {
				return gbaStorageRoot{}, "", "", errors.New("select a USB drive")
			}
			found := false
			for _, r := range group.Roots {
				if r.ID == parts[1] {
					root = r
					found = true
					break
				}
			}
			if !found {
				return root, "", "", errors.New("USB drive unavailable")
			}
			prefix += "/" + root.ID
			start = 2
		}
		relative := filepath.Join(parts[start:]...)
		candidate, err := safeWithin(root.Dir, relative)
		if err != nil {
			return root, "", "", err
		}
		actual, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return root, "", "", err
		}
		actualRoot, err := filepath.EvalSymlinks(root.Dir)
		if err != nil {
			return root, "", "", err
		}
		if _, err = safeWithin(actualRoot, actual); err != nil {
			return root, "", "", errors.New("GBA path points outside its source")
		}
		return root, candidate, prefix, nil
	}
	return gbaStorageRoot{}, "", "", errors.New("GBA storage unavailable")
}

type gbaBrowserPage struct {
	Path    string     `json:"path"`
	Root    string     `json:"root"`
	Parent  string     `json:"parent"`
	Entries []webEntry `json:"entries"`
}

func gbaBrowse(folder string) (gbaBrowserPage, error) {
	groups := gbaStorageGroups()
	page := gbaBrowserPage{Root: "/", Entries: []webEntry{}}
	if folder == "" || folder == "/" {
		if len(groups) == 1 {
			folder = "/" + groups[0].ID
		} else {
			page.Path = "/"
			for _, g := range groups {
				page.Entries = append(page.Entries, webEntry{Name: g.Label, Path: "/" + g.ID, IsDir: true, Kind: "Source"})
			}
			return page, nil
		}
	}
	page.Path = folder
	page.Parent = path.Dir(folder)
	if len(groups) == 1 {
		page.Root = "/" + groups[0].ID
	}
	for _, g := range groups {
		if folder == "/"+g.ID && len(g.Roots) > 1 {
			for _, r := range g.Roots {
				page.Entries = append(page.Entries, webEntry{Name: r.Label, Path: folder + "/" + r.ID, IsDir: true, Kind: "Drive"})
			}
			return page, nil
		}
	}
	_, file, _, err := gbaResolveBrowser(folder)
	if err != nil {
		return page, err
	}
	st, err := os.Stat(file)
	if err != nil {
		return page, err
	}
	if !st.IsDir() {
		if !gbaROMFile(file) {
			return page, errors.New("select a .gba or .zip game")
		}
		tracks, err := gbaWebTrackList(gbaROMInfo(file))
		if err != nil {
			return page, err
		}
		for i, t := range tracks {
			page.Entries = append(page.Entries, webEntry{Name: t.Title, Path: folder + "/track/" + strconv.Itoa(i), Kind: "Song"})
		}
		return page, nil
	}
	entries, err := os.ReadDir(file)
	if err != nil {
		return page, err
	}
	for _, entry := range entries {
		isDir := entry.IsDir()
		kind := "File"
		playable := gbaROMFile(entry.Name())
		if isDir {
			kind = "Folder"
		} else if playable {
			kind = "Game"
		}
		page.Entries = append(page.Entries, webEntry{Name: entry.Name(), Path: strings.TrimSuffix(folder, "/") + "/" + entry.Name(), IsDir: isDir || playable, Kind: kind, Disabled: !isDir && !playable})
	}
	sort.SliceStable(page.Entries, func(i, j int) bool {
		a, b := page.Entries[i], page.Entries[j]
		if (a.Kind == "Folder") != (b.Kind == "Folder") {
			return a.Kind == "Folder"
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return page, nil
}
func gbaWebQueue(selected string) (Queue, error) {
	at := strings.LastIndex(selected, "/track/")
	if at < 0 {
		return Queue{}, errors.New("invalid GBA song")
	}
	index, err := strconv.Atoi(selected[at+7:])
	if err != nil {
		return Queue{}, err
	}
	_, file, _, err := gbaResolveBrowser(selected[:at])
	if err != nil {
		return Queue{}, err
	}
	if !gbaROMFile(file) {
		return Queue{}, errors.New("unsupported GBA file")
	}
	tracks, err := gbaWebTrackList(gbaROMInfo(file))
	if err != nil {
		return Queue{}, err
	}
	if index < 0 || index >= len(tracks) {
		return Queue{}, fmt.Errorf("GBA song unavailable")
	}
	return Queue{Tracks: tracks, Index: index, Repeat: true}, nil
}
