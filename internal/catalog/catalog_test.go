package catalog

import (
	"errors"
	"fmt"
	"math/rand"
	"testing"
)

// mockIndex is a test double implementing SourceIndex.
type mockIndex struct {
	source      SourceKind
	tracks      []ShuffleTrack
	directories map[DirectoryKey]DirectoryListing
}

func (m *mockIndex) Source() SourceKind     { return m.source }
func (m *mockIndex) TrackCount() uint64     { return uint64(len(m.tracks)) }
func (m *mockIndex) DirectoryCount() uint64 { return uint64(len(m.directories)) }
func (m *mockIndex) Fingerprint() Fingerprint {
	return Fingerprint{TrackCount: uint64(len(m.tracks))}
}

func (m *mockIndex) RandomTrack(rng *rand.Rand) (ShuffleTrack, error) {
	if len(m.tracks) == 0 {
		return ShuffleTrack{}, fmt.Errorf("no tracks")
	}
	return m.tracks[rng.Intn(len(m.tracks))], nil
}

func (m *mockIndex) RandomTrackInDirectory(key DirectoryKey, rng *rand.Rand) (ShuffleTrack, error) {
	listing, ok := m.directories[key]
	if !ok {
		return ShuffleTrack{}, fmt.Errorf("directory not found")
	}
	if len(listing.Entries) == 0 {
		return ShuffleTrack{}, fmt.Errorf("empty directory")
	}
	entry := listing.Entries[rng.Intn(len(listing.Entries))]
	return ShuffleTrack{Path: entry.Path, Source: m.source, DirectoryKey: key}, nil
}

func (m *mockIndex) DirectoryList(key DirectoryKey) (DirectoryListing, error) {
	listing, ok := m.directories[key]
	if !ok {
		return DirectoryListing{}, fmt.Errorf("directory not found")
	}
	return listing, nil
}

func TestShuffleCatalogAvailable(t *testing.T) {
	local := &mockIndex{
		source: SourceLocal,
		tracks: []ShuffleTrack{{Path: "a.mod"}, {Path: "b.mod"}},
	}
	modland := &mockIndex{
		source: SourceModland,
		tracks: nil, // empty
	}
	cat := NewShuffleCatalog(local, modland)
	available := cat.Available()
	if len(available) != 1 {
		t.Fatalf("available = %d, want 1", len(available))
	}
	if available[0].Source() != SourceLocal {
		t.Fatalf("available source = %v, want Local", available[0].Source())
	}
}

func TestShuffleCatalogTotalTrackCount(t *testing.T) {
	local := &mockIndex{source: SourceLocal, tracks: make([]ShuffleTrack, 100)}
	modland := &mockIndex{source: SourceModland, tracks: make([]ShuffleTrack, 500)}
	cat := NewShuffleCatalog(local, modland)
	if got := cat.TotalTrackCount(); got != 600 {
		t.Fatalf("TotalTrackCount = %d, want 600", got)
	}
}

func TestShuffleCatalogRandomTrackAll(t *testing.T) {
	local := &mockIndex{
		source: SourceLocal,
		tracks: []ShuffleTrack{{Path: "local1.mod"}, {Path: "local2.mod"}},
	}
	modland := &mockIndex{
		source: SourceModland,
		tracks: []ShuffleTrack{{Path: "modland1.mod"}, {Path: "modland2.mod"}},
	}
	cat := NewShuffleCatalog(local, modland)
	rng := rand.New(rand.NewSource(42))

	seen := make(map[string]bool)
	for range 100 {
		track, err := cat.RandomTrackAll(rng)
		if err != nil {
			t.Fatalf("RandomTrackAll: %v", err)
		}
		seen[track.Path] = true
	}
	// Should see tracks from both sources.
	if !seen["local1.mod"] && !seen["local2.mod"] {
		t.Fatal("no local tracks selected")
	}
	if !seen["modland1.mod"] && !seen["modland2.mod"] {
		t.Fatal("no modland tracks selected")
	}
}

func TestShuffleCatalogRandomTrackAllNoSources(t *testing.T) {
	cat := NewShuffleCatalog()
	rng := rand.New(rand.NewSource(1))
	if _, err := cat.RandomTrackAll(rng); !errors.Is(err, ErrNoSourceAvailable) {
		t.Fatalf("got %v, want ErrNoSourceAvailable", err)
	}
}

func TestWeightedIndexStaysWithinTotal(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for range 1000 {
		if got := weightedIndex(rng, 7); got >= 7 {
			t.Fatalf("weightedIndex = %d, want value below 7", got)
		}
	}
}

func TestShuffleCatalogRandomTrackFromSource(t *testing.T) {
	local := &mockIndex{
		source: SourceLocal,
		tracks: []ShuffleTrack{{Path: "local1.mod"}},
	}
	cat := NewShuffleCatalog(local)
	rng := rand.New(rand.NewSource(1))

	track, err := cat.RandomTrackFromSource(SourceLocal, rng)
	if err != nil {
		t.Fatalf("RandomTrackFromSource: %v", err)
	}
	if track.Path != "local1.mod" {
		t.Fatalf("got path %q, want local1.mod", track.Path)
	}
}

func TestShuffleCatalogRandomTrackFromSourceUnavailable(t *testing.T) {
	cat := NewShuffleCatalog()
	rng := rand.New(rand.NewSource(1))
	if _, err := cat.RandomTrackFromSource(SourceModland, rng); err == nil {
		t.Fatal("expected error for unavailable source")
	}
}

func TestShuffleCatalogSourceIndex(t *testing.T) {
	local := &mockIndex{source: SourceLocal, tracks: make([]ShuffleTrack, 10)}
	cat := NewShuffleCatalog(local)
	if cat.SourceIndex(SourceLocal) == nil {
		t.Fatal("SourceIndex(Local) returned nil")
	}
	if cat.SourceIndex(SourceModland) != nil {
		t.Fatal("SourceIndex(Modland) should be nil")
	}
}
