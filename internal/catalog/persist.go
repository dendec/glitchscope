package catalog

import (
	"fmt"
	"os"
	"path/filepath"
)

// ShuffleIndexDir returns the directory for a source's shuffle index file.
func ShuffleIndexDir(baseDir, source string) (string, error) {
	dir := filepath.Join(baseDir, ".cache", "shuffle")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("shuffle index mkdir: %w", err)
	}
	return dir, nil
}

// WriteIndexAtomically writes caller-validated data to a temporary file in dir
// and atomically renames it to filename. The previous file is preserved until
// the rename succeeds.
func WriteIndexAtomically(dir, filename string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("index mkdir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, filename+"*.tmp")
	if err != nil {
		return fmt.Errorf("index temp: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("index write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("index sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("index close: %w", err)
	}

	target := filepath.Join(dir, filename)
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("index rename: %w", err)
	}
	return nil
}

// ReadIndexFile reads the named file from dir. Returns nil data and nil error
// if the file does not exist.
func ReadIndexFile(dir, filename string) ([]byte, error) {
	path := filepath.Join(dir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("index read: %w", err)
	}
	return data, nil
}
