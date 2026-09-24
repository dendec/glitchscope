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

func TestCollectPortableArchiveUsesBenchmarkAllowlist(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"approved.milk", "slow.milk", "unmeasured.milk"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	benchmark := filepath.Join(root, "benchmark.csv")
	data := "preset,status,compile_ms,steady_ms_per_frame,steady_fps,total_ms\n" +
		"approved.milk,ok,1,1,20,1\n" +
		"slow.milk,ok,1,1,19.9,1\n"
	if err := os.WriteFile(benchmark, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	allowed, err := loadAllowedPresets(benchmark, 20)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := collect(root, presetKind, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "approved.milk" {
		t.Fatalf("portable presets = %+v, want only approved.milk", entries)
	}
}
