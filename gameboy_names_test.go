package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadGBASongNames(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hifi-gba-songs.json"), []byte(`{"BPEE":{"350":"Littleroot Test","x":"bad"},"BZMP":{"1":"Castle"}}`), 0o644)
	got := loadGBASongNames(dir, "BPEE")
	if got[350] != "Littleroot Test" || len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	if n := loadGBASongNames(dir, "XXXX"); len(n) != 0 {
		t.Fatalf("unknown game should have no names, got %v", n)
	}
	if n := loadGBASongNames(t.TempDir(), "BPEE"); n != nil {
		t.Fatalf("missing file should give nil, got %v", n)
	}
}
