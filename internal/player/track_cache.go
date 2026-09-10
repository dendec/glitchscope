package player

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dendec/glitchscope/internal/formats"
	"github.com/dendec/glitchscope/internal/util"
)

// TrackCache manages only downloaded catalog tracks. Provider indexes and
// bundled snapshot archives deliberately live outside these roots.
type TrackCache struct {
	baseDir  string
	resolver *Resolver
	opMu     sync.Mutex
	mu       sync.Mutex
	dirs     map[string]bool
	manifest map[string]struct{}

	initialized bool
}

const (
	trackCacheManifestFile    = "cached-tracks.json"
	trackCacheManifestVersion = 1
)

type trackCacheManifest struct {
	Version int      `json:"version"`
	Paths   []string `json:"paths"`
}

type cacheFile struct {
	path   string
	size   int64
	usedAt time.Time
}

func NewTrackCache(baseDir string) *TrackCache {
	r := NewResolver()
	r.SetBaseDir(baseDir)
	return &TrackCache{baseDir: baseDir, resolver: r, dirs: make(map[string]bool)}
}

// IsCached reports whether virtualPath has a retained local file.
func (c *TrackCache) IsCached(virtualPath string) bool { return c.resolver.IsCached(virtualPath) }

// LocalPath returns the retained local path, or an empty string.
func (c *TrackCache) LocalPath(virtualPath string) string {
	return c.resolver.ResolveLocalPath(virtualPath)
}

// HasDescendant reports whether a virtual catalog directory contains at least
// one cached track.
func (c *TrackCache) HasDescendant(virtualPath string) bool {
	root := c.resolver.CacheCandidatePath(virtualPath)
	if root == "" {
		return false
	}
	c.mu.Lock()
	found := c.dirs[filepath.Clean(root)]
	c.mu.Unlock()
	return found
}

// Touch records actual use without relying on filesystem access-time support.
func (c *TrackCache) Touch(virtualPath string) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	path := c.LocalPath(virtualPath)
	if path == "" || path == virtualPath {
		return nil
	}
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		return fmt.Errorf("touch cached track: %w", err)
	}
	c.indexPath(path)
	return nil
}

// RegisterDownloaded adds a newly completed remote download to the
// reconstructible cache manifest. Touch intentionally does not call this:
// normal playback of an already-known cached file must only update its mtime.
func (c *TrackCache) RegisterDownloaded(virtualPath string) error {
	if !IsModland(virtualPath) && !IsModArchive(virtualPath) {
		return nil
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if !fileExists(c.LocalPath(virtualPath)) {
		return os.ErrNotExist
	}
	if !c.initialized {
		if err := c.reconcileManifestLocked(); err != nil {
			return err
		}
	}
	c.mu.Lock()
	_, exists := c.manifest[virtualPath]
	next := cloneManifest(c.manifest)
	next[virtualPath] = struct{}{}
	paths := manifestPaths(next)
	c.mu.Unlock()
	if exists {
		return nil
	}
	c.mu.Lock()
	c.manifest = next
	c.mu.Unlock()
	return c.saveManifest(paths)
}

// ReconcileManifest rebuilds the in-memory cache view from the manifest and
// the actual cache roots. The files are authoritative: missing, zero-byte,
// temporary, or malformed manifest entries are discarded, while discovered
// files absent from the manifest are added. The resulting manifest is written
// atomically when it changes.
func (c *TrackCache) ReconcileManifest() error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.reconcileManifestLocked()
}

// CachedVirtualPaths returns a stable snapshot of cached remote paths.
// Provider-specific shuffle metadata remains the app orchestration layer's
// responsibility.
func (c *TrackCache) CachedVirtualPaths() []string {
	c.mu.Lock()
	paths := manifestPaths(c.manifest)
	c.mu.Unlock()
	return paths
}

// Delete removes one cached virtual track. It never accepts a raw filesystem path.
func (c *TrackCache) Delete(virtualPath string) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if !c.initialized {
		if err := c.reconcileManifestLocked(); err != nil {
			return err
		}
	}
	path := c.LocalPath(virtualPath)
	if path == "" || path == virtualPath {
		return os.ErrNotExist
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete cached track: %w", err)
	}
	c.removeEmptyParents(filepath.Dir(path))
	manifestErr := c.removeManifestLocalPathLocked(path)
	c.refreshIndex()
	return manifestErr
}

