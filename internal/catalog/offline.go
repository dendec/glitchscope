package catalog

// OfflineProjection is a source-preserving view of tracks that are
// guaranteed to be playable without a network. It contains the full local
// source and cached subsets of the two remote sources; it is not a fourth
// SourceKind and it does not contain uncached catalog tracks.
type OfflineProjection struct {
	*ShuffleCatalog
}

// NewOfflineProjection creates an immutable offline view from source indexes.
// At most one index per SourceKind is retained.
func NewOfflineProjection(indexes ...SourceIndex) *OfflineProjection {
	return &OfflineProjection{ShuffleCatalog: NewShuffleCatalog(indexes...)}
}
