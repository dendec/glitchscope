// Package presets indexes MilkDrop presets from downloaded ZIP packs and local files.
package presets

import (
	"archive/zip"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const maxPresetBytes = 32 << 20

type entry struct {
	filesystem bool
	file       *zip.File
}

type presetStore struct {
	dir      string
	archives []*zip.ReadCloser
	entries  map[string]entry
	names    []string
}

var (
	storeMu sync.RWMutex
	store   *presetStore
)

// Open replaces the active index with installed ZIP packs and loose .milk files.
// Presets are read lazily from their source archive; no .milk files are extracted.
func Open(dir string) error {
	s, err := loadStore(dir)
	if err != nil {
		return err
	}

	storeMu.Lock()
	previous := store
	store = s
	InvalidateMetaCache()
	if previous != nil {
		previous.close()
	}
	storeMu.Unlock()
	return nil
}

func loadStore(dir string) (*presetStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create preset directory: %w", err)
	}

	s := &presetStore{dir: dir, entries: make(map[string]entry)}
	for _, pack := range AvailablePacks() {
		archivePath := filepath.Join(dir, pack.Filename)
		zr, err := zip.OpenReader(archivePath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			slog.Warn("preset pack open failed", "pack", pack.ID, "error", err)
			continue
		}
		if err := validatePackReader(&zr.Reader); err != nil {
			_ = zr.Close()
			slog.Warn("preset pack invalid", "pack", pack.ID, "error", err)
			continue
		}
		s.archives = append(s.archives, zr)
		s.indexPack(pack, zr)
	}
	if err := s.scanUser(); err != nil {
		s.close()
		return nil, fmt.Errorf("scan preset directory: %w", err)
	}
	s.names = make([]string, 0, len(s.entries))
	for name := range s.entries {
		s.names = append(s.names, name)
	}
	sort.Strings(s.names)

	return s, nil
}

// Close releases ZIP handles and clears the active index.
func Close() {
	storeMu.Lock()
	if store != nil {
		store.close()
		store = nil
	}
	InvalidateMetaCache()
	storeMu.Unlock()
}

func (s *presetStore) close() {
	for _, archive := range s.archives {
		if err := archive.Close(); err != nil {
			slog.Debug("preset pack close failed", "error", err)
		}
	}
}

func (s *presetStore) indexPack(pack Pack, archive *zip.ReadCloser) {
	for _, file := range archive.File {
		if file.FileInfo().IsDir() || !regularZipFile(file) || file.UncompressedSize64 > maxPresetBytes || !strings.EqualFold(filepath.Ext(file.Name), ".milk") {
			continue
		}
		name, ok := packPresetKey(pack, file.Name)
		if !ok {
			continue
		}
		if _, exists := s.entries[name]; exists {
			continue
		}
		s.entries[name] = entry{file: file}
	}
}

func packPresetKey(pack Pack, archiveName string) (string, bool) {
	name, safe := safeZipName(archiveName)
	if !safe {
		return "", false
	}
	parts := strings.Split(name, "/")
	if len(parts) < 2 || !strings.EqualFold(parts[0], "Presets") {
		return "", false
	}
	if strings.EqualFold(filepath.Ext(name), ".milk") {
		if len(parts) > 2 && pack.RedundantPresetRoot != "" && parts[1] == pack.RedundantPresetRoot {
			parts = append(parts[:1], parts[2:]...)
		}
		return pack.Name + "/" + strings.Join(parts[1:], "/"), true
	}
	return "", false
}

func (s *presetStore) scanUser() error {
	return filepath.WalkDir(s.dir, func(filePath string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if filePath != s.dir && (strings.HasPrefix(d.Name(), ".") || d.Type()&os.ModeSymlink != 0) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(d.Name()), ".milk") {
			return nil
		}
		relative, err := filepath.Rel(s.dir, filePath)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(relative)
		if _, exists := s.entries[key]; !exists {
			s.entries[key] = entry{filesystem: true}
		}
		return nil
	})
}

// Names returns sorted preset keys.
func Names() []string {
	storeMu.RLock()
	defer storeMu.RUnlock()
	if store == nil {
		return nil
	}
	return append([]string(nil), store.names...)
}

// Read returns decompressed preset bytes for the given key.
func Read(key string) ([]byte, error) {
	storeMu.RLock()
	defer storeMu.RUnlock()
	if store == nil {
		return nil, fmt.Errorf("presets not loaded")
	}
	e, ok := store.entries[key]
	if !ok {
		return nil, fmt.Errorf("preset %q not found", key)
	}
	if e.filesystem {
		return os.ReadFile(filepath.Join(store.dir, filepath.FromSlash(key)))
	}
	r, err := e.file.Open()
	if err != nil {
		return nil, fmt.Errorf("open preset %q in ZIP: %w", key, err)
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, maxPresetBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read preset %q in ZIP: %w", key, err)
	}
	if len(data) > maxPresetBytes {
		return nil, fmt.Errorf("preset %q exceeds %d bytes", key, maxPresetBytes)
	}
	return data, nil
}

// SourceFingerprint returns a cheap identity for the preset bytes currently
// indexed by the store. ZIP entries use their central-directory checksum;
// loose files use size and modification time.
func SourceFingerprint(key string) (string, error) {
	storeMu.RLock()
	defer storeMu.RUnlock()
	if store == nil {
		return "", fmt.Errorf("presets not loaded")
	}
	e, ok := store.entries[key]
	if !ok {
		return "", fmt.Errorf("preset %q not found", key)
	}
	if e.file != nil {
		return fmt.Sprintf("zip:%s:%08x:%d", e.file.Name, e.file.CRC32, e.file.UncompressedSize64), nil
	}
	info, err := os.Stat(filepath.Join(store.dir, filepath.FromSlash(key)))
	if err != nil {
		return "", fmt.Errorf("stat preset %q: %w", key, err)
	}
	return fmt.Sprintf("file:%d:%d", info.Size(), info.ModTime().UnixNano()), nil
}

// categoryOf returns the category a preset key belongs to.
func categoryOf(name string) string {
	parts := strings.Split(name, "/")
	switch {
	case len(parts) >= 3:
		return parts[0] + "/" + parts[1]
	case len(parts) == 2:
		return parts[0]
	default:
		return "Default"
	}
}

// Categories returns sorted category names from subdirectory structure.
func Categories() []string {
	storeMu.RLock()
	defer storeMu.RUnlock()
	if store == nil {
		return nil
	}
	cats := make(map[string]bool)
	for _, name := range store.names {
		cats[categoryOf(name)] = true
	}
	out := make([]string, 0, len(cats))
	for c := range cats {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// PresetsInCategory returns sorted preset keys belonging to cat.
func PresetsInCategory(cat string) []string {
	storeMu.RLock()
	defer storeMu.RUnlock()
	if store == nil {
		return nil
	}
	var out []string
	for _, name := range store.names {
		if categoryOf(name) == cat {
			out = append(out, name)
		}
	}
	return out
}

// IsTransition reports whether a preset is under a transition-marked folder or
// has a transition-marked filename.
func IsTransition(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, "!") {
			return true
		}
	}
	return false
}

// DefaultPreset returns a minimal built-in preset.
func DefaultPreset() []byte {
	return []byte(`[preset00]
wave_r=1
`)
}
