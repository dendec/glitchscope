// Package modland manages the modland.com module catalog.
// The catalog is shipped as catalog.json.gz with the application.
package modland

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	catalogFile = "catalog.json.gz"
	filesDir    = "files"
)

// SupportedExts is the set of file extensions we accept from modland.
var SupportedExts map[string]bool

func init() {
	SupportedExts = map[string]bool{
		".mp3": true, ".ogg": true, ".flac": true, ".wav": true,
		".mod": true, ".xm": true, ".it": true, ".s3m": true,
		".mptm": true, ".stm": true, ".nst": true, ".wow": true,
		".ult": true, ".669": true, ".mtm": true, ".med": true,
		".far": true, ".mdl": true, ".ams": true, ".dsm": true,
		".amf": true, ".okt": true, ".dmf": true, ".ptm": true,
		".psm": true, ".mt2": true, ".dbm": true,
	}
}

// Track holds a module filename and its expected size from the listing.
type Track struct {
	Name string `json:"name"` // filename (e.g. "song.mod")
	Size int64  `json:"size"` // expected size in bytes (0 if unknown)
}

// Album represents a modland format/author folder with its module paths.
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
	Albums    []Album
	UpdatedAt time.Time
}

type cacheEntry struct {
	Albums    []Album   `json:"albums"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CacheDir returns the modland cache directory next to the binary.
func CacheDir(baseDir string) (string, error) {
	dir := filepath.Join(baseDir, "modland")
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
	cacheDir, err := CacheDir(baseDir)
	if err != nil {
		return nil
	}
	path := filepath.Join(cacheDir, catalogFile)

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
		slog.Info("modland catalog loaded", "albums", len(entry.Albums), "age", time.Since(entry.UpdatedAt).Round(time.Hour))
	} else {
		slog.Info("modland catalog loaded", "albums", len(entry.Albums))
	}
	return &Catalog{Albums: entry.Albums, UpdatedAt: entry.UpdatedAt}
}

// SaveCatalog writes the catalog to disk as gzipped JSON.
func SaveCatalog(baseDir string, cat *Catalog) error {
	cacheDir, err := CacheDir(baseDir)
	if err != nil {
		return err
	}
	path := filepath.Join(cacheDir, catalogFile)

	tmp, err := os.CreateTemp(cacheDir, "catalog*.tmp")
	if err != nil {
		return fmt.Errorf("modland catalog temp: %w", err)
	}
	tmpPath := tmp.Name()

	gz := gzip.NewWriter(tmp)
	if err := json.NewEncoder(gz).Encode(cacheEntry{
		Albums:    cat.Albums,
		UpdatedAt: cat.UpdatedAt,
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

// FileURL returns the primary (fast mirror) download URL for a modland file.
func FileURL(remotePath string) string {
	return "https://modland.antarctica.no/pub/modules/" + strings.TrimPrefix(remotePath, "/")
}

// FileFallbackURL returns the official modland.com fallback URL.
func FileFallbackURL(remotePath string) string {
	return "https://modland.com/pub/modules/" + strings.TrimPrefix(remotePath, "/")
}

// ParseListing parses a tab-separated text listing: "size\tpath".
// Used by cmd/modland-catalog to build the catalog from allmods.zip.
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

		// Tab-separated "size\tpath"
		if idx := strings.IndexByte(line, '\t'); idx >= 0 {
			fmt.Sscanf(line[:idx], "%d", &size)
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
