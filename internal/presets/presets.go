// Package presets loads .milk preset files from presets.pmv and presets/ dir.
package presets

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dendec/pmv/internal/archive"
)

type entry struct {
	name    string
	dataOff int64 // offset in .pmv, -1 = filesystem
}

type presetStore struct {
	dir     string
	pmvFile *archive.Archive
	entries map[string]entry
	names   []string
}

var store *presetStore

// Open loads presets from dir/presets.pmv and scans dir/*.milk + dir/*/*.milk.
// User .milk files override same-named entries from presets.pmv.
func Open(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return err
	}

	s := &presetStore{
		dir:     dir,
		entries: make(map[string]entry),
	}

	// Load presets.pmv if present.
	pmvPath := filepath.Join(dir, "presets.pmv")
	if a, err := archive.Open(pmvPath, 100000); err == nil {
		s.pmvFile = a
		s.loadPMV()
	}

	// Scan user .milk files (override same names from presets.pmv).
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

func (s *presetStore) loadPMV() {
	for _, item := range s.pmvFile.Entries() {
		s.entries[item.Name] = entry{name: item.Name, dataOff: 0}
	}
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

// Names returns sorted preset keys.
func Names() []string {
	if store == nil {
		return nil
	}
	return store.names
}

// Read returns decompressed preset bytes for the given key.
// User .milk files take priority over presets.pmv entries.
func Read(key string) ([]byte, error) {
	if store == nil {
		return nil, fmt.Errorf("presets not loaded")
	}
	e, ok := store.entries[key]
	if !ok {
		return nil, fmt.Errorf("preset %q not found", key)
	}

	if e.dataOff < 0 {
		return os.ReadFile(filepath.Join(store.dir, key))
	}

	// Read from presets.pmv archive.
	return store.pmvFile.Read(key)
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

// DefaultPreset returns a minimal built-in preset.
func DefaultPreset() []byte {
	return []byte(`[preset00]
wave_r=1
`)
}
