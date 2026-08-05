package app

import "github.com/dendec/pmv/internal/player"

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
