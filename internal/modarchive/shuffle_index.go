package modarchive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dendec/glitchscope/internal/archive"
	"github.com/dendec/glitchscope/internal/catalog"
	"github.com/dendec/glitchscope/internal/player"
)

const (
	_shuffleIndexFile  = "modarchive.idx"
	_shuffleMaxBuckets uint32 = 4096
	_shuffleSchema            = "modarchive-shuffle-v1"
)

// ShuffleDirRecord is one directory/bucket entry inside modarchive.idx.
// For snapshot/addendum sources the full record is rebuilt from the
// source GSA on demand; for cached directories it is stored in the .idx
// so that no external data is needed at read time.
type ShuffleDirRecord struct {
	Locator     string                 `json:"locator"` // canonical URL of the directory/bucket
	DisplayName string                 `json:"display_name"`
	TrackCount  uint64                 `json:"track_count"`
	Version     catalog.ListingVersion `json:"version"`
	Tracks      []ShuffleTrackEntry    `json:"tracks"`
}

// ShuffleTrackEntry is a single track inside a directory record.
type ShuffleTrackEntry struct {
	Path             string `json:"path"` // modarchive: URL for playback
	Name             string `json:"name"` // display name
	Key              string `json:"key"`  // stable identity (entry name or URL)
	Size             int64  `json:"size"`
	ArchiveOffset    int64  `json:"archive_offset,omitempty"`
	ArchiveEndOffset int64  `json:"archive_end_offset,omitempty"`
	CompressedSize   uint64 `json:"compressed_size,omitempty"`
	CRC32            uint32 `json:"crc32,omitempty"`
	Compression      uint16 `json:"compression,omitempty"`
}

// ShuffleIndexMeta is the manifest stored as manifest.json inside modarchive.idx.
type ShuffleIndexMeta struct {
	Schema         string                  `json:"schema"`
	Version        int                     `json:"version"`
	Source         catalog.SourceKind      `json:"source"`
	Fingerprint    catalog.Fingerprint     `json:"fingerprint"`
	TrackCount     uint64                  `json:"track_count"`
	DirectoryCount uint64                  `json:"directory_count"`
	Entries        []catalog.ManifestEntry `json:"entries"`
	CreatedAt      time.Time               `json:"created_at"`
}

