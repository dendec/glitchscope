package modarchive

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/dendec/glitchscope/internal/archive"
	"github.com/dendec/glitchscope/internal/catalog"
)

// writeMultiBucketSnapshot creates a single snapshot GSA with multiple bucket entries.
func writeMultiBucketSnapshot(t *testing.T, baseDir string, buckets map[string][]snapshotRecord) {
	t.Helper()
	gsaDir := filepath.Dir(SnapshotCatalogPath(baseDir))
	if err := os.MkdirAll(gsaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var entries []archive.SourceEntry
	for name, records := range buckets {
		data, err := json.Marshal(records)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, archive.SourceEntry{Name: name, Data: data})
	}
	if err := archive.Write(SnapshotCatalogPath(baseDir), entries); err != nil {
		t.Fatal(err)
	}
}

func TestBuildAndOpenShuffleIndex(t *testing.T) {
	ResetMemCache()
	defer ResetMemCache()
	CloseSnapshotCatalog()
	defer CloseSnapshotCatalog()

	baseDir := t.TempDir()
	writeMultiBucketSnapshot(t, baseDir, map[string][]snapshotRecord{
		"A/A0.zip": {{Name: "alpha.mod", Size: 100, ArchiveOffset: 10, ArchiveEndOffset: 50, CompressedSize: 40}},
		"B/B0.zip": {{Name: "beta.xm", Size: 200, ArchiveOffset: 10, ArchiveEndOffset: 80, CompressedSize: 70}},
	})

	if err := BuildShuffleIndex(baseDir, nil); err != nil {
		t.Fatalf("BuildShuffleIndex: %v", err)
	}

	src := OpenShuffleSource(baseDir)
	if src == nil {
		t.Fatal("OpenShuffleSource returned nil")
	}
	defer src.Close()

	if src.Source() != catalog.SourceModArchive {
		t.Fatalf("Source = %v, want ModArchive", src.Source())
	}
	if src.TrackCount() != 2 {
		t.Fatalf("TrackCount = %d, want 2", src.TrackCount())
	}
	if src.DirectoryCount() != 2 {
		t.Fatalf("DirectoryCount = %d, want 2", src.DirectoryCount())
	}
}

func TestShuffleSourceRandomTrack(t *testing.T) {
	ResetMemCache()
	defer ResetMemCache()
	CloseSnapshotCatalog()
	defer CloseSnapshotCatalog()

	baseDir := t.TempDir()
	writeMultiBucketSnapshot(t, baseDir, map[string][]snapshotRecord{
		"A/A0.zip": {{Name: "alpha.mod", Size: 100, ArchiveOffset: 10, ArchiveEndOffset: 50, CompressedSize: 40}},
		"B/B0.zip": {{Name: "beta.xm", Size: 200, ArchiveOffset: 10, ArchiveEndOffset: 80, CompressedSize: 70}},
	})

	if err := BuildShuffleIndex(baseDir, nil); err != nil {
		t.Fatal(err)
	}

	src := OpenShuffleSource(baseDir)
	if src == nil {
		t.Fatal("OpenShuffleSource returned nil")
	}
	defer src.Close()

	rng := rand.New(rand.NewSource(42))
	seen := make(map[string]bool)
	for range 100 {
		track, err := src.RandomTrack(rng)
		if err != nil {
			t.Fatalf("RandomTrack: %v", err)
		}
		if track.Source != catalog.SourceModArchive {
			t.Fatalf("Source = %v, want ModArchive", track.Source)
		}
		seen[track.Path] = true
	}
	if len(seen) < 2 {
		t.Fatalf("expected tracks from at least 2 buckets, got %d unique paths", len(seen))
	}
}

