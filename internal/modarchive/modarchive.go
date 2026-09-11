// Package modarchive manages browsing and downloading module files from textfiles.com mirror.
package modarchive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dendec/glitchscope/internal/formats"
	"github.com/dendec/glitchscope/internal/player"
	"github.com/dendec/glitchscope/internal/util"
)

const (
	BaseURL       = "http://modarchive.textfiles.com/"
	SnapshotDir   = "modarchive_2007_official_snapshot_120000_modules"
	SnapshotLabel = "1987-2007"
	AddendumDir   = "modarchive_2007_official_snapshot_addendum1"
	AddendumLabel = "2007"
	indexSubDir   = "index"
	filesSubDir   = "files"
	httpTimeout   = 30 * time.Second
)

var (
	memCacheMu sync.RWMutex
	memCache   = make(map[string][]DirItem)
)

// ItemKind indicates whether a directory entry is a folder or a file.
type ItemKind int

const (
	KindDir ItemKind = iota
	KindFile
	KindArchive
)

// DirItem represents a single folder or file in a ModArchive directory.
type DirItem struct {
	Name             string   `json:"name"`       // Display name (e.g. "2023 Additions", "song.mod")
	URL              string   `json:"url"`        // Absolute or relative URL path
	Kind             ItemKind `json:"kind"`       // KindDir, KindFile, or KindArchive
	Size             int64    `json:"size"`       // Uncompressed size in bytes (0 if unknown)
	CleanName        string   `json:"clean_name"` // Stripped name for sorting/display (e.g. "song.mod" from "song.mod.zip")
	ArchiveOffset    int64    `json:"archive_offset,omitempty"`
	ArchiveEndOffset int64    `json:"archive_end_offset,omitempty"`
	CompressedSize   uint64   `json:"compressed_size,omitempty"`
	CRC32            uint32   `json:"crc32,omitempty"`
	Compression      uint16   `json:"compression,omitempty"`
}

// CacheDir returns the main modarchive cache directory (.cache/modarchive).
func CacheDir(baseDir string) (string, error) {
	dir := filepath.Join(baseDir, ".cache", "modarchive")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("modarchive mkdir: %w", err)
	}
	return dir, nil
}

// IndexDir returns the directory where HTML index JSON caches are stored.
func IndexDir(baseDir string) (string, error) {
	cd, err := CacheDir(baseDir)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cd, indexSubDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("modarchive index mkdir: %w", err)
	}
	return dir, nil
}

// FilesDir returns the directory where downloaded module files are stored.
func FilesDir(baseDir string) (string, error) {
	cd, err := CacheDir(baseDir)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cd, filesSubDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("modarchive files mkdir: %w", err)
	}
	return dir, nil
}

// MigrateLegacyCache moves old modarchive-cache/ or modarchive catalog files into .cache/modarchive/.
func MigrateLegacyCache(baseDir string) {
	targetDir, err := CacheDir(baseDir)
	if err != nil {
		return
	}

	oldCat := filepath.Join(baseDir, "modarchive")
	newCat := filepath.Join(targetDir, "catalog")
	if _, err := os.Stat(oldCat); err == nil {
		if _, err := os.Stat(newCat); os.IsNotExist(err) {
			_ = os.Rename(oldCat, newCat)
		} else {
			_ = os.Remove(oldCat)
		}
	}

	oldCacheDir := filepath.Join(baseDir, "modarchive-cache")
	if info, err := os.Stat(oldCacheDir); err == nil && info.IsDir() {
		_ = filepath.Walk(oldCacheDir, func(path string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return nil //nolint:nilerr // intentional: skip errors and dirs during cache migration
			}
			rel, err := filepath.Rel(oldCacheDir, path)
			if err != nil {
				return nil //nolint:nilerr // intentional: skip unresolvable paths
			}
			dst := filepath.Join(targetDir, rel)
			if _, err := os.Stat(dst); os.IsNotExist(err) {
				_ = os.MkdirAll(filepath.Dir(dst), 0o755)
				_ = os.Rename(path, dst)
			}
			return nil
		})
		_ = os.RemoveAll(oldCacheDir)
	}
}

// FormatDirName formats technical folder names into human-friendly labels.
func FormatDirName(raw string) string {
	raw = strings.Trim(raw, "/")
	if raw == "" {
		return "ModArchive"
	}
	if raw == SnapshotDir {
		return SnapshotLabel
	}
	if raw == AddendumDir {
		return AddendumLabel
	}
	// modarchive_2023_additions -> 2023
	if matches := yearAdditionsExtract.FindStringSubmatch(raw); len(matches) == 2 {
		return matches[1]
	}
	if strings.HasSuffix(raw, " Additions") {
		return strings.TrimSuffix(raw, " Additions")
	}
	return raw
}

// hrefRegex extracts href attributes from HTML anchor tags.
var hrefRegex = regexp.MustCompile(`(?i)<a\s+[^>]*href=["']([^"']+)["'][^>]*>([^<]*)</a>`)

// yearAdditionsRegex matches modarchive year-based addition directories.
var yearAdditionsRegex = regexp.MustCompile(`^modarchive_(\d{4})_additions/?$`)

