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

// ShuffleIndexMeta is the manifest stored as manifest.json inside modland.idx.
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

	if uint64(len(cat.Albums))+1 > uint64(_idxMaxEntries) {
		return fmt.Errorf("modland: too many albums %d (max %d)", len(cat.Albums), _idxMaxEntries-1)
	}

	entries := make([]catalog.ManifestEntry, 0, len(cat.Albums))
	gsaEntries := make([]archive.SourceEntry, 0, len(cat.Albums)+1)
	var totalTracks uint64

	albums := make([]Album, len(cat.Albums))
	copy(albums, cat.Albums)
	sort.Slice(albums, func(i, j int) bool { return albums[i].Name < albums[j].Name })

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
	}

	meta := ShuffleIndexMeta{
		Schema:         _shuffleSchema,
		Version:        1,
		Source:         catalog.SourceModland,
		Fingerprint:    fp,
		TrackCount:     totalTracks,
		DirectoryCount: uint64(len(entries)),
		Entries:        entries,
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
