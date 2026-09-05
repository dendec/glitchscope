package catalog

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	// ManifestSchema is the schema identifier for index manifests.
	ManifestSchema = "glitchscope-shuffle-index"
	// ManifestVersion is the current manifest schema version.
	ManifestVersion = 1
)

// ManifestMeta is the top-level metadata stored as manifest.json inside
// each .idx GSA file. It identifies the source, its fingerprint, and
// the list of records the index contains.
type ManifestMeta struct {
	Schema         string          `json:"schema"`
	Version        int             `json:"version"`
	Source         SourceKind      `json:"source"`
	Fingerprint    Fingerprint     `json:"fingerprint"`
	TrackCount     uint64          `json:"track_count"`
	DirectoryCount uint64          `json:"directory_count"`
	Entries        []ManifestEntry `json:"entries"`
	CreatedAt      time.Time       `json:"created_at"`
}

// ManifestEntry describes one record inside the .idx GSA file.
type ManifestEntry struct {
	Name           string `json:"name"` // GSA record path, e.g. "directories/abc123.json"
	TrackCount     uint64 `json:"track_count"`
	DirectoryCount uint64 `json:"directory_count,omitempty"`
}

// Validate checks structural correctness of the manifest.
func (m *ManifestMeta) Validate() error {
	if m.Schema != ManifestSchema {
		return fmt.Errorf("manifest schema: got %q, want %q", m.Schema, ManifestSchema)
	}
	if m.Version != ManifestVersion {
		return fmt.Errorf("manifest version: got %d, want %d", m.Version, ManifestVersion)
	}
	if m.Entries == nil {
		m.Entries = []ManifestEntry{}
	}
	return nil
}

// MarshalManifest encodes a manifest to JSON bytes.
func MarshalManifest(m *ManifestMeta) ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// UnmarshalManifest decodes JSON bytes into a manifest and validates it.
func UnmarshalManifest(data []byte) (*ManifestMeta, error) {
	var m ManifestMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("manifest decode: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}
