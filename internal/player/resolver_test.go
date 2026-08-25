package player

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveLocalPathModland(t *testing.T) {
	dir := t.TempDir()
	// Create a fake cached modland file.
	cacheDir := filepath.Join(dir, ".cache", "modland", "files", "Protracker", "Curt Cool")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	songPath := filepath.Join(cacheDir, "song.mod")
	if err := os.WriteFile(songPath, []byte("MOD"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewResolver()
	r.SetBaseDir(dir)
	virtualPath := ModlandPrefix + "Protracker/Curt Cool/song.mod"
	got := r.ResolveLocalPath(virtualPath)
	if got != songPath {
		t.Fatalf("ResolveLocalPath(%q) = %q, want %q", virtualPath, got, songPath)
	}
}

func TestResolveLocalPathModlandNotCached(t *testing.T) {
	dir := t.TempDir()
	r := NewResolver()
	r.SetBaseDir(dir)
	virtualPath := ModlandPrefix + "Protracker/Curt Cool/song.mod"
	got := r.ResolveLocalPath(virtualPath)
	if got != "" {
		t.Fatalf("ResolveLocalPath(uncached) = %q, want empty", got)
	}
}

func TestResolveLocalPathModArchive(t *testing.T) {
	dir := t.TempDir()
	// Create a fake cached modarchive file (extracted from zip).
	cacheDir := filepath.Join(dir, ".cache", "modarchive", "files", "2014", "IT", "J")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	songPath := filepath.Join(cacheDir, "song.it")
	if err := os.WriteFile(songPath, []byte("IT"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewResolver()
	r.SetBaseDir(dir)
	virtualPath := ModArchivePrefix + "http://modarchive.textfiles.com/2014/IT/J/song.it.zip"
	got := r.ResolveLocalPath(virtualPath)
	if got != songPath {
		t.Fatalf("ResolveLocalPath(%q) = %q, want %q", virtualPath, got, songPath)
	}
}

func TestResolveLocalPathLocalFile(t *testing.T) {
	r := NewResolver()
	r.SetBaseDir(t.TempDir())
	localPath := "/music/song.it"
	got := r.ResolveLocalPath(localPath)
	if got != localPath {
		t.Fatalf("ResolveLocalPath(local) = %q, want %q", got, localPath)
	}
}

func TestResolverIsCached(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, ".cache", "modland", "files", "Protracker")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "song.mod"), []byte("MOD"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewResolver()
	r.SetBaseDir(dir)
	if !r.IsCached(ModlandPrefix + "Protracker/song.mod") {
		t.Fatal("IsCached should return true for cached file")
	}
	if r.IsCached(ModlandPrefix + "Protracker/missing.mod") {
		t.Fatal("IsCached should return false for missing file")
	}
}

func TestResolveLocalPathEmptyBaseDir(t *testing.T) {
	r := NewResolver()
	if got := r.ResolveLocalPath(ModlandPrefix + "Protracker/song.mod"); got != "" {
		t.Fatalf("ResolveLocalPath without baseDir = %q, want empty", got)
	}
}