// Prune enforces age and size limits using least-recently-used order. The
// protected virtual path is never removed (normally the playing track).
func (c *TrackCache) Prune(retention time.Duration, keepForever bool, maxBytes int64, protected string) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if !c.initialized {
		if err := c.reconcileManifestLocked(); err != nil {
			return err
		}
	}
	files, listErr := c.files()
	if listErr != nil {
		return listErr
	}
	protectedPath := c.LocalPath(protected)
	now := time.Now()
	remaining := files[:0]
	var total int64
	var errs []error
	for _, file := range files {
		expired := !keepForever && (retention == 0 || now.Sub(file.usedAt) >= retention)
		if expired && file.path != protectedPath {
			if err := os.Remove(file.path); err != nil {
				errs = append(errs, fmt.Errorf("remove expired %s: %w", file.path, err))
				remaining = append(remaining, file)
				total += file.size
			}
			continue
		}
		remaining = append(remaining, file)
		total += file.size
	}
	if maxBytes > 0 && total > maxBytes {
		sort.Slice(remaining, func(i, j int) bool { return remaining[i].usedAt.Before(remaining[j].usedAt) })
		for _, file := range remaining {
			if total <= maxBytes {
				break
			}
			if file.path == protectedPath {
				continue
			}
			if err := os.Remove(file.path); err != nil {
				errs = append(errs, fmt.Errorf("remove LRU %s: %w", file.path, err))
				continue
			}
			total -= file.size
		}
	}
	for _, root := range c.roots() {
		c.removeEmptyTree(root)
	}
	manifestErr := c.removeMissingManifestEntriesLocked()
	c.refreshIndex()
	return errors.Join(errors.Join(errs...), manifestErr)
}

func (c *TrackCache) refreshIndex() {
	dirs := make(map[string]bool)
	for _, root := range c.roots() {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
				dirs[filepath.Clean(dir)] = true
				if dir == root {
					break
				}
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			slog.Warn("track cache index", "root", root, "error", err)
		}
	}
	c.mu.Lock()
	c.dirs = dirs
	c.mu.Unlock()
}

func (c *TrackCache) reconcileManifestLocked() error {
	manifestPath := c.manifestPath()
	var disk trackCacheManifest
	manifestReadable := false
	manifestNeedsRewrite := false
	if err := util.LoadJSON(manifestPath, &disk); err == nil {
		manifestReadable = disk.Version == trackCacheManifestVersion
		manifestNeedsRewrite = !manifestReadable
	} else if !os.IsNotExist(err) {
		slog.Warn("track cache manifest unreadable, rebuilding", "path", manifestPath, "error", err)
		manifestNeedsRewrite = true
	}

	paths := make(map[string]struct{})
	manifestPathsSet := make(map[string]struct{})
	if manifestReadable {
		for _, virtualPath := range disk.Paths {
			// A single stale entry must not invalidate the rest of the
			// manifest. Preserve the original ModArchive fragment for every
			// still-existing file.
			if isCacheVirtualPath(virtualPath) && fileExists(c.LocalPath(virtualPath)) {
				paths[virtualPath] = struct{}{}
				manifestPathsSet[virtualPath] = struct{}{}
			} else {
				manifestNeedsRewrite = true
			}
		}
	}
	discovered, discoverErr := c.discoverVirtualPaths()
	if discoverErr != nil {
		slog.Warn("track cache discovery incomplete", "error", discoverErr)
	}
	for _, virtualPath := range discovered {
		paths[virtualPath] = struct{}{}
	}

	// One physical file can be addressed by multiple ModArchive URLs (for
	// example a ZIP path with or without its .zip suffix). Keep the manifest
	// deterministic and avoid selecting the same file twice.
	byLocalPath := make(map[string]string, len(paths))
	byLocalPathPreferred := make(map[string]bool, len(paths))
	for virtualPath := range paths {
		localPath := c.LocalPath(virtualPath)
		if !fileExists(localPath) {
			continue
		}
		preferred := false
		if _, ok := manifestPathsSet[virtualPath]; ok {
			preferred = true
		}
		previous, ok := byLocalPath[localPath]
		if !ok || preferred && !byLocalPathPreferred[localPath] ||
			preferred == byLocalPathPreferred[localPath] && virtualPath < previous {
			byLocalPath[localPath] = virtualPath
			byLocalPathPreferred[localPath] = preferred
		}
	}
	paths = make(map[string]struct{}, len(byLocalPath))
	for _, virtualPath := range byLocalPath {
		paths[virtualPath] = struct{}{}
	}

	current := manifestPaths(paths)
	diskPaths := slices.Clone(disk.Paths)
	slices.Sort(diskPaths)
	if manifestReadable && slices.Equal(diskPaths, current) {
		manifestNeedsRewrite = false
	}
	if discoverErr != nil {
		return discoverErr
	}
	c.mu.Lock()
	c.manifest = paths
	c.initialized = true
	c.mu.Unlock()
	c.refreshIndex()
	if manifestNeedsRewrite || !manifestReadable {
		return c.saveManifest(current)
	}
	return nil
}

