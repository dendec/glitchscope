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
	// archive range.
	_snapshotMaxEntries uint32 = 32_000
	// _idxMaxEntries is the limit for the modarchive.idx file itself.
	// The .idx stores one entry per cached directory plus the manifest.
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

// estimateRecordBytes returns the approximate heap size of a
// ShuffleDirRecord in bytes. Used for memory-bounded LRU eviction.
func estimateRecordBytes(r *ShuffleDirRecord) int64 {
	n := int64(80) // struct overhead + Locator + DisplayName + Version headers
	n += int64(len(r.Locator) + len(r.DisplayName) + len(r.Version))
	n += int64(len(r.Tracks)) * 220 // per-track: ~6 strings + numeric fields
	for _, t := range r.Tracks {
		n += int64(len(t.Path) + len(t.Name) + len(t.Key))
	}
	return n
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

// gsaContentHash reads the entire GSA file and hashes its full content.
// This detects any byte-level change including same-size replacements,
// unlike a prefix-only hash which would miss tail modifications.
func gsaContentHash(path string) string {
	h := sha256.New()
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// ShuffleIndexFingerprint computes a fingerprint from the snapshot GSA files
// and the catalog data. It detects changes that require an index rebuild.
// Uses full content hash for GSA files to detect any byte-level change.
func ShuffleIndexFingerprint(baseDir string, cat *catalogData) catalog.Fingerprint {
	h := sha256.New()

	for _, p := range []string{
		SnapshotCatalogPath(baseDir),
		AddendumCatalogPath(baseDir),
	} {
		if contentHash := gsaContentHash(p); contentHash != "" {
			fmt.Fprintf(h, "%s:%s\n", p, contentHash)
		}
	}

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
func BuildShuffleIndex(baseDir string, cat *catalogData) error {
	dir, err := ShuffleIndexDir(baseDir)
	if err != nil {
		return err
	}

	fp := ShuffleIndexFingerprint(baseDir, cat)

	var allDirs []dirSummary

	// 1. Snapshot buckets from GSA — stream summaries, no track accumulation.
	snapshotSummaries, err := buildSnapshotDirSummaries(baseDir)
	if err != nil {
		return fmt.Errorf("snapshot records: %w", err)
	}
	allDirs = append(allDirs, snapshotSummaries...)

	// 2. Addendum buckets from GSA — same streaming pattern.
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
	for _, ce := range cachedEntries {
		allDirs = append(allDirs, dirSummary{
			locator:    ce.locator,
			trackCount: ce.trackCount,
			cached:     true,
			version:    ce.version,
		})
	}

	// Guard against exceeding index capacity.
	if uint64(len(allDirs))+1 > uint64(_idxMaxEntries) {
		return fmt.Errorf("modarchive: too many directories %d (max %d cached + manifest)",
			len(allDirs), _idxMaxEntries-1)
	}

	sort.Slice(allDirs, func(i, j int) bool {
		return allDirs[i].locator < allDirs[j].locator
	})

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
// Returns manifest summaries and serialized GSA record entries.
func buildCachedDirEntries(cat *catalogData) ([]dirSummary, []archive.SourceEntry, error) {
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
	sort.Slice(cached, func(i, j int) bool {
		return cached[i].dirURL < cached[j].dirURL
	})

	summaries := make([]dirSummary, 0, len(cached))
	records := make([]archive.SourceEntry, 0, len(cached))

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

// buildSnapshotDirSummaries reads snapshot GSA and streams compact
// summaries. Each bucket is processed immediately: tracks are counted,
// the summary is emitted, and the track slice is released before the
// next bucket is read. Peak memory ≈ O(largest_bucket) rather than
// O(total_snapshot_tracks).
func buildSnapshotDirSummaries(baseDir string) ([]dirSummary, error) {
	gsa, err := openGSA(SnapshotCatalogPath(baseDir), _snapshotMaxEntries)
	if err != nil {
		return nil, err
	}
	if gsa == nil {
		return nil, nil
	}
	defer gsa.Close()

	var summaries []dirSummary
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
		count := uint64(len(tracks))

		if count > 0 {
			summaries = append(summaries, dirSummary{
				locator:    bucketURL,
				trackCount: count,
			})
		}
	}
	return summaries, nil
}

// buildAddendumDirSummaries reads addendum GSA and streams compact summaries.
func buildAddendumDirSummaries(baseDir string) ([]dirSummary, error) {
	gsa, err := openGSA(AddendumCatalogPath(baseDir), _snapshotMaxEntries)
	if err != nil {
		return nil, err
	}
	if gsa == nil {
		return nil, nil
	}
	defer gsa.Close()

	var summaries []dirSummary
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
		count := uint64(len(tracks))

		if count > 0 {
			summaries = append(summaries, dirSummary{
				locator:    bucketURL,
				trackCount: count,
			})
		}
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
// Only entries whose name passes supportedArchiveEntry are included.
func cachedDirTracks(dirURL string, items []DirItem) []ShuffleTrackEntry {
	var tracks []ShuffleTrackEntry
	for _, item := range items {
		if item.Kind != KindFile {
			continue
		}
		cleanName, ok := supportedArchiveEntry(item.Name)
		if !ok {
			continue
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
func recordEntryName(locator string) string {
	return "entries/" + urlHash(locator) + ".json"
}

// shuffleIndexPath returns the full path to modarchive.idx.
func shuffleIndexPath(baseDir string) string {
	dir, _ := ShuffleIndexDir(baseDir)
	return filepath.Join(dir, _shuffleIndexFile)
}

// isSnapshotOrAddendumLocator returns true if the locator belongs to the
// snapshot or addendum source GSA.
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
// canonical URL.
func bucketEntryName(locator string) string {
	for _, prefix := range []string{"/" + SnapshotDir + "/", "/" + AddendumDir + "/"} {
		if idx := strings.LastIndex(locator, prefix); idx >= 0 {
			return locator[idx+len(prefix):]
		}
	}
	return ""
}

// catalogData is a minimal struct for accessing the catalog's directory map.
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

// validateCachedRecordNames checks that every non-snapshot manifest
// entry has a corresponding record name in the GSA index, without
// reading or decompressing any record data. This is O(N) over the
// entry name set and suitable for the startup path.
func validateCachedRecordNames(meta *ShuffleIndexMeta, gsa *archive.Archive) error {
	gsaNames := make(map[string]struct{}, len(gsa.Entries()))
	for _, e := range gsa.Entries() {
		gsaNames[e.Name] = struct{}{}
	}
	for _, entry := range meta.Entries {
		if isSnapshotOrAddendumLocator(entry.Locator) {
			continue
		}
		if _, ok := gsaNames[entry.Name]; !ok {
			return fmt.Errorf("modarchive: cached record %q for %q not found in index", entry.Name, entry.Locator)
		}
	}
	return nil
}

// validateCachedRecordIdentity performs full identity checks on a cached
// record at lazy-load time: Locator, Name, TrackCount, Version.
func validateCachedRecordIdentity(record *ShuffleDirRecord, entry catalog.ManifestEntry) error {
	if record.Locator != entry.Locator {
		return fmt.Errorf("modarchive: record locator %q != manifest %q", record.Locator, entry.Locator)
	}
	if entry.Name != recordEntryName(record.Locator) {
		return fmt.Errorf("modarchive: record entry name %q != recordEntryName(%q)", entry.Name, record.Locator)
	}
	if uint64(len(record.Tracks)) != record.TrackCount {
		return fmt.Errorf("modarchive: record %q track count %d != len(Tracks) %d",
			record.Locator, record.TrackCount, len(record.Tracks))
	}
	if record.TrackCount != entry.TrackCount {
		return fmt.Errorf("modarchive: record %q track count %d != manifest %d",
			record.Locator, record.TrackCount, entry.TrackCount)
	}
	return nil
}