func TestShuffleSourceRandomTrackInDirectory(t *testing.T) {
	ResetMemCache()
	defer ResetMemCache()
	CloseSnapshotCatalog()
	defer CloseSnapshotCatalog()

	baseDir := t.TempDir()
	writeMultiBucketSnapshot(t, baseDir, map[string][]snapshotRecord{
		"A/A0.zip": {{Name: "alpha.mod", Size: 100, ArchiveOffset: 10, ArchiveEndOffset: 50, CompressedSize: 40}},
	})

	if err := BuildShuffleIndex(baseDir, nil); err != nil {
		t.Fatal(err)
	}

	src := OpenShuffleSource(baseDir)
	if src == nil {
		t.Fatal("OpenShuffleSource returned nil")
	}
	defer src.Close()

	entries := src.SortedEntries()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}

	rng := rand.New(rand.NewSource(1))
	// Use the canonical URL (Locator) — the .idx no longer stores
	// snapshot records, so loadRecord must reconstruct from source GSA.
	key := catalog.DirectoryKey{
		Source:  catalog.SourceModArchive,
		Locator: entries[0].Locator,
	}
	track, err := src.RandomTrackInDirectory(key, rng)
	if err != nil {
		t.Fatalf("RandomTrackInDirectory: %v", err)
	}
	if track.Source != catalog.SourceModArchive {
		t.Fatalf("Source = %v, want ModArchive", track.Source)
	}
	if track.DirectoryKey.Source != catalog.SourceModArchive {
		t.Fatalf("DirectoryKey.Source = %v, want ModArchive", track.DirectoryKey.Source)
	}
	wantURL := BaseURL + SnapshotDir + "/A/A0.zip"
	if track.DirectoryKey.Locator != wantURL {
		t.Fatalf("DirectoryKey.Locator = %q, want %q", track.DirectoryKey.Locator, wantURL)
	}
}

func TestShuffleSourceDirectoryList(t *testing.T) {
	ResetMemCache()
	defer ResetMemCache()
	CloseSnapshotCatalog()
	defer CloseSnapshotCatalog()

	baseDir := t.TempDir()
	writeMultiBucketSnapshot(t, baseDir, map[string][]snapshotRecord{
		"A/A0.zip": {{Name: "alpha.mod", Size: 100, ArchiveOffset: 10, ArchiveEndOffset: 50, CompressedSize: 40}},
	})

	if err := BuildShuffleIndex(baseDir, nil); err != nil {
		t.Fatal(err)
	}

	src := OpenShuffleSource(baseDir)
	if src == nil {
		t.Fatal("OpenShuffleSource returned nil")
	}
	defer src.Close()

	entries := src.SortedEntries()
	key := catalog.DirectoryKey{
		Source:  catalog.SourceModArchive,
		Locator: entries[0].Locator,
	}

	listing, err := src.DirectoryList(key)
	if err != nil {
		t.Fatalf("DirectoryList: %v", err)
	}
	if len(listing.Entries) != 1 {
		t.Fatalf("Entries = %d, want 1", len(listing.Entries))
	}
	if listing.Entries[0].Name != "alpha.mod" {
		t.Fatalf("Entry name = %q, want alpha.mod", listing.Entries[0].Name)
	}
}

func TestShuffleSourceWithCachedDirectories(t *testing.T) {
	ResetMemCache()
	defer ResetMemCache()
	CloseSnapshotCatalog()
	defer CloseSnapshotCatalog()

	baseDir := t.TempDir()

	cat := &catalogData{
		Directories: map[string][]DirItem{
			"http://modarchive.textfiles.com/modarchive_2023_additions/MOD/": {
				{Name: "track1.mod", URL: "http://modarchive.textfiles.com/modarchive_2023_additions/MOD/track1.mod", Kind: KindFile, CleanName: "track1.mod"},
				{Name: "track2.xm", URL: "http://modarchive.textfiles.com/modarchive_2023_additions/MOD/track2.xm", Kind: KindFile, CleanName: "track2.xm"},
			},
		},
	}

	if err := BuildShuffleIndex(baseDir, cat); err != nil {
		t.Fatal(err)
	}

	src := OpenShuffleSource(baseDir)
	if src == nil {
		t.Fatal("OpenShuffleSource returned nil")
	}
	defer src.Close()

	if src.TrackCount() != 2 {
		t.Fatalf("TrackCount = %d, want 2", src.TrackCount())
	}
	if src.DirectoryCount() != 1 {
		t.Fatalf("DirectoryCount = %d, want 1", src.DirectoryCount())
	}

	rng := rand.New(rand.NewSource(1))
	track, err := src.RandomTrack(rng)
	if err != nil {
		t.Fatalf("RandomTrack: %v", err)
	}
	if track.Source != catalog.SourceModArchive {
		t.Fatalf("Source = %v", track.Source)
	}
}

