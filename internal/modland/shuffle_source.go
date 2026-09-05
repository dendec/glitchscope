package modland

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"

	"github.com/dendec/glitchscope/internal/archive"
	"github.com/dendec/glitchscope/internal/catalog"
)

const (
	// maxCachedBytes is the memory budget for the ShuffleSource record
	// cache. Eviction triggers when estimated total byte size exceeds
	// this threshold, regardless of entry count.
	maxCachedBytes int64 = 50 << 20 // 50 MiB
)

// recordCache is a bounded LRU cache for ShuffleAlbumRecord, limited by
// estimated memory usage. Eviction removes the oldest (least recently
// used) entry until the byte budget is satisfied.
type recordCache struct {
	entries       map[string]*cacheSlot
	order         []string // access-ordered, oldest first
	totalBytes    int64
	accessCounter uint64
}

type cacheSlot struct {
	record *ShuffleAlbumRecord
	bytes  int64
	access uint64
}

func newRecordCache() *recordCache {
	return &recordCache{
		entries: make(map[string]*cacheSlot),
		order:   make([]string, 0, 64),
	}
}

func (c *recordCache) get(key string) (*ShuffleAlbumRecord, bool) {
	slot, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.accessCounter++
	slot.access = c.accessCounter
	if c.accessCounter > 1<<48 {
		c.normalize()
	}
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			c.order = append(c.order, key)
			break
		}
	}
	return slot.record, true
}

func (c *recordCache) put(key string, record *ShuffleAlbumRecord) {
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
		for i, k := range c.order {
			if k == key {
				c.order = append(c.order[:i], c.order[i+1:]...)
				c.order = append(c.order, key)
				break
			}
		}
		c.evictToBudget()
		return
	}

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

func (c *recordCache) evictToBudget() {
	for len(c.order) > 0 && c.totalBytes > maxCachedBytes {
		c.evictOldest()
	}
}

func (c *recordCache) normalize() {
	for _, slot := range c.entries {
		slot.access /= 2
	}
	c.accessCounter /= 2
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

// ShuffleSource implements catalog.SourceIndex for Modland.
// It reads modland.idx (a GSA file) at construction time and provides
// lazy random selection by reading only the selected album record.
type ShuffleSource struct {
	meta           *ShuffleIndexMeta
	locatorToEntry map[string]string // album name → GSA entry name
	gsa            *archive.Archive
	records        *recordCache
	mu             sync.Mutex
}

// OpenShuffleSource opens modland.idx and loads only the manifest.
// Full album records are loaded lazily on first access.
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

	gsaNames := make(map[string]struct{}, len(gsa.Entries()))
	for _, e := range gsa.Entries() {
		gsaNames[e.Name] = struct{}{}
	}
	locatorToEntry := make(map[string]string, len(meta.Entries))
	for _, entry := range meta.Entries {
		if _, ok := gsaNames[entry.Name]; !ok {
			_ = gsa.Close()
			return nil
		}
		locatorToEntry[entry.Locator] = entry.Name
	}

	return &ShuffleSource{
		meta:           &meta,
		locatorToEntry: locatorToEntry,
		gsa:            gsa,
		records:        newRecordCache(),
	}
}

func (s *ShuffleSource) Source() catalog.SourceKind { return catalog.SourceModland }

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

// RandomTrack selects one track from all Modland albums using
// track-weighted album selection, then uniform selection within
// the chosen album.
func (s *ShuffleSource) RandomTrack(rng *rand.Rand) (catalog.ShuffleTrack, error) {
	if s.meta == nil || s.meta.TrackCount == 0 {
		return catalog.ShuffleTrack{}, fmt.Errorf("modland: no tracks")
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
// album. The DirectoryKey.Locator is the album name.
func (s *ShuffleSource) RandomTrackInDirectory(key catalog.DirectoryKey, rng *rand.Rand) (catalog.ShuffleTrack, error) {
	record, err := s.loadRecord(key.Locator)
	if err != nil {
		return catalog.ShuffleTrack{}, err
	}
	return s.pickTrack(record, rng)
}

// DirectoryList returns the listing for one album.
func (s *ShuffleSource) DirectoryList(key catalog.DirectoryKey) (catalog.DirectoryListing, error) {
	record, err := s.loadRecord(key.Locator)
	if err != nil {
		return catalog.DirectoryListing{}, err
	}

	entries := make([]catalog.DirectoryEntry, len(record.Tracks))
	for i, t := range record.Tracks {
		entries[i] = catalog.DirectoryEntry{Path: t.Path, Name: t.Name, Size: t.Size}
	}
	return catalog.DirectoryListing{
		Key:         catalog.DirectoryKey{Source: catalog.SourceModland, Locator: record.Locator},
		Version:     record.Version,
		DisplayName: record.DisplayName,
		Entries:     entries,
	}, nil
}

func (s *ShuffleSource) randomTrackFromEntry(entry catalog.ManifestEntry, rng *rand.Rand) (catalog.ShuffleTrack, error) {
	record, err := s.loadRecord(entry.Locator)
	if err != nil {
		return catalog.ShuffleTrack{}, err
	}
	return s.pickTrack(record, rng)
}

func (s *ShuffleSource) pickTrack(record *ShuffleAlbumRecord, rng *rand.Rand) (catalog.ShuffleTrack, error) {
	if len(record.Tracks) == 0 {
		return catalog.ShuffleTrack{}, fmt.Errorf("modland: album %q has no tracks", record.Locator)
	}
	idx := rng.Intn(len(record.Tracks))
	t := record.Tracks[idx]
	return catalog.ShuffleTrack{
		Path:           t.Path,
		Source:         catalog.SourceModland,
		DirectoryKey:   catalog.DirectoryKey{Source: catalog.SourceModland, Locator: record.Locator},
		TrackIndex:     uint32(idx),
		TrackKey:       t.Path,
		ListingVersion: record.Version,
		AlbumName:      record.DisplayName,
	}, nil
}

// loadRecord resolves an album locator to a full record, with identity
// validation against the manifest. Records are cached under a
// memory-bounded LRU.
func (s *ShuffleSource) loadRecord(locator string) (*ShuffleAlbumRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entryName, ok := s.locatorToEntry[locator]
	if !ok {
		return nil, fmt.Errorf("modland: unknown album locator %q", locator)
	}

	if r, ok := s.records.get(entryName); ok {
		return r, nil
	}

	if s.gsa == nil {
		return nil, fmt.Errorf("modland: index not open")
	}
	data, err := s.gsa.Read(entryName)
	if err != nil {
		return nil, fmt.Errorf("modland: read record %s: %w", entryName, err)
	}
	record := &ShuffleAlbumRecord{}
	if err := json.Unmarshal(data, record); err != nil {
		return nil, fmt.Errorf("modland: decode record %s: %w", entryName, err)
	}

	var manifestEntry catalog.ManifestEntry
	for _, e := range s.meta.Entries {
		if e.Name == entryName {
			manifestEntry = e
			break
		}
	}
	if manifestEntry.Name != "" {
		if err := validateRecordIdentity(record, manifestEntry); err != nil {
			return nil, err
		}
	}

	s.records.put(entryName, record)
	return record, nil
}

// Close releases the underlying GSA file handle.
func (s *ShuffleSource) Close() error {
	if s.gsa != nil {
		return s.gsa.Close()
	}
	return nil
}
