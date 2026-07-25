package modland

import (
	"bytes"
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
			// Size matches — verify magic bytes.
			if verifyErr := verifyModuleFile(localPath); verifyErr != nil {
				slog.Warn("modland: corrupted cache, re-downloading", "path", remotePath, "error", verifyErr)
				os.Remove(localPath)
			} else {
				return localPath, nil
			}
		} else {
			// Size mismatch — re-download.
			slog.Warn("modland: size mismatch, re-downloading", "path", remotePath, "cached", info.Size(), "expected", expectedSize)
			os.Remove(localPath)
		}
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

	// Verify magic bytes — corrupted files pass size check.
	if err := verifyModuleFile(localPath); err != nil {
		os.Remove(localPath)
		return "", fmt.Errorf("modland download %s: %w", remotePath, err)
	}

	slog.Info("modland: cached", "path", remotePath)
	return localPath, nil
}

// verifyModuleFile checks that a file starts with known module magic bytes.
func verifyModuleFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Read enough for all format checks: XM header + IT header + S3M offset.
	buf := make([]byte, 64)
	n, err := f.Read(buf)
	if err != nil || n < 4 {
		return fmt.Errorf("file too small (%d bytes)", n)
	}
	buf = buf[:n]

	// XM: "Extended Module: " at offset 0.
	if bytes.HasPrefix(buf, []byte("Extended Module: ")) {
		return nil
	}
	// IT: "IMPM" at offset 0.
	if bytes.HasPrefix(buf, []byte("IMPM")) {
		return nil
	}
	// MTM: "MTM" at offset 0.
	if bytes.HasPrefix(buf, []byte("MTM")) {
		return nil
	}
	// S3M: "SCRM" at offset 44.
	if n >= 48 && bytes.Equal(buf[44:48], []byte("SCRM")) {
		return nil
	}
	// MOD: magic at offset 1080 — need to seek.
	f.Seek(1080, io.SeekStart)
	modMagic := make([]byte, 4)
	if _, err := io.ReadFull(f, modMagic); err == nil {
		for _, sig := range [][]byte{
			[]byte("M.K."), []byte("M!K!"), []byte("4CHN"), []byte("6CHN"), []byte("8CHN"),
			[]byte("CD81"), []byte("OKTA"), []byte("FLT4"), []byte("FLT8"),
		} {
			if bytes.Equal(modMagic, sig) {
				return nil
			}
		}
	}

	return fmt.Errorf("unknown module format (magic: %q)", buf[:min(4, n)])
}

// ClearFiles removes all downloaded module files from the cache.
func ClearFiles(baseDir string) error {
	filesDir, err := FilesDir(baseDir)
	if err != nil {
		return err
	}
	return os.RemoveAll(filesDir)
}
