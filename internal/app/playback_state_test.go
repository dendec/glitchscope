package app

import (
	"testing"

	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/player"
)

func TestPlaybackStateAdvancesWithinVirtualPlaylist(t *testing.T) {
	state := playbackState{
		pl:            &player.Player{},
		lib:           &player.Library{},
		playlist:      []string{"modarchive:first.mod", "modarchive:second.mod"},
		playlistIdx:   0,
		playlistAlbum: "ModArchive: folder",
	}

	track, ok := state.advance(config.PlaybackSettings{ShuffleMode: config.ShuffleOff})
	if !ok {
		t.Fatal("advance returned false, want next virtual track")
	}
	if track.path != "modarchive:second.mod" || track.album != "ModArchive: folder" {
		t.Fatalf("advance returned %+v, want second track from the same album", track)
	}

	if _, ok := state.advance(config.PlaybackSettings{ShuffleMode: config.ShuffleOff}); ok {
		t.Fatal("advance returned a track after the virtual playlist ended")
	}
}

func TestPlaybackStateManualNavigationUsesActivePlaylist(t *testing.T) {
	state := playbackState{
		pl:            &player.Player{},
		lib:           &player.Library{},
		playlist:      []string{"folder:first.mod", "folder:second.mod", "folder:third.mod"},
		playlistIdx:   0,
		playlistAlbum: "folder",
	}

	path, album, ok := state.nextTrack()
	if !ok || path != "folder:second.mod" || album != "folder" {
		t.Fatalf("nextTrack returned (%q, %q, %v), want second playlist track", path, album, ok)
	}

	path, album, ok = state.previousTrack()
	if !ok || path != "folder:first.mod" || album != "folder" {
		t.Fatalf("previousTrack returned (%q, %q, %v), want first playlist track", path, album, ok)
	}
}
