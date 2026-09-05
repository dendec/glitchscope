package modland

import (
	"math/rand"
	"testing"

	"github.com/dendec/glitchscope/internal/catalog"
)

func testCatalog() *Catalog {
	return &Catalog{
		Albums: []Album{
			{Name: "Protracker/Curt Cool", Tracks: []Track{
				{Name: "song1.mod", Size: 100},
				{Name: "song2.mod", Size: 200},
			}},
			{Name: "Protracker/Skaven", Tracks: []Track{
				{Name: "song3.mod", Size: 150},
			}},
			{Name: "ScreamTracker3/Purple Motion", Tracks: []Track{
				{Name: "tune.s3m", Size: 300},
			}},
		},
	}
}

func TestBuildAndOpenModlandShuffleIndex(t *testing.T) {
	baseDir := t.TempDir()
	if err := BuildShuffleIndex(baseDir, testCatalog()); err != nil {
		t.Fatalf("BuildShuffleIndex: %v", err)
	}

	src := OpenShuffleSource(baseDir)
	if src == nil {
		t.Fatal("OpenShuffleSource returned nil")
	}
	defer src.Close()

	if src.Source() != catalog.SourceModland {
		t.Fatalf("Source = %v, want Modland", src.Source())
	}
	if src.TrackCount() != 4 {
		t.Fatalf("TrackCount = %d, want 4", src.TrackCount())
	}
	if src.DirectoryCount() != 3 {
		t.Fatalf("DirectoryCount = %d, want 3", src.DirectoryCount())
	}
}

func TestModlandRandomTrackNeverEmpty(t *testing.T) {
	baseDir := t.TempDir()
	if err := BuildShuffleIndex(baseDir, testCatalog()); err != nil {
		t.Fatal(err)
	}
	src := OpenShuffleSource(baseDir)
	if src == nil {
		t.Fatal("OpenShuffleSource returned nil")
	}
	defer src.Close()

	rng := rand.New(rand.NewSource(1))
	seen := map[string]bool{}
	for range 200 {
		tr, err := src.RandomTrack(rng)
		if err != nil {
			t.Fatal(err)
		}
		if tr.Path == "" {
			t.Fatal("empty track path")
		}
		if tr.Source != catalog.SourceModland {
			t.Fatalf("Source = %v, want Modland", tr.Source)
		}
		seen[tr.Path] = true
	}
	if len(seen) != 4 {
		t.Fatalf("expected all 4 tracks to be reachable, saw %d", len(seen))
	}
}

