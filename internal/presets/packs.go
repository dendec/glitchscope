package presets

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/dendec/glitchscope/internal/util"
)

const (
	maxPackEntries       = 30000
	maxPackUncompressed  = 512 << 20
	maxPackDownloadBytes = 256 << 20
	maxTexturePackBytes  = 64 << 20
	maxTextureFileBytes  = 16 << 20
	textureCacheDirName  = ".texture-cache"
	textureStampFileName = ".source"
)

// Pack describes an author-approved downloadable preset collection.
type Pack struct {
	ID       string
	Name     string
	Filename string
	URL      string
	// RedundantPresetRoot is an archive folder omitted from visible preset paths.
	RedundantPresetRoot string
}

// PackStatus is the installed state used by the collection manager UI.
type PackStatus struct {
	Pack      Pack
	Installed bool
	Error     string
}

var availablePacks = []Pack{
	{
		ID:       "cream-of-the-crop",
		Name:     "Cream of the Crop",
		Filename: "isosceles-cream-of-the-crop.zip",
		URL:      "https://www.patreon.com/file?h=91682111&i=16310421",
	},
	{
		ID:       "mashups-2020",
		Name:     "Isosceles Mashups 2020",
		Filename: "isosceles-mashups-2020.zip",
		URL:      "https://www.patreon.com/file?h=91682111&i=16310422",
	},
	{
		ID:                  "mashups-2024",
		Name:                "Isosceles Mashups 2024",
		Filename:            "isosceles-mashups-2024.zip",
		URL:                 "https://www.patreon.com/file?h=115453098&m=375145864",
		RedundantPresetRoot: "Mashups 2024",
	},
}

// AvailablePacks returns the collections exposed by the app. Spout Jamming is
// intentionally omitted until its compatibility has been evaluated.
func AvailablePacks() []Pack {
	return append([]Pack(nil), availablePacks...)
}

// PackStatuses reports available, installed, and invalid archives.
func PackStatuses(dir string) []PackStatus {
	statuses := make([]PackStatus, 0, len(availablePacks))
	for _, pack := range availablePacks {
		status := PackStatus{Pack: pack}
		archivePath := filepath.Join(dir, pack.Filename)
		if _, err := os.Stat(archivePath); err == nil {
			if err := ValidatePackArchive(archivePath); err != nil {
				status.Error = err.Error()
			} else {
				status.Installed = true
			}
		} else if !os.IsNotExist(err) {
			status.Error = err.Error()
		}
		statuses = append(statuses, status)
	}
	return statuses
}

