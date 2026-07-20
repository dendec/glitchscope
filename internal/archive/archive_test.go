package archive

import (
	"path/filepath"
	"testing"
)

func TestOpenAndExtractMDP(t *testing.T) {
	archivePath := filepath.Join("..", "..", "portmaster", "presets", "textures.mdp")

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
