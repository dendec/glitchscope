package catalog

import (
	"errors"
	"math/rand"
	"testing"
)

func offlineTrack(source SourceKind, path string) ShuffleTrack {
	return ShuffleTrack{
		Path:         path,
		Source:       source,
		DirectoryKey: DirectoryKey{Source: source, Locator: path},
		TrackKey:     path,
	}
}

func TestOfflineProjectionPreservesSourceAndWeightsTracks(t *testing.T) {
	localTracks := []ShuffleTrack{offlineTrack(SourceLocal, "/music/local.mod")}
	remoteTracks := make([]ShuffleTrack, 9)
	for i := range remoteTracks {
		remoteTracks[i] = offlineTrack(SourceModland, "modland:remote/track"+string(rune('a'+i))+".mod")
	}
	projection := NewOfflineProjection(
		NewListSource(SourceLocal, localTracks),
		NewListSource(SourceModland, remoteTracks),
	)

	rng := rand.New(rand.NewSource(42))
	counts := map[SourceKind]int{}
	for range 10000 {
		track, err := projection.RandomTrackAll(rng)
		if err != nil {
			t.Fatal(err)
		}
		counts[track.Source]++
	}
	if counts[SourceLocal] < 700 || counts[SourceLocal] > 1300 {
		t.Fatalf("local selections = %d, want approximately 10%%", counts[SourceLocal])
	}
	if counts[SourceModland] < 8700 || counts[SourceModland] > 9300 {
		t.Fatalf("Modland selections = %d, want approximately 90%%", counts[SourceModland])
	}

	track, err := projection.RandomTrackFromSource(SourceLocal, rng)
	if err != nil || track.Source != SourceLocal || track.Path != localTracks[0].Path {
		t.Fatalf("source-restricted pick = %+v, %v", track, err)
	}
	var unavailable ErrSourceUnavailable
	if _, err := projection.RandomTrackFromSource(SourceModArchive, rng); !errors.As(err, &unavailable) {
		t.Fatalf("source-restricted unavailable error = %v", err)
	}
}

func TestListSourceNormalizesSourceIdentityAndDirectories(t *testing.T) {
	tracks := []ShuffleTrack{
		{Path: "modland:A/one.mod", DirectoryKey: DirectoryKey{Locator: "A"}, AlbumName: "A"},
		{Path: "modland:A/two.mod", DirectoryKey: DirectoryKey{Source: SourceModland, Locator: "A"}, AlbumName: "A"},
	}
	source := NewListSource(SourceModland, tracks)
	if source.TrackCount() != 2 || source.DirectoryCount() != 1 {
		t.Fatalf("source counts = (%d, %d), want (2, 1)", source.TrackCount(), source.DirectoryCount())
	}
	listing, err := source.DirectoryList(DirectoryKey{Source: SourceModland, Locator: "A"})
	if err != nil || len(listing.Entries) != 2 {
		t.Fatalf("directory listing = %+v, %v", listing, err)
	}
	picked, err := source.RandomTrack(rand.New(rand.NewSource(1)))
	if err != nil || picked.Source != SourceModland {
		t.Fatalf("picked track = %+v, %v", picked, err)
	}
}
