package modarchive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	_shuffleIndexFile = "modarchive.idx"
	// _snapshotMaxEntries is the limit for source GSA files (snapshot/
	// addendum). These have a fixed number of buckets determined by the
	// archive range. 2048 matches the existing snapshot catalog limit.
	_snapshotMaxEntries uint32 = 2048
	// _idxMaxEntries is the limit for the modarchive.idx file itself.
	// The .idx stores one entry per cached directory plus the manifest.
	// 256K entries allows ~256K cached directories (well beyond any
	// real ModArchive collection) while staying within uint32.
	_idxMaxEntries uint32 = 256_000
	_shuffleSchema        = "modarchive-shuffle-v1"
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

// gsaContentHash reads a GSA file and hashes size + first 4 KiB of
// content. This detects same-size file replacements that a pure
// mtime/size fingerprint would miss, at the cost of one extra read.
// The collision probability is bounded: same size, same first 4 KiB,
// but different full content → ~2^{-32} for the partial hash and
// ~2^{-16} for size match (practical: ~2^{-48}).
func gsaContentHash(path string) string {
	h := sha256.New()
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	fmt.Fprintf(h, "size:%d\n", info.Size())
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 4096)
	n, _ := io.ReadAtLeast(f, buf, len(buf))
	if n > 0 {
		fmt.Fprintf(h, "head:%x\n", buf[:n])
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// ShuffleIndexFingerprint computes a fingerprint from the snapshot GSA files
// and the catalog data. It detects changes that require an index rebuild.
// Uses GSA content hash (size + first 4 KiB) instead of pure mtime to
// detect same-size file replacements.
func ShuffleIndexFingerprint(baseDir string, cat *catalogData) catalog.Fingerprint {
	h := sha256.New()

	// GSA file content hashes.
	for _, p := range []string{
		SnapshotCatalogPath(baseDir),
		AddendumCatalogPath(baseDir),
	} {
		if contentHash := gsaContentHash(p); contentHash != "" {
			fmt.Fprintf(h, "%s:%s\n", p, contentHash)
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

// dirSummary is compact metadata for one directory during index build.
// For snapshot/addendum dirs, only the entry metadata is kept (no
// tracks slice), avoiding O(total_snapshot_tracks) memory.
type dirSummary struct {
	locator    string
	trackCount uint64
	cached     bool                   // true = cached dir, needs full record in .idx
	version    catalog.ListingVersion // set only for cached dirs
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
//
// Builder memory: snapshot/addendum directories contribute only compact
// dirSummary (locator + count), not full track slices. Cached dirs do
// carry tracks since they have no other source.
func BuildShuffleIndex(baseDir string, cat *catalogData) error {
	dir, err := ShuffleIndexDir(baseDir)
	if err != nil {
		return err
	}

	fp := ShuffleIndexFingerprint(baseDir, cat)

	// Collect summaries — not full dirInfo — to limit builder memory.
	var allDirs []dirSummary

	// 1. Snapshot buckets from GSA — metadata only, no .idx record.
	snapshotSummaries, err := buildSnapshotDirSummaries(baseDir)
	if err != nil {
		return fmt.Errorf("snapshot records: %w", err)
	}
	allDirs = append(allDirs, snapshotSummaries...)

	// 2. Addendum buckets from GSA — metadata only.
	addendumSummaries, err := buildAddendumDirSummaries(baseDir)
	if err != nil {
		return fmt.Errorf("addendum records: %w", err)
	}
	allDirs = append(allDirs, addendumSummaries...)

	// 3. Cached directory listings — full records in .idx.
	cachedEntries, cachedRecordEntries, err := buildCachedDirEntries(cat)
	if err != nil {
		return fmt.Errorf("cached directories: %w", err)
	}
	// Merge cached entries into allDirs for the manifest.
	for _, ce := range cachedEntries {
		allDirs = append(allDirs, dirSummary{
			locator:    ce.locator,
			trackCount: ce.trackCount,
			cached:     true,
			version:    ce.version,
		})
	}

	// Sort by locator for deterministic output.
	sort.Slice(allDirs, func(i, j int) bool {
		return allDirs[i].locator < allDirs[j].locator
	})

	// Build manifest with compact entries.
	var totalTracks uint64
	entries := make([]catalog.ManifestEntry, len(allDirs))
	for i, d := range allDirs {
		totalTracks += d.trackCount
		entries[i] = catalog.ManifestEntry{
			Name:       recordEntryName(d.locator),
			Locator:    d.locator,
			TrackCount: d.trackCount,
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
	gsaEntries := make([]archive.SourceEntry, 0, len(cachedRecordEntries)+1)
	manifestData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("manifest encode: %w", err)
	}
	gsaEntries = append(gsaEntries, archive.SourceEntry{Name: "manifest.json", Data: manifestData})
	gsaEntries = append(gsaEntries, cachedRecordEntries...)

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

// buildCachedDirEntries processes all cached directories from the catalog.
// It returns both manifest summaries and GSA record entries.
// Only supported files are included (verified via supportedArchiveEntry).
func buildCachedDirEntries(cat *catalogData) (summaries []dirSummary, records []archive.SourceEntry, err error) {
	if cat == nil {
		return nil, nil, nil
	}
	type rawEntry struct {
		dirURL string
		items  []DirItem
	}
	var cached []rawEntry
	for dirURL, items := range cat.Directories {
		if isSnapshotOrAddendumDir(dirURL) {
			continue
		}
		cached = append(cached, rawEntry{dirURL: dirURL, items: items})
	}
	// Sort for deterministic output.
	sort.Slice(cached, func(i, j int) bool {
		return cached[i].dirURL < cached[j].dirURL
	})

	summaries = make([]dirSummary, 0, len(cached))
	records = make([]archive.SourceEntry, 0, len(cached))

	for _, ce := range cached {
		tracks := cachedDirTracks(ce.dirURL, ce.items)
		if len(tracks) == 0 {
			continue
		}
		record := ShuffleDirRecord{
			Locator:     ce.dirURL,
			DisplayName: AlbumLabel(ce.dirURL),
			TrackCount:  uint64(len(tracks)),
			Version:     listingVersion(ce.dirURL, tracks),
			Tracks:      tracks,
		}
		data, err := json.Marshal(record)
		if err != nil {
			return nil, nil, fmt.Errorf("record encode %s: %w", ce.dirURL, err)
		}
		summaries = append(summaries, dirSummary{
			locator:    ce.dirURL,
			trackCount: uint64(len(tracks)),
			cached:     true,
			version:    record.Version,
		})
		records = append(records, archive.SourceEntry{
			Name: recordEntryName(ce.dirURL),
			Data: data,
		})
	}
	return summaries, records, nil
}

// openGSA opens a GSA file, returning (nil, nil) only when the file
// does not exist (os.IsNotExist). Any other error is returned.
func openGSA(path string, maxEntries uint32) (*archive.Archive, error) {
	gsa, err := archive.Open(path, maxEntries)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return gsa, nil
}

// dirSummaryRaw is an intermediate type for snapshot/addendum dir summaries
// that also carries the track slice needed for version computation.
type dirSummaryRaw struct {
	summary dirSummary
	tracks  []ShuffleTrackEntry
}

// buildSnapshotDirSummaries reads snapshot GSA and returns compact summaries.
// Track data is freed after version computation — only locator + count
// survive into the returned dirSummary slice.
func buildSnapshotDirSummaries(baseDir string) ([]dirSummary, error) {
	gsa, err := openGSA(SnapshotCatalogPath(baseDir), _snapshotMaxEntries)
	if err != nil {
		return nil, err
	}
	if gsa == nil {
		return nil, nil
	}
	defer gsa.Close()

	var raws []dirSummaryRaw
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
			raws = append(raws, dirSummaryRaw{
				summary: dirSummary{
					locator:    bucketURL,
					trackCount: uint64(len(tracks)),
				},
				tracks: tracks,
			})
		}
	}

	// Build compact summaries — release track data.
	summaries := make([]dirSummary, len(raws))
	for i, r := range raws {
		summaries[i] = r.summary
		// Allow GC to reclaim track slices.
		raws[i].tracks = nil
	}
	return summaries, nil
}

// buildAddendumDirSummaries reads addendum GSA and returns compact summaries.
func buildAddendumDirSummaries(baseDir string) ([]dirSummary, error) {
	gsa, err := openGSA(AddendumCatalogPath(baseDir), _snapshotMaxEntries)
	if err != nil {
		return nil, err
	}
	if gsa == nil {
		return nil, nil
	}
	defer gsa.Close()

	var raws []dirSummaryRaw
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
			raws = append(raws, dirSummaryRaw{
				summary: dirSummary{
					locator:    bucketURL,
					trackCount: uint64(len(tracks)),
				},
				tracks: tracks,
			})
		}
	}

	summaries := make([]dirSummary, len(raws))
	for i, r := range raws {
		summaries[i] = r.summary
		raws[i].tracks = nil
	}
	return summaries, nil
}

// snapshotRecordTracks converts snapshot records into ShuffleTrackEntry
// list, validating each entry. Only supported archive entries are included.
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
// Only entries whose name passes supportedArchiveEntry are included,
// ensuring the resulting TrackCount matches actually playable files.
func cachedDirTracks(dirURL string, items []DirItem) []ShuffleTrackEntry {
	var tracks []ShuffleTrackEntry
	for _, item := range items {
		if item.Kind != KindFile {
			continue
		}
		cleanName, ok := supportedArchiveEntry(item.Name)
		if !ok {
			continue // skip unsupported formats — don't inflate TrackCount
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

// ValidateCachedRecordPresence checks that every non-snapshot manifest
// entry has a corresponding record in the GSA. Snapshot/addendum entries
// are expected to be absent (reconstructed from source GSA).
func ValidateCachedRecordPresence(meta *ShuffleIndexMeta, gsa *archive.Archive) error {
	for _, entry := range meta.Entries {
		if isSnapshotOrAddendumLocator(entry.Locator) {
			continue // these are not stored in .idx
		}
		data, err := gsa.Read(entry.Name)
		if err != nil {
			return fmt.Errorf("cached record %q for %q not found in index: %w", entry.Name, entry.Locator, err)
		}
		var record ShuffleDirRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return fmt.Errorf("cached record %q decode: %w", entry.Name, err)
		}
		if record.TrackCount != entry.TrackCount {
			return fmt.Errorf("cached record %q track count %d != manifest %d", entry.Name, record.TrackCount, entry.TrackCount)
		}
	}
	return nil
}
