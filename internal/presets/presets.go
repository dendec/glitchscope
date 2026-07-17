// Package presets loads .milk preset files from .mdp archive and presets/ dir.
package presets

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/klauspost/compress/zstd"
)

var zstdDec, _ = zstd.NewReader(nil)

type entry struct {
	name    string
	zstLen  uint32
	dataOff int64 // offset in .mdp, -1 = filesystem
}

type presetStore struct {
	dir     string
	mdpFile *os.File
	entries map[string]entry
	names   []string
}

var store *presetStore

// Open loads presets from dir/.mdp and scans dir/*.milk + dir/*/*.milk.
// User .milk files override same-named entries from .mdp.
func Open(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return err
	}

	s := &presetStore{
		dir:     dir,
		entries: make(map[string]entry),
	}

	// Load .mdp archive if present.
	mdpPath := filepath.Join(dir, ".mdp")
	if f, err := os.Open(mdpPath); err == nil {
		s.mdpFile = f
		if err := s.loadMDP(); err != nil {
			_ = f.Close()
			s.mdpFile = nil
		}
	}

	// Scan user .milk files (override same names from .mdp).
	s.scanUser()

	// Build sorted names list.
	s.names = make([]string, 0, len(s.entries))
	for n := range s.entries {
		s.names = append(s.names, n)
	}
	sort.Strings(s.names)

	store = s
	return nil
}

func (s *presetStore) loadMDP() error {
	var magic [4]byte
	if _, err := io.ReadFull(s.mdpFile, magic[:]); err != nil {
		return err
	}
	if magic != [4]byte{'M', 'D', 'P', 0} {
		return fmt.Errorf("bad magic: %x", magic)
	}

	var version uint32
	if err := binary.Read(s.mdpFile, binary.LittleEndian, &version); err != nil {
		return err
	}
	if version != 1 {
		return fmt.Errorf("unsupported version: %d", version)
	}

	var numEntries uint32
	if err := binary.Read(s.mdpFile, binary.LittleEndian, &numEntries); err != nil {
		return err
	}
	if numEntries > 100000 {
		return fmt.Errorf("too many entries: %d", numEntries)
	}

	type rawEntry struct {
		name   string
		zstLen uint32
	}
	raw := make([]rawEntry, numEntries)

	for i := range raw {
		var nameLen uint16
		if err := binary.Read(s.mdpFile, binary.LittleEndian, &nameLen); err != nil {
			return err
		}
		nameBytes := make([]byte, nameLen)
		if _, err := io.ReadFull(s.mdpFile, nameBytes); err != nil {
			return err
		}
		raw[i].name = string(nameBytes)
		if err := binary.Read(s.mdpFile, binary.LittleEndian, &raw[i].zstLen); err != nil {
			return err
		}
	}

	// Compute data offsets (data starts at current file position).
	dataStart, _ := s.mdpFile.Seek(0, io.SeekCurrent)
	offset := dataStart
	for _, r := range raw {
		s.entries[r.name] = entry{
			name:    r.name,
			zstLen:  r.zstLen,
			dataOff: offset,
		}
		offset += int64(r.zstLen)
	}

	return nil
}

func (s *presetStore) scanUser() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir() {
			// Level 2: dir/subdir/*.milk
			sub, err := os.ReadDir(filepath.Join(s.dir, e.Name()))
			if err != nil {
				continue
			}
			for _, se := range sub {
				if !se.IsDir() && strings.HasSuffix(se.Name(), ".milk") {
					key := e.Name() + "/" + se.Name()
					s.entries[key] = entry{name: key, dataOff: -1}
				}
			}
		} else if strings.HasSuffix(e.Name(), ".milk") {
			// Level 1: dir/*.milk
			key := e.Name()
			s.entries[key] = entry{name: key, dataOff: -1}
		}
	}
}

// Names returns sorted preset keys. Returns nil if no presets loaded.
func Names() []string {
	if store == nil {
		return nil
	}
	return store.names
}

// Read returns decompressed preset bytes for the given key.
// User .milk files take priority over .mdp entries.
func Read(key string) ([]byte, error) {
	if store == nil {
		return nil, fmt.Errorf("presets not loaded")
	}
	e, ok := store.entries[key]
	if !ok {
		return nil, fmt.Errorf("preset %q not found", key)
	}

	if e.dataOff < 0 {
		// User file on disk.
		return os.ReadFile(filepath.Join(store.dir, key))
	}

	// Read from .mdp archive.
	buf := make([]byte, e.zstLen)
	if _, err := store.mdpFile.ReadAt(buf, e.dataOff); err != nil {
		return nil, err
	}
	dst, err := zstdDec.DecodeAll(buf, nil)
	if err != nil {
		return nil, err
	}
	return dst, nil
}

// categoryOf returns the category a preset key belongs to. Presets nested at
// least two directories deep (top/sub/.../file.milk) are grouped by their
// first two path segments (top/sub) so large top-level folders split into
// smaller, more specific categories. Presets one directory deep (top/file.milk)
// use just that directory, and root-level presets belong to "Default".
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

// Categories returns sorted category names derived from subdirectory structure.
// Root-level presets belong to "Default".
func Categories() []string {
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

// DefaultPreset returns a minimal built-in preset (fallback).
func DefaultPreset() []byte {
	return []byte(`[preset00]
wave_r=1
`)
}
