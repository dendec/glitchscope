package player

import (
	"math/rand"
	"testing"

	"github.com/dendec/glitchscope/internal/catalog"
)

func testLocalAlbums() []Album {
	return []Album{
		{Name: "Album1", Path: "/music/Album1", Tracks: []string{"/music/Album1/a.mp3", "/music/Album1/b.mp3"}},
		{Name: "Album2", Path: "/music/Album2", Tracks: []string{"/music/Album2/c.flac"}},
		{Name: "Virtual", Path: ModlandPrefix + "Format/Author", Tracks: []string{ModlandPrefix + "Format/Author/x.mod"}},
	}
}

func TestLocalShuffleSourceExcludesVirtual(t *testing.T) {
	src := NewLocalShuffleSource(testLocalAlbums(), "fp1")
	if src.Source() != catalog.SourceLocal {
		t.Fatalf("Source = %v, want Local", src.Source())
	}
	if src.TrackCount() != 3 {
		t.Fatalf("TrackCount = %d, want 3 (virtual album excluded)", src.TrackCount())
	}
	if src.DirectoryCount() != 2 {
		t.Fatalf("DirectoryCount = %d, want 2", src.DirectoryCount())
	}
}

func TestLocalShuffleSourceRandomTrackReachesAll(t *testing.T) {
	src := NewLocalShuffleSource(testLocalAlbums(), "fp1")
	rng := rand.New(rand.NewSource(1))
	seen := map[string]bool{}
	for range 200 {
		tr, err := src.RandomTrack(rng)
		if err != nil {
			t.Fatal(err)
		}
		if tr.Source != catalog.SourceLocal {
			t.Fatalf("Source = %v, want Local", tr.Source)
		}
		seen[tr.Path] = true
	}
	if len(seen) != 3 {
		t.Fatalf("expected all 3 real tracks to be reachable, saw %d", len(seen))
	}
}

func TestLocalShuffleSourceRandomTrackInDirectory(t *testing.T) {
	src := NewLocalShuffleSource(testLocalAlbums(), "fp1")
	key := catalog.DirectoryKey{Source: catalog.SourceLocal, Locator: "/music/Album1"}
	rng := rand.New(rand.NewSource(1))
	for range 20 {
		tr, err := src.RandomTrackInDirectory(key, rng)
		if err != nil {
			t.Fatal(err)
		}
		if tr.DirectoryKey != key {
			t.Fatalf("DirectoryKey = %v, want %v", tr.DirectoryKey, key)
		}
	}
}

func TestLocalShuffleSourceDirectoryList(t *testing.T) {
	src := NewLocalShuffleSource(testLocalAlbums(), "fp1")
	listing, err := src.DirectoryList(catalog.DirectoryKey{Source: catalog.SourceLocal, Locator: "/music/Album2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 1 || listing.Entries[0].Name != "c.flac" {
		t.Fatalf("Entries = %+v, want 1 entry named c.flac", listing.Entries)
	}
}

func TestLocalShuffleSourceUnknownDirectory(t *testing.T) {
	src := NewLocalShuffleSource(testLocalAlbums(), "fp1")
	if _, err := src.DirectoryList(catalog.DirectoryKey{Source: catalog.SourceLocal, Locator: "/nope"}); err == nil {
		t.Fatal("expected error for unknown directory")
	}
}

func TestLocalShuffleSourceEmpty(t *testing.T) {
	src := NewLocalShuffleSource(nil, "fp1")
	if _, err := src.RandomTrack(rand.New(rand.NewSource(1))); err == nil {
		t.Fatal("expected error for empty source")
	}
}

func TestLocalShuffleSourceFingerprint(t *testing.T) {
	src := NewLocalShuffleSource(testLocalAlbums(), "scan-gen-1")
	fp := src.Fingerprint()
	if fp.SourceHash != "scan-gen-1" {
		t.Fatalf("SourceHash = %q, want scan-gen-1", fp.SourceHash)
	}
	if fp.TrackCount != 3 || fp.DirectoryCount != 2 {
		t.Fatalf("Fingerprint = %+v, want TrackCount=3 DirectoryCount=2", fp)
	}
}
