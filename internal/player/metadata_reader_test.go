package player

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMetadataReaderCachesEmptyTagsAndInvalidatesChangedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.mod")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := NewMetadataReader(dir)
	calls := 0
	reader.read = func(string) TrackMeta { calls++; return TrackMeta{} }
	album := Album{Path: DownloadsPrefix, Tracks: []string{path}}
	for range 3 {
		infos := reader.Load(context.Background(), album)
		if len(infos) != 1 || !infos[0].Cached {
			t.Fatal("missing cached track")
		}
	}
	if calls != 1 {
		t.Fatalf("empty metadata re-read %d times", calls)
	}
	if err := os.WriteFile(path, []byte("changed size"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader.Load(context.Background(), album)
	if calls != 2 {
		t.Fatal("changed file reused stale metadata")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if infos := reader.Load(context.Background(), album); infos[0].Cached {
		t.Fatal("deleted file remains cached")
	}
}

func TestMetadataReaderPersistsEmptyLocalMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.mod")
	if err := os.WriteFile(path, []byte("track"), 0o600); err != nil {
		t.Fatal(err)
	}
	album := Album{Path: dir, Tracks: []string{path}}
	first := NewMetadataReader(dir)
	first.read = func(string) TrackMeta { return TrackMeta{} }
	first.Load(context.Background(), album)
	second := NewMetadataReader(dir)
	second.read = func(string) TrackMeta { t.Fatal("empty tags were not persisted"); return TrackMeta{} }
	second.Load(context.Background(), album)
}

func TestDownloadsAreVirtualAndMetadataKeysDoNotCollide(t *testing.T) {
	dir := t.TempDir()
	album := Album{Path: DownloadsPrefix}
	for _, sub := range []string{"a", "b"} {
		folder := filepath.Join(dir, sub)
		if err := os.Mkdir(folder, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(folder, "same.mod")
		if err := os.WriteFile(path, []byte("track"), 0o600); err != nil {
			t.Fatal(err)
		}
		album.Tracks = append(album.Tracks, path)
	}
	if !IsVirtual(album) || len(RealAlbumsOnly([]Album{album})) != 0 {
		t.Fatal("downloads treated as a filesystem album")
	}
	reader := NewMetadataReader(dir)
	reader.read = func(path string) TrackMeta { return TrackMeta{Title: filepath.Base(filepath.Dir(path))} }
	for range 2 {
		infos := reader.Load(context.Background(), album)
		if infos[0].Title != "a" || infos[1].Title != "b" {
			t.Fatal("same basenames collided")
		}
	}
	for _, path := range album.Tracks {
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), metaFileName)); !os.IsNotExist(err) {
			t.Fatal("virtual album wrote a directory cache")
		}
	}
}

func TestMetadataReaderCancellationStopsBetweenFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "track.mod")
	if err := os.WriteFile(path, []byte("track"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := NewMetadataReader(dir)
	calls := 0
	reader.read = func(string) TrackMeta { calls++; cancel(); return TrackMeta{} }
	if infos := reader.Load(ctx, Album{Tracks: []string{path, path}}); infos != nil || calls != 1 {
		t.Fatal("canceled batch published partial metadata")
	}
}
