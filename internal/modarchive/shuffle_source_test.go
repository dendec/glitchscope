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

func TestRecordCacheSkipsOversizedRecord(t *testing.T) {
	c := newRecordCache()
	// A single record bigger than the whole budget must never be retained.
	oversizedTracks := make([]ShuffleTrackEntry, 400_000)
	for i := range oversizedTracks {
		oversizedTracks[i] = ShuffleTrackEntry{Path: "p", Name: "n", Key: "k"}
	}
	record := &ShuffleDirRecord{Locator: "huge", Tracks: oversizedTracks}
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
	if _, ok := c.get("huge"); ok {
		t.Fatal("oversized record should not be retrievable from cache")
	}
}

func TestRecordCacheEvictionByMemory(t *testing.T) {
	c := newRecordCache()
	// Create records that are estimated > maxCachedBytes/2 each.
	largeTracks := make([]ShuffleTrackEntry, 100_000)
	for i := range largeTracks {
		largeTracks[i] = ShuffleTrackEntry{
			Path: "http://example.com/dir/track" + string(rune('A'+i%26)) + ".mod",
			Name: "track.mod",
			Key:  "key",
			Size: 1000,
		}
	}
	r1 := &ShuffleDirRecord{Locator: "dir1", Tracks: largeTracks}
	r2 := &ShuffleDirRecord{Locator: "dir2", Tracks: largeTracks}
	r3 := &ShuffleDirRecord{Locator: "dir3", Tracks: largeTracks}

	c.put("dir1", r1)
	c.put("dir2", r2)
	c.put("dir3", r3)

	// At most one large record should survive the memory budget.
	if len(c.entries) > 1 {
		t.Fatalf("expected ≤1 entries under memory budget, got %d (totalBytes=%d)",
			len(c.entries), c.totalBytes)
	}
	if c.totalBytes > maxCachedBytes {
		t.Fatalf("totalBytes %d exceeds budget %d", c.totalBytes, maxCachedBytes)
	}
}

func TestRecordCacheAccessOrder(t *testing.T) {
	// Create records large enough that 2 exceed half the budget.
	c := newRecordCache()
	// ~116.6K tracks ≈ 24.8 MiB per record: two fit just under the 50 MiB
	// budget (with ~419 KiB headroom), but adding "medium" on top of two
	// "big" pushes the total over it.
	hugeTracks := make([]ShuffleTrackEntry, 116_591)
	for i := range hugeTracks {
		hugeTracks[i] = ShuffleTrackEntry{Path: "p", Name: "n", Key: "k"}
	}
	mediumTracks := make([]ShuffleTrackEntry, 2000)
	for i := range mediumTracks {
		mediumTracks[i] = ShuffleTrackEntry{Path: "p", Name: "n", Key: "k"}
	}
	medium := &ShuffleDirRecord{Locator: "x", Tracks: mediumTracks}
	big := &ShuffleDirRecord{Locator: "x", Tracks: hugeTracks}

	c.put("a", big)
	c.put("b", big)
	// Access "a" to protect it from eviction.
	c.get("a")
	// "c" pushes totalBytes over budget → evict oldest ("b").
	c.put("c", medium)
	if _, ok := c.entries["a"]; !ok {
		t.Fatal("a should still be present (was recently accessed)")
	}
	if _, ok := c.entries["b"]; ok {
		t.Fatal("b should have been evicted (oldest unaccessed)")
	}
}

func TestRecordCacheNormalize(t *testing.T) {
	c := newRecordCache()
	c.accessCounter = 1<<49 - 1
	c.put("a", &ShuffleDirRecord{Locator: "a"})
	c.put("b", &ShuffleDirRecord{Locator: "b"})
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

	if src.TrackCount() != 1 {
		t.Fatalf("TrackCount = %d, want 1 (unsupported files excluded)", src.TrackCount())
	}
}

func TestOpenRejectsMissingCachedRecord(t *testing.T) {
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

	src := OpenShuffleSource(baseDir)
	if src != nil {
		src.Close()
		t.Fatal("expected nil when cached record is missing from .idx")
	}
}

