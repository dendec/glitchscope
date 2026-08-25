package player

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/dendec/glitchscope/internal/openmpt"
	"github.com/dendec/glitchscope/internal/soloud"
	"github.com/dendec/glitchscope/internal/xmp"
)

type TrackMeta struct {
	Duration float64 `json:"d"`
	BPM      float64 `json:"b,omitempty"`
	Channels int     `json:"c,omitempty"`
	Comment  string  `json:"m,omitempty"` // tracker message/comment
}

type albumMeta struct {
	Version int                  `json:"v"`
	Tracks  map[string]TrackMeta `json:"t"`
}

const (
	metaFileName = ".gsa_meta.json"
	metaVersion  = 2
)

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

// extractMetaFromFile extracts track metadata (duration, BPM, channels,
// comment) from a local file. Pure function — no caching, no side effects —
// so it can be reused for on-demand fills and comment refreshes alike.
func extractMetaFromFile(path string) TrackMeta {
	ext := strings.ToLower(filepath.Ext(path))
	m := TrackMeta{}
	if isTrackerExt(ext) {
		if bpm, ch, dur, err := xmp.GetTrackerMeta(path); err == nil {
			m.Duration = dur
			m.BPM = bpm
			m.Channels = ch
		} else if openmpt.HasExt(ext) {
			if fileBuf, err := os.ReadFile(path); err == nil {
				if bpm, ch, dur, err := openmpt.GetTrackerMeta(fileBuf); err == nil {
					m.Duration = dur
					m.BPM = bpm
					m.Channels = ch
				}
			}
		}
		// Tracker message/comment (XM/IT/MOD/S3M text).
		if fileBuf, err := os.ReadFile(path); err == nil {
			if msg := openmpt.GetMessage(fileBuf); msg != "" {
				m.Comment = msg
			}
		}
	} else if isFfmpegExt(ext) {
		if fileBuf, err := os.ReadFile(path); err == nil {
			if source, err := soloud.NewFfmpeg(fileBuf); err == nil {
				m.Duration = source.GetLength()
				m.Channels = source.GetChannels()
				source.Destroy()
			}
		}
	} else if w, err := soloud.LoadWav(path); err == nil {
		m.Duration = w.GetLength()
		w.Destroy()
	}
	return m
}