// InstallPack downloads, validates, and atomically publishes a pack and its
// extracted texture cache. Preset files and preview images stay in the ZIP.
func InstallPack(ctx context.Context, id, dir string, onProgress func(read, total int64)) error {
	pack, ok := packByID(id)
	if !ok {
		return fmt.Errorf("unknown preset pack %q", id)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create preset directory: %w", err)
	}
	archivePath := filepath.Join(dir, pack.Filename)
	if _, err := os.Stat(archivePath); err == nil {
		if err := ValidatePackArchive(archivePath); err == nil {
			return fmt.Errorf("preset pack %q is already installed", id)
		}
	}

	stagedArchive := filepath.Join(dir, "."+pack.ID+".download.zip")
	_ = os.Remove(stagedArchive)
	defer os.Remove(stagedArchive)
	if err := downloadPackArchive(ctx, pack.URL, stagedArchive, maxPackDownloadBytes, onProgress); err != nil {
		return fmt.Errorf("download %s: %w", pack.Name, err)
	}
	if err := ValidatePackArchive(stagedArchive); err != nil {
		return fmt.Errorf("validate %s: %w", pack.Name, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	cacheRoot := filepath.Join(dir, textureCacheDirName)
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		return fmt.Errorf("create texture cache: %w", err)
	}
	stagedTextures, err := os.MkdirTemp(cacheRoot, "."+pack.ID+"-")
	if err != nil {
		return fmt.Errorf("create texture staging directory: %w", err)
	}
	defer os.RemoveAll(stagedTextures)
	if err := extractTextures(ctx, stagedArchive, stagedTextures); err != nil {
		return fmt.Errorf("extract %s textures: %w", pack.Name, err)
	}
	if err := writeTextureStamp(stagedTextures, stagedArchive); err != nil {
		return fmt.Errorf("write texture cache stamp: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	cachePath := textureDir(pack.ID, dir)
	if err := replaceDir(stagedTextures, cachePath); err != nil {
		return fmt.Errorf("publish texture cache: %w", err)
	}
	if err := replaceFile(stagedArchive, archivePath); err != nil {
		return fmt.Errorf("publish preset pack: %w", err)
	}
	return nil
}

func downloadPackArchive(ctx context.Context, url, target string, maxBytes int64, onProgress func(read, total int64)) error {
	downloadCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	tooLarge := false
	progress := func(read, total int64) {
		if read > maxBytes {
			tooLarge = true
			cancel()
		}
		if onProgress != nil {
			onProgress(read, total)
		}
	}
	if err := util.DownloadWithFallback(downloadCtx, []string{url}, target, 0, progress); err != nil {
		if tooLarge {
			return fmt.Errorf("archive exceeds %d bytes", maxBytes)
		}
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("stat downloaded archive: %w", err)
	}
	if tooLarge || info.Size() > maxBytes {
		_ = os.Remove(target)
		return fmt.Errorf("archive exceeds %d bytes", maxBytes)
	}
	return nil
}

// EnsureTextureCache creates or refreshes the texture-only cache for packID.
func EnsureTextureCache(packID, dir string) (string, error) {
	pack, ok := packByID(packID)
	if !ok {
		return "", fmt.Errorf("unknown preset pack %q", packID)
	}
	archivePath := filepath.Join(dir, pack.Filename)
	if err := ValidatePackArchive(archivePath); err != nil {
		return "", err
	}
	cacheRoot := filepath.Join(dir, textureCacheDirName)
	cachePath := textureDir(packID, dir)
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		return "", fmt.Errorf("create texture cache: %w", err)
	}
	if textureStampMatches(cachePath, archivePath) {
		return cachePath, nil
	}

	staged, err := os.MkdirTemp(cacheRoot, "."+packID+"-")
	if err != nil {
		return "", fmt.Errorf("create texture staging directory: %w", err)
	}
	defer os.RemoveAll(staged)
	if err := extractTextures(context.Background(), archivePath, staged); err != nil {
		return "", err
	}
	if err := writeTextureStamp(staged, archivePath); err != nil {
		return "", err
	}
	if err := replaceDir(staged, cachePath); err != nil {
		return "", fmt.Errorf("publish texture cache: %w", err)
	}
	return cachePath, nil
}

// TextureDir returns the managed texture-cache directory for a pack.
func TextureDir(packID, dir string) string { return textureDir(packID, dir) }

// RemovePackFiles removes one installed ZIP and its derived texture cache.
func RemovePackFiles(id, dir string) error {
	pack, ok := packByID(id)
	if !ok {
		return fmt.Errorf("unknown preset pack %q", id)
	}
	if err := os.Remove(filepath.Join(dir, pack.Filename)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove preset pack: %w", err)
	}
	if err := os.RemoveAll(textureDir(id, dir)); err != nil {
		return fmt.Errorf("remove preset textures: %w", err)
	}
	return nil
}

// RemovePackAndReload removes a collection while excluding readers from the
// store transition, then publishes the remaining ZIP and loose presets.
func RemovePackAndReload(id, dir string) error {
	storeMu.Lock()
	defer storeMu.Unlock()
	if store != nil {
		store.close()
		store = nil
	}
	removeErr := RemovePackFiles(id, dir)
	next, openErr := loadStore(dir)
	if openErr == nil {
		store = next
	}
	InvalidateMetaCache()
	if removeErr != nil {
		return removeErr
	}
	return openErr
}

// ValidatePackArchive checks the archive bounds and the required preset and
// texture roots without decompressing its contents.
func ValidatePackArchive(archivePath string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open ZIP: %w", err)
	}
	defer zr.Close()
	return validatePackReader(&zr.Reader)
}

func validatePackReader(reader *zip.Reader) error {
	if len(reader.File) == 0 || len(reader.File) > maxPackEntries {
		return fmt.Errorf("ZIP entry count %d is outside supported limits", len(reader.File))
	}
	var totalSize, textureSize uint64
	hasPreset, hasTexture := false, false
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if file.UncompressedSize64 > maxPackUncompressed-totalSize {
			return fmt.Errorf("ZIP expands beyond %d bytes", maxPackUncompressed)
		}
		totalSize += file.UncompressedSize64
		name, safe := safeZipName(file.Name)
		if !safe {
			continue
		}
		parts := strings.Split(name, "/")
		if len(parts) > 1 && strings.EqualFold(parts[0], "Presets") && strings.EqualFold(filepath.Ext(name), ".milk") {
			if !regularZipFile(file) {
				continue
			}
			if file.UncompressedSize64 > maxPresetBytes {
				return fmt.Errorf("preset entry %q exceeds %d bytes", name, maxPresetBytes)
			}
			hasPreset = true
		}
		if len(parts) > 1 && strings.EqualFold(parts[0], "Textures") && supportedTextureExt(filepath.Ext(name)) {
			if !regularZipFile(file) {
				continue
			}
			if file.UncompressedSize64 > maxTextureFileBytes {
				return fmt.Errorf("texture entry %q exceeds %d bytes", name, maxTextureFileBytes)
			}
			if file.UncompressedSize64 > maxTexturePackBytes-textureSize {
				return fmt.Errorf("texture directory expands beyond %d bytes", maxTexturePackBytes)
			}
			textureSize += file.UncompressedSize64
			hasTexture = true
		}
	}
	if !hasPreset {
		return errors.New("ZIP has no MilkDrop presets under Presets/")
	}
	if !hasTexture {
		return errors.New("ZIP has no supported textures under Textures/")
	}
	return nil
}

