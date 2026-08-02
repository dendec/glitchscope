package util_test

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/dendec/pmv/internal/util"
)

type sampleData struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestGzipJSON(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sub", "test.gz")

	original := sampleData{Name: "test_gzip", Count: 42}
	if err := util.SaveGzipJSON(file, original); err != nil {
		t.Fatalf("SaveGzipJSON failed: %v", err)
	}

	var loaded sampleData
	if err := util.LoadGzipJSON(file, &loaded); err != nil {
		t.Fatalf("LoadGzipJSON failed: %v", err)
	}

	if loaded.Name != original.Name || loaded.Count != original.Count {
		t.Errorf("got %+v, want %+v", loaded, original)
	}
}

func TestJSONAtomic(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sub", "test.json")

	original := sampleData{Name: "test_json", Count: 99}
	if err := util.SaveJSONAtomic(file, original); err != nil {
		t.Fatalf("SaveJSONAtomic failed: %v", err)
	}

	var loaded sampleData
	if err := util.LoadJSON(file, &loaded); err != nil {
		t.Fatalf("LoadJSON failed: %v", err)
	}

	if loaded.Name != original.Name || loaded.Count != original.Count {
		t.Errorf("got %+v, want %+v", loaded, original)
	}
}

func TestExtractModuleFromZip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "test.zip")
	targetPath := filepath.Join(dir, "out", "song.mod")

	// Create dummy zip
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	txtWriter, err := zw.Create("readme.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := txtWriter.Write([]byte("some readme text")); err != nil {
		t.Fatal(err)
	}

	modWriter, err := zw.Create("song.mod")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := modWriter.Write([]byte("module content 12345")); err != nil {
		t.Fatal(err)
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	extracted, err := util.ExtractModuleFromZip(zipPath, targetPath, func(ext string) bool {
		return ext == ".mod"
	})
	if err != nil {
		t.Fatalf("ExtractModuleFromZip failed: %v", err)
	}

	if extracted != targetPath {
		t.Errorf("got path %q, want %q", extracted, targetPath)
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read extracted file failed: %v", err)
	}

	if string(content) != "module content 12345" {
		t.Errorf("got content %q, want %q", string(content), "module content 12345")
	}
}
