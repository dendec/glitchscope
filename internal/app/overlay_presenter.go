package app

import (
	"fmt"

	"github.com/dendec/pmv/internal/player"
	"github.com/dendec/pmv/internal/prof"
)

type overlayView interface {
	SetStats(string)
	SetPlayback(float64, float64, float32, float64, float64, int, bool, bool)
	SetAlbums([]player.Album, int)
	UIVisible() bool
	AlbumCursor() int
	SetTrackInfos([]player.TrackInfo, int)
	SetPlaying(string, string)
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

type overlayPresenter struct {
	overlay      overlayView
	lastAlbumIdx int
}

func newOverlayPresenter(overlay overlayView) overlayPresenter {
	return overlayPresenter{overlay: overlay, lastAlbumIdx: -1}
}

func (p *overlayPresenter) selectedAlbumIndex() int {
	if p.overlay == nil || !p.overlay.UIVisible() {
		return -1
	}
	return p.overlay.AlbumCursor()
}

func (p *overlayPresenter) needsTrackInfos(currentAlbum, selectedAlbum int) bool {
	target := currentAlbum
	if selectedAlbum >= 0 {
		target = selectedAlbum
	}
	return target != p.lastAlbumIdx
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

	p.overlay.SetAlbums(playback.albums, playback.currentAlbum)
	trackAlbumIdx := playback.currentAlbum
	if selected := p.selectedAlbumIndex(); selected >= 0 {
		trackAlbumIdx = selected
	}
	if trackAlbumIdx != p.lastAlbumIdx {
		p.overlay.SetTrackInfos(playback.trackInfos, playback.trackCursor)
		p.lastAlbumIdx = trackAlbumIdx
	}

	p.overlay.SetPlaying(playback.playingAlbum, playback.playingTrack)
	p.overlay.SetLoading(playback.loading, playback.loadPercent)
}
