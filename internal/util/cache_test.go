package util_test

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/dendec/glitchscope/internal/util"
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

func TestZstdJSON(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sub", "test.zst")

	original := sampleData{Name: "test_zstd", Count: 77}
	if err := util.SaveZstdJSON(file, original); err != nil {
		t.Fatalf("SaveZstdJSON failed: %v", err)
	}

	var loaded sampleData
	if err := util.LoadZstdJSON(file, &loaded); err != nil {
		t.Fatalf("LoadZstdJSON failed: %v", err)
	}

	if loaded.Name != original.Name || loaded.Count != original.Count {
		t.Errorf("got %+v, want %+v", loaded, original)
	}
}

func TestLoadZstdOrGzipJSON_ZstdFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "test.zst")

	original := sampleData{Name: "zstd_first", Count: 10}
	if err := util.SaveZstdJSON(file, original); err != nil {
		t.Fatalf("SaveZstdJSON failed: %v", err)
	}

	var loaded sampleData
	migrateFn, err := util.LoadZstdOrGzipJSON(file, &loaded)
	if err != nil {
		t.Fatalf("LoadZstdOrGzipJSON failed: %v", err)
	}
	if migrateFn != nil {
		t.Fatal("expected no migration callback for zstd file")
	}
	if loaded != original {
		t.Errorf("got %+v, want %+v", loaded, original)
	}
}

func TestLoadZstdOrGzipJSON_GzipFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "test.gz")

	original := sampleData{Name: "gzip_legacy", Count: 55}
	if err := util.SaveGzipJSON(file, original); err != nil {
		t.Fatalf("SaveGzipJSON failed: %v", err)
	}

	var loaded sampleData
	migrateFn, err := util.LoadZstdOrGzipJSON(file, &loaded)
	if err != nil {
		t.Fatalf("LoadZstdOrGzipJSON failed: %v", err)
	}
	if migrateFn == nil {
		t.Fatal("expected migration callback for gzip file")
	}
	if loaded != original {
		t.Errorf("got %+v, want %+v", loaded, original)
	}

	// Run migration and verify the file is now zstd-readable.
	migrateFn()
	var reloaded sampleData
	if err := util.LoadZstdJSON(file, &reloaded); err != nil {
		t.Fatalf("LoadZstdJSON after migration failed: %v", err)
	}
	if reloaded != original {
		t.Errorf("after migration: got %+v, want %+v", reloaded, original)
	}
}

func TestIsGzipFile(t *testing.T) {
	dir := t.TempDir()
	gzFile := filepath.Join(dir, "test.gz")
	zstFile := filepath.Join(dir, "test.zst")

	if err := util.SaveGzipJSON(gzFile, sampleData{Name: "gz"}); err != nil {
		t.Fatal(err)
	}
	if err := util.SaveZstdJSON(zstFile, sampleData{Name: "zs"}); err != nil {
		t.Fatal(err)
	}

	if !util.IsGzipFile(gzFile) {
		t.Error("expected gzip file to be detected")
	}
	if util.IsGzipFile(zstFile) {
		t.Error("expected zstd file not to be detected as gzip")
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
