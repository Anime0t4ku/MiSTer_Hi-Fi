//go:build !cgo

package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeGBAStorage(t *testing.T, multiple bool) string {
	t.Helper()
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "Folder"), 0755)
	os.WriteFile(filepath.Join(dir, "Folder", "Game (USA).gba"), []byte("test"), 0600)
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("notes"), 0600)
	oldGroups, oldTracks := gbaStorageGroups, gbaWebTrackList
	gbaStorageGroups = func() []gbaStorageGroup {
		groups := []gbaStorageGroup{{"sd", "SD CARD", []gbaStorageRoot{{"sd", "SD CARD", dir}}}}
		if multiple {
			groups = append(groups, gbaStorageGroup{"cifs", "CIFS", []gbaStorageRoot{{"cifs", "CIFS", dir}}})
		}
		return groups
	}
	gbaWebTrackList = func(r gameBoyROM) ([]Track, error) {
		return []Track{{Path: "gba:1:" + r.Path, Title: "Opening"}, {Path: "gba:2:" + r.Path, Title: "Battle"}}, nil
	}
	t.Cleanup(func() { gbaStorageGroups, gbaWebTrackList = oldGroups, oldTracks })
	return dir
}
func TestGBAStorageAndFolderBrowsing(t *testing.T) {
	dir := fakeGBAStorage(t, false)
	page, err := gbaBrowse("")
	if err != nil || page.Path != "/sd" || page.Root != "/sd" {
		t.Fatal(page, err)
	}
	if len(page.Entries) != 2 || page.Entries[0].Name != "Folder" || page.Entries[1].Name != "readme.txt" || !page.Entries[1].Disabled {
		t.Fatal(page.Entries)
	}
	page, err = gbaBrowse("/sd/Folder")
	if err != nil || page.Entries[0].Name != "Game (USA).gba" {
		t.Fatal(page, err)
	}
	page, err = gbaBrowse(page.Entries[0].Path)
	if err != nil || len(page.Entries) != 2 || page.Entries[1].Name != "Battle" {
		t.Fatal(page, err)
	}
	q, err := gbaWebQueue(page.Entries[1].Path)
	if err != nil || q.Index != 1 || len(q.Tracks) != 2 {
		t.Fatal(q, err)
	}
	for _, bad := range []string{"/sd/../etc", "/sd/Folder/Game (USA).gba/track/-1", "/sd/Folder/Game (USA).gba/track/2", "/missing/track/0", "/sd/readme.txt/track/0"} {
		if _, err := gbaWebQueue(bad); err == nil {
			t.Fatal("accepted invalid", bad)
		}
	}
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(dir, "escape"))
	if _, err := gbaBrowse("/sd/escape"); err == nil {
		t.Fatal("symlink escaped source")
	}
}
func TestGBAMultipleStorageChoice(t *testing.T) {
	fakeGBAStorage(t, true)
	page, err := gbaBrowse("/")
	if err != nil || page.Path != "/" || len(page.Entries) != 2 {
		t.Fatal(page, err)
	}
	if page.Entries[0].Name != "SD CARD" || page.Entries[1].Name != "CIFS" {
		t.Fatal(page)
	}
}
func TestGBAWebDisabledAndEnabledRoutes(t *testing.T) {
	fakeGBAStorage(t, false)
	app, w := settingsTestApp(t)
	rec := httptest.NewRecorder()
	w.handleBrowse(rec, httptest.NewRequest("GET", "/api/browse?source=gba", nil))
	if rec.Code != 403 {
		t.Fatal(rec.Code)
	}
	rec = httptest.NewRecorder()
	w.handlePlay(rec, httptest.NewRequest("POST", "/api/play", strings.NewReader(`{"source":"gba","path":"/bad/track/0"}`)))
	if rec.Code != 403 {
		t.Fatal(rec.Code)
	}
	_, err := app.settingsCall(context.Background(), func() (any, error) { app.cfg.GBAMusicEnabled = true; return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	w.handleBrowse(rec, httptest.NewRequest("GET", "/api/browse?source=gba&path=/sd/Folder", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Game (USA).gba") {
		t.Fatal(rec.Code, rec.Body.String())
	}
}

func TestGBAUSBDriveChoices(t *testing.T) {
	dir := fakeGBAStorage(t, false)
	gbaStorageGroups = func() []gbaStorageGroup {
		return []gbaStorageGroup{{"usb", "USB", []gbaStorageRoot{{"usb0", "USB0", dir}, {"usb1", "USB1", dir}}}}
	}
	page, err := gbaBrowse("")
	if err != nil || page.Path != "/usb" || len(page.Entries) != 2 || page.Entries[0].Name != "USB0" {
		t.Fatal(page, err)
	}
	page, err = gbaBrowse("/usb/usb1/Folder")
	if err != nil || page.Entries[0].Name != "Game (USA).gba" {
		t.Fatal(page, err)
	}
	gbaStorageGroups = func() []gbaStorageGroup {
		return []gbaStorageGroup{{"usb", "USB", []gbaStorageRoot{{"usb0", "USB0", dir}}}}
	}
	page, err = gbaBrowse("")
	if err != nil || page.Path != "/usb" || page.Entries[0].Name != "Folder" {
		t.Fatal(page, err)
	}
}
