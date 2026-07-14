package player

import (
	"math/rand"
	"path/filepath"
	"strings"
)

// Playlist manages a queue of audio file paths with shuffle/repeat.
// One directory = one playlist.
type Playlist struct {
	tracks  []string
	orig    []string // preserves original order for unshuffle
	current int      // index into tracks, -1 = empty/start
	shuffle bool
	repeat  bool
}

// NewPlaylist creates a playlist from the given track slice.
// The slice is copied; caller can reuse the original.
func NewPlaylist(tracks []string) *Playlist {
	c := make([]string, len(tracks))
	copy(c, tracks)
	return &Playlist{
		tracks:  c,
		current: -1,
	}
}

// Current returns the current track path, or "" if empty.
func (p *Playlist) Current() string {
	if len(p.tracks) == 0 || p.current < 0 || p.current >= len(p.tracks) {
		return ""
	}
	return p.tracks[p.current]
}

// Next advances to the next track and returns it.
// Returns "" if the playlist has ended (no repeat).
func (p *Playlist) Next() string {
	if len(p.tracks) == 0 {
		return ""
	}
	if p.current+1 >= len(p.tracks) {
		if p.repeat {
			p.current = 0
		} else {
			p.current = len(p.tracks) // past end
			return ""
		}
	} else {
		p.current++
	}
	return p.tracks[p.current]
}

// Prev goes back to the previous track and returns it.
// If at start, wraps to last when repeat is on, otherwise stays.
func (p *Playlist) Prev() string {
	if len(p.tracks) == 0 {
		return ""
	}
	if p.current <= 0 {
		if p.repeat {
			p.current = len(p.tracks) - 1
		} else {
			p.current = 0
		}
	} else {
		p.current--
	}
	return p.tracks[p.current]
}

// Len returns the number of tracks in the playlist.
func (p *Playlist) Len() int {
	return len(p.tracks)
}

// TrackTitle returns a display-friendly name for a track path.
func TrackTitle(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

// SetShuffle enables or disables random ordering.
// Switching toggles the order and resets current to start.
func (p *Playlist) SetShuffle(on bool) {
	if p.shuffle == on || len(p.tracks) == 0 {
		return
	}
	p.shuffle = on
	if on {
		p.orig = make([]string, len(p.tracks))
		copy(p.orig, p.tracks)
		rand.Shuffle(len(p.tracks), func(i, j int) {
			p.tracks[i], p.tracks[j] = p.tracks[j], p.tracks[i]
		})
	} else {
		copy(p.tracks, p.orig)
		p.orig = nil
	}
	p.current = -1
}

// SetRepeat enables or disables repeat.
func (p *Playlist) SetRepeat(on bool) {
	p.repeat = on
}

// Shuffled returns the current shuffle state.
func (p *Playlist) Shuffled() bool {
	return p.shuffle
}

// Repeat returns the current repeat state.
func (p *Playlist) Repeat() bool {
	return p.repeat
}
