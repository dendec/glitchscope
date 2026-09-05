package modland

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
)

const (
	_shuffleIndexFile        = "modland.idx"
	_idxMaxEntries    uint32 = 256_000
	_shuffleSchema           = "modland-shuffle-v1"
)

// ShuffleAlbumRecord is one album/format-author directory entry.
// Every album carries its full track list — Modland has no other
// persistent random-access source, unlike ModArchive's GSA snapshot.
type ShuffleAlbumRecord struct {
	Locator     string                 `json:"locator"` // Album.Name, e.g. "Protracker/Curt Cool"
	DisplayName string                 `json:"display_name"`
	TrackCount  uint64                 `json:"track_count"`
	Version     catalog.ListingVersion `json:"version"`
	Tracks      []ShuffleTrackEntry    `json:"tracks"`
}

// ShuffleTrackEntry is a single track inside an album record.
type ShuffleTrackEntry struct {
	Path string `json:"path"` // modland: prefixed path for playback
	Name string `json:"name"` // display name (filename)
	Size int64  `json:"size"`
}

// FormatAlbumSummary is one album's compact metadata inside a format record.
// It provides fast format -> albums navigation without reading every full
// album record (which also carries the track list).
type FormatAlbumSummary struct {
	Locator     string `json:"locator"` // Album.Name, matches ManifestEntry.Locator
	DisplayName string `json:"display_name"`
	TrackCount  uint64 `json:"track_count"`
}

// ShuffleFormatRecord lists every album belonging to one format, for the
// format -> albums navigation branch.
type ShuffleFormatRecord struct {
	Format string               `json:"format"`
	Albums []FormatAlbumSummary `json:"albums"`
}

// FormatSummary is compact per-format metadata read directly from the
// manifest (no GSA record read required).
type FormatSummary struct {
	Name       string
	AlbumCount uint64
	TrackCount uint64
}

// ShuffleIndexMeta is the manifest stored as manifest.json inside modland.idx.
type ShuffleIndexMeta struct {
	Schema         string                  `json:"schema"`
	Version        int                     `json:"version"`
	Source         catalog.SourceKind      `json:"source"`
	Fingerprint    catalog.Fingerprint     `json:"fingerprint"`
	TrackCount     uint64                  `json:"track_count"`
	DirectoryCount uint64                  `json:"directory_count"`
	Entries        []catalog.ManifestEntry `json:"entries"`
	// FormatEntries lists one entry per format, giving fast format -> albums
	// navigation. Locator is the format name; DirectoryCount is the album
	// count within the format; TrackCount is the format's total tracks.
	FormatEntries []catalog.ManifestEntry `json:"format_entries"`
	CreatedAt     time.Time               `json:"created_at"`
}

// estimateRecordBytes returns the approximate heap size of a
// ShuffleAlbumRecord in bytes. Used for memory-bounded LRU eviction.
func estimateRecordBytes(r *ShuffleAlbumRecord) int64 {
	n := int64(64) // struct overhead + Locator + DisplayName + Version headers
	n += int64(len(r.Locator) + len(r.DisplayName) + len(r.Version))
	n += int64(len(r.Tracks)) * 120 // per-track: ~3 strings + numeric fields
	for _, t := range r.Tracks {
		n += int64(len(t.Path) + len(t.Name))
	}
	return n
}

// albumVersion computes a content-based ListingVersion for an album from
// its track list. It changes whenever tracks are added, removed,
// reordered, or a track field changes.
func albumVersion(locator string, tracks []ShuffleTrackEntry) catalog.ListingVersion {
	h := sha256.New()
	fmt.Fprintf(h, "locator:%s\n", locator)
	for _, t := range tracks {
		fmt.Fprintf(h, "track:%s:%s:%d\n", t.Path, t.Name, t.Size)
	}
	return catalog.ListingVersion(hex.EncodeToString(h.Sum(nil)[:16]))
}

