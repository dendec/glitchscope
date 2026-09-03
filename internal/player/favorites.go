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

// PlaylistID identifies a favorites playlist.
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

// PlaylistSpec describes one favorites playlist for UI display.
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

// PlaylistSymbol returns the symbol for a playlist ID, or "" if unknown.
func PlaylistSymbol(id PlaylistID) string {
	for _, s := range playlistSpecs {
		if s.ID == id {
			return string(s.Symbol)
		}
	}
	return ""
}

// favoritesFile is the on-disk JSON format.
type favoritesFile struct {
	Version   int                     `json:"version"`
	Playlists map[PlaylistID][]string `json:"playlists"`
}

// Favorites manages three mutually exclusive playlists. A track can be in at
// most one playlist. All mutations are transactional: the on-disk file is
// updated atomically before in-memory state changes.
type Favorites struct {
	playlists map[PlaylistID][]string
	lookup    map[string]PlaylistID // path → playlist, O(1)
	path      string                // file path for Save
	writable  bool                  // false when loaded from corrupted file
}

// LoadFavorites reads favorites from path. A missing file returns empty
// writable favorites. A corrupted file returns read-only favorites with an
// error; the original file is never overwritten.
func LoadFavorites(path string) (*Favorites, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Debug("favorites: file not found, using empty", "path", path)
			return newEmpty(path, true), nil
		}
		return nil, fmt.Errorf("favorites read: %w", err)
	}

	var raw favoritesFile
	if err := json.Unmarshal(data, &raw); err != nil {
		slog.Warn("favorites: malformed JSON", "path", path, "error", err)
		return newEmpty(path, false), fmt.Errorf("favorites decode: %w", err)
	}
	if raw.Version != 1 {
		return newEmpty(path, false), fmt.Errorf("favorites: unsupported version %d", raw.Version)
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
			return newEmpty(path, false), fmt.Errorf("favorites: unknown playlist %q", id)
		}
		for _, track := range tracks {
			if track == "" {
				return newEmpty(path, false), fmt.Errorf("favorites: empty path in playlist %q", id)
			}
			normalised, err := normalisePath(track)
			if err != nil {
				return newEmpty(path, false), fmt.Errorf("favorites: invalid path %q: %w", track, err)
			}
			if prev, exists := seen[normalised]; exists {
				return newEmpty(path, false), fmt.Errorf("favorites: duplicate path %q in playlists %q and %q", track, prev, id)
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

	slog.Info("favorites: loaded", "path", path,
		string(PlaylistStar), len(playlists[PlaylistStar]),
		string(PlaylistHeart), len(playlists[PlaylistHeart]),
		string(PlaylistNote), len(playlists[PlaylistNote]))
	return &Favorites{playlists: playlists, lookup: lookup, path: path, writable: true}, nil
}

// NewReadOnlyFavorites returns an empty read-only Favorites. Used when the
// on-disk file is corrupted and must not be overwritten.
func NewReadOnlyFavorites() *Favorites {
	return newEmpty("", false)
}

func newEmpty(path string, writable bool) *Favorites {
	playlists := make(map[PlaylistID][]string, len(playlistOrder))
	for _, id := range playlistOrder {
		playlists[id] = nil
	}
	return &Favorites{playlists: playlists, lookup: make(map[string]PlaylistID), path: path, writable: writable}
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
func (f *Favorites) GetPlaylist(trackPath string) PlaylistID {
	return f.lookup[trackPath]
}

// Symbol returns the display symbol for the track's playlist, or "".
func (f *Favorites) Symbol(trackPath string) string {
	id := f.lookup[trackPath]
	for _, spec := range playlistSpecs {
		if spec.ID == id {
			return string(spec.Symbol)
		}
	}
	return ""
}

// Tracks returns a copy of the track list for the given playlist.
func (f *Favorites) Tracks(id PlaylistID) []string {
	tracks := f.playlists[id]
	out := make([]string, len(tracks))
	copy(out, tracks)
	return out
}

// Count returns the number of tracks in the given playlist.
func (f *Favorites) Count(id PlaylistID) int {
	return len(f.playlists[id])
}

// TotalCount returns the total number of tracks across all playlists.
func (f *Favorites) TotalCount() int {
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
func (f *Favorites) Cycle(trackPath string) (PlaylistID, error) {
	if !f.writable {
		return "", fmt.Errorf("favorites: read-only")
	}
	normalised, err := normalisePath(trackPath)
	if err != nil {
		return "", fmt.Errorf("favorites: normalise: %w", err)
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
		return current, fmt.Errorf("favorites: save: %w", err)
	}

	f.playlists = newPlaylists
	f.lookup = newLookup
	slog.Debug("favorites: cycle", "path", normalised, "from", current, "to", next)
	return next, nil
}

// Remove deletes the track from whichever playlist it belongs to.
// The on-disk file is written atomically before in-memory state changes.
func (f *Favorites) Remove(trackPath string) error {
	if !f.writable {
		return fmt.Errorf("favorites: read-only")
	}
	normalised, err := normalisePath(trackPath)
	if err != nil {
		return fmt.Errorf("favorites: normalise: %w", err)
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
		return fmt.Errorf("favorites: save: %w", err)
	}

	f.playlists = newPlaylists
	f.lookup = newLookup
	slog.Debug("favorites: remove", "path", normalised, "from", current)
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

// save writes the favorites atomically (temp+rename).
// Order is preserved exactly as in-memory (no sorting).
func (f *Favorites) save(playlists map[PlaylistID][]string) error {
	if f.path == "" {
		return fmt.Errorf("favorites: no path set")
	}

	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("favorites mkdir: %w", err)
	}

	raw := favoritesFile{Version: 1, Playlists: playlists}

	tmp, err := os.CreateTemp(dir, "favorites*.tmp")
	if err != nil {
		return fmt.Errorf("favorites temp: %w", err)
	}
	tmpPath := tmp.Name()

	if err := json.NewEncoder(tmp).Encode(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("favorites encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("favorites close: %w", err)
	}
	if err := os.Rename(tmpPath, f.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("favorites rename: %w", err)
	}
	return nil
}

// Writable reports whether mutations are allowed.
func (f *Favorites) Writable() bool { return f.writable }

// FavoriteTrackTitle returns a display name for a favourited track path.
func FavoriteTrackTitle(path string) string {
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

// IsLocalPath reports whether path is a local filesystem path (not a
// catalog URL like modland:... or modarchive:...).
func IsLocalPath(path string) bool {
	return !IsModland(path) && !IsModArchive(path)
}

// FilterAvailable returns only the tracks that exist on disk. Local files
// are checked with os.Stat; catalog URLs are kept unconditionally.
func FilterAvailable(tracks []string) []string {
	out := make([]string, 0, len(tracks))
	for _, t := range tracks {
		if !IsLocalPath(t) {
			out = append(out, t)
			continue
		}
		if _, err := os.Stat(t); err == nil {
			out = append(out, t)
		}
	}
	return out
}
