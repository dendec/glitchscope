// Package player manages audio playback and music library.
package player

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// PlaylistID identifies a favourites playlist.
type PlaylistID string

// String returns the human-readable label for the playlist.
func (id PlaylistID) String() string {
	for _, spec := range playlistSpecs {
		if spec.ID == id {
			return spec.Label
		}
	}
	return string(id)
}

const (
	PlaylistStar  PlaylistID = "star"
	PlaylistHeart PlaylistID = "heart"
	PlaylistNote  PlaylistID = "note"
)

// playlistOrder defines the cycle order and display order.
var playlistOrder = []PlaylistID{PlaylistStar, PlaylistHeart, PlaylistNote}

// PlaylistSpec describes one favourites playlist for UI display.
type PlaylistSpec struct {
	ID     PlaylistID
	Symbol rune
	Label  string
}

// playlistSpecs is the canonical list of playlists with their display metadata.
var playlistSpecs = []PlaylistSpec{
	{PlaylistStar, '★', "Star"},
	{PlaylistHeart, '♥', "Heart"},
	{PlaylistNote, '♪', "Note"},
}

// PlaylistSpecs returns the canonical playlist specifications. The caller must
// not modify the returned slice.
func PlaylistSpecs() []PlaylistSpec { return playlistSpecs }

// favouritesFile is the on-disk JSON format.
type favouritesFile struct {
	Version   int                     `json:"version"`
	Playlists map[PlaylistID][]string `json:"playlists"`
}

// Favourites manages three mutually exclusive playlists. A track can be in at
// most one playlist. All mutations are transactional: the on-disk file is
// updated atomically before in-memory state changes.
type Favourites struct {
	playlists map[PlaylistID][]string
	lookup    map[string]PlaylistID // path → playlist, O(1)
	path      string                // file path for Save
	writable  bool                  // false when loaded from corrupted file
}

// LoadFavourites reads favourites from path. A missing file returns empty
// writable favourites. A corrupted file returns read-only favourites with an
// error; the original file is never overwritten.
func LoadFavourites(path string) (*Favourites, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Debug("favourites: file not found, using empty", "path", path)
			return newEmpty(path, true), nil
		}
		return nil, fmt.Errorf("favourites read: %w", err)
	}

	var raw favouritesFile
	if err := json.Unmarshal(data, &raw); err != nil {
		slog.Warn("favourites: malformed JSON", "path", path, "error", err)
		return newEmpty(path, false), fmt.Errorf("favourites decode: %w", err)
	}
	if raw.Version != 1 {
		return newEmpty(path, false), fmt.Errorf("favourites: unsupported version %d", raw.Version)
	}
	if raw.Playlists == nil {
		raw.Playlists = make(map[PlaylistID][]string)
	}

	// Validate playlist IDs and normalise paths.
	known := make(map[PlaylistID]bool, len(playlistOrder))
	for _, id := range playlistOrder {
		known[id] = true
	}
	seen := make(map[string]PlaylistID) // path → first playlist (duplicate detection)
	playlists := make(map[PlaylistID][]string, len(playlistOrder))
	for _, id := range playlistOrder {
		playlists[id] = nil
	}
	for id, tracks := range raw.Playlists {
		if !known[id] {
			return newEmpty(path, false), fmt.Errorf("favourites: unknown playlist %q", id)
		}
		for _, track := range tracks {
			if track == "" {
				return newEmpty(path, false), fmt.Errorf("favourites: empty path in playlist %q", id)
			}
			normalised, err := normalisePath(track)
			if err != nil {
				return newEmpty(path, false), fmt.Errorf("favourites: invalid path %q: %w", track, err)
			}
			if prev, exists := seen[normalised]; exists {
				return newEmpty(path, false), fmt.Errorf("favourites: duplicate path %q in playlists %q and %q", track, prev, id)
			}
			seen[normalised] = id
			playlists[id] = append(playlists[id], normalised)
		}
	}

	lookup := make(map[string]PlaylistID, len(seen))
	for id, tracks := range playlists {
		for _, t := range tracks {
			lookup[t] = id
		}
	}

	slog.Info("favourites: loaded", "path", path,
		"star", len(playlists[PlaylistStar]),
		"heart", len(playlists[PlaylistHeart]),
		"note", len(playlists[PlaylistNote]))
	return &Favourites{playlists: playlists, lookup: lookup, path: path, writable: true}, nil
}