func TestShuffleSourceNeedsRebuild(t *testing.T) {
	ResetMemCache()
	defer ResetMemCache()
	CloseSnapshotCatalog()
	defer CloseSnapshotCatalog()

	baseDir := t.TempDir()

	if !ShuffleSourceNeedsRebuild(baseDir, nil) {
		t.Fatal("expected NeedsRebuild=true with no index")
	}

	writeMultiBucketSnapshot(t, baseDir, map[string][]snapshotRecord{
		"A/A0.zip": {{Name: "alpha.mod", Size: 100, ArchiveOffset: 10, ArchiveEndOffset: 50, CompressedSize: 40}},
	})
	if err := BuildShuffleIndex(baseDir, nil); err != nil {
		t.Fatal(err)
	}

	if ShuffleSourceNeedsRebuild(baseDir, nil) {
		t.Fatal("expected NeedsRebuild=false after build")
	}

	cat := &catalogData{
		Directories: map[string][]DirItem{
			"http://example.com/": {{Name: "new.mod", Kind: KindFile}},
		},
	}
	if !ShuffleSourceNeedsRebuild(baseDir, cat) {
		t.Fatal("expected NeedsRebuild=true after catalog change")
	}
}

func TestShuffleSourceOpenMissingIndex(t *testing.T) {
	src := OpenShuffleSource(t.TempDir())
	if src != nil {
		src.Close()
		t.Fatal("expected nil for missing index")
	}
}

func TestShuffleSourceTrackWeightedDistribution(t *testing.T) {
	ResetMemCache()
	defer ResetMemCache()
	CloseSnapshotCatalog()
	defer CloseSnapshotCatalog()

	baseDir := t.TempDir()

	recordsB := make([]snapshotRecord, 9)
	for i := range recordsB {
		recordsB[i] = snapshotRecord{Name: "b" + string(rune('0'+i)) + ".mod", Size: 100, ArchiveOffset: 10, ArchiveEndOffset: 50, CompressedSize: 40}
	}
	writeMultiBucketSnapshot(t, baseDir, map[string][]snapshotRecord{
		"A/A.zip": {{Name: "a.mod", Size: 100, ArchiveOffset: 10, ArchiveEndOffset: 50, CompressedSize: 40}},
		"B/B.zip": recordsB,
	})

	if err := BuildShuffleIndex(baseDir, nil); err != nil {
		t.Fatal(err)
	}

	src := OpenShuffleSource(baseDir)
	if src == nil {
		t.Fatal("OpenShuffleSource returned nil")
	}
	defer src.Close()

	rng := rand.New(rand.NewSource(42))
	counts := map[string]int{}
	const n = 10000
	for range n {
		track, err := src.RandomTrack(rng)
		if err != nil {
			t.Fatal(err)
		}
		if track.TrackKey == "a.mod" {
			counts["a"]++
		} else {
			counts["b"]++
		}
	}

	ratio := float64(counts["b"]) / float64(counts["a"])
	if ratio < 5 || ratio > 20 {
		t.Fatalf("expected ~9:1 ratio, got a=%d b=%d (ratio=%.1f)", counts["a"], counts["b"], ratio)
	}
}