func (c *TrackCache) saveManifest(paths []string) error {
	return util.SaveJSONAtomic(c.manifestPath(), trackCacheManifest{
		Version: trackCacheManifestVersion,
		Paths:   paths,
	})
}

func (c *TrackCache) manifestPath() string {
	return filepath.Join(c.baseDir, ".cache", "shuffle", trackCacheManifestFile)
}

func (c *TrackCache) discoverVirtualPaths() ([]string, error) {
	var paths []string
	var errs []error
	for _, root := range c.roots() {
		err := filepath.WalkDir(root, func(filePath string, entry os.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() || info.Size() <= 0 || strings.HasPrefix(entry.Name(), "tmp_") {
				return nil
			}
			rel, err := filepath.Rel(root, filePath)
			if err != nil {
				return fmt.Errorf("relative cache path: %w", err)
			}
			if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil
			}
			virtualPath := c.resolver.VirtualPathForCacheFile(filePath)
			ext := filepath.Ext(filePath)
			playable := formats.IsSupportedExt(ext) || IsModArchive(virtualPath) && ext == ""
			if virtualPath == "" || !playable {
				return nil
			}
			paths = append(paths, virtualPath)
			return nil
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("scan %s: %w", root, err))
		}
	}
	sort.Strings(paths)
	return paths, errors.Join(errs...)
}

func isCacheVirtualPath(virtualPath string) bool {
	return IsModland(virtualPath) || IsModArchive(virtualPath)
}

func manifestPaths(manifest map[string]struct{}) []string {
	paths := make([]string, 0, len(manifest))
	for virtualPath := range manifest {
		paths = append(paths, virtualPath)
	}
	sort.Strings(paths)
	return paths
}

func cloneManifest(manifest map[string]struct{}) map[string]struct{} {
	cloned := make(map[string]struct{}, len(manifest)+1)
	for virtualPath := range manifest {
		cloned[virtualPath] = struct{}{}
	}
	return cloned
}

func (c *TrackCache) removeManifestLocalPathLocked(localPath string) error {
	c.mu.Lock()
	next := cloneManifest(c.manifest)
	c.mu.Unlock()
	for virtualPath := range next {
		if c.resolver.CacheCandidatePath(virtualPath) == localPath {
			delete(next, virtualPath)
		}
	}
	return c.commitManifestLocked(next)
}

func (c *TrackCache) removeMissingManifestEntriesLocked() error {
	c.mu.Lock()
	next := cloneManifest(c.manifest)
	c.mu.Unlock()
	for virtualPath := range next {
		if !fileExists(c.LocalPath(virtualPath)) {
			delete(next, virtualPath)
		}
	}
	return c.commitManifestLocked(next)
}

func (c *TrackCache) commitManifestLocked(next map[string]struct{}) error {
	paths := manifestPaths(next)
	c.mu.Lock()
	c.manifest = next
	c.initialized = true
	c.mu.Unlock()
	return c.saveManifest(paths)
}

func (c *TrackCache) indexPath(path string) {
	dirs := make([]string, 0, 8)
	for _, root := range c.roots() {
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
			dirs = append(dirs, filepath.Clean(dir))
			if dir == root {
				break
			}
		}
		break
	}
	c.mu.Lock()
	if c.dirs == nil {
		c.dirs = make(map[string]bool)
	}
	for _, dir := range dirs {
		c.dirs[dir] = true
	}
	c.mu.Unlock()
}

func (c *TrackCache) roots() []string {
	return []string{
		filepath.Join(c.baseDir, ".cache", "modland", "files"),
		filepath.Join(c.baseDir, ".cache", "modarchive", "files"),
	}
}

func (c *TrackCache) files() ([]cacheFile, error) {
	var files []cacheFile
	var errs []error
	for _, root := range c.roots() {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() && info.Size() > 0 && !strings.HasPrefix(entry.Name(), "tmp_") {
				files = append(files, cacheFile{path: path, size: info.Size(), usedAt: info.ModTime()})
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("scan track cache %s: %w", root, err))
		}
	}
	return files, errors.Join(errs...)
}

func (c *TrackCache) removeEmptyParents(dir string) {
	for _, root := range c.roots() {
		rel, err := filepath.Rel(root, dir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		for dir != root {
			if err := os.Remove(dir); err != nil {
				break
			}
			dir = filepath.Dir(dir)
		}
		return
	}
}

func (c *TrackCache) removeEmptyTree(root string) {
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.IsDir() && path != root {
			dirs = append(dirs, path)
		}
		return nil
	})
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, dir := range dirs {
		if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Debug("track cache: keep non-empty directory", "path", dir, "error", err)
		}
	}
}
