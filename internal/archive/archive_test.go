package archive

import (
	"path/filepath"
	"testing"
)

func TestOpenAndExtractGSA(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "textures.gsa")
	entries := []SourceEntry{
		{Name: "textures/example.png", Data: []byte("fixture")},
	}
	if err := Write(archivePath, entries); err != nil {
		t.Fatal(err)
	}

	a, err := Open(archivePath, 10000)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()

	dir := t.TempDir()
	count, err := a.Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("archive extracted no textures")
	}
}
