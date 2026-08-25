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

func TestPlaybackStatePlaySynchronizesCatalogTrack(t *testing.T) {
	path := "modarchive:http://modarchive.textfiles.com/2013/IT/B/bacter_vs_saga_musix_-_funky_junkie.it.zip"
	state := playbackState{
		pl: &player.Player{},
		lib: &player.Library{Albums: []player.Album{
			{Name: "Modland: Protracker/Asylum", Tracks: []string{"modland:asylum.mod"}},
			{Name: "ModArchive: 2013/IT/B", Tracks: []string{
				"modarchive:http://modarchive.textfiles.com/2013/IT/B/ba-piler.it.zip",
				path,
				"modarchive:http://modarchive.textfiles.com/2013/IT/B/bad_dreamz.it.zip",
			}},
		}},
	}

	state.selectLibraryTrack(path)

	if got := state.lib.CurrentAlbumIndex(); got != 1 {
		t.Fatalf("current album index = %d, want catalog album 1", got)
	}
	if got := state.lib.CurrentTrackIndex(); got != 1 {
		t.Fatalf("current track index = %d, want bacter track 1", got)
	}

	track, ok := state.advance(config.PlaybackSettings{ShuffleMode: config.ShuffleOff})
	if !ok || track.path != "modarchive:http://modarchive.textfiles.com/2013/IT/B/bad_dreamz.it.zip" {
		t.Fatalf("advance returned (%+v, %v), want bad_dreamz", track, ok)
	}
}