// catalogContentHash reads the catalog file and hashes its full content.
func catalogContentHash(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// ShuffleIndexFingerprint computes a fingerprint from the modland catalog
// file. It changes whenever the catalog is refreshed with different content.
func ShuffleIndexFingerprint(baseDir string) catalog.Fingerprint {
	return catalog.Fingerprint{
		SourceHash: catalogContentHash(CatalogPath(baseDir)),
	}
}

// ShuffleIndexDir returns the directory where modland.idx is stored.
func ShuffleIndexDir(baseDir string) (string, error) {
	return catalog.ShuffleIndexDir(baseDir, "modland")
}

// recordEntryName derives a GSA record name from an album locator.
func recordEntryName(locator string) string {
	h := sha256.Sum256([]byte(locator))
	return "albums/" + hex.EncodeToString(h[:16]) + ".json"
}

// formatEntryName derives a GSA record name from a format name.
func formatEntryName(format string) string {
	h := sha256.Sum256([]byte(format))
	return "formats/" + hex.EncodeToString(h[:16]) + ".json"
}

// formatOf returns the format name for an album — the first path segment
// of Album.Name (e.g. "Protracker" for "Protracker/Curt Cool").
func formatOf(albumName string) string {
	if i := strings.IndexByte(albumName, '/'); i >= 0 {
		return albumName[:i]
	}
	return albumName
}

// shuffleIndexPath returns the full path to modland.idx.
func shuffleIndexPath(baseDir string) string {
	dir, _ := ShuffleIndexDir(baseDir)
	return filepath.Join(dir, _shuffleIndexFile)
}

// BuildShuffleIndex builds modland.idx from the parsed Modland catalog.
// Every album is stored as a full record because Modland has no other
// persistent random-access source for track data.
func BuildShuffleIndex(baseDir string, cat *Catalog) error {
	dir, err := ShuffleIndexDir(baseDir)
	if err != nil {
		return err
	}

	fp := ShuffleIndexFingerprint(baseDir)

	entries := make([]catalog.ManifestEntry, 0, len(cat.Albums))
	gsaEntries := make([]archive.SourceEntry, 0, len(cat.Albums)+1)
	var totalTracks uint64

	albums := make([]Album, len(cat.Albums))
	copy(albums, cat.Albums)
	sort.Slice(albums, func(i, j int) bool { return albums[i].Name < albums[j].Name })

	formatAlbums := make(map[string][]FormatAlbumSummary)
	var formatOrder []string

	for _, album := range albums {
		if len(album.Tracks) == 0 {
			continue
		}
		tracks := make([]ShuffleTrackEntry, len(album.Tracks))
		for i, t := range album.Tracks {
			tracks[i] = ShuffleTrackEntry{
				Path: ModlandPrefixTrackPath(album.TrackPath(i)),
				Name: t.Name,
				Size: t.Size,
			}
		}
		record := ShuffleAlbumRecord{
			Locator:     album.Name,
			DisplayName: album.Name,
			TrackCount:  uint64(len(tracks)),
			Version:     albumVersion(album.Name, tracks),
			Tracks:      tracks,
		}
		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("modland: encode album %q: %w", album.Name, err)
		}
		name := recordEntryName(album.Name)
		gsaEntries = append(gsaEntries, archive.SourceEntry{Name: name, Data: data})
		entries = append(entries, catalog.ManifestEntry{
			Name:       name,
			Locator:    album.Name,
			TrackCount: record.TrackCount,
		})
		totalTracks += record.TrackCount

		format := formatOf(album.Name)
		if _, ok := formatAlbums[format]; !ok {
			formatOrder = append(formatOrder, format)
		}
		formatAlbums[format] = append(formatAlbums[format], FormatAlbumSummary{
			Locator:     album.Name,
			DisplayName: album.Name,
			TrackCount:  record.TrackCount,
		})
	}

	sort.Strings(formatOrder)
	formatEntries := make([]catalog.ManifestEntry, 0, len(formatOrder))

	if uint64(len(entries))+uint64(len(formatOrder))+1 > uint64(_idxMaxEntries) {
		return fmt.Errorf("modland: too many entries %d (max %d)", len(entries)+len(formatOrder)+1, _idxMaxEntries)
	}

	for _, format := range formatOrder {
		summaries := formatAlbums[format]
		var formatTracks uint64
		for _, s := range summaries {
			formatTracks += s.TrackCount
		}
		formatRecord := ShuffleFormatRecord{Format: format, Albums: summaries}
		data, err := json.Marshal(formatRecord)
		if err != nil {
			return fmt.Errorf("modland: encode format %q: %w", format, err)
		}
		name := formatEntryName(format)
		gsaEntries = append(gsaEntries, archive.SourceEntry{Name: name, Data: data})
		formatEntries = append(formatEntries, catalog.ManifestEntry{
			Name:           name,
			Locator:        format,
			TrackCount:     formatTracks,
			DirectoryCount: uint64(len(summaries)),
		})
	}

	meta := ShuffleIndexMeta{
		Schema:         _shuffleSchema,
		Version:        1,
		Source:         catalog.SourceModland,
		Fingerprint:    fp,
		TrackCount:     totalTracks,
		DirectoryCount: uint64(len(entries)),
		Entries:        entries,
		FormatEntries:  formatEntries,
		CreatedAt:      time.Now(),
	}
	manifestData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("modland: manifest encode: %w", err)
	}
	allEntries := make([]archive.SourceEntry, 0, len(gsaEntries)+1)
	allEntries = append(allEntries, archive.SourceEntry{Name: "manifest.json", Data: manifestData})
	allEntries = append(allEntries, gsaEntries...)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("modland: index mkdir: %w", err)
	}
	targetPath := filepath.Join(dir, _shuffleIndexFile)
	tmp, tmpErr := os.CreateTemp(dir, "modland-idx*.tmp")
	if tmpErr != nil {
		return fmt.Errorf("modland: index temp: %w", tmpErr)
	}
	tmpPath := tmp.Name()
	if tmpErr = tmp.Close(); tmpErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("modland: index close temp: %w", tmpErr)
	}
	defer os.Remove(tmpPath)

	if err := archive.Write(tmpPath, allEntries); err != nil {
		return fmt.Errorf("modland: index write: %w", err)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("modland: index rename: %w", err)
	}

	slog.Info("modland: shuffle index built",
		"albums", len(entries),
		"tracks", totalTracks,
		"path", targetPath,
	)
	return nil
}

