package player

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeCacheFile(t *testing.T, path string, size int, used time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, used, used); err != nil {
		t.Fatal(err)
	}
}

func TestTrackCachePruneAgeAndProtectedTrack(t *testing.T) {
	base := t.TempDir()
	cache := NewTrackCache(base)
	oldPath := filepath.Join(base, ".cache", "modland", "files", "MOD", "old.mod")
	keepPath := filepath.Join(base, ".cache", "modland", "files", "MOD", "playing.mod")
	writeCacheFile(t, oldPath, 8, time.Now().Add(-48*time.Hour))
	writeCacheFile(t, keepPath, 8, time.Now().Add(-48*time.Hour))
	if err := cache.Prune(24*time.Hour, false, 0, ModlandPrefix+"MOD/playing.mod"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expired path still exists: %v", err)
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Fatalf("protected path removed: %v", err)
	}
}

func TestTrackCachePruneUsesLRUForSize(t *testing.T) {
	base := t.TempDir()
	cache := NewTrackCache(base)
	older := filepath.Join(base, ".cache", "modarchive", "files", "older.mod")
	newer := filepath.Join(base, ".cache", "modarchive", "files", "newer.mod")
	writeCacheFile(t, older, 8, time.Now().Add(-time.Hour))
	writeCacheFile(t, newer, 8, time.Now())
	if err := cache.Prune(0, true, 8, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(older); !os.IsNotExist(err) {
		t.Fatalf("oldest path still exists: %v", err)
	}
	if _, err := os.Stat(newer); err != nil {
		t.Fatalf("newest path removed: %v", err)
	}
}

func TestTrackCacheReconcileDiscoversRemoteFilesAndIgnoresUnplayableFiles(t *testing.T) {
	base := t.TempDir()
	cache := NewTrackCache(base)
	modlandPath := filepath.Join(base, ".cache", "modland", "files", "Artist", "song.mod")
	archivePath := filepath.Join(base, ".cache", "modarchive", "files", "1999", "song.it")
	zeroPath := filepath.Join(base, ".cache", "modland", "files", "Artist", "empty.mod")
	tempPath := filepath.Join(base, ".cache", "modarchive", "files", "tmp_partial.it")
	unsupportedPath := filepath.Join(base, ".cache", "modland", "files", "Artist", "notes.txt")
	writeCacheFile(t, modlandPath, 8, time.Now())
	writeCacheFile(t, archivePath, 8, time.Now())
	writeCacheFile(t, zeroPath, 0, time.Now())
	writeCacheFile(t, tempPath, 8, time.Now())
	writeCacheFile(t, unsupportedPath, 8, time.Now())

	if err := cache.ReconcileManifest(); err != nil {
		t.Fatal(err)
	}
	paths := cache.CachedVirtualPaths()
	if len(paths) != 2 {
		t.Fatalf("cached tracks = %d, want 2", len(paths))
	}
	if !cache.IsCached(ModlandPrefix + "Artist/song.mod") {
		t.Fatal("discovered Modland track is not cached")
	}
	if !cache.IsCached(ModArchivePrefix + "http://modarchive.textfiles.com/1999/song.it") {
		t.Fatal("discovered ModArchive track is not cached")
	}
}

func TestTrackCacheReconcilePreservesValidManifestPathAndDropsStaleEntry(t *testing.T) {
	base := t.TempDir()
	cache := NewTrackCache(base)
	physical := filepath.Join(base, ".cache", "modarchive", "files", "1999", "archive", "song.it")
	writeCacheFile(t, physical, 8, time.Now())
	original := ModArchivePrefix + "http://modarchive.textfiles.com/1999/archive.zip#song.it"
	manifestPath := cache.manifestPath()
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := trackCacheManifest{
		Version: trackCacheManifestVersion,
		Paths:   []string{original, ModlandPrefix + "missing/song.mod"},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cache.ReconcileManifest(); err != nil {
		t.Fatal(err)
	}
	paths := cache.CachedVirtualPaths()
	if len(paths) != 1 || paths[0] != original {
		t.Fatalf("reconciled paths = %+v, want original ModArchive path", paths)
	}
	if got := cache.LocalPath(original); got != physical {
		t.Fatalf("preserved path resolves to %q, want %q", got, physical)
	}
}

func TestTrackCacheCorruptManifestRebuildsFromDisk(t *testing.T) {
	base := t.TempDir()
	cache := NewTrackCache(base)
	physical := filepath.Join(base, ".cache", "modland", "files", "MOD", "song.xm")
	writeCacheFile(t, physical, 8, time.Now())
	manifestPath := cache.manifestPath()
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cache.ReconcileManifest(); err != nil {
		t.Fatal(err)
	}
	paths := cache.CachedVirtualPaths()
	if len(paths) != 1 || paths[0] != ModlandPrefix+"MOD/song.xm" {
		t.Fatalf("rebuilt paths = %+v", paths)
	}
	var saved trackCacheManifest
	if err := json.Unmarshal(mustReadFile(t, manifestPath), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Version != trackCacheManifestVersion || len(saved.Paths) != 1 {
		t.Fatalf("saved manifest = %+v", saved)
	}
}

func TestTrackCacheCorruptManifestRecoversExtractedModArchiveZIP(t *testing.T) {
	base := t.TempDir()
	cache := NewTrackCache(base)
	physical := filepath.Join(base, ".cache", "modarchive", "files", "1999", "archive")
	writeCacheFile(t, physical, 8, time.Now())
	manifestPath := cache.manifestPath()
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cache.ReconcileManifest(); err != nil {
		t.Fatal(err)
	}
	want := ModArchivePrefix + "http://modarchive.textfiles.com/1999/archive.zip"
	paths := cache.CachedVirtualPaths()
	if len(paths) != 1 || paths[0] != want || cache.LocalPath(want) != physical {
		t.Fatalf("recovered paths = %+v, local = %q", paths, cache.LocalPath(want))
	}
}

func TestTrackCacheTouchDoesNotCreateManifest(t *testing.T) {
	base := t.TempDir()
	cache := NewTrackCache(base)
	virtual := ModlandPrefix + "MOD/song.mod"
	writeCacheFile(t, filepath.Join(base, ".cache", "modland", "files", "MOD", "song.mod"), 8, time.Now())
	if err := cache.Touch(virtual); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cache.manifestPath()); !os.IsNotExist(err) {
		t.Fatalf("Touch created manifest, stat error = %v", err)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