// NewReadOnlyFavourites returns an empty read-only Favourites. Used when the
// on-disk file is corrupted and must not be overwritten.
func NewReadOnlyFavourites() *Favourites {
	return newEmpty("", false)
}

func newEmpty(path string, writable bool) *Favourites {
	playlists := make(map[PlaylistID][]string, len(playlistOrder))
	for _, id := range playlistOrder {
		playlists[id] = nil
	}
	return &Favourites{playlists: playlists, lookup: make(map[string]PlaylistID), path: path, writable: writable}
}

// normalisePath canonicalises a track path. Local paths get Abs+Clean; virtual
// paths (with a known provider prefix) are kept as-is after basic validation.
func normalisePath(track string) (string, error) {
	if IsModland(track) || IsModArchive(track) {
		return track, nil
	}
	abs, err := filepath.Abs(track)
	if err != nil {
		return "", fmt.Errorf("abs: %w", err)
	}
	return filepath.Clean(abs), nil
}

// GetPlaylist returns which playlist the track belongs to, or empty string.
func (f *Favourites) GetPlaylist(trackPath string) PlaylistID {
	return f.lookup[trackPath]
}

// Symbol returns the display symbol for the track's playlist, or "".
func (f *Favourites) Symbol(trackPath string) string {
	id := f.lookup[trackPath]
	for _, spec := range playlistSpecs {
		if spec.ID == id {
			return string(spec.Symbol)
		}
	}
	return ""
}

// Tracks returns a copy of the track list for the given playlist.
func (f *Favourites) Tracks(id PlaylistID) []string {
	tracks := f.playlists[id]
	out := make([]string, len(tracks))
	copy(out, tracks)
	return out
}

// Count returns the number of tracks in the given playlist.
func (f *Favourites) Count(id PlaylistID) int {
	return len(f.playlists[id])
}

// TotalCount returns the total number of tracks across all playlists.
func (f *Favourites) TotalCount() int {
	n := 0
	for _, id := range playlistOrder {
		n += len(f.playlists[id])
	}
	return n
}

// Cycle moves the track to the next playlist in the cycle:
//
//	None -> Star -> Heart -> Note -> None (removed)
//
// The on-disk file is written atomically before in-memory state changes.
// Returns the new playlist ID (PlaylistID("") when removed).
func (f *Favourites) Cycle(trackPath string) (PlaylistID, error) {
	if !f.writable {
		return "", fmt.Errorf("favourites: read-only")
	}
	normalised, err := normalisePath(trackPath)
	if err != nil {
		return "", fmt.Errorf("favourites: normalise: %w", err)
	}

	current := f.lookup[normalised]
	next := nextPlaylist(current)

	// Build candidate state.
	newLookup := make(map[string]PlaylistID, len(f.lookup))
	for k, v := range f.lookup {
		newLookup[k] = v
	}
	newPlaylists := make(map[PlaylistID][]string, len(f.playlists))
	for id, tracks := range f.playlists {
		copied := make([]string, len(tracks))
		copy(copied, tracks)
		newPlaylists[id] = copied
	}

	// Remove from old playlist.
	if current != "" {
		newPlaylists[current] = removeTrack(newPlaylists[current], normalised)
		delete(newLookup, normalised)
	}

	// Add to new playlist.
	if next != "" {
		newPlaylists[next] = append(newPlaylists[next], normalised)
		newLookup[normalised] = next
	}

	// Transactional write.
	if err := f.save(newPlaylists); err != nil {
		return current, fmt.Errorf("favourites: save: %w", err)
	}

	f.playlists = newPlaylists
	f.lookup = newLookup
	slog.Debug("favourites: cycle", "path", normalised, "from", current, "to", next)
	return next, nil
}

