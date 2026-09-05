// Package catalog defines provider-neutral types for the shuffle index system.
// It owns SourceKind, DirectoryKey, Fingerprint, ShuffleTrack, directory
// listing types, and the SourceIndex interface that providers implement.
package catalog

import (
	"fmt"
	"strings"
)

// SourceKind identifies a track source.
type SourceKind int

const (
	SourceLocal      SourceKind = iota // local filesystem
	SourceModland                      // Modland remote catalog
	SourceModArchive                   // ModArchive remote catalog
)

func (k SourceKind) String() string {
	switch k {
	case SourceLocal:
		return "local"
	case SourceModland:
		return "modland"
	case SourceModArchive:
		return "modarchive"
	default:
		return fmt.Sprintf("SourceKind(%d)", int(k))
	}
}

// MarshalJSON encodes SourceKind as a lowercase string.
func (k SourceKind) MarshalJSON() ([]byte, error) {
	return []byte(`"` + k.String() + `"`), nil
}

// UnmarshalJSON decodes a lowercase string or legacy integer.
func (k *SourceKind) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	switch s {
	case "local":
		*k = SourceLocal
	case "modland":
		*k = SourceModland
	case "modarchive":
		*k = SourceModArchive
	default:
		// Try legacy integer.
		var n int
		if _, err := fmt.Sscanf(s, "%d", &n); err == nil {
			*k = SourceKind(n)
			return nil
		}
		return fmt.Errorf("unknown source kind: %q", s)
	}
	return nil
}

// AllSourceKinds returns all valid source kinds.
func AllSourceKinds() []SourceKind {
	return []SourceKind{SourceLocal, SourceModland, SourceModArchive}
}

// DirectoryKey uniquely identifies a directory within a source.
// It is a comparable struct suitable for map keys.
type DirectoryKey struct {
	Source  SourceKind
	Locator string // normalized local path, canonical URL, or GSA entry locator
}

// String returns a human-readable representation for diagnostics.
func (k DirectoryKey) String() string {
	return k.Source.String() + ":" + k.Locator
}

// Fingerprint identifies the source input and index schema.
// Providers construct it from their own file/cache metadata;
// the catalog package compares and stores it.
type Fingerprint struct {
	SourceHash     string // content hash or modification token of the source input
	TrackCount     uint64
	DirectoryCount uint64
}

// ListingVersion changes whenever one directory listing changes.
// It is separate from the source fingerprint.
type ListingVersion string

// ShuffleTrack is the result of a random selection. It carries enough
// context for UI navigation without scanning unrelated catalog data.
type ShuffleTrack struct {
	Path           string
	Source         SourceKind
	DirectoryKey   DirectoryKey
	TrackIndex     uint32
	TrackKey       string // stable identity: normalized path, URL, or record key
	ListingVersion ListingVersion
	AlbumName      string
}

// DirectoryEntry represents one track inside a directory listing.
type DirectoryEntry struct {
	Path string // file path or URL for playback
	Name string // display name (base filename or track title)
	// Size is optional metadata; zero means unknown.
	Size int64
}

// DirectoryListing is the content of one directory/bucket.
type DirectoryListing struct {
	Key         DirectoryKey
	Version     ListingVersion
	DisplayName string
	Entries     []DirectoryEntry
}
