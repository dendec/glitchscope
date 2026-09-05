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

const (
	// maxCachedBytes is the memory budget for the ShuffleSource record
	// cache. On 512 MB devices this leaves headroom for other subsystems.
	// Eviction is triggered when the estimated total byte size exceeds
	// this threshold, regardless of how many entries are stored.
	maxCachedBytes int64 = 50 << 20 // 50 MiB
)

// recordCache is a bounded LRU cache for ShuffleDirRecord, limited
// by both entry count and estimated memory usage. Eviction removes
// the oldest (least recently used) entry until the byte budget is
// satisfied.
type recordCache struct {
	entries    map[string]*cacheSlot
	order      []string // access-ordered, oldest first
	totalBytes int64
	// accessCounter is monotonically increasing per entry. It is
	// normalized (halved all counters) when it exceeds 1<<48.
	accessCounter uint64
}

type cacheSlot struct {
	record *ShuffleDirRecord
	bytes  int64 // estimated heap size
	access uint64
}

func newRecordCache() *recordCache {
	return &recordCache{
		entries: make(map[string]*cacheSlot),
		order:   make([]string, 0, 64),
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
	estimatedBytes := estimateRecordBytes(record)

	// A record larger than the whole budget can never be retained without
	// starving every other entry. Serve it to the caller without caching
	// it, and drop any existing cached copy for the same key.
	if estimatedBytes > maxCachedBytes {
		if existing, ok := c.entries[key]; ok {
			c.totalBytes -= existing.bytes
			delete(c.entries, key)
			for i, k := range c.order {
				if k == key {
					c.order = append(c.order[:i], c.order[i+1:]...)
					break
				}
			}
		}
		return
	}

	if existing, ok := c.entries[key]; ok {
		c.totalBytes -= existing.bytes
		existing.record = record
		existing.bytes = estimatedBytes
		c.totalBytes += estimatedBytes
		c.accessCounter++
		existing.access = c.accessCounter
		if c.accessCounter > 1<<48 {
			c.normalize()
		}
		// Move to end of order.
		for i, k := range c.order {
			if k == key {
				c.order = append(c.order[:i], c.order[i+1:]...)
				c.order = append(c.order, key)
				break
			}
		}
		// Evict if over memory budget.
		c.evictToBudget()
		return
	}

	// Evict oldest entries until we have room.
	for len(c.order) > 0 && c.totalBytes+estimatedBytes > maxCachedBytes {
		c.evictOldest()
	}

	c.accessCounter++
	c.entries[key] = &cacheSlot{record: record, bytes: estimatedBytes, access: c.accessCounter}
	c.order = append(c.order, key)
	c.totalBytes += estimatedBytes
	if c.accessCounter > 1<<48 {
		c.normalize()
	}
}

// evictOldest removes the entry at the front of the order slice.
func (c *recordCache) evictOldest() {
	if len(c.order) == 0 {
		return
	}
	oldest := c.order[0]
	c.order = c.order[1:]
	slot := c.entries[oldest]
	c.totalBytes -= slot.bytes
	delete(c.entries, oldest)
}

// evictToBudget evicts oldest entries until totalBytes ≤ maxCachedBytes.
func (c *recordCache) evictToBudget() {
	for len(c.order) > 0 && c.totalBytes > maxCachedBytes {
		c.evictOldest()
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

	// Check that cached record names exist in the GSA index.
	// This is O(N) over the entry name set, no JSON decode or
	// decompression — suitable for the startup path.
	if err := validateCachedRecordNames(&meta, gsa); err != nil {
		_ = gsa.Close()
		return nil
	}

	locatorToEntry := make(map[string]string, len(meta.Entries))
	for _, entry := range meta.Entries {
		locatorToEntry[entry.Locator] = entry.Name
	}

	return &ShuffleSource{
		baseDir:        baseDir,
		meta:           &meta,
		locatorToEntry: locatorToEntry,
		gsa:            gsa,
		records:        newRecordCache(),
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
// rejection sampling to avoid modulo bias.
func rejectSampleUint64(rng *rand.Rand, max uint64) uint64 {
	if max == 0 {
		return 0
	}
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

	pick := rejectSampleUint64(rng, s.meta.TrackCount)
	var cumulative uint64
	for _, entry := range s.meta.Entries {
		cumulative += entry.TrackCount
		if pick < cumulative {
			return s.randomTrackFromEntry(entry, rng)
		}
	}
	last := s.meta.Entries[len(s.meta.Entries)-1]
	return s.randomTrackFromEntry(last, rng)
}

// RandomTrackInDirectory selects one track uniformly within a specific
// directory.
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

// loadRecord resolves a locator to a full directory record with
// identity validation. For cached directories: reads from the .idx GSA
// (lazy, LRU-cached). For snapshot/addendum: reconstructs from the
// source GSA on demand and caches the result.
func (s *ShuffleSource) loadRecord(locator string) (*ShuffleDirRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entryName := locator
	if en, ok := s.locatorToEntry[locator]; ok {
		entryName = en
	}

	if r, ok := s.records.get(entryName); ok {
		return r, nil
	}

	isSnapAdd := isSnapshotOrAddendumLocator(entryName)
	if !isSnapAdd {
		isSnapAdd = isSnapshotOrAddendumLocator(locator)
	}

	var record *ShuffleDirRecord
	var manifestEntry catalog.ManifestEntry

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
		// Find the manifest entry for identity validation.
		for _, e := range s.meta.Entries {
			if e.Locator == canonicalURL {
				manifestEntry = e
				break
			}
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
		// Find the manifest entry for identity validation.
		for _, e := range s.meta.Entries {
			if e.Name == entryName {
				manifestEntry = e
				break
			}
		}
	}

	// Validate record identity against manifest.
	if manifestEntry.Name != "" {
		if err := validateCachedRecordIdentity(record, manifestEntry); err != nil {
			return nil, err
		}
	}

	s.records.put(entryName, record)
	return record, nil
}

// loadSnapshotRecord reconstructs a ShuffleDirRecord from the snapshot
// or addendum source GSA.
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
// or incompatible.
// ShuffleSourceNeedsRebuild reports whether the on-disk index is stale
// compared to the on-disk cached-directory catalog.
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

// ShuffleIndexStale reports whether the given fingerprint (from an already-
// opened index) is outdated compared to the current cached-directory catalog.
// Used to avoid a second GSA open when the fast path already opened the source.
func ShuffleIndexStale(baseDir string, indexFP catalog.Fingerprint) bool {
	fp := ShuffleIndexFingerprint(baseDir, cachedCatalogData(baseDir))
	return indexFP.SourceHash != fp.SourceHash
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