func TestShuffleSourceFingerprintDeterministic(t *testing.T) {
	ResetMemCache()
	defer ResetMemCache()
	CloseSnapshotCatalog()
	defer CloseSnapshotCatalog()

	baseDir := t.TempDir()
	writeMultiBucketSnapshot(t, baseDir, map[string][]snapshotRecord{
		"A/A.zip": {{Name: "a.mod", Size: 1, ArchiveOffset: 1, ArchiveEndOffset: 3, CompressedSize: 2}},
	})

	fp1 := ShuffleIndexFingerprint(baseDir, nil)
	fp2 := ShuffleIndexFingerprint(baseDir, nil)
	if fp1.SourceHash != fp2.SourceHash {
		t.Fatalf("fingerprint not deterministic: %s != %s", fp1.SourceHash, fp2.SourceHash)
	}
}

func TestBuildSnapshotCorruptedGSA(t *testing.T) {
	baseDir := t.TempDir()

	gsaDir := filepath.Dir(SnapshotCatalogPath(baseDir))
	if err := os.MkdirAll(gsaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(SnapshotCatalogPath(baseDir), []byte("not a gsa file"), 0o644); err != nil {
		t.Fatal(err)
	}

	records, err := buildSnapshotDirSummaries(baseDir)
	if err == nil {
		t.Fatalf("expected error for corrupted snapshot, got %d dirs", len(records))
	}
}

func TestBuildAddendumCorruptedGSA(t *testing.T) {
	baseDir := t.TempDir()

	gsaDir := filepath.Dir(AddendumCatalogPath(baseDir))
	if err := os.MkdirAll(gsaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(AddendumCatalogPath(baseDir), []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}

	records, err := buildAddendumDirSummaries(baseDir)
	if err == nil {
		t.Fatalf("expected error for corrupted addendum, got %d dirs", len(records))
	}
}

func TestOpenShuffleSourceRejectsCorruptedManifest(t *testing.T) {
	baseDir := t.TempDir()

	writeMultiBucketSnapshot(t, baseDir, map[string][]snapshotRecord{
		"A/A.zip": {{Name: "a.mod", Size: 100, ArchiveOffset: 10, ArchiveEndOffset: 50, CompressedSize: 40}},
	})
	if err := BuildShuffleIndex(baseDir, nil); err != nil {
		t.Fatal(err)
	}

	idxDir, _ := ShuffleIndexDir(baseDir)
	idxPath := filepath.Join(idxDir, _shuffleIndexFile)

	gsa, err := archive.Open(idxPath, _idxMaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	var entries []archive.SourceEntry
	for _, e := range gsa.Entries() {
		data, err := gsa.Read(e.Name)
		if err != nil {
			t.Fatal(err)
		}
		if e.Name == "manifest.json" {
			var meta ShuffleIndexMeta
			if err := json.Unmarshal(data, &meta); err != nil {
				t.Fatal(err)
			}
			meta.Schema = "wrong-schema"
			data, _ = json.Marshal(meta)
		}
		entries = append(entries, archive.SourceEntry{Name: e.Name, Data: data})
	}
	gsa.Close()
	if err := archive.Write(idxPath, entries); err != nil {
		t.Fatal(err)
	}

	src := OpenShuffleSource(baseDir)
	if src != nil {
		src.Close()
		t.Fatal("expected nil for corrupted manifest schema")
	}
}

func TestOpenShuffleSourceRejectsBadVersion(t *testing.T) {
	baseDir := t.TempDir()

	writeMultiBucketSnapshot(t, baseDir, map[string][]snapshotRecord{
		"A/A.zip": {{Name: "a.mod", Size: 100, ArchiveOffset: 10, ArchiveEndOffset: 50, CompressedSize: 40}},
	})
	if err := BuildShuffleIndex(baseDir, nil); err != nil {
		t.Fatal(err)
	}

	idxDir, _ := ShuffleIndexDir(baseDir)
	idxPath := filepath.Join(idxDir, _shuffleIndexFile)

	gsa, err := archive.Open(idxPath, _idxMaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	var entries []archive.SourceEntry
	for _, e := range gsa.Entries() {
		data, err := gsa.Read(e.Name)
		if err != nil {
			t.Fatal(err)
		}
		if e.Name == "manifest.json" {
			var meta ShuffleIndexMeta
			if err := json.Unmarshal(data, &meta); err != nil {
				t.Fatal(err)
			}
			meta.Version = 0
			data, _ = json.Marshal(meta)
		}
		entries = append(entries, archive.SourceEntry{Name: e.Name, Data: data})
	}
	gsa.Close()
	if err := archive.Write(idxPath, entries); err != nil {
		t.Fatal(err)
	}

	src := OpenShuffleSource(baseDir)
	if src != nil {
		src.Close()
		t.Fatal("expected nil for version=0")
	}
}

func TestShuffleIndexFingerprintContentSensitive(t *testing.T) {
	ResetMemCache()
	defer ResetMemCache()
	CloseSnapshotCatalog()
	defer CloseSnapshotCatalog()

	baseDir := t.TempDir()
	cat1 := &catalogData{
		Directories: map[string][]DirItem{
			"http://example.com/dir/": {
				{Name: "a.mod", URL: "http://example.com/dir/a.mod", Kind: KindFile, Size: 100, CleanName: "a.mod"},
			},
		},
	}
	cat2 := &catalogData{
		Directories: map[string][]DirItem{
			"http://example.com/dir/": {
				{Name: "a.mod", URL: "http://example.com/dir/a.mod", Kind: KindFile, Size: 200, CleanName: "a.mod"},
			},
		},
	}

	fp1 := ShuffleIndexFingerprint(baseDir, cat1)
	fp2 := ShuffleIndexFingerprint(baseDir, cat2)
	if fp1.SourceHash == fp2.SourceHash {
		t.Fatalf("fingerprint should change when item size changes: %s", fp1.SourceHash)
	}
}

func TestShuffleSourceNeedsRebuildOnCorruptedIndex(t *testing.T) {
	baseDir := t.TempDir()

	idxDir, _ := ShuffleIndexDir(baseDir)
	idxPath := filepath.Join(idxDir, _shuffleIndexFile)
	if err := os.WriteFile(idxPath, []byte("not a valid gsa"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !ShuffleSourceNeedsRebuild(baseDir, nil) {
		t.Fatal("expected NeedsRebuild=true for corrupted index")
	}
}

func TestSnapshotRecordsNotInIDX(t *testing.T) {
	ResetMemCache()
	defer ResetMemCache()
	CloseSnapshotCatalog()
	defer CloseSnapshotCatalog()

	baseDir := t.TempDir()
	writeMultiBucketSnapshot(t, baseDir, map[string][]snapshotRecord{
		"A/A0.zip": {{Name: "alpha.mod", Size: 100, ArchiveOffset: 10, ArchiveEndOffset: 50, CompressedSize: 40}},
	})

	if err := BuildShuffleIndex(baseDir, nil); err != nil {
		t.Fatal(err)
	}

	idxDir, _ := ShuffleIndexDir(baseDir)
	idxPath := filepath.Join(idxDir, _shuffleIndexFile)
	gsa, err := archive.Open(idxPath, _idxMaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	defer gsa.Close()

	names := make(map[string]bool)
	for _, e := range gsa.Entries() {
		names[e.Name] = true
	}
	if !names["manifest.json"] {
		t.Fatal(".idx missing manifest.json")
	}
	if len(names) != 1 {
		t.Fatalf(".idx has %d entries (want 1 = manifest only), entries: %v", len(names), names)
	}
}

func TestOpenShuffleSourceRejectsVersion2(t *testing.T) {
	baseDir := t.TempDir()

	writeMultiBucketSnapshot(t, baseDir, map[string][]snapshotRecord{
		"A/A.zip": {{Name: "a.mod", Size: 100, ArchiveOffset: 10, ArchiveEndOffset: 50, CompressedSize: 40}},
	})
	if err := BuildShuffleIndex(baseDir, nil); err != nil {
		t.Fatal(err)
	}

	idxDir, _ := ShuffleIndexDir(baseDir)
	idxPath := filepath.Join(idxDir, _shuffleIndexFile)

	gsa, err := archive.Open(idxPath, _idxMaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	var entries []archive.SourceEntry
	for _, e := range gsa.Entries() {
		data, err := gsa.Read(e.Name)
		if err != nil {
			t.Fatal(err)
		}
		if e.Name == "manifest.json" {
			var meta ShuffleIndexMeta
			if err := json.Unmarshal(data, &meta); err != nil {
				t.Fatal(err)
			}
			meta.Version = 2
			data, _ = json.Marshal(meta)
		}
		entries = append(entries, archive.SourceEntry{Name: e.Name, Data: data})
	}
	gsa.Close()
	if err := archive.Write(idxPath, entries); err != nil {
		t.Fatal(err)
	}

	src := OpenShuffleSource(baseDir)
	if src != nil {
		src.Close()
		t.Fatal("expected nil for version=2")
	}
}

func TestOpenShuffleSourceRejectsDirectoryCountMismatch(t *testing.T) {
	baseDir := t.TempDir()

	writeMultiBucketSnapshot(t, baseDir, map[string][]snapshotRecord{
		"A/A.zip": {{Name: "a.mod", Size: 100, ArchiveOffset: 10, ArchiveEndOffset: 50, CompressedSize: 40}},
	})
	if err := BuildShuffleIndex(baseDir, nil); err != nil {
		t.Fatal(err)
	}

	idxDir, _ := ShuffleIndexDir(baseDir)
	idxPath := filepath.Join(idxDir, _shuffleIndexFile)

	gsa, err := archive.Open(idxPath, _idxMaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	var entries []archive.SourceEntry
	for _, e := range gsa.Entries() {
		data, err := gsa.Read(e.Name)
		if err != nil {
			t.Fatal(err)
		}
		if e.Name == "manifest.json" {
			var meta ShuffleIndexMeta
			if err := json.Unmarshal(data, &meta); err != nil {
				t.Fatal(err)
			}
			meta.DirectoryCount = 999
			data, _ = json.Marshal(meta)
		}
		entries = append(entries, archive.SourceEntry{Name: e.Name, Data: data})
	}
	gsa.Close()
	if err := archive.Write(idxPath, entries); err != nil {
		t.Fatal(err)
	}

	src := OpenShuffleSource(baseDir)
	if src != nil {
		src.Close()
		t.Fatal("expected nil for directory count mismatch")
	}
}

func TestOpenShuffleSourceRejectsDuplicateLocator(t *testing.T) {
	baseDir := t.TempDir()

	writeMultiBucketSnapshot(t, baseDir, map[string][]snapshotRecord{
		"A/A.zip": {{Name: "a.mod", Size: 100, ArchiveOffset: 10, ArchiveEndOffset: 50, CompressedSize: 40}},
	})
	if err := BuildShuffleIndex(baseDir, nil); err != nil {
		t.Fatal(err)
	}

	idxDir, _ := ShuffleIndexDir(baseDir)
	idxPath := filepath.Join(idxDir, _shuffleIndexFile)

	gsa, err := archive.Open(idxPath, _idxMaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	var entries []archive.SourceEntry
	for _, e := range gsa.Entries() {
		data, err := gsa.Read(e.Name)
		if err != nil {
			t.Fatal(err)
		}
		if e.Name == "manifest.json" {
			var meta ShuffleIndexMeta
			if err := json.Unmarshal(data, &meta); err != nil {
				t.Fatal(err)
			}
			if len(meta.Entries) > 0 {
				dup := meta.Entries[0]
				dup.Name = "entries/dup.json"
				meta.Entries = append(meta.Entries, dup)
				meta.DirectoryCount++
			}
			data, _ = json.Marshal(meta)
		}
		entries = append(entries, archive.SourceEntry{Name: e.Name, Data: data})
	}
	gsa.Close()
	if err := archive.Write(idxPath, entries); err != nil {
		t.Fatal(err)
	}

	src := OpenShuffleSource(baseDir)
	if src != nil {
		src.Close()
		t.Fatal("expected nil for duplicate locator")
	}
}

func TestListingVersionContentDependent(t *testing.T) {
	locator := "http://example.com/dir/"
	tracks1 := []ShuffleTrackEntry{{Path: "a.mod", Key: "a"}}
	tracks2 := []ShuffleTrackEntry{{Path: "b.mod", Key: "b"}}
	v1 := listingVersion(locator, tracks1)
	v2 := listingVersion(locator, tracks2)
	if v1 == v2 {
		t.Fatalf("ListingVersion should change with different tracks: %s", v1)
	}
	v1b := listingVersion(locator, tracks1)
	if v1 != v1b {
		t.Fatalf("ListingVersion not deterministic: %s != %s", v1, v1b)
	}
}

func TestRecordCacheEviction(t *testing.T) {
	c := newRecordCache(3)
	for i := range 5 {
		key := "key" + string(rune('0'+i))
		c.put(key, &ShuffleDirRecord{Locator: key})
	}
	// Only the last 3 should remain.
	if len(c.entries) != 3 {
		t.Fatalf("cache size = %d, want 3", len(c.entries))
	}
	if _, ok := c.entries["key0"]; ok {
		t.Fatal("key0 should have been evicted")
	}
	if _, ok := c.entries["key1"]; ok {
		t.Fatal("key1 should have been evicted")
	}
	if _, ok := c.entries["key2"]; !ok {
		t.Fatal("key2 should still be present")
	}
}

func TestRecordCacheAccessOrder(t *testing.T) {
	c := newRecordCache(4)
	c.put("a", &ShuffleDirRecord{Locator: "a"})
	c.put("b", &ShuffleDirRecord{Locator: "b"})
	c.put("c", &ShuffleDirRecord{Locator: "c"})
	c.put("d", &ShuffleDirRecord{Locator: "d"})
	// Access "a" to make it recently used.
	c.get("a")
	// Add "e" — should evict "b" (oldest unaccessed), not "a".
	c.put("e", &ShuffleDirRecord{Locator: "e"})
	if _, ok := c.entries["a"]; !ok {
		t.Fatal("a should still be present (was recently accessed)")
	}
	if _, ok := c.entries["b"]; ok {
		t.Fatal("b should have been evicted (oldest unaccessed)")
	}
}

func TestRecordCacheNormalize(t *testing.T) {
	c := newRecordCache(2)
	// Set high counter.
	c.accessCounter = 1<<49 - 1
	c.put("a", &ShuffleDirRecord{Locator: "a"})
	c.put("b", &ShuffleDirRecord{Locator: "b"})
	// Access "a" to trigger normalization.
	c.get("a")
	if c.accessCounter >= 1<<48 {
		t.Fatalf("counter should have been normalized, got %d", c.accessCounter)
	}
}

func TestCachedDirTracksSkipsUnsupported(t *testing.T) {
	dirURL := "http://example.com/dir/"
	items := []DirItem{
		{Name: "track.mod", URL: dirURL + "track.mod", Kind: KindFile, CleanName: "track.mod"},
		{Name: "readme.txt", URL: dirURL + "readme.txt", Kind: KindFile, CleanName: "readme.txt"},
		{Name: "image.png", URL: dirURL + "image.png", Kind: KindFile, CleanName: "image.png"},
	}
	tracks := cachedDirTracks(dirURL, items)
	if len(tracks) != 1 {
		t.Fatalf("expected 1 supported track, got %d", len(tracks))
	}
	if tracks[0].Name != "track.mod" {
		t.Fatalf("unexpected track: %q", tracks[0].Name)
	}
}

func TestBuildSkipsUnsupportedCachedFiles(t *testing.T) {
	ResetMemCache()
	defer ResetMemCache()
	CloseSnapshotCatalog()
	defer CloseSnapshotCatalog()

	baseDir := t.TempDir()
	cat := &catalogData{
		Directories: map[string][]DirItem{
			"http://example.com/dir/": {
				{Name: "track.mod", URL: "http://example.com/dir/track.mod", Kind: KindFile, CleanName: "track.mod"},
				{Name: "readme.txt", URL: "http://example.com/dir/readme.txt", Kind: KindFile, CleanName: "readme.txt"},
				{Name: "cover.jpg", URL: "http://example.com/dir/cover.jpg", Kind: KindFile, CleanName: "cover.jpg"},
			},
		},
	}

	if err := BuildShuffleIndex(baseDir, cat); err != nil {
		t.Fatal(err)
	}

	src := OpenShuffleSource(baseDir)
	if src == nil {
		t.Fatal("OpenShuffleSource returned nil")
	}
	defer src.Close()

	// TrackCount should be 1 (only the .mod file), not 3.
	if src.TrackCount() != 1 {
		t.Fatalf("TrackCount = %d, want 1 (unsupported files excluded)", src.TrackCount())
	}
}

func TestOpenRejectsMissingCachedRecord(t *testing.T) {
	baseDir := t.TempDir()

	// Build valid index.
	writeMultiBucketSnapshot(t, baseDir, map[string][]snapshotRecord{
		"A/A.zip": {{Name: "a.mod", Size: 100, ArchiveOffset: 10, ArchiveEndOffset: 50, CompressedSize: 40}},
	})
	if err := BuildShuffleIndex(baseDir, nil); err != nil {
		t.Fatal(err)
	}

	// Manually add a cached entry to the manifest without its record.
	idxDir, _ := ShuffleIndexDir(baseDir)
	idxPath := filepath.Join(idxDir, _shuffleIndexFile)

	gsa, err := archive.Open(idxPath, _idxMaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	var entries []archive.SourceEntry
	for _, e := range gsa.Entries() {
		data, err := gsa.Read(e.Name)
		if err != nil {
			t.Fatal(err)
		}
		if e.Name == "manifest.json" {
			var meta ShuffleIndexMeta
			if err := json.Unmarshal(data, &meta); err != nil {
				t.Fatal(err)
			}
			// Add a fake cached entry (not snapshot/addendum).
			meta.Entries = append(meta.Entries, catalog.ManifestEntry{
				Name:       "entries/ff.json",
				Locator:    "http://example.com/cached/",
				TrackCount: 5,
			})
			meta.DirectoryCount++
			meta.TrackCount += 5
			data, _ = json.Marshal(meta)
		}
		entries = append(entries, archive.SourceEntry{Name: e.Name, Data: data})
	}
	gsa.Close()
	if err := archive.Write(idxPath, entries); err != nil {
		t.Fatal(err)
	}

	// OpenShuffleSource should reject: cached record not in GSA.
	src := OpenShuffleSource(baseDir)
	if src != nil {
		src.Close()
		t.Fatal("expected nil when cached record is missing from .idx")
	}
}

func TestGSASameSizeReplacementDetected(t *testing.T) {
	ResetMemCache()
	defer ResetMemCache()
	CloseSnapshotCatalog()
	defer CloseSnapshotCatalog()

	baseDir := t.TempDir()

	// Build a snapshot GSA.
	writeMultiBucketSnapshot(t, baseDir, map[string][]snapshotRecord{
		"A/A.zip": {{Name: "a.mod", Size: 100, ArchiveOffset: 10, ArchiveEndOffset: 50, CompressedSize: 40}},
	})

	fp1 := ShuffleIndexFingerprint(baseDir, nil)

	// Replace with different content of the same size.
	newRecords := []snapshotRecord{{Name: "b.xm", Size: 999, ArchiveOffset: 20, ArchiveEndOffset: 100, CompressedSize: 80}}
	data, _ := json.Marshal(newRecords)
	// Pad data to match original file size exactly.
	origInfo, _ := os.Stat(SnapshotCatalogPath(baseDir))
	for int64(len(data)) < origInfo.Size() {
		data = append(data, ' ')
	}
	data = data[:int(origInfo.Size())]
	if err := os.WriteFile(SnapshotCatalogPath(baseDir), data, 0o644); err != nil {
		t.Fatal(err)
	}

	fp2 := ShuffleIndexFingerprint(baseDir, nil)
	if fp1.SourceHash == fp2.SourceHash {
		t.Fatalf("fingerprint should change when GSA content changes at same size: %s", fp1.SourceHash)
	}
}
