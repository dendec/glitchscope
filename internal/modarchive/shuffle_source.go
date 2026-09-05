package modarchive

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"sync"

	"github.com/dendec/glitchscope/internal/archive"
	"github.com/dendec/glitchscope/internal/catalog"
)

// ShuffleSource implements catalog.SourceIndex for ModArchive.
// It reads modarchive.idx (a GSA file) at construction time and
// provides lazy random selection by reading only the selected
// directory record — not the full catalog.
type ShuffleSource struct {
	baseDir        string
	meta           *ShuffleIndexMeta
	records        map[string]*ShuffleDirRecord // entry name → record
	locatorToEntry map[string]string            // canonical URL → entry name
	gsa            *archive.Archive
	mu             sync.Mutex
}

// OpenShuffleSource opens modarchive.idx and loads the manifest.
// Returns nil if the index does not exist or is invalid.
func OpenShuffleSource(baseDir string) *ShuffleSource {
	idxPath := shuffleIndexPath(baseDir)
	gsa, err := archive.Open(idxPath, _shuffleMaxBuckets)
	if err != nil {
		return nil
	}

	manifestData, err := gsa.Read("manifest.json")
	if err != nil {
		_ = gsa.Close()
		return nil
	}

	var meta ShuffleIndexMeta
	if err := json.Unmarshal(manifestData, &meta); err != nil {
		_ = gsa.Close()
		return nil
	}
	if meta.Schema != _shuffleSchema || meta.Source != catalog.SourceModArchive {
		_ = gsa.Close()
		return nil
	}

	src := &ShuffleSource{
		baseDir:        baseDir,
		meta:           &meta,
		records:        make(map[string]*ShuffleDirRecord, len(meta.Entries)),
		locatorToEntry: make(map[string]string, len(meta.Entries)),
		gsa:            gsa,
	}

	// Build reverse map: canonical URL → GSA entry name.
	for _, entry := range meta.Entries {
		data, err := gsa.Read(entry.Name)
		if err != nil {
			gsa.Close()
			return nil
		}
		var record ShuffleDirRecord
		if err := json.Unmarshal(data, &record); err != nil {
			gsa.Close()
			return nil
		}
		src.records[entry.Name] = &record
		src.locatorToEntry[record.Locator] = entry.Name
	}

	return src
}

func (s *ShuffleSource) Source() catalog.SourceKind { return catalog.SourceModArchive }

func (s *ShuffleSource) TrackCount() uint64 {
	if s.meta == nil {
		return 0
	}
	return s.meta.TrackCount
}

func (s *ShuffleSource) DirectoryCount() uint64 {
	if s.meta == nil {
		return 0
	}
	return s.meta.DirectoryCount
}

func (s *ShuffleSource) Fingerprint() catalog.Fingerprint {
	if s.meta == nil {
		return catalog.Fingerprint{}
	}
	return s.meta.Fingerprint
}

// RandomTrack selects one track from all ModArchive directories using
// track-weighted directory selection, then uniform selection within
// the chosen directory.
func (s *ShuffleSource) RandomTrack(rng *rand.Rand) (catalog.ShuffleTrack, error) {
	if s.meta == nil || s.meta.TrackCount == 0 {
		return catalog.ShuffleTrack{}, fmt.Errorf("modarchive: no tracks")
	}

	// Track-weighted directory selection.
	pick := rng.Uint64() % s.meta.TrackCount
	var cumulative uint64
	for _, entry := range s.meta.Entries {
		cumulative += entry.TrackCount
		if pick < cumulative {
			dirKey := catalog.DirectoryKey{
				Source:  catalog.SourceModArchive,
				Locator: entry.Name,
			}
			return s.randomTrackFromEntry(dirKey, &entry, rng)
		}
	}
	// Fallback: last entry.
	last := s.meta.Entries[len(s.meta.Entries)-1]
	dirKey := catalog.DirectoryKey{
		Source:  catalog.SourceModArchive,
		Locator: last.Name,
	}
	return s.randomTrackFromEntry(dirKey, &last, rng)
}

