package player

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dendec/glitchscope/internal/filesystem"
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
	Size     int64
	Comment  string // tracker message/comment (XM/IT/MOD/S3M text)
	Cached   bool   // true when a local cache file exists on disk
}

// Library manages a list of albums scanned from a music root directory.
// UP/DOWN navigates albums, LEFT/RIGHT navigates tracks within an album.
// Virtual (catalog) albums live in the same list; cache-path resolution is
// delegated to Resolver.
type Library struct {
	Albums   []Album
	resolver *Resolver // maps virtual track paths to local cache files
	albumIdx int       // -1 = no album loaded
	trackIdx int       // -1 = no track loaded, 0+ = index within current album
}

// NewLibrary scans rootDir for leaf dirs containing audio files.
func NewLibrary(rootDir string) (*Library, error) {
	lib := &Library{resolver: NewResolver(), albumIdx: -1, trackIdx: -1}
	var err error
	lib.Albums, err = scanRoot(rootDir)
	if err != nil {
		return nil, err
	}
	if len(lib.Albums) > 0 {
		lib.albumIdx = 0
		if len(lib.Albums[0].Tracks) > 0 {
			lib.trackIdx = 0
		}
	}
	return lib, nil
}

// NewEmptyLibrary returns an empty library that can be populated later with
// virtual albums (modland/modarchive).
func NewEmptyLibrary() *Library {
	return &Library{resolver: NewResolver(), albumIdx: -1, trackIdx: -1}
}

// SetBaseDir sets the application root directory for resolving virtual cache
// paths (modland/modarchive downloaded files).
func (l *Library) SetBaseDir(dir string) {
	l.resolver.SetBaseDir(dir)
}

// scanRoot walks rootDir and returns sorted Albums from the filesystem.
func scanRoot(rootDir string) ([]Album, error) {
	dirTracks := map[string][]string{}
	report := filesystem.Walk(context.Background(), rootDir, filesystem.Options{
		Include: filesystem.IsAudioFile,
		Descend: func(entry filesystem.Entry) bool {
			return !entry.IsSymlink() && !shouldSkipDir(entry.Path)
		},
	}, func(entry filesystem.Entry) {
		dirTracks[filepath.Dir(entry.Path)] = append(dirTracks[filepath.Dir(entry.Path)], entry.Path)
	})
	if report.Status != filesystem.StatusOK {
		for _, issue := range report.Issues {
			slog.Warn("library scan issue", "path", issue.Path, "error", issue.Err, "status", report.Status)
		}
		return nil, fmt.Errorf("scan music directory: %w", report.Err())
	}

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
	return albums, nil
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
// computes missing entries on demand. Virtual paths (modland/modarchive)
// are resolved to local cache paths before metadata extraction.
func (l *Library) GetAlbumTracks(idx int) []TrackInfo {
	if idx < 0 || idx >= len(l.Albums) {
		return nil
	}
	album := l.Albums[idx]
	var cache *albumMeta
	if !IsVirtual(album) {
		cache = readMetaCache(album.Path)
	}
	if cache == nil {
		cache = &albumMeta{Tracks: map[string]TrackMeta{}}
	}

	infos := make([]TrackInfo, len(album.Tracks))
	dirty := false
	for i, tp := range album.Tracks {
		fname := filepath.Base(TrimPrefixes(tp))
		info := TrackInfo{Path: tp, Cached: l.resolver.ResolveLocalPath(tp) != ""}
		if localPath := l.resolver.ResolveLocalPath(tp); localPath != "" {
			if fi, err := os.Stat(localPath); err == nil {
				info.Size = fi.Size()
			}
		}
		if m, ok := cache.Tracks[fname]; ok {
			info.Duration = m.Duration
			info.BPM = m.BPM
			info.Channels = m.Channels
			info.Comment = m.Comment
		} else {
			// Compute metadata on demand from the local cache file (virtual
			// paths resolved first). extractMetaFromFile is pure — no side
			// effects — and reused by the comment refresh below.
			m := TrackMeta{}
			if localPath := l.resolver.ResolveLocalPath(tp); localPath != "" {
				m = extractMetaFromFile(localPath)
			}
			cache.Tracks[fname] = m
			info.Duration = m.Duration
			info.BPM = m.BPM
			info.Channels = m.Channels
			info.Comment = m.Comment
			dirty = true
		}
		// Always refresh the comment for tracker files, even when the rest of
		// the metadata came from cache (the cache may predate the comment).
		if info.Comment == "" {
			if localPath := l.resolver.ResolveLocalPath(tp); localPath != "" {
				if m := extractMetaFromFile(localPath); m.Comment != "" {
					info.Comment = m.Comment
					if cm, ok := cache.Tracks[fname]; ok {
						cm.Comment = m.Comment
						cache.Tracks[fname] = cm
						dirty = true
					}
					slog.Debug("tracker comment", "file", filepath.Base(tp), "comment", m.Comment)
				}
			}
		}
		infos[i] = info
	}
	if dirty && !IsVirtual(album) {
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

// IsVirtual reports provider-browsed albums (modland/modarchive).
func IsVirtual(a Album) bool {
	return strings.HasPrefix(a.Path, ModlandPrefix) || strings.HasPrefix(a.Path, ModArchivePrefix)
}

// RealAlbumsOnly filters virtual provider albums out of a list.
func RealAlbumsOnly(list []Album) []Album {
	out := make([]Album, 0, len(list))
	for _, a := range list {
		if !IsVirtual(a) {
			out = append(out, a)
		}
	}
	return out
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

// AddCatalogAlbum appends an on-the-fly album (e.g. from modarchive navigation)
// and returns its stable index in Albums. If an album with the same path already
// exists, its existing index is returned instead of duplicating.
func (l *Library) AddCatalogAlbum(album Album) int {
	for i, a := range l.Albums {
		if a.Path == album.Path {
			return i
		}
	}
	l.Albums = append(l.Albums, album)
	return len(l.Albums) - 1
}

// Rescan re-reads rootDir and rebuilds local albums, preserving virtual
// (modland/modarchive) albums that were added via AddVirtualAlbums.
func (l *Library) Rescan(rootDir string) error {
	localAlbums, err := scanRoot(rootDir)
	if err != nil {
		return err
	}
	// Save virtual albums before wiping.
	var virtuals []Album
	for _, a := range l.Albums {
		if IsModland(a.Path) || IsModArchive(a.Path) {
			virtuals = append(virtuals, a)
		}
	}

	l.Albums = localAlbums
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
	return nil
}
