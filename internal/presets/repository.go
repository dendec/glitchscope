package presets

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	repositoryTextureURL  = "https://codeload.github.com/projectM-visualizer/presets-milkdrop-texture-pack/zip/refs/heads/master"
	repositoryTextureRoot = "presets-milkdrop-texture-pack-master/textures/"
)

// prepareRepositoryPack builds the canonical archive before validation and
// publication. ZIP entries retain compressed data; RES payloads are compressed
// into new entries. Presets never touch the disk as loose files.
// Each collection includes its own texture pack so removal
// cannot invalidate another collection's cache.
func prepareRepositoryPack(ctx context.Context, pack Pack, archivePath string) error {
	textures := archivePath + ".textures"
	defer os.Remove(textures)
	if err := downloadPackArchive(ctx, repositoryTextureURL, textures, maxPackDownloadBytes, nil); err != nil {
		return fmt.Errorf("download MilkDrop textures: %w", err)
	}
	normalized := archivePath + ".normalized"
	defer os.Remove(normalized)
	if err := normalizeRepositoryPack(ctx, pack, archivePath, textures, normalized); err != nil {
		return err
	}
	if err := ValidatePackArchive(normalized); err != nil {
		return err
	}
	return replaceFile(normalized, archivePath)
}

func normalizeRepositoryPack(ctx context.Context, pack Pack, presetsPath, texturesPath, target string) error {
	out, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("create normalized ZIP: %w", err)
	}
	writer := zip.NewWriter(out)
	var copyErr error
	if pack.SourceResource {
		copyErr = copyResourcePresets(ctx, writer, presetsPath)
	} else {
		copyErr = copyRepositoryEntries(ctx, writer, presetsPath, pack.SourcePresetRoot, "Presets/", false)
	}
	if copyErr == nil {
		copyErr = copyRepositoryEntries(ctx, writer, texturesPath, repositoryTextureRoot, "Textures/", true)
	}
	zipErr := writer.Close()
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if zipErr != nil {
		return fmt.Errorf("finish normalized ZIP: %w", zipErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close normalized ZIP: %w", closeErr)
	}
	return ctx.Err()
}

func copyRepositoryEntries(ctx context.Context, writer *zip.Writer, source, root, targetRoot string, textures bool) error {
	reader, err := zip.OpenReader(source)
	if err != nil {
		return fmt.Errorf("open repository ZIP: %w", err)
	}
	defer reader.Close()
	if root == "" || len(reader.File) == 0 || len(reader.File) > maxPackEntries {
		return fmt.Errorf("invalid repository root or ZIP entry count: %d", len(reader.File))
	}
	var total uint64
	for _, file := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if file.UncompressedSize64 > maxPackUncompressed-total {
			return fmt.Errorf("repository ZIP expands beyond %d bytes", maxPackUncompressed)
		}
		total += file.UncompressedSize64
		name, safe := safeZipName(file.Name)
		if !safe || file.FileInfo().IsDir() || !regularZipFile(file) {
			continue
		}
		wrapper := strings.Split(root, "/")[0] + "/"
		base := strings.ToLower(filepath.Base(name))
		notice := strings.HasPrefix(name, wrapper) && (strings.HasPrefix(base, "license") || strings.HasPrefix(base, "copying") || base == "readme.md")
		ext := filepath.Ext(name)
		if !notice && (!strings.HasPrefix(name, root) || (textures && !supportedTextureExt(ext)) || (!textures && !strings.EqualFold(ext, ".milk"))) {
			continue
		}
		// Copy the header to avoid changing the source reader's entry name.
		entry := *file
		entry.Name = targetRoot + strings.TrimPrefix(name, root)
		if notice {
			entry.Name = "Sources/" + targetRoot + strings.TrimPrefix(name, wrapper)
		}
		if err := writer.Copy(&entry); err != nil {
			return fmt.Errorf("copy repository entry %q: %w", name, err)
		}
	}
	return nil
}
