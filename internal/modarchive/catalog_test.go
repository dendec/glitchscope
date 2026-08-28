package modarchive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dendec/glitchscope/internal/archive"
)

func TestCatalog_SaveLoadInit(t *testing.T) {
	ResetMemCache()
	defer ResetMemCache()

	tmpDir, err := os.MkdirTemp("", "modarchive_catalog_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cat := &Catalog{
		Directories: map[string][]DirItem{
			BaseURL: {
				{Name: "modarchive_2023_additions", URL: BaseURL + "modarchive_2023_additions/", Kind: KindDir, CleanName: "2023 Additions"},
			},
			BaseURL + "modarchive_2023_additions/": {
				{Name: "MOD", URL: BaseURL + "modarchive_2023_additions/MOD/", Kind: KindDir, CleanName: "MOD"},
			},
		},
		UpdatedAt: time.Now(),
	}

	// Save catalog
	if err := SaveCatalog(tmpDir, cat); err != nil {
		t.Fatalf("SaveCatalog failed: %v", err)
	}

	// Load catalog
	loadedCat := LoadCatalog(tmpDir)
	if loadedCat == nil {
		t.Fatalf("LoadCatalog returned nil")
	}

	if len(loadedCat.Directories) != 2 {
		t.Fatalf("expected 2 directories in catalog, got %d", len(loadedCat.Directories))
	}

	// Init catalog to populate memory cache
	InitCatalog(tmpDir)

	// Verify memCache contains preloaded directories
	items, ok := FetchDirectoryCached(tmpDir, BaseURL)
	if !ok || len(items) != 2 {
		t.Fatalf("expected snapshot plus preloaded root item, got ok=%v, count=%d", ok, len(items))
	}
	if items[0].CleanName != SnapshotLabel || items[1].CleanName != "2023" {
		t.Errorf("unexpected root items: %+v", items)
	}
}

func TestCatalog_InitSkipsEmptyDirectories(t *testing.T) {
	ResetMemCache()
	defer ResetMemCache()

	tmpDir, err := os.MkdirTemp("", "modarchive_catalog_empty_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cat := &Catalog{
		Directories: map[string][]DirItem{
			BaseURL + "modarchive_2023_additions/HVL/S/": {},
		},
		UpdatedAt: time.Now(),
	}
	if err := SaveCatalog(tmpDir, cat); err != nil {
		t.Fatalf("SaveCatalog failed: %v", err)
	}

	InitCatalog(tmpDir)
	if _, ok := FetchDirectoryCached(tmpDir, BaseURL+"modarchive_2023_additions/HVL/S/"); ok {
		t.Fatal("empty catalog directory should be refreshed instead of served from memory")
	}
}

