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
	_shuffleIndexFile         = "modarchive.idx"
	_shuffleMaxBuckets uint32 = 4096
	_shuffleSchema            = "modarchive-shuffle-v1"
)

// ShuffleDirRecord is one directory/bucket entry.
// For snapshot/addendum sources, records are NOT stored in the .idx;
// they are reconstructed from the source GSA on demand.
// For cached directories, the full record (with tracks) is stored in
// the .idx since no other source is available.
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

// listingVersion computes a content-based ListingVersion from a locator
// and its sorted track entries. It changes when tracks are added,
// removed, reordered, or when any track field changes.
func listingVersion(locator string, tracks []ShuffleTrackEntry) catalog.ListingVersion {
	h := sha256.New()
	fmt.Fprintf(h, "locator:%s\n", locator)
	for _, t := range tracks {
		fmt.Fprintf(h, "track:%s:%s:%s:%d:%d:%d:%d:%d:%d\n",
			t.Path, t.Name, t.Key, t.Size,
			t.ArchiveOffset, t.ArchiveEndOffset,
			t.CompressedSize, t.CRC32, t.Compression)
	}
	return catalog.ListingVersion(hex.EncodeToString(h.Sum(nil)[:16]))
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
// cached directory listings.
//
// The .idx GSA contains:
//   - manifest.json with compact entries (locator, track_count) for ALL
//     directories including snapshot/addendum.
//   - Full ShuffleDirRecord entries ONLY for cached directories.
//
// Snapshot/addendum records are NOT duplicated; they are reconstructed
// from the source GSA on demand by loadSnapshotRecord.
func BuildShuffleIndex(baseDir string, cat *catalogData) error {
	dir, err := ShuffleIndexDir(baseDir)
	if err != nil {
		return err
	}

	fp := ShuffleIndexFingerprint(baseDir, cat)

	var allDirs []dirInfo

	// 1. Snapshot buckets from GSA — metadata only, no .idx record.
	snapshotDirs, err := buildSnapshotDirInfos(baseDir)
	if err != nil {
		return fmt.Errorf("snapshot records: %w", err)
	}
	allDirs = append(allDirs, snapshotDirs...)

	// 2. Addendum buckets from GSA — metadata only.
	addendumDirs, err := buildAddendumDirInfos(baseDir)
	if err != nil {
		return fmt.Errorf("addendum records: %w", err)
	}
	allDirs = append(allDirs, addendumDirs...)

	// 3. Cached directory listings — full records in .idx.
	if cat != nil {
		for dirURL, items := range cat.Directories {
			if isSnapshotOrAddendumDir(dirURL) {
				continue
			}
			tracks := cachedDirTracks(dirURL, items)
			if len(tracks) > 0 {
				allDirs = append(allDirs, dirInfo{
					locator: dirURL,
					tracks:  tracks,
					cached:  true,
				})
			}
		}
	}

	// Sort by locator for deterministic output.
	sort.Slice(allDirs, func(i, j int) bool {
		return allDirs[i].locator < allDirs[j].locator
	})

	// Build manifest with compact entries.
	var totalTracks uint64
	entries := make([]catalog.ManifestEntry, len(allDirs))
	for i, d := range allDirs {
		totalTracks += uint64(len(d.tracks))
		entries[i] = catalog.ManifestEntry{
			Name:       recordEntryName(d.locator),
			Locator:    d.locator,
			TrackCount: uint64(len(d.tracks)),
		}
	}

	meta := ShuffleIndexMeta{
		Schema:         _shuffleSchema,
		Version:        1,
		Source:         catalog.SourceModArchive,
		Fingerprint:    fp,
		TrackCount:     totalTracks,
		DirectoryCount: uint64(len(allDirs)),
		Entries:        entries,
		CreatedAt:      time.Now(),
	}

	// Build GSA entries: manifest + cached dir records only.
	gsaEntries := make([]archive.SourceEntry, 0, len(allDirs)+1)
	manifestData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("manifest encode: %w", err)
	}
	gsaEntries = append(gsaEntries, archive.SourceEntry{Name: "manifest.json", Data: manifestData})

	for _, d := range allDirs {
		if !d.cached {
			continue // snapshot/addendum: no record in .idx
		}
		record := ShuffleDirRecord{
			Locator:     d.locator,
			DisplayName: AlbumLabel(d.locator),
			TrackCount:  uint64(len(d.tracks)),
			Version:     listingVersion(d.locator, d.tracks),
			Tracks:      d.tracks,
		}
		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("record encode %s: %w", d.locator, err)
		}
		gsaEntries = append(gsaEntries, archive.SourceEntry{
			Name: recordEntryName(d.locator),
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
		"directories", len(allDirs),
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

// buildSnapshotDirInfos reads snapshot GSA and returns compact dir
// info with track metadata (no .idx record will be written).
func buildSnapshotDirInfos(baseDir string) ([]dirInfo, error) {
	gsa, err := openGSA(SnapshotCatalogPath(baseDir), _shuffleMaxBuckets)
	if err != nil {
		return nil, err
	}
	if gsa == nil {
		return nil, nil
	}
	defer gsa.Close()

	var dirs []dirInfo
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
		tracks := snapshotRecordTracks(bucketURL, snapRecords)
		if len(tracks) > 0 {
			dirs = append(dirs, dirInfo{locator: bucketURL, tracks: tracks})
		}
	}
	return dirs, nil
}

// buildAddendumDirInfos reads addendum GSA and returns compact dir info.
func buildAddendumDirInfos(baseDir string) ([]dirInfo, error) {
	gsa, err := openGSA(AddendumCatalogPath(baseDir), _shuffleMaxBuckets)
	if err != nil {
		return nil, err
	}
	if gsa == nil {
		return nil, nil
	}
	defer gsa.Close()

	var dirs []dirInfo
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
		tracks := snapshotRecordTracks(bucketURL, snapRecords)
		if len(tracks) > 0 {
			dirs = append(dirs, dirInfo{locator: bucketURL, tracks: tracks})
		}
	}
	return dirs, nil
}

// snapshotRecordTracks converts snapshot records into ShuffleTrackEntry
// list, validating each entry.
func snapshotRecordTracks(bucketURL string, snapRecords []snapshotRecord) []ShuffleTrackEntry {
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
	return tracks
}

// cachedDirTracks extracts track entries from cached DirItem list.
func cachedDirTracks(dirURL string, items []DirItem) []ShuffleTrackEntry {
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
	return tracks
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

// isSnapshotOrAddendumLocator returns true if the locator belongs to the
// snapshot or addendum source GSA (as opposed to a cached directory).
func isSnapshotOrAddendumLocator(locator string) bool {
	return strings.Contains(locator, "/"+SnapshotDir+"/") || strings.Contains(locator, "/"+AddendumDir+"/")
}

// sourceGSAPath returns the source GSA path for a snapshot/addendum locator.
func sourceGSAPath(baseDir, locator string) string {
	if strings.Contains(locator, "/"+SnapshotDir+"/") {
		return SnapshotCatalogPath(baseDir)
	}
	return AddendumCatalogPath(baseDir)
}

// bucketEntryName extracts the GSA entry name from a snapshot/addendum
// canonical URL. E.g. "http://modarchive.textfiles.com/.../A/A0.zip"
// → "A/A0.zip".
func bucketEntryName(locator string) string {
	// The locator is BaseURL + sourceDir + "/" + entryName.
	// Find the entry name after the source dir prefix.
	for _, prefix := range []string{"/" + SnapshotDir + "/", "/" + AddendumDir + "/"} {
		if idx := strings.LastIndex(locator, prefix); idx >= 0 {
			return locator[idx+len(prefix):]
		}
	}
	return ""
}

// dirInfo holds compact metadata for one directory during index build.
// For snapshot/addendum, cached=false means no .idx record is written.
// For cached directories, cached=true means the full record is stored.
type dirInfo struct {
	locator string
	tracks  []ShuffleTrackEntry
	cached  bool // true = cached dir, needs full record in .idx
}

// catalogData is a minimal struct for accessing the catalog's directory map.
// Defined here to avoid circular imports with the catalog package during
// the migration phase; will be replaced by catalog.DirectoryCache later.
type catalogData struct {
	Directories map[string][]DirItem
}

// validateShuffleManifest checks structural consistency of a loaded manifest.
func validateShuffleManifest(meta *ShuffleIndexMeta) error {
	if meta.Schema != _shuffleSchema {
		return fmt.Errorf("modarchive: index schema %q, want %q", meta.Schema, _shuffleSchema)
	}
	if meta.Version != 1 {
		return fmt.Errorf("modarchive: index version %d, want 1", meta.Version)
	}
	if meta.Source != catalog.SourceModArchive {
		return fmt.Errorf("modarchive: index source %v, want ModArchive", meta.Source)
	}
	if len(meta.Entries) == 0 {
		return errors.New("modarchive: index has no entries")
	}
	if uint64(len(meta.Entries)) != meta.DirectoryCount {
		return fmt.Errorf("modarchive: manifest directory count %d, entries %d", meta.DirectoryCount, len(meta.Entries))
	}
	seenNames := make(map[string]struct{}, len(meta.Entries))
	seenLocators := make(map[string]struct{}, len(meta.Entries))
	var totalTracks uint64
	for _, entry := range meta.Entries {
		if entry.Name == "" {
			return errors.New("modarchive: index entry with empty name")
		}
		if entry.Locator == "" {
			return fmt.Errorf("modarchive: index entry %q has empty locator", entry.Name)
		}
		if entry.Name != recordEntryName(entry.Locator) {
			return fmt.Errorf("modarchive: entry name %q != recordEntryName(%q) = %q",
				entry.Name, entry.Locator, recordEntryName(entry.Locator))
		}
		if _, ok := seenNames[entry.Name]; ok {
			return fmt.Errorf("modarchive: duplicate entry name %q", entry.Name)
		}
		seenNames[entry.Name] = struct{}{}
		if _, ok := seenLocators[entry.Locator]; ok {
			return fmt.Errorf("modarchive: duplicate locator %q", entry.Locator)
		}
		seenLocators[entry.Locator] = struct{}{}
		totalTracks += entry.TrackCount
	}
	if totalTracks != meta.TrackCount {
		return fmt.Errorf("modarchive: manifest track count %d, entries sum %d", meta.TrackCount, totalTracks)
	}
	return nil
}
