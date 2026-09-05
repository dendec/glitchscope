package app

import (
	"math/rand"
	"testing"

	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/player"
)

func TestPlaybackStateAdvancesWithinVirtualPlaylist(t *testing.T) {
	state := playbackState{
		pl:            &player.Player{},
		lib:           &player.Library{},
		playlist:      []string{"modarchive:first.mod", "modarchive:second.mod"},
		playlistIdx:   0,
		playlistAlbum: "ModArchive: folder",
	}

	track, ok := state.advance(config.PlaybackSettings{ShuffleMode: config.ShuffleOff})
	if !ok {
		t.Fatal("advance returned false, want next virtual track")
	}
	if track.path != "modarchive:second.mod" || track.album != "ModArchive: folder" {
		t.Fatalf("advance returned %+v, want second track from the same album", track)
	}

	if _, ok := state.advance(config.PlaybackSettings{ShuffleMode: config.ShuffleOff}); ok {
		t.Fatal("advance returned a track after the virtual playlist ended")
	}
}

func TestPlaybackStateManualNavigationUsesActivePlaylist(t *testing.T) {
	state := playbackState{
		pl:            &player.Player{},
		lib:           &player.Library{},
		playlist:      []string{"folder:first.mod", "folder:second.mod", "folder:third.mod"},
		playlistIdx:   0,
		playlistAlbum: "folder",
	}

	path, album, ok := state.nextTrack()
	if !ok || path != "folder:second.mod" || album != "folder" {
		t.Fatalf("nextTrack returned (%q, %q, %v), want second playlist track", path, album, ok)
	}

	path, album, ok = state.previousTrack()
	if !ok || path != "folder:first.mod" || album != "folder" {
		t.Fatalf("previousTrack returned (%q, %q, %v), want first playlist track", path, album, ok)
	}
}

func TestPlaybackStatePlaySynchronizesCatalogTrack(t *testing.T) {
	path := "modarchive:http://modarchive.textfiles.com/2013/IT/B/bacter_vs_saga_musix_-_funky_junkie.it.zip"
	state := playbackState{
		pl: &player.Player{},
		lib: &player.Library{Albums: []player.Album{
			{Name: "Modland: Protracker/Asylum", Tracks: []string{"modland:asylum.mod"}},
			{Name: "ModArchive: 2013/IT/B", Tracks: []string{
				"modarchive:http://modarchive.textfiles.com/2013/IT/B/ba-piler.it.zip",
				path,
				"modarchive:http://modarchive.textfiles.com/2013/IT/B/bad_dreamz.it.zip",
			}},
		}},
	}

	state.selectLibraryTrack(path)

	if got := state.lib.CurrentAlbumIndex(); got != 1 {
		t.Fatalf("current album index = %d, want catalog album 1", got)
	}
	if got := state.lib.CurrentTrackIndex(); got != 1 {
		t.Fatalf("current track index = %d, want bacter track 1", got)
	}

	track, ok := state.advance(config.PlaybackSettings{ShuffleMode: config.ShuffleOff})
	if !ok || track.path != "modarchive:http://modarchive.textfiles.com/2013/IT/B/bad_dreamz.it.zip" {
		t.Fatalf("advance returned (%+v, %v), want bad_dreamz", track, ok)
	}
}

// --- Shuffle tests ---

