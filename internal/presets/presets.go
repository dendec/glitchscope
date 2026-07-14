// Package presets embeds all .milk preset files into the binary.
package presets

import (
	"embed"
	"sort"
)

//go:embed *.milk
var embedFS embed.FS

// Names returns sorted preset filenames.
func Names() []string {
	entries, err := embedFS.ReadDir(".")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// Read returns the content of a named preset file.
func Read(name string) ([]byte, error) {
	return embedFS.ReadFile(name)
}
