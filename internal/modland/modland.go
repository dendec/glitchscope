// Package modland manages the modland.com module catalog.
package modland

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dendec/glitchscope/internal/formats"
	"github.com/dendec/glitchscope/internal/util"
)

// Track holds a module filename and its expected size.
type Track struct {
	Name string `json:"name"` // filename (e.g. "song.mod")
	Size int64  `json:"size"` // expected size in bytes (0 if unknown)
}

// Album represents a modland format/author folder.
type Album struct {
	Name   string  // display name (e.g. "Protracker/Curt Cool")
	Tracks []Track // tracks with sizes
}

// TrackPath returns the full modland path for a track.
func (a *Album) TrackPath(i int) string {
	return a.Name + "/" + a.Tracks[i].Name
}

// Catalog is the parsed modland module listing.
type Catalog struct {
	Albums          []Album
	ExcludedFormats []string // formats that failed validation (e.g. "DefleMask")
	UpdatedAt       time.Time
}

type cacheEntry struct {
	Albums          []Album   `json:"albums"`
	ExcludedFormats []string  `json:"excluded_formats,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// CacheDir returns the modland cache directory (.cache/modland).
func CacheDir(baseDir string) (string, error) {
	dir := filepath.Join(baseDir, ".cache", "modland")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("modland mkdir: %w", err)
	}
	return dir, nil
}

// FilesDir returns the directory where downloaded module files are cached (.cache/modland/files).
func FilesDir(baseDir string) (string, error) {
	cacheDir, err := CacheDir(baseDir)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cacheDir, "files")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("modland files mkdir: %w", err)
	}
	return dir, nil
}

// CatalogPath returns the path to the modland catalog file.
func CatalogPath(baseDir string) string {
	cacheDir, _ := CacheDir(baseDir)
	return filepath.Join(cacheDir, "catalog")
}

// MigrateLegacyCache moves old modland-cache/ or modland catalog files into .cache/modland/.
func MigrateLegacyCache(baseDir string) {
	targetDir, err := CacheDir(baseDir)
	if err != nil {
		return
	}

	oldCat := filepath.Join(baseDir, "modland")
	newCat := filepath.Join(targetDir, "catalog")
	if _, err := os.Stat(oldCat); err == nil {
		if _, err := os.Stat(newCat); os.IsNotExist(err) {
			_ = os.Rename(oldCat, newCat)
		} else {
			_ = os.Remove(oldCat)
		}
	}

	oldFiles := filepath.Join(baseDir, "modland-cache")
	newFiles, _ := FilesDir(baseDir)
	if info, err := os.Stat(oldFiles); err == nil && info.IsDir() {
		nested := filepath.Join(oldFiles, "modland-cache")
		if nInfo, nErr := os.Stat(nested); nErr == nil && nInfo.IsDir() {
			oldFiles = nested
		}
		_ = filepath.Walk(oldFiles, func(path string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return nil //nolint:nilerr // intentional: skip errors and dirs during cache migration
			}
			rel, err := filepath.Rel(oldFiles, path)
			if err != nil {
				return nil //nolint:nilerr // intentional: skip unresolvable paths
			}
			dst := filepath.Join(newFiles, rel)
			if _, err := os.Stat(dst); os.IsNotExist(err) {
				_ = os.MkdirAll(filepath.Dir(dst), 0o755)
				_ = os.Rename(path, dst)
			}
			return nil
		})
		_ = os.RemoveAll(filepath.Join(baseDir, "modland-cache"))
	}
}

// LoadCatalog reads the catalog from disk.
func LoadCatalog(baseDir string) *Catalog {
	MigrateLegacyCache(baseDir)
	path := CatalogPath(baseDir)

	var entry cacheEntry
	if err := util.LoadGzipJSON(path, &entry); err != nil {
		return nil
	}

	if !entry.UpdatedAt.IsZero() {
		slog.Info("modland catalog loaded", "albums", len(entry.Albums), "excluded", len(entry.ExcludedFormats), "age", time.Since(entry.UpdatedAt).Round(time.Hour))
	} else {
		slog.Info("modland catalog loaded", "albums", len(entry.Albums), "excluded", len(entry.ExcludedFormats))
	}
	cat := &Catalog{Albums: entry.Albums, ExcludedFormats: entry.ExcludedFormats, UpdatedAt: entry.UpdatedAt}
	if len(entry.ExcludedFormats) > 0 {
		cat = cat.FilterExcluded()
	}
	return cat
}

// SaveCatalog writes the catalog to disk as gzipped JSON.
func SaveCatalog(baseDir string, cat *Catalog) error {
	path := CatalogPath(baseDir)
	entry := cacheEntry{
		Albums:          cat.Albums,
		ExcludedFormats: cat.ExcludedFormats,
		UpdatedAt:       cat.UpdatedAt,
	}
	if err := util.SaveGzipJSON(path, entry); err != nil {
		return fmt.Errorf("modland catalog save: %w", err)
	}
	return nil
}

// FileURL returns the primary (fast mirror) download URL.
func FileURL(remotePath string) string {
	return (&url.URL{
		Scheme: "https",
		Host:   "modland.antarctica.no",
		Path:   "/pub/modules/" + strings.TrimPrefix(remotePath, "/"),
	}).String()
}

// FileFallbackURL returns the official modland.com fallback URL.
func FileFallbackURL(remotePath string) string {
	return (&url.URL{
		Scheme: "https",
		Host:   "modland.com",
		Path:   "/pub/modules/" + strings.TrimPrefix(remotePath, "/"),
	}).String()
}

// FileURLLower returns the primary URL with lowercased filename.
func FileURLLower(remotePath string) string {
	return (&url.URL{
		Scheme: "https",
		Host:   "modland.antarctica.no",
		Path:   "/pub/modules/" + lowerFilename(strings.TrimPrefix(remotePath, "/")),
	}).String()
}

// FileFallbackURLLower returns the fallback URL with lowercased filename.
func FileFallbackURLLower(remotePath string) string {
	return (&url.URL{
		Scheme: "https",
		Host:   "modland.com",
		Path:   "/pub/modules/" + lowerFilename(strings.TrimPrefix(remotePath, "/")),
	}).String()
}

// lowerFilename lowercases only the filename part, keeping directory prefix.
func lowerFilename(remotePath string) string {
	if i := strings.LastIndex(remotePath, "/"); i >= 0 {
		return remotePath[:i+1] + strings.ToLower(remotePath[i+1:])
	}
	return strings.ToLower(remotePath)
}

// ParseListing parses a tab-separated text listing: "size\tpath".
func ParseListing(data []byte) (*Catalog, error) {
	type trackEntry struct {
		name string
		size int64
	}
	albumTracks := map[string][]trackEntry{}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var size int64
		path := line

		// Tab-separated "size\tpath".
		if idx := strings.IndexByte(line, '\t'); idx >= 0 {
			if parsed, err := strconv.ParseInt(line[:idx], 10, 64); err == nil {
				size = parsed
			}
			path = line[idx+1:]
		}

		path = filepath.ToSlash(path)

		ext := strings.ToLower(filepath.Ext(path))
		if !formats.IsSupportedExt(ext) {
			continue
		}

		album := extractAlbum(path)
		if album == "" {
			continue
		}

		name := path[len(album)+1:]
		albumTracks[album] = append(albumTracks[album], trackEntry{name: name, size: size})
	}

	var albums []Album
	for name, entries := range albumTracks {
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].name < entries[j].name
		})
		tracks := make([]Track, len(entries))
		for i, e := range entries {
			tracks[i] = Track{Name: e.name, Size: e.size}
		}
		albums = append(albums, Album{Name: name, Tracks: tracks})
	}
	sort.Slice(albums, func(i, j int) bool {
		return albums[i].Name < albums[j].Name
	})

	return &Catalog{Albums: albums}, nil
}

// extractAlbum returns "Format/Author" from a modland path.
func extractAlbum(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.EqualFold(p, "modules") && i+2 < len(parts) {
			if i+3 < len(parts) {
				return parts[i+1] + "/" + parts[i+2]
			}
			return parts[i+1]
		}
	}
	if len(parts) >= 3 {
		return parts[0] + "/" + parts[1]
	}
	if len(parts) == 2 {
		return parts[0]
	}
	return ""
}

// FormatName returns the format from an album name.
func FormatName(album string) string {
	if i := strings.IndexByte(album, '/'); i >= 0 {
		return album[:i]
	}
	return album
}

func IsExcluded(excluded []string, format string) bool {
	for _, e := range excluded {
		if e == format {
			return true
		}
	}
	return false
}

// FilterExcluded returns a new Catalog with excluded format albums removed.
func (c *Catalog) FilterExcluded() *Catalog {
	if len(c.ExcludedFormats) == 0 {
		return c
	}
	var filtered []Album
	for _, a := range c.Albums {
		if !IsExcluded(c.ExcludedFormats, FormatName(a.Name)) {
			filtered = append(filtered, a)
		}
	}
	return &Catalog{Albums: filtered, ExcludedFormats: c.ExcludedFormats, UpdatedAt: c.UpdatedAt}
}
