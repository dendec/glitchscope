// Package modland manages the modland.com module catalog.
package modland

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	catalogFile = "modland"
	filesDir    = "modland-cache"
)

// SupportedExts is the set of file extensions accepted from modland.
var SupportedExts map[string]bool

func init() {
	SupportedExts = map[string]bool{
		".mp3": true, ".ogg": true, ".flac": true, ".wav": true,
		// Core tracker formats
		".mod": true, ".xm": true, ".it": true, ".s3m": true,
		// Additional tracker formats
		".mptm": true, ".stm": true, ".nst": true, ".wow": true,
		".ult": true, ".669": true, ".mtm": true, ".med": true,
		".far": true, ".mdl": true, ".ams": true, ".dsm": true,
		".amf": true, ".okt": true, ".dmf": true, ".ptm": true,
		".psm": true, ".mt2": true, ".dbm": true,
		// Exotic formats supported by libxmp
		".abk": true, ".digi": true, ".dtt": true,
		".flx": true, ".gtk": true, ".imf": true, ".liq": true,
		".masi": true, ".mgt": true, ".mmd": true, ".mmdc": true,
		".mmcmp": true, ".muse": true, ".nt": true, ".pmd": true,
		".ppm": true, ".pru": true, ".pt36": true, ".rh": true,
		".rtm": true, ".sfx": true, ".sfx2": true, ".stim": true,
		".stx": true, ".tcb": true, ".tdd": true, ".tp": true,
		".uni": true, ".xd": true,
		// Game Music Emu formats
		".ay": true, ".nsf": true, ".nsfe": true, ".spc": true, ".gbs": true,
		".hes": true, ".kss": true, ".sap": true,
		".vgm": true, ".vgz": true,
		// libopenmpt fallback formats
		".mo3": true, ".ktm": true, ".ims": true, ".mdc": true,
		".spx": true, ".txn": true,
	}
}

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

// CacheDir returns the modland cache directory.
func CacheDir(baseDir string) (string, error) {
	dir := filepath.Join(baseDir, filesDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("modland mkdir: %w", err)
	}
	return dir, nil
}

// FilesDir returns the directory where downloaded module files are cached.
func FilesDir(baseDir string) (string, error) {
	cacheDir, err := CacheDir(baseDir)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cacheDir, filesDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("modland files mkdir: %w", err)
	}
	return dir, nil
}

// LoadCatalog reads the catalog from disk.
func LoadCatalog(baseDir string) *Catalog {
	path := filepath.Join(baseDir, catalogFile)

	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil
	}
	defer gz.Close()

	var entry cacheEntry
	if err := json.NewDecoder(gz).Decode(&entry); err != nil {
		slog.Error("modland catalog decode failed", "error", err)
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
	path := filepath.Join(baseDir, catalogFile)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("modland catalog mkdir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "catalog*.tmp")
	if err != nil {
		return fmt.Errorf("modland catalog temp: %w", err)
	}
	tmpPath := tmp.Name()

	gz := gzip.NewWriter(tmp)
	if err := json.NewEncoder(gz).Encode(cacheEntry{
		Albums:          cat.Albums,
		ExcludedFormats: cat.ExcludedFormats,
		UpdatedAt:       cat.UpdatedAt,
	}); err != nil {
		gz.Close()
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("modland catalog encode: %w", err)
	}
	if err := gz.Close(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("modland catalog rename: %w", err)
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
		if !SupportedExts[ext] {
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
		if strings.ToLower(p) == "modules" && i+2 < len(parts) {
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
