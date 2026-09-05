package player

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"path/filepath"

	"github.com/dendec/glitchscope/internal/catalog"
)

// LocalShuffleSource implements catalog.SourceIndex over the local
// filesystem albums already scanned into player.Library. It holds
// slice references to the Album list Library already owns rather than
// duplicating track data (per shuffle-optimization instructions §4.1).
type LocalShuffleSource struct {
	albums     []Album
	pathToIdx  map[string]int
	trackCount uint64
	fp         catalog.Fingerprint
}

// ScanFingerprint computes a content-based fingerprint from a successfully
// (StatusOK) scanned album list: it changes whenever tracks are added,
// removed, renamed, or reordered. Callers must only derive this from an OK
// scan; a Partial/Failed scan must keep using the previous fingerprint.
func ScanFingerprint(albums []Album) string {
	h := sha256.New()
	real := RealAlbumsOnly(albums)
	for _, a := range real {
		fmt.Fprintf(h, "album:%s\n", a.Path)
		for _, t := range a.Tracks {
			fmt.Fprintf(h, "track:%s\n", t)
		}
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// NewLocalShuffleSource builds a shuffle index from an already-scanned
// album list. Callers must only pass albums from a successful (OK)
// filesystem scan; a Partial/Failed scan must keep using the previous
// source instead of calling this constructor.
func NewLocalShuffleSource(albums []Album, scanFingerprint string) *LocalShuffleSource {
	real := RealAlbumsOnly(albums)
	pathToIdx := make(map[string]int, len(real))
	var total uint64
	for i, a := range real {
		pathToIdx[a.Path] = i
		total += uint64(len(a.Tracks))
	}
	return &LocalShuffleSource{
		albums:     real,
		pathToIdx:  pathToIdx,
		trackCount: total,
		fp: catalog.Fingerprint{
			SourceHash:     scanFingerprint,
			TrackCount:     total,
			DirectoryCount: uint64(len(real)),
		},
	}
}

func (s *LocalShuffleSource) Source() catalog.SourceKind       { return catalog.SourceLocal }
func (s *LocalShuffleSource) TrackCount() uint64               { return s.trackCount }
func (s *LocalShuffleSource) DirectoryCount() uint64           { return uint64(len(s.albums)) }
func (s *LocalShuffleSource) Fingerprint() catalog.Fingerprint { return s.fp }

// rejectSampleUint64 returns a uniform random value in [0, max) using
// rejection sampling to avoid modulo bias.
func rejectSampleUint64(rng *rand.Rand, maxVal uint64) uint64 {
	if maxVal == 0 {
		return 0
	}
	limit := ^uint64(0) - (^uint64(0) % maxVal) //nolint:gocritic // clear intent
	for {
		v := rng.Uint64()
		if v < limit {
			return v % maxVal
		}
	}
}

// RandomTrack selects one track from all local albums using
// track-weighted album selection, then uniform selection within the
// chosen album.
func (s *LocalShuffleSource) RandomTrack(rng *rand.Rand) (catalog.ShuffleTrack, error) {
	if s.trackCount == 0 {
		return catalog.ShuffleTrack{}, fmt.Errorf("player: no local tracks")
	}
	pick := rejectSampleUint64(rng, s.trackCount)
	var cumulative uint64
	for _, a := range s.albums {
		n := uint64(len(a.Tracks))
		if pick < cumulative+n {
			return s.pickTrack(a, int(pick-cumulative)), nil
		}
		cumulative += n
	}
	last := s.albums[len(s.albums)-1]
	return s.pickTrack(last, rng.Intn(len(last.Tracks))), nil
}

// RandomTrackInDirectory selects one track uniformly within a specific
// local directory. key.Locator is the album's full directory path.
func (s *LocalShuffleSource) RandomTrackInDirectory(key catalog.DirectoryKey, rng *rand.Rand) (catalog.ShuffleTrack, error) {
	a, err := s.findAlbum(key.Locator)
	if err != nil {
		return catalog.ShuffleTrack{}, err
	}
	if len(a.Tracks) == 0 {
		return catalog.ShuffleTrack{}, fmt.Errorf("player: directory %q has no tracks", key.Locator)
	}
	return s.pickTrack(a, rng.Intn(len(a.Tracks))), nil
}

// DirectoryList returns the listing for one local directory.
func (s *LocalShuffleSource) DirectoryList(key catalog.DirectoryKey) (catalog.DirectoryListing, error) {
	a, err := s.findAlbum(key.Locator)
	if err != nil {
		return catalog.DirectoryListing{}, err
	}
	entries := make([]catalog.DirectoryEntry, len(a.Tracks))
	for i, t := range a.Tracks {
		entries[i] = catalog.DirectoryEntry{Path: t, Name: filepath.Base(t)}
	}
	return catalog.DirectoryListing{
		Key:         catalog.DirectoryKey{Source: catalog.SourceLocal, Locator: a.Path},
		Version:     albumListingVersion(a),
		DisplayName: a.Name,
		Entries:     entries,
	}, nil
}

// albumListingVersion computes a content-based ListingVersion from an
// album's track paths. It changes whenever tracks are added, removed,
// or reordered.
func albumListingVersion(a Album) catalog.ListingVersion {
	h := sha256.New()
	fmt.Fprintf(h, "path:%s\n", a.Path)
	for _, t := range a.Tracks {
		fmt.Fprintf(h, "track:%s\n", t)
	}
	return catalog.ListingVersion(hex.EncodeToString(h.Sum(nil)[:16]))
}

func (s *LocalShuffleSource) findAlbum(locator string) (Album, error) {
	idx, ok := s.pathToIdx[locator]
	if !ok {
		return Album{}, fmt.Errorf("player: unknown local directory %q", locator)
	}
	return s.albums[idx], nil
}

func (s *LocalShuffleSource) pickTrack(a Album, idx int) catalog.ShuffleTrack {
	path := a.Tracks[idx]
	return catalog.ShuffleTrack{
		Path:           path,
		Source:         catalog.SourceLocal,
		DirectoryKey:   catalog.DirectoryKey{Source: catalog.SourceLocal, Locator: a.Path},
		TrackIndex:     uint32(idx),
		TrackKey:       path,
		ListingVersion: albumListingVersion(a),
		AlbumName:      a.Name,
	}
}