// dirContentHash computes a stable hash of all meaningful DirItem fields
// for a cached directory listing. It detects changes that a simple
// URL+count fingerprint would miss.
func dirContentHash(dirURL string, items []DirItem) string {
	h := sha256.New()
	fmt.Fprintf(h, "dir:%s\n", dirURL)
	// Sort by name for determinism.
	sorted := make([]DirItem, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})
	for _, item := range sorted {
		fmt.Fprintf(h, "item:%s:%s:%d:%d:%d:%d:%d:%d:%d\n",
			item.Name, item.URL, int64(item.Kind), item.Size,
			item.ArchiveOffset, item.ArchiveEndOffset,
			item.CompressedSize, item.CRC32, item.Compression)
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// ShuffleIndexFingerprint computes a fingerprint from the snapshot GSA files
// and the catalog data. It detects changes that require an index rebuild.
// For snapshot/addendum: file size + mtime (external data, content hash
// would be expensive to recompute).
// For cached directories: full content hash of all DirItem fields.
func ShuffleIndexFingerprint(baseDir string, cat *catalogData) catalog.Fingerprint {
	h := sha256.New()

	// GSA file sizes and mod times.
	for _, p := range []string{
		SnapshotCatalogPath(baseDir),
		AddendumCatalogPath(baseDir),
	} {
		info, err := os.Stat(p)
		if err == nil {
			fmt.Fprintf(h, "%s:%d:%d\n", p, info.Size(), info.ModTime().UnixMilli())
		}
	}

	// Cached directory content hashes (sorted for determinism).
	if cat != nil {
		urls := make([]string, 0, len(cat.Directories))
		for u := range cat.Directories {
			urls = append(urls, u)
		}
		sort.Strings(urls)
		for _, u := range urls {
			items := cat.Directories[u]
			// Include full content hash of all DirItem fields.
			contentHash := dirContentHash(u, items)
			fmt.Fprintf(h, "dir:%s:%s\n", u, contentHash)
		}
	}

	return catalog.Fingerprint{
		SourceHash: hex.EncodeToString(h.Sum(nil)[:16]),
	}
}

// ShuffleIndexDir returns the directory where modarchive.idx is stored.
func ShuffleIndexDir(baseDir string) (string, error) {
	return catalog.ShuffleIndexDir(baseDir, "modarchive")
}

// BuildShuffleIndex builds modarchive.idx from snapshot GSA data and
// cached directory listings. It reads every snapshot bucket and every
// cached directory to produce complete directory records.
//
// For snapshot/addendum sources the full record (with tracks) is
// written into the .idx GSA so that the source does not need to be
// kept open at read time. The snapshot/addendum source GSAs remain
// the canonical track metadata; the .idx mirrors are rebuilt on
// fingerprint change.
func BuildShuffleIndex(baseDir string, cat *catalogData) error {
	dir, err := ShuffleIndexDir(baseDir)
	if err != nil {
		return err
	}

	fp := ShuffleIndexFingerprint(baseDir, cat)
	var records []ShuffleDirRecord

	// 1. Snapshot buckets from GSA.
	snapshotRecords, err := buildSnapshotRecords(baseDir)
	if err != nil {
		return fmt.Errorf("snapshot records: %w", err)
	}
	records = append(records, snapshotRecords...)

	// 2. Addendum buckets from GSA.
	addendumRecords, err := buildAddendumRecords(baseDir)
	if err != nil {
		return fmt.Errorf("addendum records: %w", err)
	}
	records = append(records, addendumRecords...)

	// 3. Cached directory listings (non-snapshot).
	cachedRecords := buildCachedRecords(cat)
	records = append(records, cachedRecords...)

	// Sort by locator for deterministic output.
	sort.Slice(records, func(i, j int) bool {
		return records[i].Locator < records[j].Locator
	})

	// Build manifest with compact entries including locator.
	var totalTracks uint64
	entries := make([]catalog.ManifestEntry, len(records))
	for i, r := range records {
		totalTracks += r.TrackCount
		entries[i] = catalog.ManifestEntry{
			Name:       recordEntryName(r.Locator),
			Locator:    r.Locator,
			TrackCount: r.TrackCount,
		}
	}

	meta := ShuffleIndexMeta{
		Schema:         _shuffleSchema,
		Version:        1,
		Source:         catalog.SourceModArchive,
		Fingerprint:    fp,
		TrackCount:     totalTracks,
		DirectoryCount: uint64(len(records)),
		Entries:        entries,
		CreatedAt:      time.Now(),
	}

	// Build GSA entries: manifest + one record per directory.
	gsaEntries := make([]archive.SourceEntry, 0, len(records)+1)
	manifestData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("manifest encode: %w", err)
	}
	gsaEntries = append(gsaEntries, archive.SourceEntry{Name: "manifest.json", Data: manifestData})

	for _, r := range records {
		data, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("record encode %s: %w", r.Locator, err)
		}
		gsaEntries = append(gsaEntries, archive.SourceEntry{
			Name: recordEntryName(r.Locator),
			Data: data,
		})
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("index mkdir: %w", err)
	}
	targetPath := filepath.Join(dir, _shuffleIndexFile)
	tmp, tmpErr := os.CreateTemp(dir, "modarchive-idx*.tmp")
	if tmpErr != nil {
		return fmt.Errorf("index temp: %w", tmpErr)
	}
	tmpPath := tmp.Name()
	if tmpErr = tmp.Close(); tmpErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("index close temp: %w", tmpErr)
	}
	defer os.Remove(tmpPath)

	if err := archive.Write(tmpPath, gsaEntries); err != nil {
		return fmt.Errorf("index write: %w", err)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("index rename: %w", err)
	}

	slog.Info("modarchive: shuffle index built",
		"directories", len(records),
		"tracks", totalTracks,
		"path", targetPath,
	)
	return nil
}

