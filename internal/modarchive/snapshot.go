package modarchive

import (
	"container/list"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/dendec/glitchscope/internal/archive"
)

const (
	snapshotCatalogFile = "1980-2007.gsa"
	addendumCatalogFile = "2007-addendum.gsa"
	snapshotMaxBuckets  = 2048
	snapshotCacheSize   = 8
)

type snapshotRecord struct {
	Name             string `json:"name"`
	Size             int64  `json:"size"`
	ArchiveOffset    int64  `json:"archive_offset"`
	ArchiveEndOffset int64  `json:"archive_end_offset"`
	CompressedSize   uint64 `json:"compressed_size"`
	CRC32            uint32 `json:"crc32"`
	Compression      uint16 `json:"compression"`
}

type snapshotCacheEntry struct {
	url   string
	items []DirItem
}

type snapshotBucketCache struct {
	items map[string]*list.Element
	order *list.List
}

func newSnapshotBucketCache() snapshotBucketCache {
	return snapshotBucketCache{
		items: make(map[string]*list.Element),
		order: list.New(),
	}
}

func (c *snapshotBucketCache) clear() {
	c.items = make(map[string]*list.Element)
	c.order.Init()
}

func (c *snapshotBucketCache) get(targetURL string) ([]DirItem, bool) {
	element, ok := c.items[targetURL]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(element)
	return element.Value.(snapshotCacheEntry).items, true
}

func (c *snapshotBucketCache) add(targetURL string, items []DirItem) {
	if element, ok := c.items[targetURL]; ok {
		element.Value = snapshotCacheEntry{url: targetURL, items: items}
		c.order.MoveToFront(element)
		return
	}

	element := c.order.PushFront(snapshotCacheEntry{url: targetURL, items: items})
	c.items[targetURL] = element
	if c.order.Len() <= snapshotCacheSize {
		return
	}

	oldest := c.order.Back()
	delete(c.items, oldest.Value.(snapshotCacheEntry).url)
	c.order.Remove(oldest)
}

func snapshotRecordFromItem(item DirItem) snapshotRecord {
	return snapshotRecord{
		Name:             item.Name,
		Size:             item.Size,
		ArchiveOffset:    item.ArchiveOffset,
		ArchiveEndOffset: item.ArchiveEndOffset,
		CompressedSize:   item.CompressedSize,
		CRC32:            item.CRC32,
		Compression:      item.Compression,
	}
}

func (r snapshotRecord) dirItem(archiveURL string) (DirItem, bool) {
	cleanName, supported := supportedArchiveEntry(r.Name)
	if !supported {
		return DirItem{}, false
	}
	entryURL, err := archiveEntryURL(archiveURL, r.Name)
	if err != nil {
		return DirItem{}, false
	}
	item := DirItem{
		Name:             r.Name,
		URL:              entryURL,
		Kind:             KindFile,
		Size:             r.Size,
		CleanName:        cleanName,
		ArchiveOffset:    r.ArchiveOffset,
		ArchiveEndOffset: r.ArchiveEndOffset,
		CompressedSize:   r.CompressedSize,
		CRC32:            r.CRC32,
		Compression:      r.Compression,
	}
	return item, validArchiveItem(item)
}

var (
	snapshotMu             sync.Mutex
	snapshotCatalog        *archive.Archive
	addendumCatalog        *archive.Archive
	snapshotBucketMem      = newSnapshotBucketCache()
	snapshotNavigationKeys = make(map[string]struct{})
)

// SnapshotCatalogPath returns the bundled snapshot index path.
func SnapshotCatalogPath(baseDir string) string {
	cacheDir, _ := CacheDir(baseDir)
	return filepath.Join(cacheDir, snapshotCatalogFile)
}

// AddendumCatalogPath returns the bundled 2007 addendum index path.
func AddendumCatalogPath(baseDir string) string {
	cacheDir, _ := CacheDir(baseDir)
	return filepath.Join(cacheDir, addendumCatalogFile)
}

// SaveSnapshotCatalog writes bucket indexes as independently compressed GSA entries.
func SaveSnapshotCatalog(baseDir string, buckets map[string][]DirItem) error {
	return saveOfflineCatalog(SnapshotCatalogPath(baseDir), buckets, snapshotCatalogEntryName)
}

// SaveAddendumCatalog writes 2007 addendum bucket indexes as independently compressed GSA entries.
func SaveAddendumCatalog(baseDir string, buckets map[string][]DirItem) error {
	return saveOfflineCatalog(AddendumCatalogPath(baseDir), buckets, addendumCatalogEntryName)
}

