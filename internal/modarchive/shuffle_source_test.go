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
		Locator: entries[0].Name,
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
	// The returned DirectoryKey uses the canonical URL, not the GSA entry name.
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
		Locator: entries[0].Name,
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

	// Write a corrupted snapshot GSA (bad magic).
	gsaDir := filepath.Dir(SnapshotCatalogPath(baseDir))
	if err := os.MkdirAll(gsaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(SnapshotCatalogPath(baseDir), []byte("not a gsa file"), 0o644); err != nil {
		t.Fatal(err)
	}

	// buildSnapshotRecords must return an error, not nil.
	records, err := buildSnapshotRecords(baseDir)
	if err == nil {
		t.Fatalf("expected error for corrupted snapshot, got %d records", len(records))
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

	records, err := buildAddendumRecords(baseDir)
	if err == nil {
		t.Fatalf("expected error for corrupted addendum, got %d records", len(records))
	}
}

func TestOpenShuffleSourceRejectsCorruptedManifest(t *testing.T) {
	baseDir := t.TempDir()

	// Build a valid index first.
	writeMultiBucketSnapshot(t, baseDir, map[string][]snapshotRecord{
		"A/A.zip": {{Name: "a.mod", Size: 100, ArchiveOffset: 10, ArchiveEndOffset: 50, CompressedSize: 40}},
	})
	if err := BuildShuffleIndex(baseDir, nil); err != nil {
		t.Fatal(err)
	}

	// Overwrite manifest.json with invalid schema.
	idxDir, _ := ShuffleIndexDir(baseDir)
	idxPath := filepath.Join(idxDir, _shuffleIndexFile)

	// Read the existing index, replace manifest, rewrite.
	gsa, err := archive.Open(idxPath, _shuffleMaxBuckets)
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

	// OpenShuffleSource should reject it.
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

	// Overwrite manifest.json with version=0.
	idxDir, _ := ShuffleIndexDir(baseDir)
	idxPath := filepath.Join(idxDir, _shuffleIndexFile)

	gsa, err := archive.Open(idxPath, _shuffleMaxBuckets)
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
				{Name: "a.mod", URL: "http://example.com/dir/a.mod", Kind: KindFile, Size: 200, CleanName: "a.mod"}, // size changed
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

	// Create a corrupted index file.
	idxDir, _ := ShuffleIndexDir(baseDir)
	idxPath := filepath.Join(idxDir, _shuffleIndexFile)
	if err := os.WriteFile(idxPath, []byte("not a valid gsa"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !ShuffleSourceNeedsRebuild(baseDir, nil) {
		t.Fatal("expected NeedsRebuild=true for corrupted index")
	}
}

