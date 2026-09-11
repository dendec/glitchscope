package app

import (
	"fmt"
	"hash/fnv"
	"log/slog"
	"math/rand"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/dendec/glitchscope/internal/catalog"
	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/player"
)

func (s *playbackState) snapshot(trackAlbumIdx int, includeTrackInfos bool) overlayPlaybackSnapshot {
	snapshot := overlayPlaybackSnapshot{}
	if s.pl != nil {
		snapshot.position = s.pl.Position()
		snapshot.duration = s.pl.Duration()
		snapshot.sampleRate = s.pl.SampleRate()
		snapshot.bitrate = s.pl.Bitrate()
		snapshot.bpm = s.pl.BPM()
		snapshot.channels = s.pl.Channels()
		snapshot.paused = s.pl.IsPaused()
		snapshot.tracker = s.pl.IsTracker()
		snapshot.hasPlayer = true
	}
	if s.lib == nil {
		return snapshot
	}
	snapshot.albums = s.lib.Albums
	snapshot.currentAlbum = s.lib.CurrentAlbumIndex()
	snapshot.currentTrack = s.lib.CurrentTrackIndex()
	if trackAlbumIdx < 0 {
		trackAlbumIdx = snapshot.currentAlbum
	}
	if includeTrackInfos {
		snapshot.trackInfos = s.lib.GetAlbumTracks(trackAlbumIdx)
	}
	if trackAlbumIdx == snapshot.currentAlbum {
		snapshot.trackCursor = snapshot.currentTrack
	}
	if s.pl != nil {
		snapshot.playingAlbum = s.lib.CurrentAlbum().Name
		snapshot.playingTrack = s.pl.TrackPath()
		if !s.pl.IsValidVoice() && !s.pl.Loading() {
			snapshot.playingTrack = ""
		}
		snapshot.loading, snapshot.loadPercent = s.pl.LoadProgress()
	}
	snapshot.hasLibrary = true
	return snapshot
}

// trackRef identifies a track for playback without coupling to the library cursor.
type trackRef struct {
	path  string
	album string
}

// shuffleEngine produces a randomized order of tracks from a pool.
// It tracks the pool identity to avoid unnecessary reshuffling.
type shuffleEngine struct {
	order     []trackRef
	idx       int
	sourceKey string     // identity of the current order's source
	rng       *rand.Rand // injected RNG for deterministic testing
}

// reset clears the shuffle state, forcing a rebuild on the next advance.
func (e *shuffleEngine) reset() {
	e.order = nil
	e.idx = 0
	e.sourceKey = ""
}

// next returns the next track from the shuffled order. Returns false when
// the order is exhausted; the caller should rebuild and retry.
func (e *shuffleEngine) next() (trackRef, bool) {
	if e.idx >= len(e.order) {
		return trackRef{}, false
	}
	t := e.order[e.idx]
	e.idx++
	return t, true
}

// needsRebuild reports whether the engine needs a new pool. A rebuild is
// needed when the order is empty, the source changed, or the order is
// exhausted with RepeatAll.
func (e *shuffleEngine) needsRebuild(key string, repeat config.RepeatMode) bool {
	if len(e.order) == 0 {
		return true
	}
	if e.sourceKey != key {
		return true
	}
	return e.idx >= len(e.order) && repeat == config.RepeatAll
}

// build replaces the order with a shuffled copy of pool and resets the cursor.
func (e *shuffleEngine) build(pool []trackRef, key string) {
	if e.rng == nil {
		e.rng = rand.New(rand.NewSource(0))
	}
	e.rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	e.order = pool
	e.idx = 0
	e.sourceKey = key
}

