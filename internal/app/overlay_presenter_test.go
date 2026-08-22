package app

import (
	"testing"

	"github.com/dendec/glitchscope/internal/player"
	"github.com/dendec/glitchscope/internal/prof"
)

type fakeOverlayView struct {
	visible        bool
	cursor         int
	trackInfoCalls int
	playingTrack   string
	loading        bool
	loadPercent    int64
}

func (f *fakeOverlayView) SetStats(string) {}

func (f *fakeOverlayView) SetPlayback(float64, float64, float32, float64, float64, int, bool, bool) {}

func (f *fakeOverlayView) SetAlbums([]player.Album, int) {}

func (f *fakeOverlayView) UIVisible() bool { return f.visible }

func (f *fakeOverlayView) AlbumCursor() int { return f.cursor }

func (f *fakeOverlayView) SetTrackInfos([]player.TrackInfo, int) { f.trackInfoCalls++ }

func (f *fakeOverlayView) SetPlaying(_, track string) { f.playingTrack = track }

func (f *fakeOverlayView) SetLoading(active bool, percent int64) {
	f.loading = active
	f.loadPercent = percent
}

func TestOverlayPresenterInitializesAndCachesAlbumTracks(t *testing.T) {
	view := &fakeOverlayView{}
	presenter := newOverlayPresenter(view)
	if presenter.lastAlbumIdx != -1 {
		t.Fatalf("last album index = %d, want -1", presenter.lastAlbumIdx)
	}

	snapshot := overlayPlaybackSnapshot{
		hasLibrary:   true,
		albums:       []player.Album{{Name: "album"}},
		currentAlbum: 0,
		trackInfos:   []player.TrackInfo{{Path: "track"}},
		playingAlbum: "album",
		loading:      true,
		loadPercent:  42,
	}
	stats := prof.Stats{}
	presenter.Update(60, false, 0, stats, snapshot)
	presenter.Update(60, false, 0, stats, snapshot)

	if view.trackInfoCalls != 1 {
		t.Fatalf("track info calls = %d, want 1", view.trackInfoCalls)
	}
	if view.playingTrack != "" {
		t.Fatalf("playing track = %q, want hidden track", view.playingTrack)
	}
	if !view.loading || view.loadPercent != 42 {
		t.Fatalf("loading = (%v, %d), want (true, 42)", view.loading, view.loadPercent)
	}
}

func TestOverlayPresenterRefreshesTracksForUIAlbum(t *testing.T) {
	view := &fakeOverlayView{visible: true, cursor: 1}
	presenter := newOverlayPresenter(view)
	snapshot := overlayPlaybackSnapshot{
		hasLibrary:   true,
		albums:       []player.Album{{Name: "album 0"}, {Name: "album 1"}},
		currentAlbum: 0,
		trackInfos:   []player.TrackInfo{{Path: "album 1 track"}},
		playingAlbum: "album 0",
	}

	presenter.Update(60, false, 0, prof.Stats{}, snapshot)
	if presenter.lastAlbumIdx != 1 {
		t.Fatalf("last album index = %d, want 1", presenter.lastAlbumIdx)
	}
	if view.trackInfoCalls != 1 {
		t.Fatalf("track info calls = %d, want 1", view.trackInfoCalls)
	}
}
