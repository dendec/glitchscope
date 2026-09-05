package catalog

import (
	"testing"
	"time"
)

func TestManifestRoundTrip(t *testing.T) {
	m := &ManifestMeta{
		Schema:         ManifestSchema,
		Version:        ManifestVersion,
		Source:         SourceLocal,
		Fingerprint:    Fingerprint{SourceHash: "abc123", TrackCount: 100, DirectoryCount: 10},
		TrackCount:     100,
		DirectoryCount: 10,
		Entries: []ManifestEntry{
			{Name: "directories/music.json", TrackCount: 50, DirectoryCount: 5},
			{Name: "directories/podcasts.json", TrackCount: 50, DirectoryCount: 5},
		},
		CreatedAt: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	data, err := MarshalManifest(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, err := UnmarshalManifest(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Schema != m.Schema {
		t.Errorf("schema: got %q, want %q", got.Schema, m.Schema)
	}
	if got.Source != m.Source {
		t.Errorf("source: got %v, want %v", got.Source, m.Source)
	}
	if got.TrackCount != m.TrackCount {
		t.Errorf("track count: got %d, want %d", got.TrackCount, m.TrackCount)
	}
	if len(got.Entries) != len(m.Entries) {
		t.Fatalf("entries: got %d, want %d", len(got.Entries), len(m.Entries))
	}
	if got.Entries[0].Name != m.Entries[0].Name {
		t.Errorf("entry 0 name: got %q, want %q", got.Entries[0].Name, m.Entries[0].Name)
	}
}

func TestManifestValidateBadSchema(t *testing.T) {
	m := &ManifestMeta{Schema: "wrong", Version: ManifestVersion}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for bad schema")
	}
}

func TestManifestValidateBadVersion(t *testing.T) {
	m := &ManifestMeta{Schema: ManifestSchema, Version: 999}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for bad version")
	}
}

func TestManifestValidateNilEntries(t *testing.T) {
	m := &ManifestMeta{Schema: ManifestSchema, Version: ManifestVersion}
	if err := m.Validate(); err != nil {
		t.Fatalf("nil entries should be valid: %v", err)
	}
	if m.Entries == nil {
		t.Fatal("Validate should initialize nil entries to empty slice")
	}
}

func TestUnmarshalManifestInvalidJSON(t *testing.T) {
	if _, err := UnmarshalManifest([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