func TestGSATailReplacementDetected(t *testing.T) {
	ResetMemCache()
	defer ResetMemCache()
	CloseSnapshotCatalog()
	defer CloseSnapshotCatalog()

	baseDir := t.TempDir()

	writeMultiBucketSnapshot(t, baseDir, map[string][]snapshotRecord{
		"A/A.zip": {{Name: "a.mod", Size: 100, ArchiveOffset: 10, ArchiveEndOffset: 50, CompressedSize: 40}},
	})

	fp1 := ShuffleIndexFingerprint(baseDir, nil)

	// Overwrite with data that differs only at the end.
	origData, err := os.ReadFile(SnapshotCatalogPath(baseDir))
	if err != nil {
		t.Fatal(err)
	}
	newData := make([]byte, len(origData))
	copy(newData, origData)
	// Flip a byte near the end.
	if len(newData) > 10 {
		newData[len(newData)-5] ^= 0xFF
	}
	if err := os.WriteFile(SnapshotCatalogPath(baseDir), newData, 0o644); err != nil {
		t.Fatal(err)
	}

	fp2 := ShuffleIndexFingerprint(baseDir, nil)
	if fp1.SourceHash == fp2.SourceHash {
		t.Fatalf("fingerprint should change when GSA tail is modified: %s", fp1.SourceHash)
	}
}

func TestRecordIdentityValidation(t *testing.T) {
	// Build valid index, then tamper the .idx record to have wrong Locator.
	baseDir := t.TempDir()

	writeMultiBucketSnapshot(t, baseDir, map[string][]snapshotRecord{
		"A/A.zip": {{Name: "a.mod", Size: 100, ArchiveOffset: 10, ArchiveEndOffset: 50, CompressedSize: 40}},
	})

	cat := &catalogData{
		Directories: map[string][]DirItem{
			"http://example.com/dir/": {
				{Name: "track.mod", URL: "http://example.com/dir/track.mod", Kind: KindFile, CleanName: "track.mod"},
			},
		},
	}
	if err := BuildShuffleIndex(baseDir, cat); err != nil {
		t.Fatal(err)
	}

	idxDir, _ := ShuffleIndexDir(baseDir)
	idxPath := filepath.Join(idxDir, _shuffleIndexFile)

	// Rewrite: change the record's Locator to something wrong.
	gsa, err := archive.Open(idxPath, _idxMaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	var srcEntries []archive.SourceEntry
	for _, e := range gsa.Entries() {
		data, err := gsa.Read(e.Name)
		if err != nil {
			t.Fatal(err)
		}
		if e.Name != "manifest.json" {
			var record ShuffleDirRecord
			if err := json.Unmarshal(data, &record); err == nil {
				record.Locator = "http://WRONG/"
				data, _ = json.Marshal(record)
			}
		}
		srcEntries = append(srcEntries, archive.SourceEntry{Name: e.Name, Data: data})
	}
	gsa.Close()
	if err := archive.Write(idxPath, srcEntries); err != nil {
		t.Fatal(err)
	}

	src := OpenShuffleSource(baseDir)
	if src == nil {
		t.Fatal("OpenShuffleSource returned nil (name check should pass)")
	}
	defer src.Close()

	rng := rand.New(rand.NewSource(1))
	_, err = src.RandomTrack(rng)
	if err == nil {
		t.Fatal("expected identity validation error from loadRecord")
	}
}

func TestEstimateRecordBytes(t *testing.T) {
	r := &ShuffleDirRecord{
		Locator:     "http://modarchive.textfiles.com/data/www/mods/A/A0.zip",
		DisplayName: "A0",
		Tracks: []ShuffleTrackEntry{
			{Path: "http://modarchive.textfiles.com/data/www/mods/A/A0.zip:a.mod", Name: "a.mod", Key: "a.mod"},
			{Path: "http://modarchive.textfiles.com/data/www/mods/A/A0.zip:b.xm", Name: "b.xm", Key: "b.xm"},
		},
	}
	estimated := estimateRecordBytes(r)
	if estimated < 500 {
		t.Fatalf("estimate too low: %d", estimated)
	}
	if estimated > 100_000 {
		t.Fatalf("estimate too high: %d", estimated)
	}
}