func saveOfflineCatalog(targetPath string, buckets map[string][]DirItem, entryNameForURL func(string) (string, bool)) error {
	entries := make([]archive.SourceEntry, 0, len(buckets))
	for archiveURL, items := range buckets {
		entryName, ok := entryNameForURL(archiveURL)
		if !ok {
			return fmt.Errorf("invalid snapshot archive URL %q", archiveURL)
		}
		records := make([]snapshotRecord, 0, len(items))
		for _, item := range items {
			if item.Kind != KindFile {
				continue
			}
			if !validArchiveItem(item) {
				return fmt.Errorf("snapshot archive %q has invalid track metadata for %q", archiveURL, item.Name)
			}
			records = append(records, snapshotRecordFromItem(item))
		}
		if len(records) == 0 {
			return fmt.Errorf("snapshot archive %q has no supported tracks", archiveURL)
		}
		data, err := json.Marshal(records)
		if err != nil {
			return fmt.Errorf("encode snapshot archive %q: %w", archiveURL, err)
		}
		entries = append(entries, archive.SourceEntry{Name: entryName, Data: data})
	}
	if len(entries) == 0 {
		return fmt.Errorf("snapshot catalog has no buckets")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("snapshot catalog mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(targetPath), "snapshot*.tmp")
	if err != nil {
		return fmt.Errorf("snapshot catalog temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("snapshot catalog close temp file: %w", err)
	}
	defer os.Remove(tmpPath)
	if err := archive.Write(tmpPath, entries); err != nil {
		return fmt.Errorf("snapshot catalog write: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return fmt.Errorf("snapshot catalog chmod: %w", err)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("snapshot catalog rename: %w", err)
	}
	return nil
}

// InitSnapshotCatalog opens the bundled snapshot index and builds its folder navigation.
func InitSnapshotCatalog(baseDir string) bool {
	snapshotMu.Lock()
	defer snapshotMu.Unlock()
	closeSnapshotCatalogLocked()

	hasSnapshot := initMainSnapshotCatalogLocked(baseDir)
	hasAddendum := initAddendumCatalogLocked(baseDir)
	return hasSnapshot || hasAddendum
}

func initMainSnapshotCatalogLocked(baseDir string) bool {
	catalog, err := archive.Open(SnapshotCatalogPath(baseDir), snapshotMaxBuckets)
	if err != nil {
		slog.Debug("modarchive: snapshot catalog unavailable", "error", err)
		return false
	}

	letters := make(map[string][]DirItem)
	for _, entry := range catalog.Entries() {
		parts := strings.Split(entry.Name, "/")
		if len(parts) != 2 || parts[0] == "" || !strings.EqualFold(path.Ext(parts[1]), ".zip") {
			_ = catalog.Close()
			slog.Warn("modarchive: invalid snapshot catalog entry", "entry", entry.Name)
			return false
		}
		archiveURL := BaseURL + SnapshotDir + "/" + entry.Name
		letters[parts[0]] = append(letters[parts[0]], DirItem{
			Name:      parts[1],
			URL:       archiveURL,
			Kind:      KindArchive,
			CleanName: strings.TrimSuffix(parts[1], path.Ext(parts[1])),
		})
	}

	letterNames := make([]string, 0, len(letters))
	for letter := range letters {
		letterNames = append(letterNames, letter)
		sort.Slice(letters[letter], func(i, j int) bool {
			return strings.ToLower(letters[letter][i].CleanName) < strings.ToLower(letters[letter][j].CleanName)
		})
	}
	sort.Strings(letterNames)

	snapshotURL := BaseURL + SnapshotDir + "/"
	rootItems := make([]DirItem, 0, len(letterNames))
	memCacheMu.Lock()
	for _, letter := range letterNames {
		letterURL := snapshotURL + letter + "/"
		rootItems = append(rootItems, DirItem{Name: letter, URL: letterURL, Kind: KindDir, CleanName: letter})
		memCache[letterURL] = letters[letter]
		snapshotNavigationKeys[letterURL] = struct{}{}
	}
	memCache[snapshotURL] = rootItems
	snapshotNavigationKeys[snapshotURL] = struct{}{}
	memCacheMu.Unlock()

	snapshotCatalog = catalog
	slog.Info("modarchive: snapshot catalog opened", "buckets", len(catalog.Entries()))
	return true
}

func initAddendumCatalogLocked(baseDir string) bool {
	catalog, err := archive.Open(AddendumCatalogPath(baseDir), snapshotMaxBuckets)
	if err != nil {
		slog.Debug("modarchive: addendum catalog unavailable", "error", err)
		return false
	}

	rootItems := make([]DirItem, 0, len(catalog.Entries()))
	for _, entry := range catalog.Entries() {
		if entry.Name == "" || strings.Contains(entry.Name, "/") || !strings.EqualFold(path.Ext(entry.Name), ".zip") {
			_ = catalog.Close()
			slog.Warn("modarchive: invalid addendum catalog entry", "entry", entry.Name)
			return false
		}
		rootItems = append(rootItems, DirItem{
			Name:      entry.Name,
			URL:       BaseURL + AddendumDir + "/" + entry.Name,
			Kind:      KindArchive,
			CleanName: strings.TrimSuffix(entry.Name, path.Ext(entry.Name)),
		})
	}
	sort.Slice(rootItems, func(i, j int) bool {
		return strings.ToLower(rootItems[i].CleanName) < strings.ToLower(rootItems[j].CleanName)
	})

	addendumURL := BaseURL + AddendumDir + "/"
	memCacheMu.Lock()
	memCache[addendumURL] = rootItems
	snapshotNavigationKeys[addendumURL] = struct{}{}
	memCacheMu.Unlock()

	addendumCatalog = catalog
	slog.Info("modarchive: addendum catalog opened", "buckets", len(catalog.Entries()))
	return true
}

// CloseSnapshotCatalog closes the bundled snapshot index.
func CloseSnapshotCatalog() {
	snapshotMu.Lock()
	defer snapshotMu.Unlock()
	closeSnapshotCatalogLocked()
}

func cacheOfflineOnlyRoot() {
	snapshotMu.Lock()
	defer snapshotMu.Unlock()
	if snapshotCatalog == nil && addendumCatalog == nil {
		return
	}
	memCacheMu.Lock()
	memCache[BaseURL] = []DirItem{snapshotRootItem(), addendumRootItem()}
	memCacheMu.Unlock()
	snapshotNavigationKeys[BaseURL] = struct{}{}
}

func closeSnapshotCatalogLocked() {
	if snapshotCatalog != nil {
		_ = snapshotCatalog.Close()
		snapshotCatalog = nil
	}
	if addendumCatalog != nil {
		_ = addendumCatalog.Close()
		addendumCatalog = nil
	}
	snapshotBucketMem.clear()
	memCacheMu.Lock()
	for key := range snapshotNavigationKeys {
		delete(memCache, key)
	}
	memCacheMu.Unlock()
	snapshotNavigationKeys = make(map[string]struct{})
}

func fetchSnapshotCatalogDirectory(targetURL string) ([]DirItem, bool) {
	entryName, catalogKind, ok := offlineCatalogEntry(targetURL)
	if !ok {
		return nil, false
	}

	snapshotMu.Lock()
	defer snapshotMu.Unlock()
	catalog := snapshotCatalog
	if catalogKind == AddendumDir {
		catalog = addendumCatalog
	}
	if catalog == nil {
		return nil, false
	}
	if items, ok := snapshotBucketMem.get(targetURL); ok {
		return items, true
	}
	data, err := catalog.Read(entryName)
	if err != nil {
		slog.Warn("modarchive: snapshot bucket read failed", "url", targetURL, "error", err)
		return nil, false
	}

	var records []snapshotRecord
	if err := json.Unmarshal(data, &records); err != nil {
		slog.Warn("modarchive: snapshot bucket decode failed", "url", targetURL, "error", err)
		return nil, false
	}
	items := make([]DirItem, 0, len(records))
	for _, record := range records {
		item, ok := record.dirItem(targetURL)
		if !ok {
			slog.Warn("modarchive: invalid snapshot track metadata", "url", targetURL, "entry", record.Name)
			return nil, false
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, false
	}
	snapshotBucketMem.add(targetURL, items)
	return items, true
}

func snapshotCatalogEntryName(targetURL string) (string, bool) {
	return officialCatalogEntryName(targetURL, SnapshotDir, 1)
}

func addendumCatalogEntryName(targetURL string) (string, bool) {
	return officialCatalogEntryName(targetURL, AddendumDir, 0)
}

func offlineCatalogEntry(targetURL string) (string, string, bool) {
	if entryName, ok := snapshotCatalogEntryName(targetURL); ok {
		return entryName, SnapshotDir, true
	}
	if entryName, ok := addendumCatalogEntryName(targetURL); ok {
		return entryName, AddendumDir, true
	}
	return "", "", false
}

func officialCatalogEntryName(targetURL, sourceDir string, slashCount int) (string, bool) {
	parsed, err := url.Parse(targetURL)
	base, baseErr := url.Parse(BaseURL)
	if err != nil || baseErr != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		!strings.EqualFold(parsed.Scheme, base.Scheme) || !strings.EqualFold(parsed.Host, base.Host) ||
		!strings.EqualFold(path.Ext(parsed.Path), ".zip") {
		return "", false
	}
	prefix := "/" + sourceDir + "/"
	entryName := strings.TrimPrefix(parsed.Path, prefix)
	if entryName == parsed.Path || strings.Count(entryName, "/") != slashCount {
		return "", false
	}
	return entryName, true
}