// validateShuffleManifest checks structural consistency of a loaded manifest.
func validateShuffleManifest(meta *ShuffleIndexMeta) error {
	if meta.Schema != _shuffleSchema {
		return fmt.Errorf("modland: index schema %q, want %q", meta.Schema, _shuffleSchema)
	}
	if meta.Version != 1 {
		return fmt.Errorf("modland: index version %d, want 1", meta.Version)
	}
	if meta.Source != catalog.SourceModland {
		return fmt.Errorf("modland: index source %v, want Modland", meta.Source)
	}
	if len(meta.Entries) == 0 {
		return errors.New("modland: index has no entries")
	}
	if uint64(len(meta.Entries)) != meta.DirectoryCount {
		return fmt.Errorf("modland: manifest directory count %d, entries %d", meta.DirectoryCount, len(meta.Entries))
	}
	seenNames := make(map[string]struct{}, len(meta.Entries))
	seenLocators := make(map[string]struct{}, len(meta.Entries))
	var totalTracks uint64
	for _, entry := range meta.Entries {
		if entry.Name == "" {
			return errors.New("modland: index entry with empty name")
		}
		if entry.Locator == "" {
			return fmt.Errorf("modland: index entry %q has empty locator", entry.Name)
		}
		if entry.Name != recordEntryName(entry.Locator) {
			return fmt.Errorf("modland: entry name %q != recordEntryName(%q) = %q",
				entry.Name, entry.Locator, recordEntryName(entry.Locator))
		}
		if _, ok := seenNames[entry.Name]; ok {
			return fmt.Errorf("modland: duplicate entry name %q", entry.Name)
		}
		seenNames[entry.Name] = struct{}{}
		if _, ok := seenLocators[entry.Locator]; ok {
			return fmt.Errorf("modland: duplicate locator %q", entry.Locator)
		}
		seenLocators[entry.Locator] = struct{}{}
		totalTracks += entry.TrackCount
	}
	if totalTracks != meta.TrackCount {
		return fmt.Errorf("modland: manifest track count %d, entries sum %d", meta.TrackCount, totalTracks)
	}
	if err := validateFormatEntries(meta); err != nil {
		return err
	}
	return nil
}

