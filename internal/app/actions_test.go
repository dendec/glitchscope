package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/input"
	"github.com/dendec/glitchscope/internal/player"
	"github.com/dendec/glitchscope/internal/radio"
	"github.com/dendec/glitchscope/internal/ui"
)

func TestPresetPackCalibrationDoesNotBlockPageNavigationFromLibrary(t *testing.T) {
	for _, test := range []struct {
		name       string
		action     input.Action
		wantPreset bool
	}{
		{name: "next", action: input.ActionNextPreset, wantPreset: true},
		{name: "previous", action: input.ActionPrevPreset},
	} {
		t.Run(test.name, func(t *testing.T) {
			o := &ui.Overlay{}
			o.SetPresetPacks([]ui.PresetPackItem{{ID: "pack", Name: "Pack", Testing: true}})
			o.ToggleUI()
			a := &App{overlay: o, presetPackTest: &presetPackTestState{}}

			a.handleAction(test.action, 0, 0)
			if got := o.IsPresetsPage(); got != test.wantPreset {
				t.Fatalf("IsPresetsPage() = %t after %s-page action", got, test.name)
			}
			if test.wantPreset {
				a.handleAction(input.ActionPrevPreset, 0, 0)
				if !o.IsPresetsPage() {
					t.Fatal("page navigation left the active preset test without cancellation")
				}
			}
		})
	}
}

func TestWalkAudioFiles(t *testing.T) {
	// Create temp dir with supported and unsupported files.
	root := t.TempDir()

	// Supported files.
	createFile(t, filepath.Join(root, "track1.mp3"))
	createFile(t, filepath.Join(root, "sub", "track2.ogg"))
	createFile(t, filepath.Join(root, "Sub", "track3.flac"))

	// Unsupported files — should be skipped.
	createFile(t, filepath.Join(root, "readme.txt"))
	createFile(t, filepath.Join(root, "image.jpg"))

	// Hidden files — should be skipped.
	createFile(t, filepath.Join(root, ".hidden.mp3"))

	// Metadata — should be skipped.
	createFile(t, filepath.Join(root, ".gsa_meta.json"))

	// Artwork — should be skipped.
	createFile(t, filepath.Join(root, "album.jpg"))
	createFile(t, filepath.Join(root, "album.png"))

	// Empty subdirectory — should not appear.
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	files, err := walkAudioFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("walkAudioFiles returned %d files, want 3: %v", len(files), files)
	}

	// Should be sorted lexically by full path.
	expected := []string{
		filepath.Join(root, "Sub", "track3.flac"),
		filepath.Join(root, "sub", "track2.ogg"),
		filepath.Join(root, "track1.mp3"),
	}
	for i, f := range files {
		if f != expected[i] {
			t.Errorf("files[%d] = %q, want %q", i, f, expected[i])
		}
	}
}

func TestSortTrackPathsFollowsGlobalOrder(t *testing.T) {
	tests := []struct {
		order config.SortOrder
		want  []string
	}{
		{order: config.SortSource, want: []string{"/music/Zeta.it", "/music/alpha.it", "/music/Beta.it"}},
		{order: config.SortAZ, want: []string{"/music/alpha.it", "/music/Beta.it", "/music/Zeta.it"}},
		{order: config.SortZA, want: []string{"/music/Zeta.it", "/music/Beta.it", "/music/alpha.it"}},
	}
	for _, test := range tests {
		paths := []string{"/music/Zeta.it", "/music/alpha.it", "/music/Beta.it"}
		sortTrackPaths(paths, test.order)
		if len(paths) != len(test.want) {
			t.Fatalf("order %v: paths=%v, want %v", test.order, paths, test.want)
		}
		for i := range paths {
			if paths[i] != test.want[i] {
				t.Fatalf("order %v: paths=%v, want %v", test.order, paths, test.want)
			}
		}
	}
}

func TestWalkAudioFilesEmptyDir(t *testing.T) {
	root := t.TempDir()
	files, err := walkAudioFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("walkAudioFiles on empty dir returned %d files, want 0", len(files))
	}
}

func TestWalkAudioFilesMissingRootReturnsError(t *testing.T) {
	_, err := walkAudioFiles(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("missing root should return an error")
	}
}

func TestWalkAudioFilesSymlinksSkipped(t *testing.T) {
	root := t.TempDir()

	// Create a real file.
	real := filepath.Join(root, "real.mp3")
	createFile(t, real)

	// Create a symlink to it.
	link := filepath.Join(root, "link.mp3")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	files, err := walkAudioFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("walkAudioFiles returned %d files, want 1 (symlink skipped): %v", len(files), files)
	}
	if files[0] != real {
		t.Fatalf("files[0] = %q, want %q", files[0], real)
	}
}