func TestShufflePlaylistCoversAllTracks(t *testing.T) {
	playlist := []string{"a.mod", "b.mod", "c.mod", "d.mod", "e.mod"}
	state := playbackState{
		pl:            &player.Player{},
		lib:           &player.Library{},
		playlist:      playlist,
		playlistIdx:   0,
		playlistAlbum: "test",
	}
	state.shuffle.rng = rand.New(rand.NewSource(42))
	settings := config.PlaybackSettings{
		ShuffleMode: config.ShuffleAll,
		Repeat:      config.RepeatOff,
	}

	seen := make(map[string]bool)
	for range len(playlist) {
		track, ok := state.advance(settings)
		if !ok {
			t.Fatal("advance returned false before all playlist tracks were played")
		}
		if seen[track.path] {
			t.Fatalf("track %q played twice", track.path)
		}
		seen[track.path] = true
		if track.album != "test" {
			t.Fatalf("track album = %q, want %q", track.album, "test")
		}
	}
	if len(seen) != len(playlist) {
		t.Fatalf("saw %d tracks, want %d", len(seen), len(playlist))
	}

	if _, ok := state.advance(settings); ok {
		t.Fatal("advance should return false after playlist exhaustion")
	}
}

func TestShufflePlaylistRepeatAllCycles(t *testing.T) {
	playlist := []string{"a.mod", "b.mod", "c.mod"}
	state := playbackState{
		pl:            &player.Player{},
		lib:           &player.Library{},
		playlist:      playlist,
		playlistIdx:   0,
		playlistAlbum: "test",
	}
	state.shuffle.rng = rand.New(rand.NewSource(42))
	settings := config.PlaybackSettings{
		ShuffleMode: config.ShuffleAll,
		Repeat:      config.RepeatAll,
	}

	for cycle := range 2 {
		seen := make(map[string]bool)
		for range len(playlist) {
			track, ok := state.advance(settings)
			if !ok {
				t.Fatalf("cycle %d: advance returned false", cycle)
			}
			if seen[track.path] {
				t.Fatalf("cycle %d: track %q played twice in one cycle", cycle, track.path)
			}
			seen[track.path] = true
		}
		if len(seen) != len(playlist) {
			t.Fatalf("cycle %d: saw %d tracks, want %d", cycle, len(seen), len(playlist))
		}
	}
}

func TestShuffleLibraryCoversAllTracks(t *testing.T) {
	lib := &player.Library{Albums: []player.Album{
		{Name: "Album A", Tracks: []string{"a1.mod", "a2.mod"}},
		{Name: "Album B", Tracks: []string{"b1.mod", "b2.mod", "b3.mod"}},
	}}
	state := playbackState{
		pl:  &player.Player{},
		lib: lib,
	}
	state.shuffle.rng = rand.New(rand.NewSource(42))
	settings := config.PlaybackSettings{
		ShuffleMode: config.ShuffleAll,
		Repeat:      config.RepeatOff,
	}

	total := 5
	seen := make(map[string]bool)
	for range total {
		track, ok := state.advance(settings)
		if !ok {
			t.Fatal("advance returned false before all library tracks were played")
		}
		if seen[track.path] {
			t.Fatalf("track %q played twice", track.path)
		}
		seen[track.path] = true
	}
	if len(seen) != total {
		t.Fatalf("saw %d tracks, want %d", len(seen), total)
	}

	if _, ok := state.advance(settings); ok {
		t.Fatal("advance should return false after library exhaustion")
	}
}