// RandomTrackInDirectory selects one track uniformly within a specific
// directory. The DirectoryKey.Locator is the GSA record name (entry name).
func (s *ShuffleSource) RandomTrackInDirectory(key catalog.DirectoryKey, rng *rand.Rand) (catalog.ShuffleTrack, error) {
	record, err := s.loadRecord(key.Locator)
	if err != nil {
		return catalog.ShuffleTrack{}, err
	}
	if len(record.Tracks) == 0 {
		return catalog.ShuffleTrack{}, fmt.Errorf("modarchive: directory %q has no tracks", key.Locator)
	}

	idx := rng.Intn(len(record.Tracks))
	t := record.Tracks[idx]
	return catalog.ShuffleTrack{
		Path:           t.Path,
		Source:         catalog.SourceModArchive,
		DirectoryKey:   catalog.DirectoryKey{Source: catalog.SourceModArchive, Locator: record.Locator},
		TrackIndex:     uint32(idx),
		TrackKey:       t.Key,
		ListingVersion: record.Version,
		AlbumName:      record.DisplayName,
	}, nil
}

// DirectoryList returns the listing for one directory/bucket.
func (s *ShuffleSource) DirectoryList(key catalog.DirectoryKey) (catalog.DirectoryListing, error) {
	record, err := s.loadRecord(key.Locator)
	if err != nil {
		return catalog.DirectoryListing{}, err
	}

	entries := make([]catalog.DirectoryEntry, len(record.Tracks))
	for i, t := range record.Tracks {
		entries[i] = catalog.DirectoryEntry{
			Path: t.Path,
			Name: t.Name,
			Size: t.Size,
		}
	}
	return catalog.DirectoryListing{
		Key:         catalog.DirectoryKey{Source: catalog.SourceModArchive, Locator: record.Locator},
		Version:     record.Version,
		DisplayName: record.DisplayName,
		Entries:     entries,
	}, nil
}

func (s *ShuffleSource) randomTrackFromEntry(
	dirKey catalog.DirectoryKey,
	entry *catalog.ManifestEntry,
	rng *rand.Rand,
) (catalog.ShuffleTrack, error) {
	record, err := s.loadRecord(entry.Name)
	if err != nil {
		return catalog.ShuffleTrack{}, err
	}
	if len(record.Tracks) == 0 {
		return catalog.ShuffleTrack{}, fmt.Errorf("modarchive: directory %q has no tracks", entry.Name)
	}

	idx := rng.Intn(len(record.Tracks))
	t := record.Tracks[idx]
	return catalog.ShuffleTrack{
		Path:           t.Path,
		Source:         catalog.SourceModArchive,
		DirectoryKey:   catalog.DirectoryKey{Source: catalog.SourceModArchive, Locator: record.Locator},
		TrackIndex:     uint32(idx),
		TrackKey:       t.Key,
		ListingVersion: record.Version,
		AlbumName:      record.DisplayName,
	}, nil
}

func (s *ShuffleSource) loadRecord(locator string) (*ShuffleDirRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Resolve canonical URL to GSA entry name if needed.
	entryName := locator
	if en, ok := s.locatorToEntry[locator]; ok {
		entryName = en
	}

	if r, ok := s.records[entryName]; ok {
		return r, nil
	}
	if s.gsa == nil {
		return nil, fmt.Errorf("modarchive: index not open")
	}

	data, err := s.gsa.Read(entryName)
	if err != nil {
		return nil, fmt.Errorf("modarchive: read record %s: %w", entryName, err)
	}

	var record ShuffleDirRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("modarchive: decode record %s: %w", entryName, err)
	}
	s.records[entryName] = &record
	return &record, nil
}

// Close releases the underlying GSA archive.
func (s *ShuffleSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gsa != nil {
		err := s.gsa.Close()
		s.gsa = nil
		return err
	}
	return nil
}

// NeedsRebuild reports whether the index is missing, stale, or incompatible.
func ShuffleSourceNeedsRebuild(baseDir string, cat *catalogData) bool {
	idxPath := shuffleIndexPath(baseDir)
	info, err := os.Stat(idxPath)
	if err != nil {
		return true
	}
	if info.Size() == 0 {
		return true
	}

	// Quick fingerprint check: open and compare.
	src := OpenShuffleSource(baseDir)
	if src == nil {
		return true
	}
	defer src.Close()

	currentFP := ShuffleIndexFingerprint(baseDir, cat)
	return src.Fingerprint().SourceHash != currentFP.SourceHash
}

// SortedEntries returns manifest entries in deterministic order for testing.
func (s *ShuffleSource) SortedEntries() []catalog.ManifestEntry {
	if s.meta == nil {
		return nil
	}
	out := make([]catalog.ManifestEntry, len(s.meta.Entries))
	copy(out, s.meta.Entries)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
