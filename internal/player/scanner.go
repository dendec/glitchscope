package player

import (
	"path/filepath"
	"strings"

	"github.com/dendec/glitchscope/internal/formats"
)

// skipPrefixes lists directories to skip during music scan.
var skipPrefixes = []string{
	"/usr", "/opt", "/etc", "/var", "/tmp",
	"/sys", "/proc", "/dev", "/boot",
	"/lib", "/bin", "/sbin",
}

// SupportedExts re-exports formats.SupportedExts for backward compatibility.
var SupportedExts = formats.SupportedExts

// IsSupportedExt re-exports formats.IsSupportedExt for backward compatibility.
func IsSupportedExt(ext string) bool {
	return formats.IsSupportedExt(ext)
}

func shouldSkipDir(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if strings.HasPrefix(base, ".") || base == "modland-cache" || base == "modarchive-cache" {
		return true
	}
	for _, p := range skipPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
