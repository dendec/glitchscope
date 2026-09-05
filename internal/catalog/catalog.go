package catalog

import (
	"fmt"
	"math/rand"
)

// ShuffleCatalog is the runtime coordinator over the three source indexes.
// It is not a fourth full catalog — it stores handles, counts, and selection
// metadata only. The actual track data lives in provider-specific SourceIndex
// implementations backed by persistent .idx files.
type ShuffleCatalog struct {
	indexes map[SourceKind]SourceIndex
}

// NewShuffleCatalog creates a coordinator from the provided source indexes.
// Nil or empty indexes are skipped; they will not participate in selection.
func NewShuffleCatalog(indexes ...SourceIndex) *ShuffleCatalog {
	cat := &ShuffleCatalog{
		indexes: make(map[SourceKind]SourceIndex, len(indexes)),
	}
	for _, idx := range indexes {
		if idx != nil {
			cat.indexes[idx.Source()] = idx
		}
	}
	return cat
}

// Available returns the list of non-empty, healthy source indexes.
func (c *ShuffleCatalog) Available() []SourceIndex {
	var result []SourceIndex
	for _, idx := range c.indexes {
		if idx.TrackCount() > 0 {
			result = append(result, idx)
		}
	}
	return result
}

// TotalTrackCount returns the sum of playable tracks across all available sources.
func (c *ShuffleCatalog) TotalTrackCount() uint64 {
	var total uint64
	for _, idx := range c.indexes {
		total += idx.TrackCount()
	}
	return total
}

// RandomTrackAll selects a track from all available sources using
// track-weighted source selection: probability proportional to each
// source's playable track count, then directory-weighted, then uniform
// within the directory.
func (c *ShuffleCatalog) RandomTrackAll(rng *rand.Rand) (ShuffleTrack, error) {
	available := c.Available()
	if len(available) == 0 {
		return ShuffleTrack{}, ErrNoSourceAvailable
	}

	// Track-weighted source selection.
	total := c.TotalTrackCount()
	if total == 0 {
		return ShuffleTrack{}, ErrNoSourceAvailable
	}
	pick := rng.Uint64() % total
	var cumulative uint64
	var selected SourceIndex
	for _, idx := range available {
		cumulative += idx.TrackCount()
		if pick < cumulative {
			selected = idx
			break
		}
	}
	if selected == nil {
		selected = available[len(available)-1]
	}

	return selected.RandomTrack(rng)
}

// RandomTrackFromSource selects a track from a specific source.
func (c *ShuffleCatalog) RandomTrackFromSource(source SourceKind, rng *rand.Rand) (ShuffleTrack, error) {
	idx, ok := c.indexes[source]
	if !ok || idx.TrackCount() == 0 {
		return ShuffleTrack{}, ErrSourceUnavailable{Source: source}
	}
	return idx.RandomTrack(rng)
}

// SourceIndex returns the index for a specific source, or nil if not registered.
func (c *ShuffleCatalog) SourceIndex(source SourceKind) SourceIndex {
	return c.indexes[source]
}

// --- Errors ---

// ErrNoSourceAvailable is returned when no source has playable tracks.
var ErrNoSourceAvailable = fmt.Errorf("catalog: no source available")

// ErrSourceUnavailable is returned when a specific source is missing or empty.
type ErrSourceUnavailable struct {
	Source SourceKind
}

func (e ErrSourceUnavailable) Error() string {
	return fmt.Sprintf("catalog: source %s unavailable", e.Source)
}
