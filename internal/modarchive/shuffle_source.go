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

// maxRecordCacheCap is the maximum number of directory records kept
// in the ShuffleSource runtime cache. For low-RAM devices (512 MB),
// 128 cached records with ~30 K tracks each is ~40 MiB worst case;
// typical records are much smaller (~200 bytes/track).
const maxRecordCacheCap = 128

// recordCache is a bounded LRU cache for ShuffleDirRecord.
// Lookup is O(1) via the map; eviction is O(n) over the slice but
// n ≤ 128 so it's negligible per operation.
type recordCache struct {
	entries  map[string]*cacheSlot
	order    []string // access-ordered, oldest first
	capacity int
	// accessCounter is monotonically increasing per entry. It is
	// normalized (halved all counters) when it exceeds 1<<48 to
	// prevent effective counter convergence over long runtimes.
	accessCounter uint64
}

type cacheSlot struct {
	record *ShuffleDirRecord
	access uint64
}

func newRecordCache(cap int) *recordCache {
	return &recordCache{
		entries:  make(map[string]*cacheSlot, cap),
		order:    make([]string, 0, cap),
		capacity: cap,
	}
}

func (c *recordCache) get(key string) (*ShuffleDirRecord, bool) {
	slot, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.accessCounter++
	slot.access = c.accessCounter
	if c.accessCounter > 1<<48 {
		c.normalize()
	}
	// Move to end of order (most recently used).
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			c.order = append(c.order, key)
			break
		}
	}
	return slot.record, true
}

