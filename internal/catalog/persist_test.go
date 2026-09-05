package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndReadIndexFile(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`{"test": "data"}`)

	if err := WriteIndexAtomically(dir, "test.idx", data); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := ReadIndexFile(dir, "test.idx")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

func TestWriteIndexAtomicallyPreservesPrevious(t *testing.T) {
	dir := t.TempDir()
	v1 := []byte("version1")
	v2 := []byte("version2")

	if err := WriteIndexAtomically(dir, "test.idx", v1); err != nil {
		t.Fatal(err)
	}
	if err := WriteIndexAtomically(dir, "test.idx", v2); err != nil {
		t.Fatal(err)
	}

	got, err := ReadIndexFile(dir, "test.idx")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(v2) {
		t.Fatalf("after rewrite: got %q, want %q", got, v2)
	}

	// Temp file should not remain.
	matches, _ := filepath.Glob(filepath.Join(dir, "test.idx*.tmp"))
	if len(matches) > 0 {
		t.Fatalf("temp files remain: %v", matches)
	}
}

func TestReadIndexFileNotExist(t *testing.T) {
	dir := t.TempDir()
	data, err := ReadIndexFile(dir, "nonexistent.idx")
	if err != nil {
		t.Fatalf("non-existent file should return nil error, got: %v", err)
	}
	if data != nil {
		t.Fatalf("non-existent file should return nil data, got: %v", data)
	}
}

func TestWriteIndexAtomicallyCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "deep")
	if err := WriteIndexAtomically(dir, "test.idx", []byte("data")); err != nil {
		t.Fatal(err)
	}
	got, err := ReadIndexFile(dir, "test.idx")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "data" {
		t.Fatalf("got %q, want data", got)
	}
}

func TestWriteIndexAtomicallyNoTempLeftOnSuccess(t *testing.T) {
	dir := t.TempDir()
	if err := WriteIndexAtomically(dir, "x.idx", []byte("ok")); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("temp file left after success: %s", e.Name())
		}
	}
}
