// Package util provides shared HTTP, download, archive, cache, and persistence utilities.
package util

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
)

// ProgressReader wraps an io.Reader and reports cumulative bytes read.
type ProgressReader struct {
	r          io.Reader
	read       int64
	total      int64
	onProgress func(read, total int64)
}

// NewProgressReader wraps r and reports cumulative bytes read.
func NewProgressReader(r io.Reader, total int64, onProgress func(read, total int64)) io.Reader {
	if onProgress == nil {
		return r
	}
	return &ProgressReader{r: r, total: total, onProgress: onProgress}
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	if n > 0 {
		pr.read += int64(n)
		if pr.onProgress != nil {
			pr.onProgress(pr.read, pr.total)
		}
	}
	return n, err
}

// DownloadWithFallback downloads from the first succeeding URL in urls to targetPath atomically.
// The context controls cancellation of the entire operation including HTTP requests.
func DownloadWithFallback(ctx context.Context, urls []string, targetPath string, expectedSize int64, onProgress func(read, total int64)) error {
	if info, err := os.Stat(targetPath); err == nil {
		if (expectedSize > 0 && info.Size() == expectedSize) || (expectedSize <= 0 && info.Size() > 0) {
			return nil
		}
		slog.Warn("download: invalid or size mismatch, re-downloading", "path", targetPath, "cached", info.Size(), "expected", expectedSize)
		_ = os.Remove(targetPath)
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	var resp *http.Response
	var lastErr error
	lastStatus := 0

	for i, u := range urls {
		if err := ctx.Err(); err != nil {
			return err
		}
		slog.Debug("downloading", "url", u)
		resp, lastErr = Get(ctx, u, nil)
		if lastErr != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				lastStatus = resp.StatusCode
				resp.Body.Close()
			}
			slog.Debug("download URL failed", "url", u, "status", lastStatus, "error", lastErr)
			continue
		}
		if i > 0 {
			slog.Warn("download succeeded via fallback URL", "url", u)
		}
		break
	}

	if resp == nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			lastStatus = resp.StatusCode
		}
		return fmt.Errorf("download failed (last status %d): %w", lastStatus, lastErr)
	}
	defer resp.Body.Close()

	total := expectedSize
	if total <= 0 && resp.ContentLength > 0 {
		total = resp.ContentLength
	}

	tmp, err := os.CreateTemp(filepath.Dir(targetPath), "dl*.tmp")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	tmpPath := tmp.Name()

	reader := NewProgressReader(resp.Body, total, onProgress)

	if _, err := io.Copy(tmp, reader); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("write file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	if expectedSize > 0 {
		if info, err := os.Stat(targetPath); err == nil && info.Size() != expectedSize {
			_ = os.Remove(targetPath)
			return fmt.Errorf("download size mismatch (got %d, want %d)", info.Size(), expectedSize)
		}
	}

	return nil
}