// validateFormatEntries checks structural consistency of the format-level
// manifest entries: unique names/locators, correct entry-name derivation,
// and that format track counts sum to the album-level total.
func validateFormatEntries(meta *ShuffleIndexMeta) error {
	seenNames := make(map[string]struct{}, len(meta.FormatEntries))
	seenLocators := make(map[string]struct{}, len(meta.FormatEntries))
	var totalTracks uint64
	for _, entry := range meta.FormatEntries {
		if entry.Name == "" {
			return errors.New("modland: format entry with empty name")
		}
		if entry.Locator == "" {
			return fmt.Errorf("modland: format entry %q has empty locator", entry.Name)
		}
		if entry.Name != formatEntryName(entry.Locator) {
			return fmt.Errorf("modland: format entry name %q != formatEntryName(%q) = %q",
				entry.Name, entry.Locator, formatEntryName(entry.Locator))
		}
		if _, ok := seenNames[entry.Name]; ok {
			return fmt.Errorf("modland: duplicate format entry name %q", entry.Name)
		}
		seenNames[entry.Name] = struct{}{}
		if _, ok := seenLocators[entry.Locator]; ok {
			return fmt.Errorf("modland: duplicate format %q", entry.Locator)
		}
		seenLocators[entry.Locator] = struct{}{}
		totalTracks += entry.TrackCount
	}
	if len(meta.FormatEntries) > 0 && totalTracks != meta.TrackCount {
		return fmt.Errorf("modland: format track count %d, want %d", totalTracks, meta.TrackCount)
	}
	return nil
}

// validateRecordIdentity checks a loaded record against its manifest entry.
func validateRecordIdentity(record *ShuffleAlbumRecord, entry catalog.ManifestEntry) error {
	if record.Locator != entry.Locator {
		return fmt.Errorf("modland: record locator %q != manifest %q", record.Locator, entry.Locator)
	}
	if entry.Name != recordEntryName(record.Locator) {
		return fmt.Errorf("modland: record entry name %q != recordEntryName(%q)", entry.Name, record.Locator)
	}
	if uint64(len(record.Tracks)) != record.TrackCount {
		return fmt.Errorf("modland: record %q track count %d != len(Tracks) %d",
			record.Locator, record.TrackCount, len(record.Tracks))
	}
	if record.TrackCount != entry.TrackCount {
		return fmt.Errorf("modland: record %q track count %d != manifest %d",
			record.Locator, record.TrackCount, entry.TrackCount)
	}
	return nil
}

// ShuffleIndexNeedsRebuild reports whether the index is missing, stale, or
// incompatible. It opens the index and compares the fingerprint without
// loading any album record.
func ShuffleIndexNeedsRebuild(baseDir string) bool {
	idxPath := shuffleIndexPath(baseDir)
	info, err := os.Stat(idxPath)
	if err != nil || info.Size() == 0 {
		return true
	}
	gsa, err := archive.Open(idxPath, _idxMaxEntries)
	if err != nil {
		return true
	}
	defer gsa.Close()

	data, err := gsa.Read("manifest.json")
	if err != nil {
		return true
	}
	var meta ShuffleIndexMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return true
	}
	if err := validateShuffleManifest(&meta); err != nil {
		return true
	}
	fp := ShuffleIndexFingerprint(baseDir)
	return meta.Fingerprint.SourceHash != fp.SourceHash
}