func TestShuffleAlbumStaysWithinAlbum(t *testing.T) {
	lib := &player.Library{Albums: []player.Album{
		{Name: "Album A", Tracks: []string{"a1.mod", "a2.mod", "a3.mod"}},
		{Name: "Album B", Tracks: []string{"b1.mod", "b2.mod"}},
	}}
	state := playbackState{
		pl:  &player.Player{},
		lib: lib,
	}
	state.shuffle.rng = rand.New(rand.NewSource(42))
	lib.SelectAlbum(0)
	settings := config.PlaybackSettings{
		ShuffleMode: config.ShuffleAlbum,
		Repeat:      config.RepeatOff,
	}

	seen := make(map[string]bool)
	for range 3 {
		track, ok := state.advance(settings)
		if !ok {
			t.Fatal("advance returned false")
		}
		seen[track.path] = true
		if track.album != "Album A" {
			t.Fatalf("track %q from album %q, want Album A", track.path, track.album)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("saw %d tracks, want 3 from Album A", len(seen))
	}
}

func TestShuffleLocalExcludesRemote(t *testing.T) {
	lib := &player.Library{Albums: []player.Album{
		{Name: "Local", Path: "/music/local", Tracks: []string{"local1.mod", "local2.mod"}},
		{Name: "Modland", Path: "modland:MODS", Tracks: []string{"remote1.mod"}},
	}}
	state := playbackState{
		pl:  &player.Player{},
		lib: lib,
	}
	state.shuffle.rng = rand.New(rand.NewSource(42))
	settings := config.PlaybackSettings{
		ShuffleMode: config.ShuffleLocal,
		Repeat:      config.RepeatOff,
	}

	seen := make(map[string]bool)
	for range 2 {
		track, ok := state.advance(settings)
		if !ok {
			t.Fatal("advance returned false")
		}
		seen[track.path] = true
	}
	if _, ok := seen["remote1.mod"]; ok {
		t.Fatal("remote track was included in ShuffleLocal")
	}
	if len(seen) != 2 {
		t.Fatalf("saw %d tracks, want 2 local tracks", len(seen))
	}
}

func TestShufflePlaylistRepeatOneRepeatsCurrent(t *testing.T) {
	playlist := []string{"a.mod", "b.mod", "c.mod"}
	state := playbackState{
		pl:            &player.Player{},
		lib:           &player.Library{},
		playlist:      playlist,
		playlistIdx:   1,
		playlistAlbum: "test",
	}
	settings := config.PlaybackSettings{
		ShuffleMode: config.ShuffleAll,
		Repeat:      config.RepeatOne,
	}

	for range 5 {
		track, ok := state.advance(settings)
		if !ok {
			t.Fatal("advance returned false")
		}
		if track.path != "b.mod" {
			t.Fatalf("RepeatOne returned %q, want b.mod", track.path)
		}
	}
}

func TestShuffleLibraryRepeatOneRepeatsCurrent(t *testing.T) {
	lib := &player.Library{Albums: []player.Album{
		{Name: "Album A", Tracks: []string{"a1.mod", "a2.mod"}},
	}}
	state := playbackState{
		pl:  &player.Player{},
		lib: lib,
	}
	lib.SelectAlbum(0)
	lib.SelectTrack(0)

	settings := config.PlaybackSettings{
		ShuffleMode: config.ShuffleAll,
		Repeat:      config.RepeatOne,
	}

	for range 5 {
		track, ok := state.advance(settings)
		if !ok {
			t.Fatal("advance returned false")
		}
		if track.path != "a1.mod" {
			t.Fatalf("RepeatOne returned %q, want a1.mod", track.path)
		}
	}
}

func TestShuffleExhaustedNoRepeatReturnsFalse(t *testing.T) {
	playlist := []string{"a.mod", "b.mod"}
	state := playbackState{
		pl:            &player.Player{},
		lib:           &player.Library{},
		playlist:      playlist,
		playlistIdx:   0,
		playlistAlbum: "test",
	}
	state.shuffle.rng = rand.New(rand.NewSource(42))
	settings := config.PlaybackSettings{
		ShuffleMode: config.ShuffleAll,
		Repeat:      config.RepeatOff,
	}

	for range 2 {
		if _, ok := state.advance(settings); !ok {
			t.Fatal("advance returned false too early")
		}
	}
	if _, ok := state.advance(settings); ok {
		t.Fatal("advance should return false after exhaustion with RepeatOff")
	}
}

func TestShuffleEngineReset(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	e := &shuffleEngine{rng: rng}
	pool := []trackRef{
		{path: "a.mod", album: "test"},
		{path: "b.mod", album: "test"},
		{path: "c.mod", album: "test"},
	}
	e.build(pool, "test")

	for e.idx < len(e.order) {
		if _, ok := e.next(); !ok {
			t.Fatal("next returned false before exhaustion")
		}
	}

	if _, ok := e.next(); ok {
		t.Fatal("next should return false after exhaustion")
	}

	e.reset()
	if e.needsRebuild("test", config.RepeatOff) != true {
		t.Fatal("needsRebuild should return true after reset")
	}
	e.build(pool, "test")
	if len(e.order) != 3 {
		t.Fatalf("after rebuild: order has %d tracks, want 3", len(e.order))
	}
}

func TestShuffleEngineSourceChangeTriggersRebuild(t *testing.T) {
	e := &shuffleEngine{rng: rand.New(rand.NewSource(1))}
	pool := []trackRef{{path: "a.mod", album: "test"}}
	e.build(pool, "key1")

	if e.needsRebuild("key1", config.RepeatOff) {
		t.Fatal("should not need rebuild with same key")
	}
	if !e.needsRebuild("key2", config.RepeatOff) {
		t.Fatal("should need rebuild with different key")
	}
}

func TestShuffleEngineRepeatAllTriggersRebuild(t *testing.T) {
	e := &shuffleEngine{rng: rand.New(rand.NewSource(1))}
	pool := []trackRef{{path: "a.mod", album: "test"}}
	e.build(pool, "key1")
	e.idx = len(e.order)

	if !e.needsRebuild("key1", config.RepeatAll) {
		t.Fatal("should need rebuild when exhausted with RepeatAll")
	}
	if e.needsRebuild("key1", config.RepeatOff) {
		t.Fatal("should NOT need rebuild when exhausted without RepeatAll")
	}
}

func TestPlaylistKeyChangesWithContent(t *testing.T) {
	state := playbackState{
		playlist:      []string{"a.mod", "b.mod"},
		playlistAlbum: "album1",
	}
	key1 := state.playlistKey()

	// Same album + same length, different tracks → different key.
	state.playlist = []string{"a.mod", "c.mod"}
	key2 := state.playlistKey()
	if key1 == key2 {
		t.Fatal("playlist key should change when track paths change")
	}

	// Same tracks, different album → different key.
	state.playlist = []string{"a.mod", "b.mod"}
	state.playlistAlbum = "album2"
	key3 := state.playlistKey()
	if key1 == key3 {
		t.Fatal("playlist key should change when album name changes")
	}

	// Same content → same key.
	state.playlistAlbum = "album1"
	state.playlist = []string{"a.mod", "b.mod"}
	key4 := state.playlistKey()
	if key1 != key4 {
		t.Fatal("playlist key should be stable for identical content")
	}
}

func TestExcludeCurrentTrack(t *testing.T) {
	pool := []trackRef{
		{path: "a.mod", album: "test"},
		{path: "b.mod", album: "test"},
		{path: "c.mod", album: "test"},
	}

	// Current track excluded.
	filtered := excludeCurrentTrack(pool, "b.mod")
	if len(filtered) != 2 {
		t.Fatalf("got %d tracks, want 2", len(filtered))
	}
	for _, t_ := range filtered {
		if t_.path == "b.mod" {
			t.Fatal("b.mod should be excluded")
		}
	}

	// Empty currentPath → no exclusion.
	filtered = excludeCurrentTrack(pool, "")
	if len(filtered) != 3 {
		t.Fatalf("got %d tracks, want 3", len(filtered))
	}

	// Single-element pool → no exclusion (avoid empty result).
	single := []trackRef{{path: "a.mod", album: "test"}}
	filtered = excludeCurrentTrack(single, "a.mod")
	if len(filtered) != 1 {
		t.Fatalf("single pool: got %d tracks, want 1", len(filtered))
	}

	// Unknown currentPath → no change.
	filtered = excludeCurrentTrack(pool, "zzz.mod")
	if len(filtered) != 3 {
		t.Fatalf("unknown path: got %d tracks, want 3", len(filtered))
	}
}

func TestSequentialPlaylistRepeatAllCycles(t *testing.T) {
	playlist := []string{"a.mod", "b.mod", "c.mod"}
	state := playbackState{
		pl:            &player.Player{},
		lib:           &player.Library{},
		playlist:      playlist,
		playlistIdx:   -1,
		playlistAlbum: "test",
	}
	settings := config.PlaybackSettings{
		ShuffleMode: config.ShuffleOff,
		Repeat:      config.RepeatAll,
	}

	for cycle := range 2 {
		for i, want := range playlist {
			track, ok := state.advance(settings)
			if !ok {
				t.Fatalf("cycle %d, track %d: advance returned false", cycle, i)
			}
			if track.path != want {
				t.Fatalf("cycle %d, track %d: got %q, want %q", cycle, i, track.path, want)
			}
		}
	}
}

func TestNoLibraryNoPlayerReturnsFalse(t *testing.T) {
	state := playbackState{}
	settings := config.PlaybackSettings{
		ShuffleMode: config.ShuffleAll,
		Repeat:      config.RepeatAll,
	}
	if _, ok := state.advance(settings); ok {
		t.Fatal("advance should return false with nil lib and player")
	}
}

func TestShuffleEmptyLibraryReturnsFalse(t *testing.T) {
	state := playbackState{
		pl:  &player.Player{},
		lib: &player.Library{},
	}
	state.shuffle.rng = rand.New(rand.NewSource(1))
	settings := config.PlaybackSettings{
		ShuffleMode: config.ShuffleAll,
		Repeat:      config.RepeatOff,
	}
	if _, ok := state.advance(settings); ok {
		t.Fatal("advance should return false with empty library")
	}
}

// --- Integration tests: advanceShuffled + selectPlaybackContext ---

func TestShuffleKeepsPlaylistCursorInSync(t *testing.T) {
	// Shuffle picks a track from the playlist, then play() calls
	// selectPlaybackContext which must update playlistIdx so that the next
	// manual next/prev starts from the correct position.
	playlist := []string{"a.mod", "b.mod", "c.mod", "d.mod", "e.mod"}
	state := playbackState{
		pl:            &player.Player{},
		lib:           &player.Library{},
		playlist:      playlist,
		playlistIdx:   0,
		playlistAlbum: "test",
	}
	state.shuffle.rng = rand.New(rand.NewSource(42))
	settings := config.PlaybackSettings{
		ShuffleMode: config.ShuffleAll,
		Repeat:      config.RepeatOff,
	}

	// Advance once via shuffle → get a random track.
	track, ok := state.advance(settings)
	if !ok {
		t.Fatal("advance returned false")
	}

	// Simulate what autoAdvance does: play the track.
	state.play(track.path)

	// After play, playlistIdx must point to the played track.
	if state.playlist == nil {
		t.Fatal("playlist was cleared after play")
	}
	if state.playlist[state.playlistIdx] != track.path {
		t.Fatalf("playlistIdx = %d (%q), want %q",
			state.playlistIdx, state.playlist[state.playlistIdx], track.path)
	}

	// Manual nextTrack must now work from the correct position.
	nextPath, _, ok := state.nextTrack()
	if !ok {
		t.Fatal("nextTrack returned false")
	}
	if nextPath == track.path {
		t.Fatalf("nextTrack returned the same track %q", nextPath)
	}
}

func TestShuffleFullCyclePlaylist(t *testing.T) {
	// Full cycle: advance → play → advance → play for every track.
	// Ensures playlistIdx stays in sync throughout the entire shuffle cycle.
	playlist := []string{"a.mod", "b.mod", "c.mod"}
	state := playbackState{
		pl:            &player.Player{},
		lib:           &player.Library{},
		playlist:      playlist,
		playlistIdx:   0,
		playlistAlbum: "test",
	}
	state.shuffle.rng = rand.New(rand.NewSource(99))
	settings := config.PlaybackSettings{
		ShuffleMode: config.ShuffleAll,
		Repeat:      config.RepeatOff,
	}

	played := make(map[string]bool)
	for range len(playlist) {
		track, ok := state.advance(settings)
		if !ok {
			t.Fatal("advance returned false")
		}
		if played[track.path] {
			t.Fatalf("track %q played twice", track.path)
		}
		played[track.path] = true

		state.play(track.path)

		// Verify playlistIdx points to the played track.
		if state.playlist == nil {
			t.Fatal("playlist cleared")
		}
		if state.playlist[state.playlistIdx] != track.path {
			t.Fatalf("after play: idx=%d, want path %q, got %q",
				state.playlistIdx, track.path, state.playlist[state.playlistIdx])
		}
	}
}

func TestShuffleLibraryRescanInvalidatesOrder(t *testing.T) {
	// Build a shuffle order, change the library, verify the next advance
	// uses the new content.
	lib := &player.Library{Albums: []player.Album{
		{Name: "A", Tracks: []string{"a1.mod", "a2.mod"}},
	}}
	state := playbackState{
		pl:  &player.Player{},
		lib: lib,
	}
	state.shuffle.rng = rand.New(rand.NewSource(42))
	settings := config.PlaybackSettings{
		ShuffleMode: config.ShuffleAll,
		Repeat:      config.RepeatOff,
	}

	// Play through the original library.
	for range 2 {
		if _, ok := state.advance(settings); !ok {
			t.Fatal("advance returned false")
		}
	}

	// Simulate library rescan: replace Albums.
	lib.Albums = []player.Album{
		{Name: "B", Tracks: []string{"b1.mod", "b2.mod", "b3.mod"}},
	}
	// Reset shuffle to mimic what deleteNCPath does.
	state.shuffle.reset()

	seen := make(map[string]bool)
	for range 3 {
		track, ok := state.advance(settings)
		if !ok {
			t.Fatal("advance returned false after rescan")
		}
		if seen[track.path] {
			t.Fatalf("track %q played twice", track.path)
		}
		seen[track.path] = true
		if track.album != "B" {
			t.Fatalf("track %q from album %q, want B", track.path, track.album)
		}
	}
}

func TestShuffleEngineZeroValueSafe(t *testing.T) {
	// A shuffleEngine created without explicit rng must not panic.
	var e shuffleEngine
	pool := []trackRef{
		{path: "a.mod", album: "test"},
		{path: "b.mod", album: "test"},
	}
	e.build(pool, "key")
	if len(e.order) != 2 {
		t.Fatalf("order len = %d, want 2", len(e.order))
	}
	// Second build should also work (rng already initialized).
	e.build(pool, "key2")
	if len(e.order) != 2 {
		t.Fatalf("second build: order len = %d, want 2", len(e.order))
	}
}

func TestLibraryKeyChangesAfterRescan(t *testing.T) {
	lib := &player.Library{Albums: []player.Album{
		{Name: "A", Tracks: []string{"a1.mod"}},
	}}
	state := playbackState{lib: lib}
	key1 := state.libraryKey("all")

	// Add a track.
	lib.Albums[0].Tracks = append(lib.Albums[0].Tracks, "a2.mod")
	key2 := state.libraryKey("all")
	if key1 == key2 {
		t.Fatal("library key should change after track addition")
	}

	// Remove a track.
	lib.Albums[0].Tracks = lib.Albums[0].Tracks[:1]
	key3 := state.libraryKey("all")
	if key1 != key3 {
		t.Fatal("library key should be stable after restore")
	}
}

func TestAlbumKeyIncludesTrackCount(t *testing.T) {
	lib := &player.Library{Albums: []player.Album{
		{Name: "A", Tracks: []string{"a1.mod"}},
	}}
	state := playbackState{lib: lib}
	lib.SelectAlbum(0)
	key1 := state.albumKey()

	lib.Albums[0].Tracks = append(lib.Albums[0].Tracks, "a2.mod")
	key2 := state.albumKey()
	if key1 == key2 {
		t.Fatal("album key should change after track addition")
	}
}
