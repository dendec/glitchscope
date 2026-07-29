package archive

import (
	"path/filepath"
	"testing"
)

func TestOpenAndExtractPMV(t *testing.T) {
	archivePath := filepath.Join("..", "..", "portmaster", "presets", "textures.pmv")

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
