package modland

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const downloadTimeout = 60 * time.Second

// DownloadFile downloads a single module file from modland to the local cache.
// Returns the local file path. Skips download if already cached and size matches.
// If expectedSize > 0 and cached file has different size, re-downloads.
func DownloadFile(baseDir, remotePath string, expectedSize int64) (string, error) {
	filesDir, err := FilesDir(baseDir)
	if err != nil {
		return "", err
	}

	localPath := filepath.Join(filesDir, filepath.FromSlash(remotePath))

	// Check if already cached.
	if info, err := os.Stat(localPath); err == nil {
		if expectedSize == 0 || info.Size() == expectedSize {
			return localPath, nil
		}
		// Size mismatch — re-download.
		slog.Warn("modland: size mismatch, re-downloading", "path", remotePath, "cached", info.Size(), "expected", expectedSize)
		os.Remove(localPath)
	}

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return "", fmt.Errorf("modland mkdir: %w", err)
	}

	url := FileURL(remotePath)
	slog.Debug("modland: downloading", "url", url)

	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		// Fallback to official mirror.
		fallback := FileFallbackURL(remotePath)
		slog.Warn("modland: mirror failed, trying fallback", "mirror", url, "fallback", fallback)
		resp, err = client.Get(fallback)
		if err != nil {
			return "", fmt.Errorf("modland download %s: %w", remotePath, err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return "", fmt.Errorf("modland download %s: status %d", remotePath, resp.StatusCode)
		}
	}
	defer resp.Body.Close()

	// Write to temp file, then rename for atomicity.
	tmp, err := os.CreateTemp(filesDir, "mod*.tmp")
	if err != nil {
		return "", fmt.Errorf("modland temp: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("modland write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	if err := os.Rename(tmpPath, localPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("modland rename: %w", err)
	}

	// Verify downloaded size.
	if expectedSize > 0 {
		if info, err := os.Stat(localPath); err == nil && info.Size() != expectedSize {
			os.Remove(localPath)
			return "", fmt.Errorf("modland download %s: size mismatch (got %d, want %d)", remotePath, info.Size(), expectedSize)
		}
	}

	slog.Info("modland: cached", "path", remotePath)
	return localPath, nil
}

// ClearFiles removes all downloaded module files from the cache.
func ClearFiles(baseDir string) error {
	filesDir, err := FilesDir(baseDir)
	if err != nil {
		return err
	}
	return os.RemoveAll(filesDir)
}
