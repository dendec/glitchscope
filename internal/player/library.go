package player

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
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

	// Audio file tags (MP3/FLAC/Ogg/Opus via FFmpeg).
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	Genre       string
	Date        string
	Track       string // track number string
	Composer    string
	Disc        string // disc number string
	Extra       map[string]string
}

// Library manages a list of albums scanned from a music root directory.
// UP/DOWN navigates albums, LEFT/RIGHT navigates tracks within an album.
// Virtual (catalog) albums live in the same list; cache-path resolution is
// delegated to Resolver.
type Library struct {
	Albums   []Album
	metadata *MetadataReader
	resolver *Resolver // maps virtual track paths to local cache files
	albumIdx int       // -1 = no album loaded
	trackIdx int       // -1 = no track loaded, 0+ = index within current album
}

// NewLibrary scans rootDir for leaf dirs containing audio files.
func NewLibrary(rootDir string) (*Library, error) {
	albums, err := scanRoot(rootDir)
	if err != nil {
		return nil, err
	}
	return NewLibraryFromScan(albums), nil
}

// NewLibraryFromScan builds a Library from an already-scanned album list
// (e.g. from ScanLibraryAlbums), without re-walking the filesystem.
func NewLibraryFromScan(albums []Album) *Library {
	lib := &Library{resolver: NewResolver(), albumIdx: -1, trackIdx: -1, Albums: albums}
	if len(lib.Albums) > 0 {
		lib.albumIdx = 0
		if len(lib.Albums[0].Tracks) > 0 {
			lib.trackIdx = 0
		}
	}
	return lib
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
	l.metadata = nil
}

// ScanLibraryAlbums walks rootDir and returns sorted Albums from the
// filesystem, along with the scan status. Callers must not treat a
// Partial/Failed status as authoritative: keep using the previous
// known-good album list instead of replacing it.
func ScanLibraryAlbums(rootDir string) ([]Album, filesystem.Status, error) {
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
		return nil, report.Status, fmt.Errorf("scan music directory: %w", report.Err())
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
	return albums, report.Status, nil
}

// scanRoot is a status-discarding convenience wrapper over
// ScanLibraryAlbums for callers that only need the album list or an error.
func scanRoot(rootDir string) ([]Album, error) {
	albums, _, err := ScanLibraryAlbums(rootDir)
	return albums, err
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

// GetAlbumTracks synchronously reads metadata for non-UI callers.
// The application UI uses MetadataReader through its cancellable worker.
func (l *Library) GetAlbumTracks(idx int) []TrackInfo {
	if idx < 0 || idx >= len(l.Albums) {
		return nil
	}
	if l.metadata == nil {
		l.metadata = NewMetadataReader(l.resolver.baseDir)
	}
	return l.metadata.Load(context.Background(), l.Albums[idx])
}

// TrackTitle returns a display-friendly name for a track path.
const (
	ModlandPrefix    = "modland:"
	ModArchivePrefix = "modarchive:"
	RadioPrefix      = "radio:"
	DownloadsPrefix  = "downloads:"
)

var KnownPrefixes = []string{
	ModlandPrefix,
	ModArchivePrefix,
	RadioPrefix,
}

// IsVirtual reports provider-browsed albums (modland/modarchive).
func IsVirtual(a Album) bool {
	return strings.HasPrefix(a.Path, ModlandPrefix) || strings.HasPrefix(a.Path, ModArchivePrefix) || strings.HasPrefix(a.Path, RadioPrefix) || a.Path == DownloadsPrefix
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
	if IsModArchive(path) {
		if parsed, err := url.Parse(p); err == nil && parsed.Fragment != "" {
			p = parsed.Fragment
		}
	}
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

// IsRadio reports a Radio provider virtual path.
func IsRadio(path string) bool { return strings.HasPrefix(path, RadioPrefix) }

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
// exists, its current snapshot is replaced and the stable index is returned.
func (l *Library) AddCatalogAlbum(album Album) int {
	for i, a := range l.Albums {
		if a.Path == album.Path {
			l.Albums[i] = album
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
