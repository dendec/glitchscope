package app

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/dendec/glitchscope/internal/catalog"
	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/player"
)

// fakeSourceIndex is a minimal catalog.SourceIndex for testing lazy
// selection and on-demand materialization without a real provider index.
type fakeSourceIndex struct {
	source catalog.SourceKind
	tracks []catalog.ShuffleTrack // all belong to the same directory
}

func (f *fakeSourceIndex) Source() catalog.SourceKind { return f.source }
func (f *fakeSourceIndex) TrackCount() uint64         { return uint64(len(f.tracks)) }
func (f *fakeSourceIndex) DirectoryCount() uint64     { return 1 }

func (f *fakeSourceIndex) RandomTrack(rng *rand.Rand) (catalog.ShuffleTrack, error) {
	if len(f.tracks) == 0 {
		return catalog.ShuffleTrack{}, fmt.Errorf("fake: no tracks")
	}
	return f.tracks[rng.Intn(len(f.tracks))], nil
}

func (f *fakeSourceIndex) RandomTrackInDirectory(catalog.DirectoryKey, *rand.Rand) (catalog.ShuffleTrack, error) {
	return f.RandomTrack(rand.New(rand.NewSource(1)))
}

func (f *fakeSourceIndex) DirectoryList(key catalog.DirectoryKey) (catalog.DirectoryListing, error) {
	entries := make([]catalog.DirectoryEntry, len(f.tracks))
	for i, t := range f.tracks {
		entries[i] = catalog.DirectoryEntry{Path: t.Path, Name: t.TrackKey}
	}
	return catalog.DirectoryListing{Key: key, DisplayName: "Fake Album", Entries: entries}, nil
}

func (f *fakeSourceIndex) Fingerprint() catalog.Fingerprint { return catalog.Fingerprint{} }

func fakeModlandTrack() catalog.ShuffleTrack {
	return catalog.ShuffleTrack{
		Path:         "modland:Protracker/Fake/song.mod",
		Source:       catalog.SourceModland,
		DirectoryKey: catalog.DirectoryKey{Source: catalog.SourceModland, Locator: "Protracker/Fake"},
		TrackKey:     "song.mod",
		AlbumName:    "Protracker/Fake",
	}
}

func TestAdvanceShuffleLazyUsesCoordinatorForShuffleSource(t *testing.T) {
	fake := &fakeSourceIndex{source: catalog.SourceModland, tracks: []catalog.ShuffleTrack{fakeModlandTrack()}}
	state := playbackState{
		pl:             &player.Player{},
		lib:            &player.Library{Albums: []player.Album{{Name: "Modland: Protracker/Fake", Path: "modland:Protracker/Fake", Tracks: []string{"modland:Protracker/Fake/song.mod"}}}},
		shuffleCatalog: catalog.NewShuffleCatalog(fake),
	}
	state.shuffle.rng = rand.New(rand.NewSource(1))
	// currentSource() reads the current album; point it at the modland album.
	state.lib.SelectAlbum(0)

	track, ok := state.advanceShuffleLazy(config.PlaybackSettings{ShuffleMode: config.ShuffleSource})
	if !ok {
		t.Fatal("expected the lazy coordinator path to succeed")
	}
	if track.path != "modland:Protracker/Fake/song.mod" {
		t.Fatalf("path = %q, want modland track", track.path)
	}
}

func TestAdvanceShuffleLazyFallsBackWithoutCatalog(t *testing.T) {
	state := playbackState{pl: &player.Player{}, lib: &player.Library{}}
	state.shuffle.rng = rand.New(rand.NewSource(1))

	if _, ok := state.advanceShuffleLazy(config.PlaybackSettings{ShuffleMode: config.ShuffleAll}); ok {
		t.Fatal("expected false when shuffleCatalog is nil")
	}
}

func TestAdvanceShuffleLazyMaterializesRemoteAlbum(t *testing.T) {
	fake := &fakeSourceIndex{source: catalog.SourceModland, tracks: []catalog.ShuffleTrack{fakeModlandTrack()}}
	state := playbackState{
		pl:             &player.Player{},
		lib:            &player.Library{}, // no albums materialized yet
		shuffleCatalog: catalog.NewShuffleCatalog(fake),
	}
	state.shuffle.rng = rand.New(rand.NewSource(1))

	track, ok := state.advanceShuffleLazy(config.PlaybackSettings{ShuffleMode: config.ShuffleAll})
	if !ok {
		t.Fatal("expected the lazy coordinator path to succeed")
	}
	if track.path != "modland:Protracker/Fake/song.mod" {
		t.Fatalf("path = %q, want modland track", track.path)
	}
	if len(state.lib.Albums) != 1 {
		t.Fatalf("expected the remote album to be materialized, got %d albums", len(state.lib.Albums))
	}
	if state.lib.Albums[0].Path != "modland:Protracker/Fake" {
		t.Fatalf("materialized album path = %q, want modland:Protracker/Fake", state.lib.Albums[0].Path)
	}

	// A second pick must not duplicate the already-materialized album.
	if _, ok := state.advanceShuffleLazy(config.PlaybackSettings{ShuffleMode: config.ShuffleAll}); !ok {
		t.Fatal("expected second pick to succeed")
	}
	if len(state.lib.Albums) != 1 {
		t.Fatalf("expected no duplicate album, got %d albums", len(state.lib.Albums))
	}
}

func TestAdvanceShuffleLazyUnknownSourceFallsBack(t *testing.T) {
	fake := &fakeSourceIndex{source: catalog.SourceModland, tracks: []catalog.ShuffleTrack{fakeModlandTrack()}}
	state := playbackState{
		pl:             &player.Player{},
		lib:            &player.Library{}, // currentSource() defaults to "local"
		shuffleCatalog: catalog.NewShuffleCatalog(fake),
	}
	state.shuffle.rng = rand.New(rand.NewSource(1))

	// Local has no registered source index in the fake catalog, so
	// RandomTrackFromSource must fail cleanly and fall back.
	if _, ok := state.advanceShuffleLazy(config.PlaybackSettings{ShuffleMode: config.ShuffleSource}); ok {
		t.Fatal("expected false when the current source has no index")
	}
}
