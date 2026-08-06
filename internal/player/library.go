package player

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dendec/pmv/internal/openmpt"
	"github.com/dendec/pmv/internal/soloud"
	"github.com/dendec/pmv/internal/xmp"
)

// Album represents a directory containing audio files.
type Album struct {
	Name   string   // display name (directory basename)
	Path   string   // full directory path
	Tracks []string // full paths to audio files
}

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

// NewLibrary scans rootDir for leaf dirs containing audio files.
func NewLibrary(rootDir string) (*Library, error) {
	lib := &Library{albumIdx: -1, trackIdx: -1}
	lib.Albums = scanRoot(rootDir)
	if len(lib.Albums) > 0 {
		lib.albumIdx = 0
		if len(lib.Albums[0].Tracks) > 0 {
			lib.trackIdx = 0
		}
	}
	return lib, nil
}

// scanRoot walks rootDir and returns sorted Albums from the filesystem.
func scanRoot(rootDir string) []Album {
	dirTracks := map[string][]string{}
	_ = filepath.Walk(rootDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip inaccessible — intentional
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

	var dirs []string
	for d := range dirTracks {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	albums := make([]Album, 0, len(dirs))
	for _, d := range dirs {
		tracks := dirTracks[d]
		sort.Strings(tracks)
		albums = append(albums, Album{
			Name:   filepath.Base(d),
			Path:   d,
			Tracks: tracks,
		})
	}
	return albums
}

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

func (l *Library) CurrentAlbum() Album {
	if l.albumIdx >= 0 && l.albumIdx < len(l.Albums) {
		return l.Albums[l.albumIdx]
	}
	return Album{}
}

func (l *Library) AlbumCount() int {
	return len(l.Albums)
}

func (l *Library) CurrentAlbumIndex() int { return l.albumIdx }

func (l *Library) CurrentTrackIndex() int { return l.trackIdx }

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

// GetAlbumTracks returns TrackInfo for each track. Reads cache first;
// computes missing entries on demand.
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
			if isTrackerExt(ext) {
				if bpm, ch, dur, err := xmp.GetTrackerMeta(tp); err == nil {
					m.Duration = dur
					m.BPM = bpm
					m.Channels = ch
				} else if openmpt.HasExt(ext) {
					if fileBuf, err := os.ReadFile(tp); err == nil {
						if bpm, ch, dur, err := openmpt.GetTrackerMeta(fileBuf); err == nil {
							m.Duration = dur
							m.BPM = bpm
							m.Channels = ch
						}
					}
				}
			} else if isFfmpegExt(ext) {
				if fileBuf, err := os.ReadFile(tp); err == nil {
					if source, err := soloud.NewFfmpeg(fileBuf); err == nil {
						m.Duration = source.GetLength()
						m.Channels = source.GetChannels()
						source.Destroy()
					}
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
	if dirty && !strings.HasPrefix(album.Path, ModlandPrefix) {
		if err := writeMetaCache(album.Path, cache); err != nil {
			slog.Warn("write meta cache", "album", album.Name, "error", err)
		}
	}
	return infos
}

// TrackTitle returns a display-friendly name for a track path.
const (
	ModlandPrefix    = "modland:"
	ModArchivePrefix = "modarchive:"
)

var KnownPrefixes = []string{
	ModlandPrefix,
	ModArchivePrefix,
}

// TrimPrefixes strips any virtual provider prefix (modland:, modarchive:, etc.) from path.
func TrimPrefixes(path string) string {
	for _, prefix := range KnownPrefixes {
		if strings.HasPrefix(path, prefix) {
			return strings.TrimPrefix(path, prefix)
		}
	}
	return path
}

func TrackTitle(path string) string {
	p := TrimPrefixes(path)
	base := filepath.Base(p)
	if strings.HasSuffix(strings.ToLower(base), ".zip") {
		base = base[:len(base)-4]
	}
	return base
}

func IsModland(path string) bool {
	return strings.HasPrefix(path, ModlandPrefix)
}

func IsModArchive(path string) bool {
	return strings.HasPrefix(path, ModArchivePrefix)
}

func RemotePath(path string) string {
	return TrimPrefixes(path)
}

// AddVirtualAlbums appends pre-built albums (e.g. from modland) to the library.
func (l *Library) AddVirtualAlbums(albums []Album) {
	l.Albums = append(l.Albums, albums...)
	if l.albumIdx < 0 && len(l.Albums) > 0 {
		l.albumIdx = 0
		if len(l.Albums[0].Tracks) > 0 {
			l.trackIdx = 0
		}
	}
}

// Rescan re-reads rootDir and rebuilds local albums, preserving virtual
// (modland/modarchive) albums that were added via AddVirtualAlbums.
func (l *Library) Rescan(rootDir string) {
	// Save virtual albums before wiping.
	var virtuals []Album
	for _, a := range l.Albums {
		if IsModland(a.Path) || IsModArchive(a.Path) {
			virtuals = append(virtuals, a)
		}
	}

	l.Albums = scanRoot(rootDir)
	l.Albums = append(l.Albums, virtuals...)

	// Clamp indices.
	if l.albumIdx >= len(l.Albums) {
		l.albumIdx = len(l.Albums) - 1
	}
	if l.albumIdx < 0 && len(l.Albums) > 0 {
		l.albumIdx = 0
	}
	if l.albumIdx >= 0 && l.albumIdx < len(l.Albums) {
		if l.trackIdx >= len(l.Albums[l.albumIdx].Tracks) {
			l.trackIdx = len(l.Albums[l.albumIdx].Tracks) - 1
		}
		if l.trackIdx < 0 && len(l.Albums[l.albumIdx].Tracks) > 0 {
			l.trackIdx = 0
		}
	}
}
