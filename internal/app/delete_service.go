package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	errDeleteEmpty   = errors.New("empty path")
	errDeleteDot     = errors.New("cannot delete \".\"")
	errDeleteRoot    = errors.New("cannot delete library root")
	errDeleteOutside = errors.New("path outside library")
	errDeleteSymlink = errors.New("symlinks not allowed")
)

// deleteService validates and deletes paths within the library boundary.
// baseDir is resolved once at startup; all deletes must be inside it.
type deleteService struct {
	resolvedBase string
}

func newDeleteService(baseDir string) *deleteService {
	resolved, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		resolved = filepath.Clean(baseDir)
	}
	return &deleteService{resolvedBase: resolved}
}

// Validate checks that path is a safe deletion target inside the library.
func (ds *deleteService) Validate(path string) error {
	if path == "" {
		return errDeleteEmpty
	}
	cleaned := filepath.Clean(path)
	switch {
	case cleaned == ".":
		return errDeleteDot
	case cleaned == ds.resolvedBase:
		return errDeleteRoot
	case !strings.HasPrefix(cleaned, ds.resolvedBase+string(filepath.Separator)):
		return errDeleteOutside
	}
	// v1: no symlinks anywhere in the path.
	if resolved, err := filepath.EvalSymlinks(cleaned); err != nil {
		return fmt.Errorf("path not accessible: %w", err)
	} else if resolved != cleaned {
		return errDeleteSymlink
	}
	return nil
}

// Delete validates and removes path (file or directory tree).
func (ds *deleteService) Delete(path string) error {
	if err := ds.Validate(path); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Clean(path))
}

// FileCount returns the number of regular files under path (for confirmation threshold).
func (ds *deleteService) FileCount(path string) (int, error) {
	if err := ds.Validate(path); err != nil {
		return 0, err
	}
	cleaned := filepath.Clean(path)
	info, err := os.Stat(cleaned)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 1, nil
	}
	count := 0
	_ = filepath.Walk(cleaned, func(_ string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip inaccessible entries
		}
		if !fi.IsDir() {
			count++
		}
		return nil
	})
	return count, nil
}
