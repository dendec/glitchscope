package catalog

import "math/rand"

// SourceIndex is the contract that each provider-specific index must satisfy.
// Providers implement this in their own packages; the coordinator consumes it.
type SourceIndex interface {
	// Source returns the kind of this index.
	Source() SourceKind

	// TrackCount returns the total number of playable tracks across all
	// directories in this source. Used for track-weighted source selection.
	TrackCount() uint64

	// DirectoryCount returns the total number of directories/buckets.
	DirectoryCount() uint64

	// RandomTrack selects one track uniformly from the entire source.
	// The selection is lazy: it reads only the manifest and the selected
	// directory record, not the full catalog.
	RandomTrack(rng *rand.Rand) (ShuffleTrack, error)

	// RandomTrackInDirectory selects one track uniformly within a specific
	// directory. Used after source selection narrows to one source, and then
	// directory selection narrows to one directory.
	RandomTrackInDirectory(key DirectoryKey, rng *rand.Rand) (ShuffleTrack, error)

	// DirectoryList returns the listing for one directory/bucket.
	// The result is suitable for both navigation display and in-directory
	// track selection.
	DirectoryList(key DirectoryKey) (DirectoryListing, error)

	// Fingerprint returns the current fingerprint of the source input.
	// Used to detect staleness and trigger rebuilds.
	Fingerprint() Fingerprint
}
