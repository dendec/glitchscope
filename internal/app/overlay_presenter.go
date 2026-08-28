package app

import (
	"fmt"
	"unsafe"

	"github.com/dendec/glitchscope/internal/player"
	"github.com/dendec/glitchscope/internal/prof"
)

type overlayView interface {
	SetStats(string)
	SetPlayback(float64, float64, float32, float64, float64, int, bool, bool)
	HandleRescan(prevLocal, nextLocal []player.Album)
	UIVisible() bool
	AlbumCursor() int
	SetTrackInfos([]player.TrackInfo, int)
	SetPlayingInfo(album, path string)
	SetLoading(bool, int64)
}

type overlayPlaybackSnapshot struct {
	position     float64
	duration     float64
	sampleRate   float32
	bitrate      float64
	bpm          float64
	channels     int
	paused       bool
	tracker      bool
	albums       []player.Album
	currentAlbum int
	currentTrack int
	trackInfos   []player.TrackInfo
	trackCursor  int
	playingAlbum string
	playingTrack string
	loading      bool
	loadPercent  int64
	hasPlayer    bool
	hasLibrary   bool
}

// rescanDetector tracks the previous frame's local album list so the
// presenter can notify the overlay when a rescan changed it. One concern of
// overlayPresenter, kept as its own type.
type rescanDetector struct {
	prevLocal   []player.Album
	albumsData  *player.Album
	albumsCount int
}

// handle returns local album lists only when the library snapshot changed.
func (d *rescanDetector) handle(albums []player.Album) (prev, next []player.Album, changed bool) {
	data := unsafe.SliceData(albums)
	if data == d.albumsData && len(albums) == d.albumsCount {
		return nil, nil, false
	}
	d.albumsData = data
	d.albumsCount = len(albums)
	next = player.RealAlbumsOnly(albums)
	prev = d.prevLocal
	d.prevLocal = next
	return prev, next, true
}

// metadataTracker decides when fresh track metadata must be re-read: on the
// first frame for an album, when the selected album changes, and when a
// background load completed (invalidate). One concern of overlayPresenter,
// kept as its own type.
type metadataTracker struct {
	lastAlbumIdx int
	dirty        bool // a load finished since the last metadata push
}

// needsRefresh reports whether metadata must be re-read for albumIdx.
func (t *metadataTracker) needsRefresh(albumIdx int) bool {
	return t.dirty || albumIdx != t.lastAlbumIdx
}

// observe records which album the metadata now belongs to.
func (t *metadataTracker) observe(albumIdx int) {
	t.lastAlbumIdx = albumIdx
}

// invalidate marks that metadata must be re-read on the next frame.
func (t *metadataTracker) invalidate() {
	t.dirty = true
}

// pushed clears the dirty flag after fresh metadata was delivered.
func (t *metadataTracker) pushed() {
	t.dirty = false
}

// overlayPresenter feeds the overlay from per-frame state. It has three
// distinct concerns, each delegated to its own small type: rescan detection
// (rescanDetector), track-metadata invalidation (metadataTracker), and
// snapshot plumbing (this type).
type overlayPresenter struct {
	overlay  overlayView     // snapshot plumbing
	rescan   rescanDetector  // local-album rescan detection
	metadata metadataTracker // track-metadata invalidation
}

func newOverlayPresenter(overlay overlayView) overlayPresenter {
	return overlayPresenter{overlay: overlay, metadata: metadataTracker{lastAlbumIdx: -1}}
}

func (p *overlayPresenter) selectedAlbumIndex() int {
	if p.overlay == nil || !p.overlay.UIVisible() {
		return -1
	}
	return p.overlay.AlbumCursor()
}

// invalidateTrackInfos marks that track metadata must be re-read on the next
// frame. Called when a background load completes (e.g. a catalog download
// finished), so the panel picks up newly available Cached/duration/comment.
func (p *overlayPresenter) invalidateTrackInfos() {
	p.metadata.invalidate()
}

// needsTrackInfos reports whether the snapshot must include fresh track
// metadata for this frame. True when the target album changed since the last
// push, or when a load finished since then (invalidateTrackInfos). The dirty
// flag is cleared once fresh metadata is pushed in Update.
func (p *overlayPresenter) needsTrackInfos(albumIdx int) bool {
	return p.metadata.needsRefresh(albumIdx)
}

func (p *overlayPresenter) Update(fps float64, adaptive bool, renderHeight int, stats prof.Stats, playback overlayPlaybackSnapshot) {
	if p.overlay == nil {
		return
	}

	line := fmt.Sprintf("FPS:%.0f MEM:%.0fM CPU:%.0f%%", fps, stats.MemKB/1024, stats.CPUPct)
	if stats.GPUOK {
		line += fmt.Sprintf(" GPU:%.0fM %.0f%%", stats.GPUMemKB/1024, stats.GPUUtilPct)
	}
	if adaptive {
		line += fmt.Sprintf(" %dp", renderHeight)
	}
	p.overlay.SetStats(line)

	if playback.hasPlayer {
		p.overlay.SetPlayback(playback.position, playback.duration, playback.sampleRate, playback.bitrate, playback.bpm, playback.channels, playback.paused, playback.tracker)
	}
	if !playback.hasLibrary {
		return
	}

	// Detect local album rescan only when the library snapshot changed. Most
	// frames reuse the same backing array, so avoid filtering and allocating.
	if prev, next, changed := p.rescan.handle(playback.albums); changed {
		p.overlay.HandleRescan(prev, next)
	}

	// Track which album the metadata belongs to, so needsTrackInfos can
	// detect album switches on the next frame.
	trackAlbumIdx := playback.currentAlbum
	if selected := p.selectedAlbumIndex(); selected >= 0 {
		trackAlbumIdx = selected
	}

	// Push track metadata only when the snapshot carries fresh data (album
	// changed, or a load finished). A nil snapshot must never clear the panel
	// data — the overlay keeps its own track cursor between pushes.
	if playback.trackInfos != nil {
		p.overlay.SetTrackInfos(playback.trackInfos, playback.trackCursor)
		p.metadata.pushed()
	}
	p.metadata.observe(trackAlbumIdx)

	p.overlay.SetPlayingInfo(playback.playingAlbum, playback.playingTrack)
	p.overlay.SetLoading(playback.loading, playback.loadPercent)
}
