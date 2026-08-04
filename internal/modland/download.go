package modland

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/dendec/pmv/internal/util"
)

// DownloadFile downloads a single module file from modland to the local cache.
// Returns the local file path. Skips download if already cached and size matches.
func DownloadFile(ctx context.Context, baseDir, remotePath string, expectedSize int64, onProgress func(read, total int64)) (string, error) {
	filesDir, err := FilesDir(baseDir)
	if err != nil {
		return "", err
	}

	localPath := filepath.Join(filesDir, filepath.FromSlash(remotePath))

	urls := []string{
		FileURL(remotePath),
		FileURLLower(remotePath),
		FileFallbackURL(remotePath),
		FileFallbackURLLower(remotePath),
	}

	if err := util.DownloadWithFallback(ctx, urls, localPath, expectedSize, onProgress); err != nil {
		return "", fmt.Errorf("modland download %s: %w", remotePath, err)
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