// excludeCurrentTrack returns a new slice with currentPath removed.
// If pool has ≤1 entry or currentPath is empty, the original pool is returned.
func excludeCurrentTrack(pool []trackRef, currentPath string) []trackRef {
	if len(pool) <= 1 || currentPath == "" {
		return pool
	}
	filtered := pool[:0]
	for _, t := range pool {
		if t.path != currentPath {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

type playbackState struct {
	pl            *player.Player
	lib           *player.Library
	shuffle       shuffleEngine
	playlist      []string
	playlistIdx   int
	playlistAlbum string
	offline       atomic.Bool // connectivity, independent of cached catalog visibility
	failedTracks  map[string]bool

	// shuffleCatalog is the runtime coordinator over the three provider
	// shuffle indexes, built at startup or lazily in the background (see
	// shuffle_catalog.go). Nil until built, or if no source is available;
	// advanceShuffleLazy falls back to legacy pool-based selection then.
	// Accessed atomically so a background goroutine can install it without
	// blocking the render thread.
	shuffleCatalog atomic.Pointer[catalog.ShuffleCatalog]

	// offlineProjection contains the local source and only the cached subset
	// of each remote source. It is published as a complete immutable snapshot
	// after cache reconciliation.
	offlineProjection atomic.Pointer[catalog.OfflineProjection]
}

// play starts playback of a track. It syncs the playlist/library cursor and
// delegates to the player.
func (s *playbackState) play(path string) bool {
	if s.pl == nil {
		return false
	}
	s.selectPlaybackContext(path)
	s.pl.PlayFileAsync(path)
	return true
}

// selectPlaybackContext syncs the playlist and library cursors to the track
// about to be played. Separated from launch so each concern is independently
// testable.
func (s *playbackState) selectPlaybackContext(path string) {
	// Sync library cursor (silent no-op if track isn't in the library).
	s.selectLibraryTrack(path)

	// Sync playlist cursor. If the track is in the playlist, update
	// playlistIdx so manual next/prev and shuffle continue from the right
	// position. Only clear the playlist when the track is genuinely absent.
	if len(s.playlist) > 0 {
		if idx := slices.Index(s.playlist, path); idx >= 0 {
			s.playlistIdx = idx
			return
		}
	}
	// A restored or otherwise directly selected radio station has no local
	// directory to derive a playlist from. Keep it in an isolated one-item
	// radio queue so recovery can never fall back to the library's last track.
	if player.IsRadio(path) {
		s.playlist = []string{path}
		s.playlistIdx = 0
		s.playlistAlbum = "Radio"
		return
	}
	s.playlist = nil
	s.playlistIdx = -1
	s.playlistAlbum = ""
}

func (s *playbackState) selectLibraryTrack(path string) {
	if s.lib == nil || path == "" {
		return
	}
	for albumIdx, album := range s.lib.Albums {
		for trackIdx, trackPath := range album.Tracks {
			if trackPath == path {
				s.lib.SelectAlbum(albumIdx)
				s.lib.SelectTrack(trackIdx)
				return
			}
		}
	}
}

func (s *playbackState) setPlaylist(tracks []string, index int, album string) bool {
	if index < 0 || index >= len(tracks) {
		return false
	}
	s.playlist = append([]string(nil), tracks...)
	s.playlistIdx = index
	s.playlistAlbum = album
	return true
}

// clearPlaylist resets the playlist state.
func (s *playbackState) clearPlaylist() {
	s.playlist = nil
	s.playlistIdx = -1
	s.playlistAlbum = ""
}

func (s *playbackState) previousAlbum() (string, string, bool) {
	if s.lib == nil || s.pl == nil {
		return "", "", false
	}
	path := s.lib.AlbumPrev()
	if path == "" {
		return "", "", false
	}
	return path, s.lib.CurrentAlbum().Name, true
}

func (s *playbackState) nextAlbum() (string, string, bool) {
	if s.lib == nil || s.pl == nil {
		return "", "", false
	}
	path := s.lib.AlbumNext()
	if path == "" {
		return "", "", false
	}
	return path, s.lib.CurrentAlbum().Name, true
}

func (s *playbackState) previousTrack() (string, string, bool) {
	if s.lib == nil || s.pl == nil {
		return "", "", false
	}
	if len(s.playlist) > 0 {
		if s.playlistIdx <= 0 {
			s.playlistIdx = len(s.playlist) - 1
		} else {
			s.playlistIdx--
		}
		return s.playlist[s.playlistIdx], s.playlistAlbum, true
	}
	path := s.lib.TrackPrev()
	if path == "" {
		return "", "", false
	}
	return path, s.lib.CurrentAlbum().Name, true
}

func (s *playbackState) nextTrack() (string, string, bool) {
	if s.lib == nil || s.pl == nil {
		return "", "", false
	}
	if len(s.playlist) > 0 {
		s.playlistIdx++
		if s.playlistIdx >= len(s.playlist) {
			s.playlistIdx = 0
		}
		return s.playlist[s.playlistIdx], s.playlistAlbum, true
	}
	path := s.lib.TrackNext()
	if path == "" {
		return "", "", false
	}
	return path, s.lib.CurrentAlbum().Name, true
}

// manualNext honors shuffle while overriding Repeat One and exhausted album orders.
func (s *playbackState) manualNext(settings config.PlaybackSettings) (string, string, bool) {
	if settings.ShuffleMode == config.ShuffleOff {
		return s.nextTrack()
	}
	settings.Repeat = config.RepeatAll
	t, ok := s.advance(settings)
	return t.path, t.album, ok
}

const maxPlaybackFailures = 10

// nextAfterFailure bounds recovery without maintaining a global shuffle visited set.
func (s *playbackState) nextAfterFailure(path string, settings config.PlaybackSettings) (trackRef, bool) {
	if s.failedTracks == nil {
		s.failedTracks = make(map[string]bool)
	}
	s.failedTracks[path] = true
	if len(s.failedTracks) >= maxPlaybackFailures {
		return trackRef{}, false
	}
	// A broken Repeat One track must not be retried forever.
	if settings.Repeat == config.RepeatOne {
		settings.Repeat = config.RepeatAll
	}
	for range maxPlaybackFailures {
		t, ok := s.advance(settings)
		if !ok {
			return trackRef{}, false
		}
		if !s.failedTracks[t.path] {
			return t, true
		}
		// Sequential selection needs its cursor advanced even for a rejected candidate.
		s.selectPlaybackContext(t.path)
	}
	return trackRef{}, false
}

// advance picks the next track according to shuffle/repeat settings.
// Shared by automatic advancement, shuffled manual Next, and error recovery.
func (s *playbackState) advance(settings config.PlaybackSettings) (trackRef, bool) {
	if s.lib == nil || s.pl == nil {
		return trackRef{}, false
	}

	if s.isRadioPlayback() {
		return s.advanceRadio(settings)
	}

	if settings.Repeat == config.RepeatOne {
		return s.repeatOne()
	}

	if settings.ShuffleMode != config.ShuffleOff {
		return s.advanceShuffled(settings)
	}

	return s.advanceSequential(settings)
}

func (s *playbackState) isRadioPlayback() bool {
	if s.pl != nil && player.IsRadio(s.pl.TrackPath()) {
		return true
	}
	return s.playlistIdx >= 0 && s.playlistIdx < len(s.playlist) && player.IsRadio(s.playlist[s.playlistIdx])
}

func (s *playbackState) radioCurrentPath() string {
	if s.pl != nil {
		if path := s.pl.TrackPath(); player.IsRadio(path) {
			return path
		}
	}
	if s.playlistIdx >= 0 && s.playlistIdx < len(s.playlist) {
		return s.playlist[s.playlistIdx]
	}
	return ""
}

// advanceRadio applies the same repeat/shuffle policy as other sources while
// deliberately staying inside the active radio queue. In particular, a
// failed stream must not advance through the local library by accident.
func (s *playbackState) advanceRadio(settings config.PlaybackSettings) (trackRef, bool) {
	if settings.Repeat == config.RepeatOne {
		return s.repeatOne()
	}
	if settings.ShuffleMode == config.ShuffleOff {
		return s.advancePlaylistSequential(settings)
	}
	if len(s.playlist) <= 1 {
		if settings.Repeat == config.RepeatAll && len(s.playlist) == 1 {
			return trackRef{path: s.playlist[0], album: s.playlistAlbum}, true
		}
		return trackRef{}, false
	}

	key := "radio:" + s.playlistKey()
	if s.shuffle.needsRebuild(key, settings.Repeat) {
		pool := excludeCurrentTrack(s.playlistTracks(), s.radioCurrentPath())
		s.shuffle.build(pool, key)
	}
	return s.shuffle.next()
}

// repeatOne returns the current track regardless of context.
func (s *playbackState) repeatOne() (trackRef, bool) {
	if len(s.playlist) > 0 {
		if s.playlistIdx >= 0 && s.playlistIdx < len(s.playlist) {
			return trackRef{path: s.playlist[s.playlistIdx], album: s.playlistAlbum}, true
		}
		return trackRef{}, false
	}
	path := s.lib.CurrentTrack()
	if path == "" {
		return trackRef{}, false
	}
	return trackRef{path: path, album: s.lib.CurrentAlbum().Name}, true
}

// advanceShuffled picks the next track from the shuffled order, rebuilding
// when the source or mode changes. Shuffle Album stays within the active
// playlist when one exists; broader modes derive their pool from the library.
//
// Shuffle Source and Shuffle All prefer the lazy ShuffleCatalog coordinator
// (a single weighted pick per call, no stored permutation, repeats allowed)
// and fall back to the legacy pool-based engine only while the coordinator is
// unavailable. In offline mode a remote source never falls back to an
// uncached library pool.
func (s *playbackState) advanceShuffled(settings config.PlaybackSettings) (trackRef, bool) {
	if settings.ShuffleMode == config.ShuffleSource || settings.ShuffleMode == config.ShuffleAll {
		if t, ok := s.advanceShuffleLazy(settings); ok {
			return t, true
		}
		// Before the offline projection is ready, legacy fallback is safe for
		// local playback only. Once a projection is published, an empty or
		// source-ineligible result is authoritative; never fall back to an
		// uncached library pool.
		if s.offline.Load() {
			if s.offlineProjection.Load() != nil ||
				settings.ShuffleMode == config.ShuffleSource && s.currentSource() != "local" {
				return trackRef{}, false
			}
		}
	}
	pool, key := s.shufflePool(settings)
	if s.shuffle.needsRebuild(key, settings.Repeat) {
		filtered := excludeCurrentTrack(pool, s.pl.TrackPath())
		s.shuffle.build(filtered, key)
	}
	t, ok := s.shuffle.next()
	if !ok {
		return trackRef{}, false
	}
	return t, true
}

// advanceShuffleLazy picks one track directly from the ShuffleCatalog
// coordinator for Shuffle Source / Shuffle All. Returns false when the
// coordinator or the requested source is unavailable so the caller can use
// the compatibility path where that is safe.
func (s *playbackState) advanceShuffleLazy(settings config.PlaybackSettings) (trackRef, bool) {
	if s.shuffle.rng == nil {
		return trackRef{}, false
	}

	var (
		t   catalog.ShuffleTrack
		err error
	)
	switch settings.ShuffleMode {
	case config.ShuffleSource:
		kind, ok := sourceKindOf(s.currentSource())
		if !ok {
			return trackRef{}, false
		}
		if s.offline.Load() {
			projection := s.offlineProjection.Load()
			if projection == nil {
				return trackRef{}, false
			}
			t, err = projection.RandomTrackFromSource(kind, s.shuffle.rng)
		} else {
			cat := s.shuffleCatalog.Load()
			if cat == nil {
				return trackRef{}, false
			}
			t, err = cat.RandomTrackFromSource(kind, s.shuffle.rng)
		}
	case config.ShuffleAll:
		if s.offline.Load() {
			projection := s.offlineProjection.Load()
			if projection == nil {
				return trackRef{}, false
			}
			t, err = projection.RandomTrackAll(s.shuffle.rng)
		} else {
			cat := s.shuffleCatalog.Load()
			if cat == nil {
				return trackRef{}, false
			}
			t, err = cat.RandomTrackAll(s.shuffle.rng)
		}
	default:
		return trackRef{}, false
	}
	if err != nil {
		slog.Warn("shuffle selection failed", "error", err)
		return trackRef{}, false
	}

	s.materializeShuffleTrack(t)
	return trackRef{path: t.Path, album: t.AlbumName}, true
}

// sourceKindOf maps the currentSource() string identity to a catalog.SourceKind.
func sourceKindOf(source string) (catalog.SourceKind, bool) {
	switch source {
	case "local":
		return catalog.SourceLocal, true
	case "modland":
		return catalog.SourceModland, true
	case "modarchive":
		return catalog.SourceModArchive, true
	default:
		return 0, false
	}
}

// materializeShuffleTrack ensures a lazily selected remote track's directory
// is present in the library before playback, so overlay navigation and the
// "now playing" album name resolve correctly. It reads one directory listing
// from the source index — never the full remote catalog — and is a no-op
// when the album is already materialized (e.g. Modland, which is preloaded
// in full) or for local tracks.
func (s *playbackState) materializeShuffleTrack(t catalog.ShuffleTrack) {
	if s.lib == nil || t.Source == catalog.SourceLocal {
		return
	}
	albumPath := remoteAlbumPath(t)
	for _, a := range s.lib.Albums {
		if a.Path == albumPath {
			return
		}
	}
	cat := s.shuffleCatalog.Load()
	if cat == nil {
		return
	}
	idx := cat.SourceIndex(t.Source)
	if idx == nil {
		return
	}
	listing, err := idx.DirectoryList(t.DirectoryKey)
	if err != nil {
		return
	}
	tracks := make([]string, len(listing.Entries))
	for i, e := range listing.Entries {
		tracks[i] = e.Path
	}
	s.lib.AddCatalogAlbum(player.Album{
		Name:   remoteAlbumDisplayName(t),
		Path:   albumPath,
		Tracks: tracks,
	})
}

// remoteAlbumPath returns the player.Album.Path convention for a remote
// ShuffleTrack's directory, matching how the app already materializes
// Modland/ModArchive albums (see addModlandAlbums, modarchive.BuildAlbum).
func remoteAlbumPath(t catalog.ShuffleTrack) string {
	switch t.Source {
	case catalog.SourceModland:
		return player.ModlandPrefix + t.DirectoryKey.Locator
	case catalog.SourceModArchive:
		return player.ModArchivePrefix + t.DirectoryKey.Locator
	default:
		return t.DirectoryKey.Locator
	}
}

// remoteAlbumDisplayName mirrors the existing "<Provider>: <label>" naming
// convention used elsewhere for materialized remote albums.
func remoteAlbumDisplayName(t catalog.ShuffleTrack) string {
	switch t.Source {
	case catalog.SourceModland:
		return "Modland: " + t.AlbumName
	case catalog.SourceModArchive:
		return "ModArchive: " + t.AlbumName
	default:
		return t.AlbumName
	}
}

// shufflePool returns the track pool and identity key for shuffling.
func (s *playbackState) shufflePool(settings config.PlaybackSettings) ([]trackRef, string) {
	switch settings.ShuffleMode {
	case config.ShuffleAlbum:
		if len(s.playlist) > 0 {
			return s.playlistTracks(), s.playlistKey()
		}
		return s.currentAlbumTracks(), s.albumKey()
	case config.ShuffleSource:
		source := s.currentSource()
		return s.sourceTracks(source), s.libraryKey("source:" + source)
	case config.ShuffleAll:
		if s.offline.Load() {
			return s.sourceTracks("local"), s.libraryKey("all:offline")
		}
		return s.allTracks(), s.libraryKey("all")
	default:
		return nil, ""
	}
}

// playlistTracks builds trackRefs from the active playlist.
func (s *playbackState) playlistTracks() []trackRef {
	tracks := make([]trackRef, len(s.playlist))
	for i, path := range s.playlist {
		tracks[i] = trackRef{path: path, album: s.playlistAlbum}
	}
	return tracks
}

// playlistKey returns a stable identity for the current playlist content.
// Uses FNV-1a hash over all track paths to distinguish playlists that differ
// in content even when they share the same album name and length.
func (s *playbackState) playlistKey() string {
	h := fnv.New32a()
	for _, p := range s.playlist {
		h.Write([]byte(p))
		h.Write([]byte{0}) // separator
	}
	return fmt.Sprintf("pl:%s:%x", s.playlistAlbum, h.Sum32())
}

// albumKey returns a stable identity for the current library album.
// Includes track count to detect additions/deletions within the album.
func (s *playbackState) albumKey() string {
	idx := s.lib.CurrentAlbumIndex()
	if idx < 0 || idx >= len(s.lib.Albums) {
		return fmt.Sprintf("album:%d", idx)
	}
	return fmt.Sprintf("album:%d:%d", idx, len(s.lib.Albums[idx].Tracks))
}

// libraryKey returns a key that changes when the library content changes.
// The key embeds the total track count so that a rescan (add/delete)
// invalidates any cached shuffle order.
func (s *playbackState) libraryKey(scope string) string {
	total := 0
	for _, album := range s.lib.Albums {
		total += len(album.Tracks)
	}
	return fmt.Sprintf("%s:%d", scope, total)
}

// advanceSequential advances through playlist or library in order.
func (s *playbackState) advanceSequential(settings config.PlaybackSettings) (trackRef, bool) {
	if len(s.playlist) > 0 {
		return s.advancePlaylistSequential(settings)
	}
	return s.advanceLibrarySequential(settings)
}

func (s *playbackState) advancePlaylistSequential(settings config.PlaybackSettings) (trackRef, bool) {
	next := s.playlistIdx + 1
	if next < len(s.playlist) {
		s.playlistIdx = next
		return trackRef{path: s.playlist[next], album: s.playlistAlbum}, true
	}
	if settings.Repeat == config.RepeatAll {
		s.playlistIdx = 0
		return trackRef{path: s.playlist[0], album: s.playlistAlbum}, true
	}
	return trackRef{}, false
}

func (s *playbackState) advanceLibrarySequential(settings config.PlaybackSettings) (trackRef, bool) {
	album := s.lib.CurrentAlbum()
	if s.lib.CurrentTrackIndex() < len(album.Tracks)-1 {
		path := s.lib.TrackNext()
		if path != "" {
			return trackRef{path: path, album: s.lib.CurrentAlbum().Name}, true
		}
	}
	if settings.Repeat == config.RepeatAll {
		path := s.lib.AlbumNext()
		if path != "" {
			return trackRef{path: path, album: s.lib.CurrentAlbum().Name}, true
		}
	}
	return trackRef{}, false
}

// --- Pool builders (used by shufflePool) ---

func (s *playbackState) currentAlbumTracks() []trackRef {
	albumIdx := s.lib.CurrentAlbumIndex()
	if albumIdx < 0 || albumIdx >= len(s.lib.Albums) {
		return nil
	}
	album := s.lib.Albums[albumIdx]
	tracks := make([]trackRef, len(album.Tracks))
	for i, path := range album.Tracks {
		tracks[i] = trackRef{path: path, album: album.Name}
	}
	return tracks
}

func (s *playbackState) currentSource() string {
	if s.isRadioPlayback() {
		return "radio"
	}
	if s.lib != nil {
		album := s.lib.CurrentAlbum()
		if player.IsModland(album.Path) {
			return "modland"
		}
		if player.IsModArchive(album.Path) {
			return "modarchive"
		}
	}
	return "local"
}

func (s *playbackState) sourceTracks(source string) []trackRef {
	var all []trackRef
	for _, album := range s.lib.Albums {
		albumSource := "local"
		if player.IsModland(album.Path) {
			albumSource = "modland"
		} else if player.IsModArchive(album.Path) {
			albumSource = "modarchive"
		}
		if albumSource != source {
			continue
		}
		for _, path := range album.Tracks {
			all = append(all, trackRef{path: path, album: album.Name})
		}
	}
	return all
}

func (s *playbackState) allTracks() []trackRef {
	var all []trackRef
	for _, album := range s.lib.Albums {
		for _, path := range album.Tracks {
			all = append(all, trackRef{path: path, album: album.Name})
		}
	}
	return all
}

// prunePlaylist removes entries inside deletedPath and clamps the cursor.
func (s *playbackState) prunePlaylist(deletedPath string) {
	if len(s.playlist) == 0 {
		return
	}
	sep := string(filepath.Separator)
	var kept []string
	for _, p := range s.playlist {
		if p == deletedPath || strings.HasPrefix(p, deletedPath+sep) {
			continue
		}
		kept = append(kept, p)
	}
	if len(kept) == len(s.playlist) {
		return
	}
	if len(kept) == 0 {
		s.playlist = nil
		s.playlistIdx = -1
		s.playlistAlbum = ""
		return
	}
	s.playlist = kept
	if s.playlistIdx >= len(s.playlist) {
		s.playlistIdx = len(s.playlist) - 1
	}
}
