package app

import (
	"math/rand"

	"github.com/dendec/pmv/internal/config"
	"github.com/dendec/pmv/internal/player"
)

func (s *playbackState) snapshot(trackAlbumIdx int) overlayPlaybackSnapshot {
	snapshot := overlayPlaybackSnapshot{}
	if s.pl != nil {
		snapshot.position = s.pl.Position()
		snapshot.duration = s.pl.Duration()
		snapshot.sampleRate = s.pl.SampleRate()
		snapshot.bitrate = s.pl.Bitrate()
		snapshot.bpm = s.pl.BPM()
		snapshot.channels = s.pl.Channels()
		snapshot.paused = s.pl.IsPaused()
		snapshot.tracker = s.pl.IsTracker()
		snapshot.hasPlayer = true
	}
	if s.lib == nil {
		return snapshot
	}
	snapshot.albums = s.lib.Albums
	snapshot.currentAlbum = s.lib.CurrentAlbumIndex()
	snapshot.currentTrack = s.lib.CurrentTrackIndex()
	if trackAlbumIdx < 0 {
		trackAlbumIdx = snapshot.currentAlbum
	}
	snapshot.trackInfos = s.lib.GetAlbumTracks(trackAlbumIdx)
	if trackAlbumIdx == snapshot.currentAlbum {
		snapshot.trackCursor = snapshot.currentTrack
	}
	if s.pl != nil {
		snapshot.playingAlbum = s.lib.CurrentAlbum().Name
		snapshot.playingTrack = s.pl.TrackPath()
		if !s.pl.IsValidVoice() && !s.pl.Loading() {
			snapshot.playingTrack = ""
		}
		snapshot.loading, snapshot.loadPercent = s.pl.LoadProgress()
	}
	snapshot.hasLibrary = true
	return snapshot
}

type shuffleState struct {
	order    []trackRef
	idx      int
	albumIdx int
}

type trackRef struct {
	path     string
	album    string
	albumIdx int
	trackIdx int
}

type playbackState struct {
	pl      *player.Player
	lib     *player.Library
	shuffle shuffleState
}

func (s *playbackState) play(path string) bool {
	if s.pl == nil {
		return false
	}
	s.pl.PlayFileAsync(path)
	return true
}

func (s *playbackState) previousAlbum() (string, string, bool) {
	if s.lib == nil || s.pl == nil {
		return "", "", false
	}
	path := s.lib.AlbumPrev()
	if path == "" {
		return "", "", false
	}
	return path, s.lib.CurrentAlbum().Name, true
}

func (s *playbackState) nextAlbum() (string, string, bool) {
	if s.lib == nil || s.pl == nil {
		return "", "", false
	}
	path := s.lib.AlbumNext()
	if path == "" {
		return "", "", false
	}
	return path, s.lib.CurrentAlbum().Name, true
}

func (s *playbackState) previousTrack() (string, string, bool) {
	if s.lib == nil || s.pl == nil {
		return "", "", false
	}
	path := s.lib.TrackPrev()
	if path == "" {
		return "", "", false
	}
	return path, s.lib.CurrentAlbum().Name, true
}

func (s *playbackState) nextTrack() (string, string, bool) {
	if s.lib == nil || s.pl == nil {
		return "", "", false
	}
	path := s.lib.TrackNext()
	if path == "" {
		return "", "", false
	}
	return path, s.lib.CurrentAlbum().Name, true
}

func (s *playbackState) advance(settings config.PlaybackSettings) (trackRef, bool) {
	if s.lib == nil || s.pl == nil {
		return trackRef{}, false
	}

	if settings.Repeat == config.RepeatOne {
		path := s.lib.CurrentTrack()
		if path == "" {
			return trackRef{}, false
		}
		return trackRef{path: path, album: s.lib.CurrentAlbum().Name}, true
	}

	if settings.ShuffleMode != config.ShuffleOff {
		if s.shuffleNeedsRegeneration(settings) {
			s.regenerateShuffleOrder(settings)
		}
		if s.shuffle.idx >= len(s.shuffle.order) {
			return trackRef{}, false
		}
		track := s.shuffle.order[s.shuffle.idx]
		s.shuffle.idx++
		s.lib.SelectAlbum(track.albumIdx)
		s.lib.SelectTrack(track.trackIdx)
		return track, true
	}

	album := s.lib.CurrentAlbum()
	if s.lib.CurrentTrackIndex() < len(album.Tracks)-1 {
		path := s.lib.TrackNext()
		if path != "" {
			return trackRef{path: path, album: s.lib.CurrentAlbum().Name}, true
		}
	}
	if settings.Repeat == config.RepeatAll {
		path := s.lib.AlbumNext()
		if path != "" {
			return trackRef{path: path, album: s.lib.CurrentAlbum().Name}, true
		}
	}
	return trackRef{}, false
}

func (s *playbackState) shuffleNeedsRegeneration(settings config.PlaybackSettings) bool {
	if len(s.shuffle.order) == 0 {
		return true
	}
	if settings.ShuffleMode == config.ShuffleAlbum && s.shuffle.albumIdx != s.lib.CurrentAlbumIndex() {
		return true
	}
	return s.shuffle.idx >= len(s.shuffle.order) && settings.Repeat == config.RepeatAll
}

func (s *playbackState) regenerateShuffleOrder(settings config.PlaybackSettings) {
	if s.lib == nil {
		s.shuffle = shuffleState{}
		return
	}

	var pool []trackRef
	switch settings.ShuffleMode {
	case config.ShuffleAlbum:
		pool = s.currentAlbumTracks()
		s.shuffle.albumIdx = s.lib.CurrentAlbumIndex()
	case config.ShuffleLocal:
		pool = s.localTracks()
	case config.ShuffleAll:
		pool = s.allTracks()
	default:
		s.shuffle = shuffleState{}
		return
	}

	cur := ""
	if s.pl != nil {
		cur = s.pl.TrackPath()
	}
	if cur != "" && len(pool) > 1 {
		filtered := pool[:0]
		for _, track := range pool {
			if track.path != cur {
				filtered = append(filtered, track)
			}
		}
		pool = filtered
	}

	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	s.shuffle.order = pool
	s.shuffle.idx = 0
}

func (s *playbackState) allTracks() []trackRef {
	var all []trackRef
	for albumIdx, album := range s.lib.Albums {
		for trackIdx, path := range album.Tracks {
			all = append(all, trackRef{path: path, album: album.Name, albumIdx: albumIdx, trackIdx: trackIdx})
		}
	}
	return all
}

func (s *playbackState) localTracks() []trackRef {
	var all []trackRef
	for albumIdx, album := range s.lib.Albums {
		if player.IsModland(album.Path) || player.IsModArchive(album.Path) {
			continue
		}
		for trackIdx, path := range album.Tracks {
			all = append(all, trackRef{path: path, album: album.Name, albumIdx: albumIdx, trackIdx: trackIdx})
		}
	}
	return all
}

func (s *playbackState) currentAlbumTracks() []trackRef {
	albumIdx := s.lib.CurrentAlbumIndex()
	if albumIdx < 0 || albumIdx >= len(s.lib.Albums) {
		return nil
	}
	album := s.lib.Albums[albumIdx]
	tracks := make([]trackRef, len(album.Tracks))
	for trackIdx, path := range album.Tracks {
		tracks[trackIdx] = trackRef{path: path, album: album.Name, albumIdx: albumIdx, trackIdx: trackIdx}
	}
	return tracks
}