func TestPlayDirectoryReplacesPlaylist(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, "b.ogg"))
	createFile(t, filepath.Join(root, "sub", "a.mp3"))
	createFile(t, filepath.Join(root, "c.txt")) // unsupported

	a := &App{}
	// Pre-existing playlist — should be replaced.
	a.playbackState.playlist = []string{"old.mp3"}

	a.playDirectory(root)

	// playlist replaced with 2 audio files, sorted lexically by full path.
	want := []string{
		filepath.Join(root, "b.ogg"),
		filepath.Join(root, "sub", "a.mp3"),
	}
	if len(a.playbackState.playlist) != len(want) {
		t.Fatalf("playlist len = %d, want %d: %v", len(a.playbackState.playlist), len(want), a.playbackState.playlist)
	}
	for i, p := range a.playbackState.playlist {
		if p != want[i] {
			t.Errorf("playlist[%d] = %q, want %q", i, p, want[i])
		}
	}
	if a.playbackState.playlistIdx != 0 {
		t.Errorf("playlistIdx = %d, want 0", a.playbackState.playlistIdx)
	}
	if a.playbackState.playlistAlbum != filepath.Base(root) {
		t.Errorf("playlistAlbum = %q, want %q", a.playbackState.playlistAlbum, filepath.Base(root))
	}
}

func TestPlayDirectoryEmptyPreservesPlaylist(t *testing.T) {
	root := t.TempDir() // empty dir

	a := &App{}
	a.playbackState.playlist = []string{"old.mp3"}
	a.playbackState.playlistIdx = 0

	a.playDirectory(root)

	// Playlist must not change.
	if len(a.playbackState.playlist) != 1 || a.playbackState.playlist[0] != "old.mp3" {
		t.Fatalf("playlist changed unexpectedly: %v", a.playbackState.playlist)
	}
}

func TestPlayFileSetsParentPlaylist(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, "a.mp3"))
	createFile(t, filepath.Join(root, "b.ogg"))
	createFile(t, filepath.Join(root, "c.flac"))

	target := filepath.Join(root, "b.ogg")
	a := &App{}

	a.playFile(target)

	// playlist = parent dir files, sorted lexically.
	want := []string{
		filepath.Join(root, "a.mp3"),
		filepath.Join(root, "b.ogg"),
		filepath.Join(root, "c.flac"),
	}
	if len(a.playbackState.playlist) != 3 {
		t.Fatalf("playlist len = %d, want 3: %v", len(a.playbackState.playlist), a.playbackState.playlist)
	}
	for i, p := range a.playbackState.playlist {
		if p != want[i] {
			t.Errorf("playlist[%d] = %q, want %q", i, p, want[i])
		}
	}
	// cursor on b.ogg = index 1.
	if a.playbackState.playlistIdx != 1 {
		t.Errorf("playlistIdx = %d, want 1", a.playbackState.playlistIdx)
	}
	if a.playbackState.playlistAlbum != filepath.Base(root) {
		t.Errorf("playlistAlbum = %q, want %q", a.playbackState.playlistAlbum, filepath.Base(root))
	}
}

func TestPlayFileAdvanceReturnsNext(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, "a.mp3"))
	createFile(t, filepath.Join(root, "b.ogg"))
	createFile(t, filepath.Join(root, "c.flac"))

	a := &App{}
	a.playbackState.pl = &player.Player{}   // non-nil so advance() passes guard
	a.playbackState.lib = &player.Library{} // non-nil so advance() passes guard
	a.playFile(filepath.Join(root, "b.ogg"))

	settings := config.PlaybackSettings{ShuffleMode: config.ShuffleOff}
	ref, ok := a.playbackState.advance(settings)
	if !ok {
		t.Fatal("advance returned false, want next track")
	}
	if ref.path != filepath.Join(root, "c.flac") {
		t.Errorf("advance path = %q, want c.flac", ref.path)
	}
	if ref.album != filepath.Base(root) {
		t.Errorf("advance album = %q, want %q", ref.album, filepath.Base(root))
	}
}

func TestPlayFileEmptyParentPreservesPlaylist(t *testing.T) {
	root := t.TempDir()
	// root is empty — no audio files.

	a := &App{}
	a.playbackState.playlist = []string{"old.mp3"}

	a.playFile(filepath.Join(root, "nope.mp3"))

	// walkAudioFiles returns empty → early return, playlist unchanged.
	if len(a.playbackState.playlist) != 1 || a.playbackState.playlist[0] != "old.mp3" {
		t.Fatalf("playlist changed unexpectedly: %v", a.playbackState.playlist)
	}
}

