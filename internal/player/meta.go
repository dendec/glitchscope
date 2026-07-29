package player

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type TrackMeta struct {
	Duration float64 `json:"d"`
	BPM      float64 `json:"b,omitempty"`
	Channels int     `json:"c,omitempty"`
}

type albumMeta struct {
	Version int                  `json:"v"`
	Tracks  map[string]TrackMeta `json:"t"`
}

const metaFileName = ".mdpp_meta.json"
const metaVersion = 1

// readMetaCache reads the metadata cache for an album directory.
func readMetaCache(albumPath string) *albumMeta {
	fpath := filepath.Join(albumPath, metaFileName)
	f, err := os.Open(fpath)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var m albumMeta
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return nil
	}
	if m.Version != metaVersion || m.Tracks == nil {
		return nil
	}
	return &m
}

// writeMetaCache writes the metadata cache for an album directory.
func writeMetaCache(albumPath string, m *albumMeta) error {
	m.Version = metaVersion
	fpath := filepath.Join(albumPath, metaFileName)
	f, err := os.Create(fpath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "")
	return enc.Encode(m)
}