// openGSA opens a GSA file, returning (nil, nil) only when the file
// does not exist (os.IsNotExist). Any other error is returned.
func openGSA(path string, maxBuckets uint32) (*archive.Archive, error) {
	gsa, err := archive.Open(path, maxBuckets)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return gsa, nil
}

// buildSnapshotRecords opens the snapshot GSA and enumerates all bucket records.
func buildSnapshotRecords(baseDir string) ([]ShuffleDirRecord, error) {
	gsa, err := openGSA(SnapshotCatalogPath(baseDir), _shuffleMaxBuckets)
	if err != nil {
		return nil, err
	}
	if gsa == nil {
		return nil, nil // snapshot does not exist
	}
	defer gsa.Close()

	var records []ShuffleDirRecord
	for _, entry := range gsa.Entries() {
		data, err := gsa.Read(entry.Name)
		if err != nil {
			return nil, fmt.Errorf("read bucket %s: %w", entry.Name, err)
		}

		var snapRecords []snapshotRecord
		if err := json.Unmarshal(data, &snapRecords); err != nil {
			return nil, fmt.Errorf("decode bucket %s: %w", entry.Name, err)
		}

		bucketURL := BaseURL + SnapshotDir + "/" + entry.Name
		dirRecord := snapshotEntryToDirRecord(bucketURL, entry.Name, snapRecords)
		if dirRecord != nil {
			records = append(records, *dirRecord)
		}
	}
	return records, nil
}

// buildAddendumRecords opens the addendum GSA and enumerates all bucket records.
func buildAddendumRecords(baseDir string) ([]ShuffleDirRecord, error) {
	gsa, err := openGSA(AddendumCatalogPath(baseDir), _shuffleMaxBuckets)
	if err != nil {
		return nil, err
	}
	if gsa == nil {
		return nil, nil // addendum does not exist
	}
	defer gsa.Close()

	var records []ShuffleDirRecord
	for _, entry := range gsa.Entries() {
		data, err := gsa.Read(entry.Name)
		if err != nil {
			return nil, fmt.Errorf("read addendum bucket %s: %w", entry.Name, err)
		}

		var snapRecords []snapshotRecord
		if err := json.Unmarshal(data, &snapRecords); err != nil {
			return nil, fmt.Errorf("decode addendum bucket %s: %w", entry.Name, err)
		}

		bucketURL := BaseURL + AddendumDir + "/" + entry.Name
		dirRecord := snapshotEntryToDirRecord(bucketURL, entry.Name, snapRecords)
		if dirRecord != nil {
			records = append(records, *dirRecord)
		}
	}
	return records, nil
}

func snapshotEntryToDirRecord(bucketURL, entryName string, snapRecords []snapshotRecord) *ShuffleDirRecord {
	var tracks []ShuffleTrackEntry
	for _, r := range snapRecords {
		cleanName, ok := supportedArchiveEntry(r.Name)
		if !ok {
			continue
		}
		entryURL, err := archiveEntryURL(bucketURL, r.Name)
		if err != nil {
			continue
		}
		// Validate archive metadata to avoid corrupt records reaching playback.
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
		if !validArchiveItem(item) {
			continue
		}
		tracks = append(tracks, ShuffleTrackEntry{
			Path:             player.ModArchivePrefix + entryURL,
			Name:             cleanName,
			Key:              r.Name,
			Size:             r.Size,
			ArchiveOffset:    r.ArchiveOffset,
			ArchiveEndOffset: r.ArchiveEndOffset,
			CompressedSize:   r.CompressedSize,
			CRC32:            r.CRC32,
			Compression:      r.Compression,
		})
	}
	if len(tracks) == 0 {
		return nil
	}

	displayName := AlbumLabel(bucketURL)
	return &ShuffleDirRecord{
		Locator:     bucketURL,
		DisplayName: displayName,
		TrackCount:  uint64(len(tracks)),
		Version:     catalog.ListingVersion(entryName),
		Tracks:      tracks,
	}
}

