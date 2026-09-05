package util

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
)

// SaveGzipJSON writes v encoded as gzipped JSON to path atomically using a temp file.
func SaveGzipJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "gz*.tmp")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	tmpPath := tmp.Name()

	gz := gzip.NewWriter(tmp)
	if err := json.NewEncoder(gz).Encode(v); err != nil {
		_ = gz.Close()
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("encode json: %w", err)
	}

	if err := gz.Close(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close gzip: %w", err)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	_ = os.Chmod(tmpPath, 0o644)

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// LoadGzipJSON reads and decodes a gzipped JSON file into v.
func LoadGzipJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("open gzip reader: %w", err)
	}
	defer gz.Close()

	if err := json.NewDecoder(gz).Decode(v); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}

	return nil
}

// SaveZstdJSON writes v encoded as zstd-compressed JSON to path atomically
// using a temp file. Zstd is significantly faster than gzip on low-power
// ARM CPUs (Cortex-A53 etc.) which matters for startup catalog loading.
func SaveZstdJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "zs*.tmp")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	tmpPath := tmp.Name()

	gz, err := zstd.NewWriter(tmp)
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("zstd writer: %w", err)
	}
	if err := json.NewEncoder(gz).Encode(v); err != nil {
		_ = gz.Close()
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("encode json: %w", err)
	}

	if err := gz.Close(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close zstd: %w", err)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	_ = os.Chmod(tmpPath, 0o644)

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// LoadZstdJSON reads and decodes a zstd-compressed JSON file into v.
func LoadZstdJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	r, err := zstd.NewReader(f)
	if err != nil {
		return fmt.Errorf("open zstd reader: %w", err)
	}
	defer r.Close()

	if err := json.NewDecoder(r).Decode(v); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}

	return nil
}

// LoadZstdOrGzipJSON tries zstd first, then falls back to gzip. If the file
// is gzip-compressed it returns a migration callback that re-saves it as zstd.
// Call the returned function in a goroutine to transparently upgrade the file
// on disk without blocking startup.
func LoadZstdOrGzipJSON(path string, v any) (migrateFn func(), err error) {
	// Try zstd first.
	if err := LoadZstdJSON(path, v); err == nil {
		return nil, nil // already zstd
	}

	// Sniff first two bytes to confirm gzip before the heavier decode.
	if IsGzipFile(path) {
		if err := LoadGzipJSON(path, v); err != nil {
			return nil, err
		}
		// Return a migration callback — caller runs it in background.
		migrate := func() {
			_ = SaveZstdJSON(path, v)
		}
		return migrate, nil
	}

	// Last resort: try gzip anyway (will fail if file is corrupt).
	return nil, LoadGzipJSON(path, v)
}

// IsGzipFile reports whether path starts with the gzip magic bytes (0x1f 0x8b).
func IsGzipFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var hdr [2]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return false
	}
	return hdr[0] == 0x1f && hdr[1] == 0x8b
}

// SaveJSONAtomic writes v encoded as standard JSON to path atomically using a temp file.
func SaveJSONAtomic(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "json*.tmp")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if err := json.NewEncoder(tmp).Encode(v); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("encode json: %w", err)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	_ = os.Chmod(tmpPath, 0o644)

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// LoadJSON reads and decodes a standard JSON file into v.
func LoadJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := json.NewDecoder(f).Decode(v); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}

	return nil
}
