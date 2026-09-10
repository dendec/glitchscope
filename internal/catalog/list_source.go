package catalog

import (
	"fmt"
	"math/rand"
	"path"
)

// ListSource is a compact source index for a known set of tracks. It is used
// by offline projections for cached remote tracks; it is not a persisted
// provider catalog and does not change the track's original SourceKind.
type ListSource struct {
	source       SourceKind
	tracks       []ShuffleTrack
	byDirectory  map[DirectoryKey][]int
	listingNames map[DirectoryKey]string
}

// NewListSource creates an in-memory source index from a bounded track set.
// The caller owns the source-specific filtering; every track must already be
// known to be playable from the local cache.
func NewListSource(source SourceKind, tracks []ShuffleTrack) *ListSource {
	copyTracks := append([]ShuffleTrack(nil), tracks...)
	s := &ListSource{
		source:       source,
		tracks:       copyTracks,
		byDirectory:  make(map[DirectoryKey][]int),
		listingNames: make(map[DirectoryKey]string),
	}
	for i := range copyTracks {
		key := copyTracks[i].DirectoryKey
		if key.Source != source {
			key.Source = source
			copyTracks[i].DirectoryKey = key
		}
		copyTracks[i].Source = source
		s.tracks[i] = copyTracks[i]
		s.byDirectory[key] = append(s.byDirectory[key], i)
		if _, ok := s.listingNames[key]; !ok {
			s.listingNames[key] = copyTracks[i].AlbumName
		}
	}
	return s
}

func (s *ListSource) Source() SourceKind { return s.source }
func (s *ListSource) TrackCount() uint64 { return uint64(len(s.tracks)) }
func (s *ListSource) DirectoryCount() uint64 {
	return uint64(len(s.byDirectory))
}

func (s *ListSource) Fingerprint() Fingerprint {
	return Fingerprint{
		TrackCount:     uint64(len(s.tracks)),
		DirectoryCount: uint64(len(s.byDirectory)),
	}
}

// RandomTrack selects uniformly from the cached track set.
func (s *ListSource) RandomTrack(rng *rand.Rand) (ShuffleTrack, error) {
	if len(s.tracks) == 0 {
		return ShuffleTrack{}, fmt.Errorf("catalog: cached source %s empty", s.source)
	}
	return s.tracks[rng.Intn(len(s.tracks))], nil
}

// RandomTrackInDirectory selects uniformly from one cached directory.
func (s *ListSource) RandomTrackInDirectory(key DirectoryKey, rng *rand.Rand) (ShuffleTrack, error) {
	indices := s.byDirectory[key]
	if len(indices) == 0 {
		return ShuffleTrack{}, fmt.Errorf("catalog: cached directory %q empty", key.Locator)
	}
	return s.tracks[indices[rng.Intn(len(indices))]], nil
}

// DirectoryList returns the cached entries belonging to one directory.
// It is primarily a diagnostic/test seam; remote materialization uses the
// authoritative provider index so navigation retains its full listing.
func (s *ListSource) DirectoryList(key DirectoryKey) (DirectoryListing, error) {
	indices := s.byDirectory[key]
	if len(indices) == 0 {
		return DirectoryListing{}, fmt.Errorf("catalog: cached directory %q empty", key.Locator)
	}
	entries := make([]DirectoryEntry, 0, len(indices))
	for _, index := range indices {
		t := s.tracks[index]
		entries = append(entries, DirectoryEntry{Path: t.Path, Name: path.Base(t.Path)})
	}
	return DirectoryListing{
		Key:         key,
		DisplayName: s.listingNames[key],
		Entries:     entries,
	}, nil
}
