package app

import (
	"math/rand"
	"path/filepath"
	"strings"

	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/player"
)

func (s *playbackState) snapshot(trackAlbumIdx int, includeTrackInfos bool) overlayPlaybackSnapshot {
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
	if includeTrackInfos {
		snapshot.trackInfos = s.lib.GetAlbumTracks(trackAlbumIdx)
	}
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
	pl            *player.Player
	lib           *player.Library
	shuffle       shuffleState
	playlist      []string
	playlistIdx   int
	playlistAlbum string
}

func (s *playbackState) play(path string) bool {
	if s.pl == nil {
		return false
	}
	s.selectLibraryTrack(path)
	if s.playlistIdx < 0 || s.playlistIdx >= len(s.playlist) || s.playlist[s.playlistIdx] != path {
		s.playlist = nil
		s.playlistIdx = -1
		s.playlistAlbum = ""
	}
	s.pl.PlayFileAsync(path)
	return true
}

func (s *playbackState) selectLibraryTrack(path string) {
	if s.lib == nil || path == "" {
		return
	}
	for albumIdx, album := range s.lib.Albums {
		for trackIdx, trackPath := range album.Tracks {
			if trackPath == path {
				s.lib.SelectAlbum(albumIdx)
				s.lib.SelectTrack(trackIdx)
				return
			}
		}
	}
}

func (s *playbackState) setPlaylist(tracks []string, index int, album string) bool {
	if index < 0 || index >= len(tracks) {
		return false
	}
	s.playlist = append([]string(nil), tracks...)
	s.playlistIdx = index
	s.playlistAlbum = album
	return true
}

// clearPlaylist resets the playlist state.
func (s *playbackState) clearPlaylist() {
	s.playlist = nil
	s.playlistIdx = -1
	s.playlistAlbum = ""
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
	if len(s.playlist) > 0 {
		if s.playlistIdx <= 0 {
			s.playlistIdx = len(s.playlist) - 1
		} else {
			s.playlistIdx--
		}
		return s.playlist[s.playlistIdx], s.playlistAlbum, true
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
	if len(s.playlist) > 0 {
		s.playlistIdx++
		if s.playlistIdx >= len(s.playlist) {
			s.playlistIdx = 0
		}
		return s.playlist[s.playlistIdx], s.playlistAlbum, true
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

	if len(s.playlist) > 0 {
		if settings.Repeat == config.RepeatOne {
			return trackRef{path: s.playlist[s.playlistIdx], album: s.playlistAlbum}, true
		}
		next := s.playlistIdx + 1
		if next < len(s.playlist) {
			s.playlistIdx = next
			return trackRef{path: s.playlist[next], album: s.playlistAlbum}, true
		}
		if settings.Repeat == config.RepeatAll {
			s.playlistIdx = 0
			return trackRef{path: s.playlist[0], album: s.playlistAlbum}, true
		}
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

// prunePlaylist removes entries inside deletedPath and clamps the cursor.
func (s *playbackState) prunePlaylist(deletedPath string) {
	if len(s.playlist) == 0 {
		return
	}
	sep := string(filepath.Separator)
	var kept []string
	for _, p := range s.playlist {
		if p == deletedPath || strings.HasPrefix(p, deletedPath+sep) {
			continue
		}
		kept = append(kept, p)
	}
	if len(kept) == len(s.playlist) {
		return
	}
	if len(kept) == 0 {
		s.playlist = nil
		s.playlistIdx = -1
		s.playlistAlbum = ""
		return
	}
	s.playlist = kept
	if s.playlistIdx >= len(s.playlist) {
		s.playlistIdx = len(s.playlist) - 1
	}
}
