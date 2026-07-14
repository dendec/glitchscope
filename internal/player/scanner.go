package player

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultMusicDirs contains paths to check for music files on various CFWs.
// Checked for existence at runtime; only existing dirs are scanned.
var DefaultMusicDirs = []string{
	".",
	"/userdata/roms/music",
	"/userdata/music",
	"/roms/music",
	"/mnt/SDCARD/Music",
	"/mnt/SDCARD/Data/music",
	"/storage/roms/music",
	"/storage/music",
}

// skipPrefixes — directories whose prefix causes the walk to skip.
var skipPrefixes = []string{
	"/usr", "/opt", "/etc", "/var", "/tmp",
	"/sys", "/proc", "/dev", "/boot",
	"/lib", "/bin", "/sbin",
}

// SupportedExts maps lower-case file extensions that the scanner looks for.
var SupportedExts = map[string]bool{
	".mp3": true, ".ogg": true, ".flac": true, ".wav": true,
	".mod": true, ".xm": true, ".it": true, ".s3m": true,
}

// Scan recursively walks dir and returns all audio file paths.
// Symlinks are not followed. Inaccessible dirs are skipped silently.
func Scan(dir string) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("scan dir %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("scan dir %s: not a directory", dir)
	}

	var files []string
	err = filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible
		}
		if fi.IsDir() {
			if shouldSkipDir(path) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if SupportedExts[ext] {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

// ScanAll scans each existing DefaultMusicDirs and merges results.
// Duplicates and inaccessible dirs are silently skipped.
func ScanAll() ([]string, error) {
	seen := map[string]bool{}
	var all []string
	for _, dir := range DefaultMusicDirs {
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		files, err := Scan(abs)
		if err != nil {
			continue
		}
		for _, f := range files {
			if !seen[f] {
				seen[f] = true
				all = append(all, f)
			}
		}
	}
	sort.Strings(all)
	return all, nil
}

func shouldSkipDir(path string) bool {
	for _, p := range skipPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