// buildCachedRecords creates directory records from cached catalog listings.
func buildCachedRecords(cat *catalogData) []ShuffleDirRecord {
	if cat == nil {
		return nil
	}
	var records []ShuffleDirRecord
	for dirURL, items := range cat.Directories {
		// Skip snapshot/addendum root entries — those are handled by GSA.
		if isSnapshotOrAddendumDir(dirURL) {
			continue
		}
		dirRecord := cachedDirToRecord(dirURL, items)
		if dirRecord != nil {
			records = append(records, *dirRecord)
		}
	}
	return records
}

func cachedDirToRecord(dirURL string, items []DirItem) *ShuffleDirRecord {
	var tracks []ShuffleTrackEntry
	for _, item := range items {
		if item.Kind != KindFile {
			continue
		}
		cleanName, ok := supportedArchiveEntry(item.Name)
		if !ok {
			cleanName = item.CleanName
		}
		playURL := player.ModArchivePrefix + item.URL
		tracks = append(tracks, ShuffleTrackEntry{
			Path: playURL,
			Name: cleanName,
			Key:  item.Name,
			Size: item.Size,
		})
	}
	if len(tracks) == 0 {
		return nil
	}

	displayName := AlbumLabel(dirURL)
	return &ShuffleDirRecord{
		Locator:     dirURL,
		DisplayName: displayName,
		TrackCount:  uint64(len(tracks)),
		Version:     catalog.ListingVersion(urlHash(dirURL)),
		Tracks:      tracks,
	}
}

func isSnapshotOrAddendumDir(u string) bool {
	return strings.Contains(u, "/"+SnapshotDir+"/") || strings.Contains(u, "/"+AddendumDir+"/")
}

// recordEntryName derives a GSA record name from a directory locator.
// Uses a hash to avoid path-length issues and ensure uniqueness.
func recordEntryName(locator string) string {
	return "entries/" + urlHash(locator) + ".json"
}

// shuffleIndexPath returns the full path to modarchive.idx.
func shuffleIndexPath(baseDir string) string {
	dir, _ := ShuffleIndexDir(baseDir)
	return filepath.Join(dir, _shuffleIndexFile)
}

// catalogData is a minimal struct for accessing the catalog's directory map.
// Defined here to avoid circular imports with the catalog package during
// the migration phase; will be replaced by catalog.DirectoryCache later.
type catalogData struct {
	Directories map[string][]DirItem
}

// validateShuffleManifest checks structural consistency of a loaded manifest.
// It validates schema, version, source, entry uniqueness, track count
// consistency, and that every entry has a non-empty locator.
func validateShuffleManifest(meta *ShuffleIndexMeta) error {
	if meta.Schema != _shuffleSchema {
		return fmt.Errorf("modarchive: index schema %q, want %q", meta.Schema, _shuffleSchema)
	}
	if meta.Version < 1 {
		return fmt.Errorf("modarchive: index version %d, want >= 1", meta.Version)
	}
	if meta.Source != catalog.SourceModArchive {
		return fmt.Errorf("modarchive: index source %v, want ModArchive", meta.Source)
	}
	if len(meta.Entries) == 0 {
		return errors.New("modarchive: index has no entries")
	}
	seen := make(map[string]struct{}, len(meta.Entries))
	var totalTracks uint64
	for _, entry := range meta.Entries {
		if entry.Name == "" {
			return errors.New("modarchive: index entry with empty name")
		}
		if entry.Locator == "" {
			return fmt.Errorf("modarchive: index entry %q has empty locator", entry.Name)
		}
		if _, ok := seen[entry.Name]; ok {
			return fmt.Errorf("modarchive: duplicate entry name %q", entry.Name)
		}
		seen[entry.Name] = struct{}{}
		totalTracks += entry.TrackCount
	}
	if totalTracks != meta.TrackCount {
		return fmt.Errorf("modarchive: manifest track count %d, entries sum %d", meta.TrackCount, totalTracks)
	}
	return nil
}
