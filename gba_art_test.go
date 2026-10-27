package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGBARootMetadataAndLocalArtwork(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "Sub"), 0755)
	rom := filepath.Join(root, "Sub", "Game.gba")
	os.WriteFile(rom, []byte("test"), 0600)
	cover := filepath.Join(root, "cover.png")
	os.WriteFile(cover, []byte("image"), 0600)
	os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList><game><path>./Sub/Game.gba</path><name>Named Game</name><image>./cover.png</image></game></gameList>`), 0600)
	old := gbaStorageGroups
	gbaStorageGroups = func() []gbaStorageGroup {
		return []gbaStorageGroup{{"sd", "SD CARD", []gbaStorageRoot{{"sd", "SD CARD", root}}}}
	}
	defer func() { gbaStorageGroups = old }()
	got := gbaROMInfo(rom)
	if got.Name != "Named Game" || got.Art != cover {
		t.Fatalf("root image metadata missing: %+v", got)
	}
	os.Remove(filepath.Join(root, "gamelist.xml"))
	side := filepath.Join(root, "Sub", "Game.png")
	os.WriteFile(side, []byte("image"), 0600)
	if got := gbaROMInfo(rom); got.Art != side {
		t.Fatalf("sidecar missing: %+v", got)
	}
}