func TestCanRestorePosition(t *testing.T) {
	local := filepath.Join(t.TempDir(), "track.ogg")
	createFile(t, local)

	tests := []struct {
		name        string
		position    config.PlaybackPosition
		allowRemote bool
		want        bool
	}{
		{"empty", config.PlaybackPosition{}, false, false},
		{"missing local", config.PlaybackPosition{Path: filepath.Join(t.TempDir(), "missing.mp3")}, false, false},
		{"local file", config.PlaybackPosition{Path: local}, false, true},
		{"offline modland", config.PlaybackPosition{Path: "modland:MODS/test.mod"}, false, false},
		{"online modland", config.PlaybackPosition{Path: "modland:MODS/test.mod"}, true, true},
		{"offline modarchive", config.PlaybackPosition{Path: "modarchive:https://example.test/song.zip"}, false, false},
		{"online modarchive", config.PlaybackPosition{Path: "modarchive:https://example.test/song.zip"}, true, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := canRestorePosition(test.position, test.allowRemote); got != test.want {
				t.Fatalf("canRestorePosition(%+v, %t) = %t, want %t", test.position, test.allowRemote, got, test.want)
			}
		})
	}
}

func TestCanRestoreRadioPosition(t *testing.T) {
	client := radio.New(t.TempDir())
	t.Cleanup(client.Close)
	station := radio.Station{
		StationUUID: "saved",
		Name:        "Saved Radio",
		URL:         "https://example.test/stream",
	}
	client.AddStations([]radio.Station{station})

	a := App{radio: client}
	if !a.canRestorePosition(config.PlaybackPosition{Path: station.Path()}, false) {
		t.Fatal("radio station with cached metadata should be restorable")
	}
	if (&App{}).canRestorePosition(config.PlaybackPosition{Path: station.Path()}, false) {
		t.Fatal("radio station without metadata should not be restorable")
	}
}

func TestSelectPresetDefersLoad(t *testing.T) {
	a := &App{}

	a.selectPreset("Fractal/selected.milk")

	if a.selectedPreset != "Fractal/selected.milk" {
		t.Fatalf("selectedPreset = %q, want deferred preset", a.selectedPreset)
	}
	if a.presetIdx != 0 {
		t.Fatalf("presetIdx = %d, changed before leaving Presets page", a.presetIdx)
	}
}

func TestNextAvailablePresetIndexSkipsHeavyAndWraps(t *testing.T) {
	names := []string{"a.milk", "b.milk", "c.milk"}
	heavy := map[string]bool{"a.milk": true, "c.milk": true}
	index, ok := nextAvailablePresetIndex(names, 2, func(name string) bool { return heavy[name] })
	if !ok || index != 1 {
		t.Fatalf("next available = (%d, %t), want (1, true)", index, ok)
	}
	if _, ok := nextAvailablePresetIndex(names, 0, func(string) bool { return true }); ok {
		t.Fatal("returned a preset when all presets are heavy")
	}
}

func TestRefreshPresetTreeExcludesHeavyPresets(t *testing.T) {
	activeKey := presetProfileKey{name: "slow.milk", width: 1280, height: 720}
	a := App{
		presetNames: []string{"fast.milk", "slow.milk", "!transition.milk"},
		presetTuning: presetTuning{
			active: activeKey,
			profiles: map[presetProfileKey]presetProfile{
				activeKey: {heavy: true},
			},
		},
	}
	a.refreshPresetTree()
	want := []string{"fast.milk", "!transition.milk"}
	if !slices.Equal(a.presetCats, want) {
		t.Fatalf("preset tree = %v, want %v", a.presetCats, want)
	}
}

func TestEnterPresetsPageStopsPresetTimer(t *testing.T) {
	timer := time.NewTicker(time.Hour)
	a := &App{
		settings:       &config.Settings{},
		presetTicker:   timer,
		selectedPreset: "stale.milk",
	}
	a.presetSwitch.Store(true)

	a.enterPresetsPage()

	if !a.onPresetsPage {
		t.Fatal("onPresetsPage = false, want true")
	}
	if a.presetTicker != nil {
		t.Fatal("preset timer was not stopped")
	}
	if a.presetSwitch.Load() {
		t.Fatal("pending automatic preset switch was not cleared")
	}
	if a.selectedPreset != "" {
		t.Fatalf("selectedPreset = %q, want cleared on page entry", a.selectedPreset)
	}
}

func createFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}
