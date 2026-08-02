package util

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractModuleFromZip opens zipPath, finds the best module file matching isSupportedExt
// (or the largest file if no match), and extracts it to targetPath atomically.
func ExtractModuleFromZip(zipPath, targetPath string, isSupportedExt func(string) bool) (string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer zr.Close()

	if len(zr.File) == 0 {
		return "", fmt.Errorf("empty zip archive")
	}

	var bestFile *zip.File
	if isSupportedExt != nil {
		for _, f := range zr.File {
			if f.FileInfo().IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(f.Name))
			if isSupportedExt(ext) {
				bestFile = f
				break
			}
		}
	}

	if bestFile == nil {
		var maxBytes uint64
		for _, f := range zr.File {
			if f.FileInfo().IsDir() {
				continue
			}
			if f.UncompressedSize64 > maxBytes {
				maxBytes = f.UncompressedSize64
				bestFile = f
			}
		}
	}

	if bestFile == nil {
		return "", fmt.Errorf("no suitable module file found inside zip")
	}

	rc, err := bestFile.Open()
	if err != nil {
		return "", fmt.Errorf("open zipped file %s: %w", bestFile.Name, err)
	}
	defer rc.Close()

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return "", err
	}

	tmpExtracted, err := os.CreateTemp(filepath.Dir(targetPath), "ext*.tmp")
	if err != nil {
		return "", err
	}
	tmpExtractedPath := tmpExtracted.Name()

	if _, err := io.Copy(tmpExtracted, rc); err != nil {
		_ = tmpExtracted.Close()
		_ = os.Remove(tmpExtractedPath)
		return "", fmt.Errorf("copy zipped content: %w", err)
	}
	if err := tmpExtracted.Close(); err != nil {
		_ = os.Remove(tmpExtractedPath)
		return "", err
	}

	if err := os.Rename(tmpExtractedPath, targetPath); err != nil {
		_ = os.Remove(tmpExtractedPath)
		return "", fmt.Errorf("rename extracted file: %w", err)
	}

	return targetPath, nil
}
