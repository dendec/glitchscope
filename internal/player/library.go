package player

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dendec/mdpp/internal/openmpt"
	"github.com/dendec/mdpp/internal/soloud"
)

// Album represents a directory containing audio files.
type Album struct {
	Name   string   // display name (directory basename)
	Path   string   // full directory path
	Tracks []string // full paths to audio files
}

// TrackInfo holds metadata for a single track.
type TrackInfo struct {
	Path     string
	Duration float64
	BPM      float64
	Channels int
}

// Library manages a list of albums scanned from a music root directory.
// UP/DOWN navigates albums, LEFT/RIGHT navigates tracks within an album.
type Library struct {
	Albums   []Album
	albumIdx int // -1 = no album loaded
	trackIdx int // -1 = no track loaded, 0+ = index within current album
}

// NewLibrary scans rootDir for albums (leaf dirs containing audio files).
func NewLibrary(rootDir string) (*Library, error) {
	lib := &Library{albumIdx: -1, trackIdx: -1}

	// Map each parent dir to audio files inside it.
	dirTracks := map[string][]string{}
	err := filepath.Walk(rootDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible
		}
		if fi.IsDir() {
			if shouldSkipDir(path) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if SupportedExts[ext] {
			dir := filepath.Dir(path)
			dirTracks[dir] = append(dirTracks[dir], path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort dirs then tracks within each dir.
	var dirs []string
	for d := range dirTracks {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		tracks := dirTracks[d]
		sort.Strings(tracks)
		lib.Albums = append(lib.Albums, Album{
			Name:   filepath.Base(d),
			Path:   d,
			Tracks: tracks,
		})
	}

	if len(lib.Albums) > 0 {
		lib.albumIdx = 0
		if len(lib.Albums[0].Tracks) > 0 {
			lib.trackIdx = 0
		}
	}
	return lib, nil
}

// AlbumNext advances to the next album. Returns the new track path or "".
func (l *Library) AlbumNext() string {
	if len(l.Albums) == 0 {
		return ""
	}
	l.albumIdx++
	if l.albumIdx >= len(l.Albums) {
		l.albumIdx = 0
	}
	l.trackIdx = 0
	if len(l.Albums[l.albumIdx].Tracks) > 0 {
		return l.Albums[l.albumIdx].Tracks[0]
	}
	return ""
}

// AlbumPrev goes to the previous album. Returns the new track path or "".
func (l *Library) AlbumPrev() string {
	if len(l.Albums) == 0 {
		return ""
	}
	l.albumIdx--
	if l.albumIdx < 0 {
		l.albumIdx = len(l.Albums) - 1
	}
	l.trackIdx = 0
	if len(l.Albums[l.albumIdx].Tracks) > 0 {
		return l.Albums[l.albumIdx].Tracks[0]
	}
	return ""
}

// TrackNext advances to the next track in the current album. Returns path or "".
func (l *Library) TrackNext() string {
	if l.albumIdx < 0 || l.albumIdx >= len(l.Albums) {
		return ""
	}
	album := &l.Albums[l.albumIdx]
	if len(album.Tracks) == 0 {
		return ""
	}
	l.trackIdx++
	if l.trackIdx >= len(album.Tracks) {
		l.trackIdx = 0 // wrap
	}
	return album.Tracks[l.trackIdx]
}

// TrackPrev goes to the previous track in current album. Returns path or "".
func (l *Library) TrackPrev() string {
	if l.albumIdx < 0 || l.albumIdx >= len(l.Albums) {
		return ""
	}
	album := &l.Albums[l.albumIdx]
	if len(album.Tracks) == 0 {
		return ""
	}
	l.trackIdx--
	if l.trackIdx < 0 {
		l.trackIdx = len(album.Tracks) - 1 // wrap
	}
	return album.Tracks[l.trackIdx]
}

// CurrentTrack returns the full path of the current track, or "".
func (l *Library) CurrentTrack() string {
	if l.albumIdx < 0 || l.albumIdx >= len(l.Albums) {
		return ""
	}
	album := l.Albums[l.albumIdx]
	if l.trackIdx < 0 || l.trackIdx >= len(album.Tracks) {
		return ""
	}
	return album.Tracks[l.trackIdx]
}

// CurrentAlbum returns a copy of the current album. Empty if none.
func (l *Library) CurrentAlbum() Album {
	if l.albumIdx >= 0 && l.albumIdx < len(l.Albums) {
		return l.Albums[l.albumIdx]
	}
	return Album{}
}

// AlbumCount returns the number of albums.
func (l *Library) AlbumCount() int {
	return len(l.Albums)
}

// CurrentAlbumIndex returns the current album index.
func (l *Library) CurrentAlbumIndex() int { return l.albumIdx }

// CurrentTrackIndex returns the current track index.
func (l *Library) CurrentTrackIndex() int { return l.trackIdx }

// SelectAlbum sets the album index and returns the first track path.
func (l *Library) SelectAlbum(idx int) string {
	if idx < 0 || idx >= len(l.Albums) {
		return ""
	}
	l.albumIdx = idx
	l.trackIdx = 0
	if len(l.Albums[idx].Tracks) > 0 {
		return l.Albums[idx].Tracks[0]
	}
	return ""
}

// SelectTrack sets the track index within the current album and returns its path.
func (l *Library) SelectTrack(idx int) string {
	if l.albumIdx < 0 || l.albumIdx >= len(l.Albums) {
		return ""
	}
	if idx < 0 || idx >= len(l.Albums[l.albumIdx].Tracks) {
		return ""
	}
	l.trackIdx = idx
	return l.Albums[l.albumIdx].Tracks[idx]
}

// PlayCurrent returns the current track path to be played.
// Returns "" if nothing to play.
func (l *Library) PlayCurrent() string {
	return l.CurrentTrack()
}

// GetAlbumTracks returns TrackInfo for each track in the given album index.
// Reads metadata cache first; computes missing entries on demand.
func (l *Library) GetAlbumTracks(idx int) []TrackInfo {
	if idx < 0 || idx >= len(l.Albums) {
		return nil
	}
	album := l.Albums[idx]
	cache := readMetaCache(album.Path)
	if cache == nil {
		cache = &albumMeta{Tracks: map[string]TrackMeta{}}
	}

	infos := make([]TrackInfo, len(album.Tracks))
	dirty := false
	for i, tp := range album.Tracks {
		fname := filepath.Base(tp)
		info := TrackInfo{Path: tp}
		if m, ok := cache.Tracks[fname]; ok {
			info.Duration = m.Duration
			info.BPM = m.BPM
			info.Channels = m.Channels
		} else {
			m := TrackMeta{}
			ext := strings.ToLower(filepath.Ext(tp))
			if openmpt.SupportedExts[ext] {
				if bpm, ch, dur, err := openmpt.GetTrackerMeta(tp); err == nil {
					m.Duration = dur
					m.BPM = bpm
					m.Channels = ch
				}
			} else if w, err := soloud.LoadWav(tp); err == nil {
				m.Duration = w.GetLength()
				w.Destroy()
			}
			cache.Tracks[fname] = m
			info.Duration = m.Duration
			info.BPM = m.BPM
			info.Channels = m.Channels
			dirty = true
		}
		infos[i] = info
	}
	if dirty {
		if err := writeMetaCache(album.Path, cache); err != nil {
			slog.Warn("write meta cache", "album", album.Name, "error", err)
		}
	}
	return infos
}

// TrackTitle returns a display-friendly name for a track path.
func TrackTitle(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}
