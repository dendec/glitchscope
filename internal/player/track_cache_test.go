package player

import (
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