// Remove deletes the track from whichever playlist it belongs to.
// The on-disk file is written atomically before in-memory state changes.
func (f *Favourites) Remove(trackPath string) error {
	if !f.writable {
		return fmt.Errorf("favourites: read-only")
	}
	normalised, err := normalisePath(trackPath)
	if err != nil {
		return fmt.Errorf("favourites: normalise: %w", err)
	}

	current := f.lookup[normalised]
	if current == "" {
		return nil
	}

	// Build candidate state.
	newPlaylists := make(map[PlaylistID][]string, len(f.playlists))
	for id, tracks := range f.playlists {
		copied := make([]string, len(tracks))
		copy(copied, tracks)
		newPlaylists[id] = copied
	}
	newPlaylists[current] = removeTrack(newPlaylists[current], normalised)

	newLookup := make(map[string]PlaylistID, len(f.lookup))
	for k, v := range f.lookup {
		if k != normalised {
			newLookup[k] = v
		}
	}

	// Transactional write.
	if err := f.save(newPlaylists); err != nil {
		return fmt.Errorf("favourites: save: %w", err)
	}

	f.playlists = newPlaylists
	f.lookup = newLookup
	slog.Debug("favourites: remove", "path", normalised, "from", current)
	return nil
}

// nextPlaylist returns the next playlist in the cycle, or "" for removal.
func nextPlaylist(current PlaylistID) PlaylistID {
	for i, id := range playlistOrder {
		if id == current {
			if i+1 < len(playlistOrder) {
				return playlistOrder[i+1]
			}
			return ""
		}
	}
	if len(playlistOrder) > 0 {
		return playlistOrder[0]
	}
	return ""
}

// removeTrack returns a new slice with the first occurrence of track removed.
func removeTrack(tracks []string, track string) []string {
	for i, t := range tracks {
		if t == track {
			return append(tracks[:i], tracks[i+1:]...)
		}
	}
	return tracks
}

// save writes the favourites atomically (temp+rename).
// Order is preserved exactly as in-memory (no sorting).
func (f *Favourites) save(playlists map[PlaylistID][]string) error {
	if f.path == "" {
		return fmt.Errorf("favourites: no path set")
	}

	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("favourites mkdir: %w", err)
	}

	raw := favouritesFile{Version: 1, Playlists: playlists}

	tmp, err := os.CreateTemp(dir, "favourites*.tmp")
	if err != nil {
		return fmt.Errorf("favourites temp: %w", err)
	}
	tmpPath := tmp.Name()

	if err := json.NewEncoder(tmp).Encode(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("favourites encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("favourites close: %w", err)
	}
	if err := os.Rename(tmpPath, f.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("favourites rename: %w", err)
	}
	return nil
}

// Writable reports whether mutations are allowed.
func (f *Favourites) Writable() bool { return f.writable }

// FavouriteTrackTitle returns a display name for a favourited track path.
func FavouriteTrackTitle(path string) string {
	if IsModland(path) || IsModArchive(path) {
		remote := RemotePath(path)
		if u, err := url.Parse(remote); err == nil && u.Fragment != "" {
			name := filepath.Base(u.Fragment)
			if strings.EqualFold(filepath.Ext(name), ".zip") {
				name = strings.TrimSuffix(name, filepath.Ext(name))
			}
			return name
		}
		if idx := strings.LastIndex(remote, "/"); idx >= 0 {
			name := remote[idx+1:]
			if strings.EqualFold(filepath.Ext(name), ".zip") {
				name = strings.TrimSuffix(name, filepath.Ext(name))
			}
			return name
		}
		return remote
	}
	return filepath.Base(path)
}
