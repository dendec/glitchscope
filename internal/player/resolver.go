package player

import (
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Resolver maps virtual track paths (modland:, modarchive:) to local cache
// files on disk. It is the single owner of the cache-path rules; Library
// uses it as a dependency for metadata extraction and cache checks.
type Resolver struct {
	baseDir string // application root; cache files live under .cache/
}

// NewResolver returns a Resolver with no base directory set.
func NewResolver() *Resolver {
	return &Resolver{}
}

// SetBaseDir sets the application root directory used to locate cache files.
func (r *Resolver) SetBaseDir(dir string) {
	r.baseDir = dir
}

// IsCached reports whether a virtual track path has a local cache file on disk.
func (r *Resolver) IsCached(virtualPath string) bool {
	return r.ResolveLocalPath(virtualPath) != ""
}

// CacheCandidatePath maps a virtual track or catalog directory to its cache
// location without requiring it to exist.
func (r *Resolver) CacheCandidatePath(virtualPath string) string {
	if r.baseDir == "" {
		return ""
	}
	remote := RemotePath(virtualPath)
	if IsModland(virtualPath) {
		filesDir := filepath.Join(r.baseDir, ".cache", "modland", "files")
		return safeCacheJoin(filesDir, remote)
	}
	if IsModArchive(virtualPath) {
		return ModArchiveCachePath(r.baseDir, remote)
	}
	return ""
}

// ResolveLocalPath returns the local cache path for a virtual track path
// (modland:…, modarchive:…) if it exists on disk, or the original path for
// local files. Returns "" when the virtual path has no cached file.
func (r *Resolver) ResolveLocalPath(virtualPath string) string {
	if !IsModland(virtualPath) && !IsModArchive(virtualPath) {
		return virtualPath // local file — use as-is
	}
	remote := RemotePath(virtualPath)
	if r.baseDir == "" {
		return ""
	}
	if IsModland(virtualPath) {
		filesDir := filepath.Join(r.baseDir, ".cache", "modland", "files")
		local := safeCacheJoin(filesDir, remote)
		if local == "" {
			return ""
		}
		if fileExists(local) {
			return local
		}
		return ""
	}
	if IsModArchive(virtualPath) {
		local := ModArchiveCachePath(r.baseDir, remote)
		if fileExists(local) {
			return local
		}
		return ""
	}
	return ""
}

// ModArchiveCachePath maps a ModArchive track URL to its canonical local cache path.
func ModArchiveCachePath(baseDir, remote string) string {
	parsed, err := url.Parse(remote)
	if err != nil {
		return ""
	}
	urlPath := strings.TrimPrefix(parsed.Path, "/")
	if urlPath == "" {
		return ""
	}
	filesDir := filepath.Join(baseDir, ".cache", "modarchive", "files")
	if parsed.Fragment == "" {
		local := safeCacheJoin(filesDir, urlPath)
		if local == "" {
			return ""
		}
		if strings.EqualFold(filepath.Ext(local), ".zip") {
			local = strings.TrimSuffix(local, filepath.Ext(local))
		}
		return local
	}

	entryPath := path.Clean(parsed.Fragment)
	if entryPath == "." || path.IsAbs(parsed.Fragment) || entryPath == ".." || strings.HasPrefix(entryPath, "../") || strings.Contains(parsed.Fragment, "\\") || strings.Contains(parsed.Fragment, "\x00") {
		return ""
	}
	archivePath := strings.TrimSuffix(urlPath, path.Ext(urlPath))
	local := safeCacheJoin(filesDir, path.Join(archivePath, entryPath))
	if local == "" {
		return ""
	}
	if strings.EqualFold(filepath.Ext(local), ".zip") {
		local = strings.TrimSuffix(local, filepath.Ext(local))
	}
	return local
}

func safeCacheJoin(root, slashPath string) string {
	if slashPath == "" || path.IsAbs(slashPath) || strings.Contains(slashPath, "\\") || strings.Contains(slashPath, "\x00") {
		return ""
	}
	cleaned := path.Clean(slashPath)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	local := filepath.Join(root, filepath.FromSlash(cleaned))
	rel, err := filepath.Rel(root, local)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return ""
	}
	return local
}

// fileExists reports whether path exists on disk and is non-empty. A zero-byte
// cache file is treated as absent.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}
