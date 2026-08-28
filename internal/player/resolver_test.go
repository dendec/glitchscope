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

func TestResolveLocalPathModArchiveEntry(t *testing.T) {
	dir := t.TempDir()
	remote := "http://modarchive.textfiles.com/modarchive_2007_official_snapshot_120000_modules/A/A0.zip#a0d_agep.xm.zip"
	cachePath := ModArchiveCachePath(dir, remote)
	if cachePath == "" {
		t.Fatal("ModArchiveCachePath returned empty")
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte("XM"), 0o644); err != nil {
		t.Fatal(err)
	}

	resolver := NewResolver()
	resolver.SetBaseDir(dir)
	if got := resolver.ResolveLocalPath(ModArchivePrefix + remote); got != cachePath {
		t.Fatalf("ResolveLocalPath archive entry = %q, want %q", got, cachePath)
	}
	if got := TrackTitle(ModArchivePrefix + remote); got != "a0d_agep.xm" {
		t.Fatalf("TrackTitle archive entry = %q, want a0d_agep.xm", got)
	}
}

func TestModArchiveCachePathRejectsTraversal(t *testing.T) {
	remote := "http://modarchive.textfiles.com/modarchive_2007_official_snapshot_120000_modules/A/A0.zip#../escape.mod"
	if got := ModArchiveCachePath(t.TempDir(), remote); got != "" {
		t.Fatalf("traversal cache path = %q, want empty", got)
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
