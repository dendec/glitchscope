package player

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// TrackCache manages only downloaded catalog tracks. Provider indexes and
// bundled snapshot archives deliberately live outside these roots.
type TrackCache struct {
	baseDir  string
	resolver *Resolver
	opMu     sync.Mutex
	mu       sync.Mutex
	dirs     map[string]bool
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

// Delete removes one cached virtual track. It never accepts a raw filesystem path.
func (c *TrackCache) Delete(virtualPath string) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	path := c.LocalPath(virtualPath)
	if path == "" || path == virtualPath {
		return os.ErrNotExist
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete cached track: %w", err)
	}
	c.removeEmptyParents(filepath.Dir(path))
	c.refreshIndex()
	return nil
}

// Prune enforces age and size limits using least-recently-used order. The
// protected virtual path is never removed (normally the playing track).
func (c *TrackCache) Prune(retention time.Duration, keepForever bool, maxBytes int64, protected string) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	files, err := c.files()
	if err != nil {
		return err
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
	c.refreshIndex()
	return errors.Join(errs...)
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
