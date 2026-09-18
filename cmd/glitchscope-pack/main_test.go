package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectPresetArchiveExcludesTransitionPresets(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"! Transition/Fast cut.milk": "transition",
		"! Transition/Slow cut.milk": "transition",
		"Waveform/Game of Life.milk": "normal",
		"Fractal/normal.milk":        "normal",
	}
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := collect(root, presetKind, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("collected %d presets, want 2: %+v", len(entries), entries)
	}
	for _, entry := range entries {
		if entry.Name == "! Transition/Fast cut.milk" || entry.Name == "! Transition/Slow cut.milk" {
			t.Fatalf("transition preset was included: %q", entry.Name)
		}
	}
}
