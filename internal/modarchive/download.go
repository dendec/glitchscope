package modarchive

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/dendec/pmv/internal/formats"
	"github.com/dendec/pmv/internal/util"
)

// DownloadAndExtract downloads a track from remoteURL to local modarchive-cache/files/.
// If the downloaded file is a ZIP archive, it automatically extracts the module file inside.
// Returns absolute path to the local ready-to-play file.
func DownloadAndExtract(ctx context.Context, baseDir, remoteURL string, onProgress func(read, total int64)) (string, error) {
	filesDir, err := FilesDir(baseDir)
	if err != nil {
		return "", err
	}

	parsedURL, err := url.Parse(remoteURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL %q: %w", remoteURL, err)
	}

	// Derive local path relative to filesDir
	urlPath := strings.TrimPrefix(parsedURL.Path, "/")
	rawLocalPath := filepath.Join(filesDir, filepath.FromSlash(urlPath))

	targetPath := rawLocalPath
	isZip := strings.HasSuffix(strings.ToLower(urlPath), ".zip")
	if isZip {
		targetPath = strings.TrimSuffix(rawLocalPath, filepath.Ext(rawLocalPath))
	}

	if info, err := os.Stat(targetPath); err == nil && info.Size() > 0 {
		slog.Debug("modarchive: track cached", "path", targetPath)
		return targetPath, nil
	}

	if isZip {
		tmpZipPath := filepath.Join(filesDir, "tmp_"+filepath.Base(rawLocalPath))
		defer os.Remove(tmpZipPath)

		if err := util.DownloadWithFallback(ctx, []string{remoteURL}, tmpZipPath, 0, onProgress); err != nil {
			return "", fmt.Errorf("modarchive download %s: %w", remoteURL, err)
		}

		extractedPath, err := util.ExtractModuleFromZip(tmpZipPath, targetPath, formats.IsSupportedExt)
		if err != nil {
			return "", fmt.Errorf("modarchive extract zip %s: %w", remoteURL, err)
		}
		slog.Info("modarchive: cached and extracted track", "path", extractedPath)
		return extractedPath, nil
	}

	if err := util.DownloadWithFallback(ctx, []string{remoteURL}, targetPath, 0, onProgress); err != nil {
		return "", fmt.Errorf("modarchive download %s: %w", remoteURL, err)
	}

	slog.Info("modarchive: cached track", "path", targetPath)
	return targetPath, nil
}

// ClearFiles removes all downloaded track files from the modarchive cache.
func ClearFiles(baseDir string) error {
	filesDir, err := FilesDir(baseDir)
	if err != nil {
		return err
	}
	return os.RemoveAll(filesDir)
}
