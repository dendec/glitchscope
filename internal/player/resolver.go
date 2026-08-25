package player

import (
	"net/url"
	"os"
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
		local := filepath.Join(filesDir, filepath.FromSlash(remote))
		if fileExists(local) {
			return local
		}
		return ""
	}
	if IsModArchive(virtualPath) {
		// ModArchive files may be ZIP-extracted; the URL path determines
		// the local name. Try the raw URL path and the .zip-stripped form.
		filesDir := filepath.Join(r.baseDir, ".cache", "modarchive", "files")
		parsed, err := url.Parse(remote)
		if err != nil {
			return ""
		}
		urlPath := strings.TrimPrefix(parsed.Path, "/")
		raw := filepath.Join(filesDir, filepath.FromSlash(urlPath))
		// Direct file (non-zip).
		if fileExists(raw) {
			return raw
		}
		// ZIP-extracted: strip .zip extension.
		if strings.HasSuffix(strings.ToLower(raw), ".zip") {
			extracted := strings.TrimSuffix(raw, filepath.Ext(raw))
			if fileExists(extracted) {
				return extracted
			}
		}
		return ""
	}
	return ""
}

// fileExists reports whether path exists on disk and is non-empty. A zero-byte
// cache file is treated as absent.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}
