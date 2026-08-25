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
	lastTrackInfos []player.TrackInfo
	playingAlbum   string
	playingPath    string
	loading        bool
	loadPercent    int64
}

func (f *fakeOverlayView) SetStats(string) {}

func (f *fakeOverlayView) SetPlayback(float64, float64, float32, float64, float64, int, bool, bool) {}

func (f *fakeOverlayView) HandleRescan(_, _ []player.Album) {}

func (f *fakeOverlayView) UIVisible() bool { return f.visible }

func (f *fakeOverlayView) AlbumCursor() int { return f.cursor }

func (f *fakeOverlayView) SetTrackInfos(infos []player.TrackInfo, _ int) {
	f.trackInfoCalls++
	f.lastTrackInfos = infos
}

func (f *fakeOverlayView) SetPlayingInfo(album, path string) {
	f.playingAlbum = album
	f.playingPath = path
}

func (f *fakeOverlayView) SetLoading(active bool, percent int64) {
	f.loading = active
	f.loadPercent = percent
}

// TestOverlayPresenterInitializesAndCachesAlbumTracks verifies that the
// first frame for an album pushes track metadata, while a second frame for
// the same album (no invalidate, no album change) does NOT push — matching
// production where playbackState.snapshot only populates trackInfos when
// needsTrackInfos returns true.
func TestOverlayPresenterInitializesAndCachesAlbumTracks(t *testing.T) {
	view := &fakeOverlayView{}
	presenter := newOverlayPresenter(view)
	if presenter.metadata.lastAlbumIdx != -1 {
		t.Fatalf("last album index = %d, want -1", presenter.metadata.lastAlbumIdx)
	}

	// First frame: new album → needsTrackInfos true → snapshot carries trackInfos.
	snap1 := overlayPlaybackSnapshot{
		hasLibrary:   true,
		albums:       []player.Album{{Name: "album"}},
		currentAlbum: 0,
		trackInfos:   []player.TrackInfo{{Path: "track"}},
		playingAlbum: "album",
		loading:      true,
		loadPercent:  42,
	}
	stats := prof.Stats{}
	presenter.Update(60, false, 0, stats, snap1)

	if view.trackInfoCalls != 1 {
		t.Fatalf("track info calls after first frame = %d, want 1", view.trackInfoCalls)
	}
	if len(view.lastTrackInfos) != 1 {
		t.Fatalf("last track infos length = %d, want 1", len(view.lastTrackInfos))
	}
	if view.playingPath != "" {
		t.Fatalf("playing path = %q, want empty", view.playingPath)
	}
	if !view.loading || view.loadPercent != 42 {
		t.Fatalf("loading = (%v, %d), want (true, 42)", view.loading, view.loadPercent)
	}

	// Second frame: same album, no invalidate → needsTrackInfos false →
	// snapshot.trackInfos is nil (production behavior). Presenter must NOT
	// overwrite the panel with nil.
	snap2 := overlayPlaybackSnapshot{
		hasLibrary:   true,
		albums:       snap1.albums,
		currentAlbum: 0,
		trackInfos:   nil, // production: snapshot skips trackInfos
		playingAlbum: "album",
		loading:      true,
		loadPercent:  42,
	}
	presenter.Update(60, false, 0, stats, snap2)

	if view.trackInfoCalls != 1 {
		t.Fatalf("track info calls after second frame = %d, want 1 (no overwrite)", view.trackInfoCalls)
	}
	// Panel data from first frame must be preserved.
	if len(view.lastTrackInfos) != 1 || view.lastTrackInfos[0].Path != "track" {
		t.Fatalf("panel data corrupted after second frame: %#v", view.lastTrackInfos)
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
	if presenter.metadata.lastAlbumIdx != 1 {
		t.Fatalf("last album index = %d, want 1", presenter.metadata.lastAlbumIdx)
	}
	if view.trackInfoCalls != 1 {
		t.Fatalf("track info calls = %d, want 1", view.trackInfoCalls)
	}
}

// TestOverlayPresenterCursorChangeKeepsTrackInfos verifies that moving the
// track cursor within the same album does not clear the right-panel data:
// a snapshot without fresh trackInfos must not overwrite the panel.
func TestOverlayPresenterCursorChangeKeepsTrackInfos(t *testing.T) {
	view := &fakeOverlayView{visible: true, cursor: 0}
	presenter := newOverlayPresenter(view)

	snap1 := overlayPlaybackSnapshot{
		hasLibrary:   true,
		albums:       []player.Album{{Name: "album", Tracks: []string{"a", "b"}}},
		currentAlbum: 0,
		currentTrack: 0,
		trackInfos:   []player.TrackInfo{{Path: "a", Cached: true}, {Path: "b"}},
	}
	presenter.Update(60, false, 0, prof.Stats{}, snap1)
	if view.trackInfoCalls != 1 {
		t.Fatalf("initial track info calls = %d, want 1", view.trackInfoCalls)
	}

	// Same album, cursor moved to track 1, no fresh metadata in snapshot
	// (album unchanged, no load transition).
	snap2 := overlayPlaybackSnapshot{
		hasLibrary:   true,
		albums:       snap1.albums,
		currentAlbum: 0,
		currentTrack: 1,
		// trackInfos intentionally nil — snapshot skips it when the album
		// didn't change and no download finished.
	}
	presenter.Update(60, false, 0, prof.Stats{}, snap2)

	if view.trackInfoCalls != 1 {
		t.Fatalf("track info calls after cursor change = %d, want 1 (no clear)", view.trackInfoCalls)
	}
	if len(view.lastTrackInfos) != 2 || !view.lastTrackInfos[0].Cached {
		t.Fatalf("panel data was cleared or corrupted: %#v", view.lastTrackInfos)
	}
}

// TestOverlayPresenterInvalidateRefreshesAfterLoad verifies that a load that
// completes without the presenter ever observing loading=true still triggers a
// metadata refresh via invalidateTrackInfos (called from run_loop on a
// successful CheckPending).
func TestOverlayPresenterInvalidateRefreshesAfterLoad(t *testing.T) {
	view := &fakeOverlayView{visible: true, cursor: 0}
	presenter := newOverlayPresenter(view)

	snap1 := overlayPlaybackSnapshot{
		hasLibrary:   true,
		albums:       []player.Album{{Name: "album", Tracks: []string{"catalog:track"}}},
		currentAlbum: 0,
		currentTrack: 0,
		trackInfos:   []player.TrackInfo{{Path: "catalog:track", Cached: false}},
		loading:      false, // presenter never sees loading=true
	}
	presenter.Update(60, false, 0, prof.Stats{}, snap1)
	if view.trackInfoCalls != 1 {
		t.Fatalf("initial track info calls = %d, want 1", view.trackInfoCalls)
	}

	// Load finished behind the scenes → run_loop invalidates metadata.
	presenter.invalidateTrackInfos()
	if !presenter.needsTrackInfos(0) {
		t.Fatal("needsTrackInfos should request refresh after invalidateTrackInfos")
	}

	// Fresh metadata arrives next frame and is pushed.
	snap2 := overlayPlaybackSnapshot{
		hasLibrary:   true,
		albums:       snap1.albums,
		currentAlbum: 0,
		currentTrack: 0,
		trackInfos:   []player.TrackInfo{{Path: "catalog:track", Cached: true, Duration: 120}},
		loading:      false,
	}
	presenter.Update(60, false, 0, prof.Stats{}, snap2)
	if view.trackInfoCalls != 2 {
		t.Fatalf("track info calls after invalidate = %d, want 2", view.trackInfoCalls)
	}
	if len(view.lastTrackInfos) != 1 || !view.lastTrackInfos[0].Cached {
		t.Fatalf("cached metadata not pushed after invalidate: %#v", view.lastTrackInfos)
	}
	// Dirty flag is cleared after a successful push.
	if presenter.needsTrackInfos(0) {
		t.Fatal("needsTrackInfos should not keep requesting after metadata pushed")
	}
}

// TestOverlayPresenterLoadingTransitionDoesNotRefresh verifies that the
// loading true→false transition alone does not trigger a metadata refresh.
// The only refresh triggers are an album change and invalidateTrackInfos
// (called from run_loop when CheckPending accepts a result) — the loading
// level is not observed for this decision anymore.
func TestOverlayPresenterLoadingTransitionDoesNotRefresh(t *testing.T) {
	view := &fakeOverlayView{visible: true, cursor: 0}
	presenter := newOverlayPresenter(view)

	// First frame: album 0, loading (download in flight), metadata not yet
	// cached.
	snap1 := overlayPlaybackSnapshot{
		hasLibrary:   true,
		albums:       []player.Album{{Name: "album", Tracks: []string{"catalog:track"}}},
		currentAlbum: 0,
		currentTrack: 0,
		trackInfos:   []player.TrackInfo{{Path: "catalog:track", Cached: false}},
		loading:      true,
	}
	presenter.Update(60, false, 0, prof.Stats{}, snap1)
	if view.trackInfoCalls != 1 {
		t.Fatalf("initial track info calls = %d, want 1", view.trackInfoCalls)
	}

	// Loading finished, album unchanged, no invalidate → no refresh request.
	if presenter.needsTrackInfos(0) {
		t.Fatal("loading transition alone must not request a metadata refresh")
	}
	// invalidateTrackInfos is the primary trigger for a completed load.
	presenter.invalidateTrackInfos()
	if !presenter.needsTrackInfos(0) {
		t.Fatal("needsTrackInfos should request refresh after invalidateTrackInfos")
	}
}