// yearAdditionsExtract extracts the year from modarchive_*_additions format.
var yearAdditionsExtract = regexp.MustCompile(`^modarchive_(\d{4})_additions$`)

// ParseDirectoryListing parses textfiles.com directory listing HTML into a slice of DirItem.
func ParseDirectoryListing(htmlBody string, currentURL string) ([]DirItem, error) {
	if !strings.HasSuffix(currentURL, "/") {
		currentURL += "/"
	}
	parsedCurrentURL, err := url.Parse(currentURL)
	if err != nil {
		return nil, fmt.Errorf("invalid current URL %q: %w", currentURL, err)
	}

	isRoot := parsedCurrentURL.Path == "" || parsedCurrentURL.Path == "/"

	var items []DirItem
	matches := hrefRegex.FindAllStringSubmatch(htmlBody, -1)

	for _, m := range matches {
		href := strings.TrimSpace(m[1])
		text := strings.TrimSpace(m[2])

		if href == "" || href == "#" {
			continue
		}

		// Filter parent directory links
		if strings.Contains(strings.ToLower(text), "parent directory") || href == "/" || href == "../" || strings.HasPrefix(href, "/?") {
			continue
		}

		// Skip style, css, images, README, non-audio archives on root
		lowerHref := strings.ToLower(href)
		if lowerHref == "style.css" || lowerHref == "external.css" || lowerHref == "logo.png" || lowerHref == "readme" || lowerHref == "/readme" {
			continue
		}
		if lowerHref == "kiarchive.zip" || lowerHref == "/tma-waveworld.zip" || lowerHref == "tma-waveworld.zip" || lowerHref == "woolyss-chiptune-samples.zip" {
			continue
		}

		// If root directory, only accept the official archives and yearly additions.
		if isRoot {
			trimmedHref := strings.TrimPrefix(href, "/")
			rootName := strings.Trim(trimmedHref, "/")
			if rootName != SnapshotDir && rootName != AddendumDir && !yearAdditionsRegex.MatchString(trimmedHref) {
				continue
			}
		}

		// Calculate absolute URL
		relHref := href
		if strings.HasPrefix(relHref, "/") && !strings.HasPrefix(relHref, parsedCurrentURL.Path) {
			relHref = strings.TrimPrefix(relHref, "/")
		}

		relURL, err := url.Parse(relHref)
		if err != nil {
			continue
		}
		absURL := parsedCurrentURL.ResolveReference(relURL).String()

		isDir := strings.HasSuffix(href, "/")
		name := strings.Trim(href, "/")

		// If href points to a folder path without trailing slash, check text
		if !isDir && (strings.HasSuffix(text, "/") || yearAdditionsRegex.MatchString(name) || name == SnapshotDir || name == AddendumDir) {
			isDir = true
		}

		var kind ItemKind
		cleanName := name

		if isDir {
			kind = KindDir
			cleanName = FormatDirName(name)
		} else {
			kind = KindFile
			ext := strings.ToLower(filepath.Ext(name))
			if ext == ".zip" && isSnapshotDirectoryURL(parsedCurrentURL) {
				kind = KindArchive
				cleanName = strings.TrimSuffix(name, filepath.Ext(name))
			} else if ext == ".zip" {
				// E.g. "song.mod.zip" -> cleanName = "song.mod", "song.zip" -> cleanName = "song"
				inner := name[:len(name)-4]
				innerExt := strings.ToLower(filepath.Ext(inner))
				if formats.IsSupportedExt(innerExt) {
					cleanName = inner
				} else if inner != "" {
					cleanName = inner
				}
			} else if !formats.IsSupportedExt(ext) {
				// Unsupported file
				continue
			}
		}

		items = append(items, DirItem{
			Name:      name,
			URL:       absURL,
			Kind:      kind,
			CleanName: cleanName,
		})
	}

	// Sort items: Directories first, then files alphabetically by CleanName
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind // KindDir (0) before KindFile (1)
		}
		return strings.ToLower(items[i].CleanName) < strings.ToLower(items[j].CleanName)
	})

	return items, nil
}

type cachedIndexEntry struct {
	Items     []DirItem `json:"items"`
	UpdatedAt time.Time `json:"updated_at"`
}

func urlHash(targetURL string) string {
	h := sha256.Sum256([]byte(targetURL))
	return hex.EncodeToString(h[:16])
}

// ResetMemCache clears the in-memory cache (primarily for testing).
func ResetMemCache() {
	snapshotMu.Lock()
	defer snapshotMu.Unlock()
	snapshotBucketMem.clear()
	snapshotNavigationKeys = make(map[string]struct{})

	memCacheMu.Lock()
	memCache = make(map[string][]DirItem)
	memCacheMu.Unlock()
}