func safeZipName(name string) (string, bool) {
	if name == "" || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return "", false
	}
	clean := strings.TrimSuffix(name, "/")
	return clean, clean != "" && filepath.IsLocal(clean) && path.Clean(clean) == clean
}

func regularZipFile(file *zip.File) bool {
	mode := file.Mode()
	return mode.Type() == 0 || mode.IsRegular()
}

func extractTextures(ctx context.Context, archivePath, targetDir string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open ZIP: %w", err)
	}
	defer zr.Close()
	for _, file := range zr.File {
		name, safe := safeZipName(file.Name)
		if !safe || file.FileInfo().IsDir() || !regularZipFile(file) || !supportedTextureExt(filepath.Ext(name)) {
			continue
		}
		parts := strings.Split(name, "/")
		if len(parts) < 2 || !strings.EqualFold(parts[0], "Textures") {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative := filepath.FromSlash(strings.Join(parts[1:], "/"))
		if !filepath.IsLocal(relative) {
			continue
		}
		target := filepath.Join(targetDir, relative)
		isKnownTextureTypo := strings.EqualFold(filepath.Base(name), "OIchess1..jpg")
		if isKnownTextureTypo {
			target = filepath.Join(filepath.Dir(target), "OIchess1.jpg")
		}
		if err := extractTexture(file, target); err != nil {
			if isKnownTextureTypo {
				return fmt.Errorf("create OIchess1 compatibility texture: %w", err)
			}
			return fmt.Errorf("extract texture %q: %w", file.Name, err)
		}
	}
	return nil
}

func extractTexture(file *zip.File, target string) error {
	r, err := file.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, io.LimitReader(r, maxTextureFileBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if info, err := os.Stat(target); err == nil && info.Size() > maxTextureFileBytes {
		return fmt.Errorf("texture exceeds %d bytes", maxTextureFileBytes)
	}
	return nil
}

func supportedTextureExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg", ".png", ".dds", ".tga", ".bmp", ".dib":
		return true
	default:
		return false
	}
}

func textureDir(packID, dir string) string {
	return filepath.Join(dir, textureCacheDirName, packID)
}

func replaceDir(staged, target string) error {
	backup := target + ".old"
	if err := os.RemoveAll(backup); err != nil {
		return err
	}
	if err := os.Rename(target, backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(staged, target); err != nil {
		_ = os.Rename(backup, target)
		return err
	}
	return os.RemoveAll(backup)
}

func replaceFile(staged, target string) error {
	backup := target + ".old"
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(target, backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(staged, target); err != nil {
		_ = os.Rename(backup, target)
		return err
	}
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func writeTextureStamp(texturePath, archivePath string) error {
	info, err := os.Stat(archivePath)
	if err != nil {
		return err
	}
	stamp := fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
	return os.WriteFile(filepath.Join(texturePath, textureStampFileName), []byte(stamp), 0o644)
}

func textureStampMatches(texturePath, archivePath string) bool {
	info, err := os.Stat(archivePath)
	if err != nil {
		return false
	}
	want := fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
	got, err := os.ReadFile(filepath.Join(texturePath, textureStampFileName))
	return err == nil && string(got) == want
}

func packByID(id string) (Pack, bool) {
	for _, pack := range availablePacks {
		if pack.ID == id {
			return pack, true
		}
	}
	return Pack{}, false
}
