package app

import (
	"testing"

	"github.com/dendec/glitchscope/internal/radio"
)

func TestResolveRadioPlayingInfoOverridesStaleLibraryAlbum(t *testing.T) {
	client := radio.New(t.TempDir())
	t.Cleanup(client.Close)
	station := radio.Station{
		StationUUID: "station-id",
		Name:        "102.7 KIIS FM",
		URL:         "https://example.test/stream",
	}
	client.AddStations([]radio.Station{station})
	application := App{radio: client}

	got := application.resolveRadioPlayingInfo(overlayPlaybackSnapshot{
		playingAlbum: "Modland: Asylum/Basehead",
		playingTrack: station.Path(),
	})
	if got.playingAlbum != station.Name {
		t.Fatalf("radio playing album = %q, want %q", got.playingAlbum, station.Name)
	}
}

func TestResolveRadioPlayingInfoLeavesRegularTrackAlone(t *testing.T) {
	application := App{}
	want := overlayPlaybackSnapshot{playingAlbum: "Album", playingTrack: "/music/song.mp3"}
	if got := application.resolveRadioPlayingInfo(want); got.playingAlbum != want.playingAlbum || got.playingTrack != want.playingTrack {
		t.Fatalf("regular playing info changed: %#v", got)
	}
}