// FetchDirectoryCached checks if items for targetURL are available in the in-memory or disk cache without making network calls.
func FetchDirectoryCached(baseDir string, targetURL string) ([]DirItem, bool) {
	if targetURL == "" {
		targetURL = BaseURL
	}

	memCacheMu.RLock()
	if items, ok := memCache[targetURL]; ok {
		memCacheMu.RUnlock()
		return items, true
	}
	memCacheMu.RUnlock()

	if items, ok := fetchSnapshotCatalogDirectory(targetURL); ok {
		return items, true
	}

	indexDir, err := IndexDir(baseDir)
	if err != nil {
		return nil, false
	}

	cacheFile := filepath.Join(indexDir, urlHash(targetURL)+".json")
	if items, err := loadCachedIndex(cacheFile); err == nil && len(items) > 0 {
		memCacheMu.Lock()
		memCache[targetURL] = items
		memCacheMu.Unlock()
		slog.Debug("modarchive: directory loaded from disk cache into memCache", "url", targetURL, "count", len(items))
		return items, true
	}

	return nil, false
}

// FetchDirectory retrieves directory items for a URL, using cache if available or performing a synchronous HTTP GET.
func FetchDirectory(baseDir string, targetURL string) ([]DirItem, error) {
	return FetchDirectoryContext(context.Background(), baseDir, targetURL)
}

// FetchDirectoryContext loads a listing with cancellation, including cache misses.
func FetchDirectoryContext(ctx context.Context, baseDir, targetURL string) ([]DirItem, error) {
	ctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()
	if targetURL == "" {
		targetURL = BaseURL
	}

	if items, ok := FetchDirectoryCached(baseDir, targetURL); ok {
		return items, nil
	}
	if IsSnapshotArchiveURL(targetURL) {
		return fetchAndCacheArchiveIndex(ctx, baseDir, targetURL)
	}

	return fetchAndCacheDirectory(ctx, baseDir, targetURL)
}

func fetchAndCacheDirectory(ctx context.Context, baseDir string, targetURL string) ([]DirItem, error) {
	indexDir, err := IndexDir(baseDir)
	if err != nil {
		return nil, err
	}

	cacheFile := filepath.Join(indexDir, urlHash(targetURL)+".json")

	resp, err := util.Get(ctx, targetURL, nil)
	if err != nil {
		if items, cacheErr := loadCachedIndex(cacheFile); cacheErr == nil && len(items) > 0 {
			slog.Warn("modarchive: network failed, using expired cache", "url", targetURL, "error", err)
			memCacheMu.Lock()
			memCache[targetURL] = items
			memCacheMu.Unlock()
			return items, nil
		}
		return nil, fmt.Errorf("fetch modarchive directory %s: %w", targetURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if items, cacheErr := loadCachedIndex(cacheFile); cacheErr == nil && len(items) > 0 {
			slog.Warn("modarchive: HTTP error, using expired cache", "url", targetURL, "status", resp.StatusCode)
			memCacheMu.Lock()
			memCache[targetURL] = items
			memCacheMu.Unlock()
			return items, nil
		}
		return nil, fmt.Errorf("fetch modarchive directory %s: HTTP %d", targetURL, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read modarchive response %s: %w", targetURL, err)
	}

	items, err := ParseDirectoryListing(string(body), targetURL)
	if err != nil {
		return nil, err
	}

	slog.Debug("modarchive: fetched directory", "url", targetURL, "items", len(items), "bytes", len(body))
	saveCachedIndex(cacheFile, items)

	memCacheMu.Lock()
	memCache[targetURL] = items
	memCacheMu.Unlock()

	return items, nil
}

func loadCachedIndex(cacheFile string) ([]DirItem, error) {
	var entry cachedIndexEntry
	if err := util.LoadJSON(cacheFile, &entry); err != nil {
		return nil, err
	}
	return entry.Items, nil
}

func saveCachedIndex(cacheFile string, items []DirItem) {
	entry := cachedIndexEntry{
		Items:     items,
		UpdatedAt: time.Now(),
	}
	_ = util.SaveJSONAtomic(cacheFile, entry)
}

// BuildAlbum constructs a virtual Album from a ModArchive directory listing.
// Returns nil when items contain no files (i.e. it's a folder listing).
func BuildAlbum(targetURL string, items []DirItem) *player.Album {
	hasFiles := false
	for _, item := range items {
		if item.Kind == KindFile {
			hasFiles = true
			break
		}
	}
	if !hasFiles {
		return nil
	}

	var tracks []string
	for _, item := range items {
		if item.Kind == KindFile {
			tracks = append(tracks, player.ModArchivePrefix+item.URL)
		}
	}

	return &player.Album{
		Name:   "ModArchive: " + AlbumLabel(targetURL),
		Path:   player.ModArchivePrefix + targetURL,
		Tracks: tracks,
	}
}

// AlbumLabel produces a human-readable label for a ModArchive URL path.
func AlbumLabel(targetURL string) string {
	u, err := url.Parse(targetURL)
	if err != nil {
		return targetURL
	}
	p := strings.Trim(u.Path, "/")
	parts := strings.Split(p, "/")
	if len(parts) >= 3 {
		return FormatDirName(parts[0]) + "/" + strings.Join(parts[1:], "/")
	}
	if len(parts) > 0 {
		return FormatDirName(parts[0])
	}
	return "ModArchive"
}
