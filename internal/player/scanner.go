package player

import (
	"strings"
)

// skipPrefixes — directories whose prefix causes the walk to skip.
var skipPrefixes = []string{
	"/usr", "/opt", "/etc", "/var", "/tmp",
	"/sys", "/proc", "/dev", "/boot",
	"/lib", "/bin", "/sbin",
}

// SupportedExts maps lower-case file extensions that the scanner looks for.
var SupportedExts = map[string]bool{
	".mp3": true, ".ogg": true, ".flac": true, ".wav": true,
	// Tracker formats via libopenmpt
	".mod": true, ".xm": true, ".it": true, ".s3m": true,
}

func shouldSkipDir(path string) bool {
	for _, p := range skipPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