func TestSnapshotCatalogBuildsNavigationAndLoadsBucket(t *testing.T) {
	ResetMemCache()
	CloseSnapshotCatalog()
	defer func() {
		CloseSnapshotCatalog()
		ResetMemCache()
	}()

	baseDir := t.TempDir()
	records := []snapshotRecord{{
		Name:             "alpha.mod",
		Size:             123,
		ArchiveOffset:    10,
		ArchiveEndOffset: 99,
		CompressedSize:   42,
		CRC32:            1234,
		Compression:      8,
	}}
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(SnapshotCatalogPath(baseDir)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := archive.Write(SnapshotCatalogPath(baseDir), []archive.SourceEntry{{Name: "A/A0.zip", Data: data}}); err != nil {
		t.Fatal(err)
	}

	if !InitSnapshotCatalog(baseDir) {
		t.Fatal("InitSnapshotCatalog returned false")
	}
	snapshotURL := BaseURL + SnapshotDir + "/"
	letters, ok := FetchDirectoryCached(baseDir, snapshotURL)
	if !ok || len(letters) != 1 || letters[0].CleanName != "A" {
		t.Fatalf("snapshot letters = %+v, ok=%v", letters, ok)
	}
	buckets, ok := FetchDirectoryCached(baseDir, snapshotURL+"A/")
	if !ok || len(buckets) != 1 || buckets[0].CleanName != "A0" {
		t.Fatalf("snapshot buckets = %+v, ok=%v", buckets, ok)
	}
	items, ok := FetchDirectoryCached(baseDir, buckets[0].URL)
	if !ok || len(items) != 1 || items[0].Name != "alpha.mod" || items[0].ArchiveOffset != 10 {
		t.Fatalf("snapshot items = %+v, ok=%v", items, ok)
	}
}

func TestSnapshotCatalogSaveAndInitWithoutMainCatalog(t *testing.T) {
	ResetMemCache()
	CloseSnapshotCatalog()
	defer func() {
		CloseSnapshotCatalog()
		ResetMemCache()
	}()

	baseDir := t.TempDir()
	archiveURL := BaseURL + SnapshotDir + "/Z/ZZ.zip"
	if err := SaveSnapshotCatalog(baseDir, map[string][]DirItem{
		archiveURL: {{
			Name:             "zeta.xm",
			Kind:             KindFile,
			Size:             456,
			ArchiveOffset:    100,
			ArchiveEndOffset: 200,
			CompressedSize:   50,
			CRC32:            5678,
			Compression:      8,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	if !InitCatalog(baseDir) {
		t.Fatal("InitCatalog returned false with snapshot catalog")
	}
	root, ok := FetchDirectoryCached(baseDir, BaseURL)
	if !ok || len(root) != 1 || root[0].CleanName != SnapshotLabel {
		t.Fatalf("root items = %+v, ok=%v", root, ok)
	}
	items, ok := FetchDirectoryCached(baseDir, archiveURL)
	if !ok || len(items) != 1 || items[0].URL != archiveURL+"#zeta.xm" {
		t.Fatalf("snapshot items = %+v, ok=%v", items, ok)
	}

	CloseSnapshotCatalog()
	if _, ok := FetchDirectoryCached(baseDir, BaseURL); ok {
		t.Fatal("snapshot-only root survived close")
	}
}

func TestSaveSnapshotCatalogRejectsInvalidTrackMetadata(t *testing.T) {
	archiveURL := BaseURL + SnapshotDir + "/A/A0.zip"
	err := SaveSnapshotCatalog(t.TempDir(), map[string][]DirItem{
		archiveURL: {{
			Name:             "invalid.mod",
			Kind:             KindFile,
			Size:             1,
			ArchiveOffset:    100,
			ArchiveEndOffset: 50,
		}},
	})
	if err == nil {
		t.Fatal("SaveSnapshotCatalog accepted invalid track metadata")
	}
}

func TestSnapshotBucketCacheEvictsLeastRecentlyUsed(t *testing.T) {
	cache := newSnapshotBucketCache()
	for i := 0; i < snapshotCacheSize; i++ {
		cache.add(fmt.Sprintf("bucket-%d", i), []DirItem{{Name: fmt.Sprint(i)}})
	}

	if _, ok := cache.get("bucket-0"); !ok {
		t.Fatal("oldest bucket was not cached")
	}
	cache.add("bucket-new", []DirItem{{Name: "new"}})

	if _, ok := cache.get("bucket-1"); ok {
		t.Fatal("least recently used bucket was not evicted")
	}
	if _, ok := cache.get("bucket-0"); !ok {
		t.Fatal("recently used bucket was evicted")
	}

	cache.clear()
	if _, ok := cache.get("bucket-0"); ok || cache.order.Len() != 0 {
		t.Fatal("cleared bucket cache still contains entries")
	}
}

func TestSnapshotCatalogReinitRemovesStaleNavigation(t *testing.T) {
	ResetMemCache()
	CloseSnapshotCatalog()
	defer func() {
		CloseSnapshotCatalog()
		ResetMemCache()
	}()

	firstDir := t.TempDir()
	secondDir := t.TempDir()
	writeTestSnapshotCatalog(t, firstDir, "A/A0.zip", "alpha.mod")
	writeTestSnapshotCatalog(t, secondDir, "B/B0.zip", "beta.mod")

	if !InitSnapshotCatalog(firstDir) {
		t.Fatal("first InitSnapshotCatalog returned false")
	}
	staleURL := BaseURL + SnapshotDir + "/A/"
	if _, ok := FetchDirectoryCached(firstDir, staleURL); !ok {
		t.Fatal("first snapshot navigation was not cached")
	}

	if !InitSnapshotCatalog(secondDir) {
		t.Fatal("second InitSnapshotCatalog returned false")
	}
	if _, ok := FetchDirectoryCached(secondDir, staleURL); ok {
		t.Fatal("stale snapshot navigation survived reinitialization")
	}
	currentURL := BaseURL + SnapshotDir + "/B/"
	if _, ok := FetchDirectoryCached(secondDir, currentURL); !ok {
		t.Fatal("current snapshot navigation was not cached")
	}

	CloseSnapshotCatalog()
	if _, ok := FetchDirectoryCached(secondDir, currentURL); ok {
		t.Fatal("snapshot navigation survived close")
	}
}

func TestResetMemCacheClearsSnapshotLRU(t *testing.T) {
	snapshotMu.Lock()
	snapshotBucketMem.add("bucket", []DirItem{{Name: "track.mod"}})
	snapshotMu.Unlock()

	ResetMemCache()

	snapshotMu.Lock()
	defer snapshotMu.Unlock()
	if snapshotBucketMem.order.Len() != 0 {
		t.Fatal("ResetMemCache did not clear snapshot LRU")
	}
}

func TestSnapshotCatalogEntryNameRequiresOfficialOrigin(t *testing.T) {
	official := BaseURL + SnapshotDir + "/A/A0.zip"
	if _, ok := snapshotCatalogEntryName(official); !ok {
		t.Fatal("official snapshot URL was rejected")
	}
	if _, ok := snapshotCatalogEntryName("https://example.test/" + SnapshotDir + "/A/A0.zip"); ok {
		t.Fatal("foreign snapshot origin was accepted")
	}
	if _, ok := snapshotCatalogEntryName(official + "?download=1"); ok {
		t.Fatal("snapshot URL with query was accepted")
	}
}

func TestSnapshotCatalogEmptyBucketRemainsCacheMiss(t *testing.T) {
	ResetMemCache()
	CloseSnapshotCatalog()
	defer func() {
		CloseSnapshotCatalog()
		ResetMemCache()
	}()

	baseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(SnapshotCatalogPath(baseDir)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := archive.Write(SnapshotCatalogPath(baseDir), []archive.SourceEntry{{Name: "A/A0.zip", Data: []byte("[]")}}); err != nil {
		t.Fatal(err)
	}
	if !InitSnapshotCatalog(baseDir) {
		t.Fatal("InitSnapshotCatalog returned false")
	}

	archiveURL := BaseURL + SnapshotDir + "/A/A0.zip"
	for range 2 {
		if _, ok := fetchSnapshotCatalogDirectory(archiveURL); ok {
			t.Fatal("empty snapshot bucket was cached as a hit")
		}
	}
}

func writeTestSnapshotCatalog(t *testing.T, baseDir, entryName, trackName string) {
	t.Helper()
	records := []snapshotRecord{{
		Name:             trackName,
		Size:             1,
		ArchiveOffset:    1,
		ArchiveEndOffset: 3,
		CompressedSize:   1,
	}}
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(SnapshotCatalogPath(baseDir)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := archive.Write(SnapshotCatalogPath(baseDir), []archive.SourceEntry{{Name: entryName, Data: data}}); err != nil {
		t.Fatal(err)
	}
}
