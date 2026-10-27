//go:build linux && cgo

package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestGbaUnzip(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "g.zip")
	f, _ := os.Create(zp)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("readme.txt")
	w.Write([]byte("x"))
	w, _ = zw.Create("Game (Europe).gba")
	w.Write([]byte("ROMDATA"))
	zw.Close()
	f.Close()
	tmp, err := gbaUnzip(zp)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp)
	if b, _ := os.ReadFile(tmp); string(b) != "ROMDATA" {
		t.Fatalf("got %q", b)
	}
}
