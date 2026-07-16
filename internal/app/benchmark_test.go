package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeCSV(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadBenchmarkedPresets_valid(t *testing.T) {
	dir := t.TempDir()
	path := writeCSV(t, dir, "b.csv", `preset,status,compile_ms,steady_ms_per_frame,steady_fps,total_ms
foo,ok,10.0,16.67,60.0,100.0
bar,crashed,0.0,0.0,0.0,0.0
baz,error,0.0,0.0,0.0,0.0
`)

	got := readBenchmarkedPresets(path)
	want := map[string]bool{"foo": true, "bar": true, "baz": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestReadBenchmarkedPresets_empty_file(t *testing.T) {
	dir := t.TempDir()
	path := writeCSV(t, dir, "empty.csv", "")

	got := readBenchmarkedPresets(path)
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %v", got)
	}
}

func TestReadBenchmarkedPresets_header_only(t *testing.T) {
	dir := t.TempDir()
	path := writeCSV(t, dir, "h.csv", `preset,status,compile_ms,steady_ms_per_frame,steady_fps,total_ms
`)

	got := readBenchmarkedPresets(path)
	if len(got) != 0 {
		t.Fatalf("expected empty map for header-only, got %v", got)
	}
}

func TestReadBenchmarkedPresets_missing_file(t *testing.T) {
	got := readBenchmarkedPresets("/nonexistent/path.csv")
	if len(got) != 0 {
		t.Fatalf("expected empty map for missing file, got %v", got)
	}
}

func TestReadBenchmarkedPresets_preserves_status(t *testing.T) {
	// Regression: crashed presets must be skipped on resume, not retried.
	dir := t.TempDir()
	path := writeCSV(t, dir, "b.csv", `preset,status,compile_ms,steady_ms_per_frame,steady_fps,total_ms
slow,ok,10.0,16.67,60.0,100.0
crash,crashed,0.0,0.0,0.0,0.0
`)

	got := readBenchmarkedPresets(path)
	if !got["slow"] {
		t.Fatal("expected 'slow' to be in done set")
	}
	if !got["crash"] {
		t.Fatal("expected 'crash' to be in done set (regression: crashed must be skipped)")
	}
}