func TestModlandRandomTrackInDirectory(t *testing.T) {
	baseDir := t.TempDir()
	if err := BuildShuffleIndex(baseDir, testCatalog()); err != nil {
		t.Fatal(err)
	}
	src := OpenShuffleSource(baseDir)
	if src == nil {
		t.Fatal("OpenShuffleSource returned nil")
	}
	defer src.Close()

	key := catalog.DirectoryKey{Source: catalog.SourceModland, Locator: "Protracker/Curt Cool"}
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

func TestModlandDirectoryList(t *testing.T) {
	baseDir := t.TempDir()
	if err := BuildShuffleIndex(baseDir, testCatalog()); err != nil {
		t.Fatal(err)
	}
	src := OpenShuffleSource(baseDir)
	if src == nil {
		t.Fatal("OpenShuffleSource returned nil")
	}
	defer src.Close()

	listing, err := src.DirectoryList(catalog.DirectoryKey{Source: catalog.SourceModland, Locator: "ScreamTracker3/Purple Motion"})
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 1 {
		t.Fatalf("Entries = %d, want 1", len(listing.Entries))
	}
	if listing.Entries[0].Name != "tune.s3m" {
		t.Fatalf("Name = %q, want tune.s3m", listing.Entries[0].Name)
	}
}

func TestModlandFormats(t *testing.T) {
	baseDir := t.TempDir()
	if err := BuildShuffleIndex(baseDir, testCatalog()); err != nil {
		t.Fatal(err)
	}
	src := OpenShuffleSource(baseDir)
	if src == nil {
		t.Fatal("OpenShuffleSource returned nil")
	}
	defer src.Close()

	formats := src.Formats()
	if len(formats) != 2 {
		t.Fatalf("Formats() = %d, want 2", len(formats))
	}
	byName := map[string]FormatSummary{}
	for _, f := range formats {
		byName[f.Name] = f
	}
	pt, ok := byName["Protracker"]
	if !ok {
		t.Fatal("missing Protracker format")
	}
	if pt.AlbumCount != 2 || pt.TrackCount != 3 {
		t.Fatalf("Protracker = %+v, want AlbumCount=2 TrackCount=3", pt)
	}
	st, ok := byName["ScreamTracker3"]
	if !ok {
		t.Fatal("missing ScreamTracker3 format")
	}
	if st.AlbumCount != 1 || st.TrackCount != 1 {
		t.Fatalf("ScreamTracker3 = %+v, want AlbumCount=1 TrackCount=1", st)
	}
}

func TestModlandAlbumsInFormat(t *testing.T) {
	baseDir := t.TempDir()
	if err := BuildShuffleIndex(baseDir, testCatalog()); err != nil {
		t.Fatal(err)
	}
	src := OpenShuffleSource(baseDir)
	if src == nil {
		t.Fatal("OpenShuffleSource returned nil")
	}
	defer src.Close()

	albums, err := src.AlbumsInFormat("Protracker")
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 2 {
		t.Fatalf("AlbumsInFormat(Protracker) = %d, want 2", len(albums))
	}
	names := map[string]bool{}
	for _, a := range albums {
		names[a.Locator] = true
	}
	if !names["Protracker/Curt Cool"] || !names["Protracker/Skaven"] {
		t.Fatalf("AlbumsInFormat(Protracker) = %+v, missing expected albums", albums)
	}
}

func TestModlandAlbumsInFormatUnknown(t *testing.T) {
	baseDir := t.TempDir()
	if err := BuildShuffleIndex(baseDir, testCatalog()); err != nil {
		t.Fatal(err)
	}
	src := OpenShuffleSource(baseDir)
	if src == nil {
		t.Fatal("OpenShuffleSource returned nil")
	}
	defer src.Close()

	if _, err := src.AlbumsInFormat("NoSuchFormat"); err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestModlandShuffleIndexNeedsRebuild(t *testing.T) {
	baseDir := t.TempDir()
	if ShuffleIndexNeedsRebuild(baseDir) != true {
		t.Fatal("missing index should need rebuild")
	}
	if err := SaveCatalog(baseDir, testCatalog()); err != nil {
		t.Fatal(err)
	}
	if err := BuildShuffleIndex(baseDir, testCatalog()); err != nil {
		t.Fatal(err)
	}
	if ShuffleIndexNeedsRebuild(baseDir) {
		t.Fatal("up-to-date index should not need rebuild")
	}

	// Changing the catalog file changes the fingerprint.
	cat2 := testCatalog()
	cat2.Albums = append(cat2.Albums, Album{Name: "Extra/Author", Tracks: []Track{{Name: "x.mod", Size: 10}}})
	if err := SaveCatalog(baseDir, cat2); err != nil {
		t.Fatal(err)
	}
	if !ShuffleIndexNeedsRebuild(baseDir) {
		t.Fatal("changed catalog should need rebuild")
	}
}

func TestModlandEmptyAlbumsExcluded(t *testing.T) {
	baseDir := t.TempDir()
	cat := &Catalog{Albums: []Album{
		{Name: "Empty/Author", Tracks: nil},
		{Name: "Full/Author", Tracks: []Track{{Name: "a.mod", Size: 1}}},
	}}
	if err := BuildShuffleIndex(baseDir, cat); err != nil {
		t.Fatal(err)
	}
	src := OpenShuffleSource(baseDir)
	if src == nil {
		t.Fatal("OpenShuffleSource returned nil")
	}
	defer src.Close()
	if src.DirectoryCount() != 1 {
		t.Fatalf("DirectoryCount = %d, want 1 (empty album excluded)", src.DirectoryCount())
	}
}

func TestModlandOpenShuffleSourceRejectsMissingIndex(t *testing.T) {
	baseDir := t.TempDir()
	if src := OpenShuffleSource(baseDir); src != nil {
		src.Close()
		t.Fatal("expected nil for missing index")
	}
}

func TestModlandRecordCacheEvictionByMemory(t *testing.T) {
	c := newRecordCache()
	tracks := make([]ShuffleTrackEntry, 300_000)
	for i := range tracks {
		tracks[i] = ShuffleTrackEntry{Path: "p", Name: "n"}
	}
	r1 := &ShuffleAlbumRecord{Locator: "a1", Tracks: tracks}
	r2 := &ShuffleAlbumRecord{Locator: "a2", Tracks: tracks}
	c.put("a1", r1)
	c.put("a2", r2)
	if len(c.entries) > 1 {
		t.Fatalf("expected <=1 entries under memory budget, got %d", len(c.entries))
	}
	if c.totalBytes > maxCachedBytes {
		t.Fatalf("totalBytes %d exceeds budget %d", c.totalBytes, maxCachedBytes)
	}
}

func TestModlandRecordCacheSkipsOversizedRecord(t *testing.T) {
	c := newRecordCache()
	oversizedTracks := make([]ShuffleTrackEntry, 500_000)
	for i := range oversizedTracks {
		oversizedTracks[i] = ShuffleTrackEntry{Path: "p", Name: "n"}
	}
	record := &ShuffleAlbumRecord{Locator: "huge", Tracks: oversizedTracks}
	if estimateRecordBytes(record) <= maxCachedBytes {
		t.Fatal("test record must exceed the cache budget")
	}

	c.put("huge", record)
	if len(c.entries) != 0 {
		t.Fatalf("expected oversized record not to be cached, got %d entries", len(c.entries))
	}
	if c.totalBytes != 0 {
		t.Fatalf("totalBytes = %d, want 0", c.totalBytes)
	}
}
