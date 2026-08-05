package app

import (
	"fmt"

	"github.com/dendec/pmv/internal/config"
	"github.com/dendec/pmv/internal/prof"
	"github.com/dendec/pmv/internal/ui"
)

type overlayPresenter struct {
	overlay      *ui.Overlay
	lastAlbumIdx int
}

func newOverlayPresenter(overlay *ui.Overlay) overlayPresenter {
	return overlayPresenter{overlay: overlay, lastAlbumIdx: -1}
}

func (p *overlayPresenter) Update(fps float64, settings *config.Settings, renderScaleExplicit bool, playback *playbackState, collector *prof.Collector) {
	if p.overlay == nil {
		return
	}

	s := collector.ReadStats()
	line := fmt.Sprintf("FPS:%.0f MEM:%.0fM CPU:%.0f%%", fps, s.MemKB/1024, s.CPUPct)
	if s.GPUOK {
		line += fmt.Sprintf(" GPU:%.0fM %.0f%%", s.GPUMemKB/1024, s.GPUUtilPct)
	}
	if settings.Graphics.Adaptive && !renderScaleExplicit {
		line += fmt.Sprintf(" %dp", settings.Graphics.RenderHeight)
	}
	p.overlay.SetStats(line)

	if playback.pl != nil {
		p.overlay.SetPlayback(playback.pl.Position(), playback.pl.Duration(), playback.pl.SampleRate(), playback.pl.Bitrate(), playback.pl.BPM(), playback.pl.Channels(), playback.pl.IsPaused(), playback.pl.IsTracker())
	}
	if playback.lib == nil {
		return
	}

	p.overlay.SetAlbums(playback.lib.Albums, playback.lib.CurrentAlbumIndex())
	trackAlbumIdx := playback.lib.CurrentAlbumIndex()
	if p.overlay.UIVisible() {
		trackAlbumIdx = p.overlay.AlbumCursor()
	}
	if trackAlbumIdx != p.lastAlbumIdx {
		trackCursor := 0
		if trackAlbumIdx == playback.lib.CurrentAlbumIndex() {
			trackCursor = playback.lib.CurrentTrackIndex()
		}
		p.overlay.SetTrackInfos(playback.lib.GetAlbumTracks(trackAlbumIdx), trackCursor)
		p.lastAlbumIdx = trackAlbumIdx
	}

	if playback.pl == nil {
		return
	}
	curTrack := playback.pl.TrackPath()
	if !playback.pl.IsValidVoice() && !playback.pl.Loading() {
		curTrack = ""
	}
	p.overlay.SetPlaying(playback.lib.CurrentAlbum().Name, curTrack)
	active, percent := playback.pl.LoadProgress()
	p.overlay.SetLoading(active, percent)
}
