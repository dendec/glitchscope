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

// progressReader wraps an io.Reader and reports cumulative bytes read.
type progressReader struct {
	r          io.Reader
	read       int64
	total      int64
	onProgress func(read, total int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	if n > 0 {
		pr.read += int64(n)
		pr.onProgress(pr.read, pr.total)
	}
	return n, err
}

// DownloadFile downloads a single module file from modland to the local cache.
// Returns the local file path. Skips download if already cached and size matches.
func DownloadFile(baseDir, remotePath string, expectedSize int64, onProgress func(read, total int64)) (string, error) {
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

	client := &http.Client{Timeout: downloadTimeout}

	// Try primary mirror (original case), then lowercase, then official mirror.
	urls := []struct {
		label string
		url   string
	}{
		{"mirror", FileURL(remotePath)},
		{"mirror (lowercase)", FileURLLower(remotePath)},
		{"fallback", FileFallbackURL(remotePath)},
		{"fallback (lowercase)", FileFallbackURLLower(remotePath)},
	}

	var resp *http.Response
	for i, u := range urls {
		slog.Debug("modland: trying", "url", u.url)
		resp, err = client.Get(u.url)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			slog.Warn("modland: "+u.label+" failed", "url", u.url, "status", statusCode(resp))
			continue
		}
		if i > 0 {
			slog.Warn("modland: succeeded via "+u.label, "url", u.url)
		}
		break
	}
	if resp == nil || resp.StatusCode != http.StatusOK {
		status := statusCode(resp)
		return "", fmt.Errorf("modland download %s: all mirrors failed (last status %d)", remotePath, status)
	}
	defer resp.Body.Close()

	total := expectedSize
	if total <= 0 && resp.ContentLength > 0 {
		total = resp.ContentLength
	}

	// Write to temp file, then rename for atomicity.
	tmp, err := os.CreateTemp(filesDir, "mod*.tmp")
	if err != nil {
		return "", fmt.Errorf("modland temp: %w", err)
	}
	tmpPath := tmp.Name()

	var reader io.Reader = resp.Body
	if onProgress != nil {
		reader = &progressReader{r: resp.Body, total: total, onProgress: onProgress}
	}
	if _, err := io.Copy(tmp, reader); err != nil {
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

func statusCode(resp *http.Response) int {
	if resp != nil {
		return resp.StatusCode
	}
	return 0
}