func (c *recordCache) put(key string, record *ShuffleDirRecord) {
	if _, ok := c.entries[key]; ok {
		c.entries[key].record = record
		c.accessCounter++
		c.entries[key].access = c.accessCounter
		if c.accessCounter > 1<<48 {
			c.normalize()
		}
		// Move to end of order (most recently used).
		for i, k := range c.order {
			if k == key {
				c.order = append(c.order[:i], c.order[i+1:]...)
				c.order = append(c.order, key)
				break
			}
		}
		return
	}
	// Evict oldest if at capacity.
	for len(c.order) >= c.capacity {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
	c.accessCounter++
	c.entries[key] = &cacheSlot{record: record, access: c.accessCounter}
	c.order = append(c.order, key)
	if c.accessCounter > 1<<48 {
		c.normalize()
	}
}

// normalize halves all access counters to prevent convergence.
func (c *recordCache) normalize() {
	for _, slot := range c.entries {
		slot.access /= 2
	}
	c.accessCounter /= 2
}

// ShuffleSource implements catalog.SourceIndex for ModArchive.
// It reads modarchive.idx (a GSA file) at construction time and
// provides lazy random selection by reading only the selected
// directory record — not the full catalog.
//
// The .idx contains full records only for cached directories.
// Snapshot/addendum records are reconstructed from the source GSA
// on demand and cached in memory.
type ShuffleSource struct {
	baseDir        string
	meta           *ShuffleIndexMeta
	locatorToEntry map[string]string // canonical URL → GSA entry name (from manifest)
	gsa            *archive.Archive  // .idx GSA (has cached dir records)
	records        *recordCache
	mu             sync.Mutex
}

// OpenShuffleSource opens modarchive.idx and loads only the manifest.
// Full directory records are loaded lazily on first access.
// Returns nil if the index does not exist or is invalid.
func OpenShuffleSource(baseDir string) *ShuffleSource {
	idxPath := shuffleIndexPath(baseDir)
	gsa, err := archive.Open(idxPath, _idxMaxEntries)
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
	if err := validateShuffleManifest(&meta); err != nil {
		_ = gsa.Close()
		return nil
	}

	// Validate that cached directory records actually exist in the GSA.
	if err := ValidateCachedRecordPresence(&meta, gsa); err != nil {
		_ = gsa.Close()
		return nil
	}

	// Build reverse map from manifest — no record reads needed.
	locatorToEntry := make(map[string]string, len(meta.Entries))
	for _, entry := range meta.Entries {
		locatorToEntry[entry.Locator] = entry.Name
	}

	return &ShuffleSource{
		baseDir:        baseDir,
		meta:           &meta,
		locatorToEntry: locatorToEntry,
		gsa:            gsa,
		records:        newRecordCache(maxRecordCacheCap),
	}
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

// rejectSampleUint64 returns a uniform random value in [0, max) using
// rejection sampling to avoid modulo bias. Returns 0 for max=0.
func rejectSampleUint64(rng *rand.Rand, max uint64) uint64 {
	if max == 0 {
		return 0
	}
	// Compute the largest multiple of max that fits in uint64.
	limit := ^uint64(0) - (^uint64(0) % max) //nolint:gocritic // clear intent
	for {
		v := rng.Uint64()
		if v < limit {
			return v % max
		}
	}
}

// RandomTrack selects one track from all ModArchive directories using
// track-weighted directory selection, then uniform selection within
// the chosen directory.
func (s *ShuffleSource) RandomTrack(rng *rand.Rand) (catalog.ShuffleTrack, error) {
	if s.meta == nil || s.meta.TrackCount == 0 {
		return catalog.ShuffleTrack{}, fmt.Errorf("modarchive: no tracks")
	}

	// Track-weighted directory selection with rejection sampling.
	pick := rejectSampleUint64(rng, s.meta.TrackCount)
	var cumulative uint64
	for _, entry := range s.meta.Entries {
		cumulative += entry.TrackCount
		if pick < cumulative {
			return s.randomTrackFromEntry(entry, rng)
		}
	}
	// Fallback: last entry.
	last := s.meta.Entries[len(s.meta.Entries)-1]
	return s.randomTrackFromEntry(last, rng)
}

// RandomTrackInDirectory selects one track uniformly within a specific
// directory. The DirectoryKey.Locator is the canonical URL.
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
	entry catalog.ManifestEntry,
	rng *rand.Rand,
) (catalog.ShuffleTrack, error) {
	record, err := s.loadRecord(entry.Locator)
	if err != nil {
		return catalog.ShuffleTrack{}, err
	}
	if len(record.Tracks) == 0 {
		return catalog.ShuffleTrack{}, fmt.Errorf("modarchive: directory %q has no tracks", entry.Locator)
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

// loadRecord resolves a locator (canonical URL or GSA entry name) to
// a full directory record.
//
// For cached directories: reads from the .idx GSA (lazy, cached).
// For snapshot/addendum: reconstructs from the source GSA on demand
// and caches the result.
func (s *ShuffleSource) loadRecord(locator string) (*ShuffleDirRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Resolve canonical URL to GSA entry name if needed.
	entryName := locator
	if en, ok := s.locatorToEntry[locator]; ok {
		entryName = en
	}

	if r, ok := s.records.get(entryName); ok {
		return r, nil
	}

	// Determine the source: cached dir (in .idx) vs snapshot/addendum.
	isSnapAdd := isSnapshotOrAddendumLocator(entryName)
	if !isSnapAdd {
		isSnapAdd = isSnapshotOrAddendumLocator(locator)
	}

	var record *ShuffleDirRecord
	if isSnapAdd {
		canonicalURL := locator
		if isSnapshotOrAddendumLocator(locator) {
			canonicalURL = locator
		} else {
			for _, e := range s.meta.Entries {
				if e.Name == entryName {
					canonicalURL = e.Locator
					break
				}
			}
		}
		var err error
		record, err = s.loadSnapshotRecord(canonicalURL)
		if err != nil {
			return nil, err
		}
	} else {
		if s.gsa == nil {
			return nil, fmt.Errorf("modarchive: index not open")
		}
		data, err := s.gsa.Read(entryName)
		if err != nil {
			return nil, fmt.Errorf("modarchive: read record %s: %w", entryName, err)
		}
		record = &ShuffleDirRecord{}
		if err := json.Unmarshal(data, record); err != nil {
			return nil, fmt.Errorf("modarchive: decode record %s: %w", entryName, err)
		}
	}

	s.records.put(entryName, record)
	return record, nil
}

// loadSnapshotRecord reconstructs a ShuffleDirRecord from the snapshot
// or addendum source GSA. The locator must be a canonical URL.
func (s *ShuffleSource) loadSnapshotRecord(locator string) (*ShuffleDirRecord, error) {
	gsaPath := sourceGSAPath(s.baseDir, locator)
	gsa, err := openGSA(gsaPath, _snapshotMaxEntries)
	if err != nil {
		return nil, fmt.Errorf("modarchive: open source GSA: %w", err)
	}
	if gsa == nil {
		return nil, fmt.Errorf("modarchive: source GSA not found for %q", locator)
	}
	defer gsa.Close()

	entryName := bucketEntryName(locator)
	if entryName == "" {
		return nil, fmt.Errorf("modarchive: cannot extract bucket name from %q", locator)
	}

	data, err := gsa.Read(entryName)
	if err != nil {
		return nil, fmt.Errorf("modarchive: read bucket %s: %w", entryName, err)
	}
	var snapRecords []snapshotRecord
	if err := json.Unmarshal(data, &snapRecords); err != nil {
		return nil, fmt.Errorf("modarchive: decode bucket %s: %w", entryName, err)
	}

	tracks := snapshotRecordTracks(locator, snapRecords)
	if len(tracks) == 0 {
		return nil, fmt.Errorf("modarchive: bucket %q has no supported tracks", entryName)
	}

	return &ShuffleDirRecord{
		Locator:     locator,
		DisplayName: AlbumLabel(locator),
		TrackCount:  uint64(len(tracks)),
		Version:     listingVersion(locator, tracks),
		Tracks:      tracks,
	}, nil
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

// ShuffleSourceNeedsRebuild reports whether the index is missing, stale,
// or incompatible. It opens the index and compares the fingerprint
// without loading any directory records.
func ShuffleSourceNeedsRebuild(baseDir string, cat *catalogData) bool {
	idxPath := shuffleIndexPath(baseDir)
	info, err := os.Stat(idxPath)
	if err != nil {
		return true
	}
	if info.Size() == 0 {
		return true
	}

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
