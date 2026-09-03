package ui

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dendec/glitchscope/internal/filesystem"
	"github.com/dendec/glitchscope/internal/modarchive"
	"github.com/dendec/glitchscope/internal/player"
)

func TestDisplayTrackPath(t *testing.T) {
	o := &Overlay{baseDir: "/opt/glitchscope"}

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "local file relative to application",
			path: filepath.Join("/opt/glitchscope", "music", "2010", "IT", "1_9.it"),
			want: "music/2010/IT/1_9.it",
		},
		{
			name: "modarchive URL",
			path: "modarchive:http://modarchive.textfiles.com/modarchive_2011_additions/M03/B/bibix-life.mo3.zip",
			want: "modarchive/2011/M03/B/bibix-life.mo3",
		},
		{
			name: "modland URL",
			path: "modland:Protracker/Curt Cool/song.mod",
			want: "modland/Protracker/Curt Cool/song.mod",
		},
		{
			name: "external local file",
			path: "/tmp/track.it",
			want: "/tmp/track.it",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := o.displayTrackPath(test.path); got != test.want {
				t.Fatalf("displayTrackPath(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}

func TestScrollPosition(t *testing.T) {
	tests := []struct {
		name                    string
		current, delta, content int
		viewport                int
		want                    int
		wantChanged             bool
	}{
		{name: "moves within content", current: 10, delta: 3, content: 30, viewport: 10, want: 13, wantChanged: true},
		{name: "clamps at start", current: 2, delta: -5, content: 30, viewport: 10, want: 0, wantChanged: true},
		{name: "clamps at end", current: 20, delta: 5, content: 30, viewport: 10, want: 20, wantChanged: false},
		{name: "content fits", current: 0, delta: 5, content: 5, viewport: 10, want: 0, wantChanged: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, changed := scrollPosition(test.current, test.delta, test.content, test.viewport)
			if got != test.want || changed != test.wantChanged {
				t.Fatalf("scrollPosition(%d, %d, %d, %d) = (%d, %t), want (%d, %t)", test.current, test.delta, test.content, test.viewport, got, changed, test.want, test.wantChanged)
			}
		})
	}
}

func TestCatalogInfoFocusLeftScrollsBeforeChangingPanel(t *testing.T) {
	o := &Overlay{
		panelEntered: true,
		focusPanel:   1,
		navStack: []navLevel{{entries: []navEntry{{
			kind: entryCatalogTrack,
		}}}},
		albumEntries: []navEntry{{kind: entryCatalogTrack}},
		albumCursor:  0,
		infoMarquee:  marqueeState{tex: 1, texW: 240, maxPx: 160, offset: 40},
	}

	o.FocusLeft()

	if o.focusPanel != 1 {
		t.Fatalf("focusPanel = %d, want right panel while scrolling", o.focusPanel)
	}
	if o.infoMarquee.offset != 0 {
		t.Fatalf("info marquee offset = %v, want 0", o.infoMarquee.offset)
	}
}

func TestNCInfoFocusMovesToActionsAtScrollEnd(t *testing.T) {
	o := &Overlay{
		panelEntered:  true,
		focusPanel:    1,
		ncRight:       ncRightInfo,
		ncInfoLines:   10,
		ncInfoVisible: 5,
		ncInfoScroll:  5,
		navStack:      []navLevel{{ctx: ctxNC}},
	}

	o.moveCursor(1)
	if o.ncRight != ncRightPlay {
		t.Fatalf("right focus after scrolling to end = %d, want Play", o.ncRight)
	}

	o.moveCursor(1)
	if o.ncRight != ncRightDelete {
		t.Fatalf("right focus after Play = %d, want Delete", o.ncRight)
	}

	o.moveCursor(-1)
	if o.ncRight != ncRightPlay {
		t.Fatalf("right focus after Delete = %d, want Play", o.ncRight)
	}

	o.moveCursor(-1)
	if o.ncRight != ncRightInfo {
		t.Fatalf("right focus after Play = %d, want Info", o.ncRight)
	}
}

func TestTrackTitleKeepsExtension(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/music/song.it", want: "song.it"},
		{path: player.ModlandPrefix + "Protracker/song.mod", want: "song.mod"},
		{path: player.ModArchivePrefix + "http://modarchive.textfiles.com/song.mo3.zip", want: "song.mo3"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			if got := player.TrackTitle(test.path); got != test.want {
				t.Fatalf("TrackTitle(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}

func TestTrackInfoLinesFiltersAndExpandsExtraTags(t *testing.T) {
	lines := trackInfoLines("song.m4a", &player.TrackInfo{
		Extra: map[string]string{
			"language":     " und ",
			"handler_name": "AudioHandler",
			"lyrics":       "[Intro]\r\nFirst line\nSecond line",
			"comment":      "Comment one\r\nComment two",
		},
	})

	joined := strings.Join(lines, "\n")
	for _, hidden := range []string{"Language:", "Handler_name:", "AudioHandler"} {
		if strings.Contains(joined, hidden) {
			t.Fatalf("metadata contains hidden field %q: %q", hidden, joined)
		}
	}
	for _, visible := range []string{"Lyrics:", "    [Intro]", "    First line", "    Second line", "Comment:", "    Comment one", "    Comment two"} {
		if !strings.Contains(joined, visible) {
			t.Fatalf("metadata is missing %q: %q", visible, joined)
		}
	}
}

func TestSelectModArchiveDirectoryShowsFiles(t *testing.T) {
	targetURL := "http://modarchive.textfiles.com/2014/IT/J/"
	entries := []navEntry{
		{label: "2014", kind: entryModArchiveDir, url: targetURL},
		{label: "other", kind: entryModArchiveDir, url: "other/"},
	}
	var all []player.Album
	o := &Overlay{
		navStack: []navLevel{{ctx: ctxCatalog, entries: entries}},
		albumEntries: []navEntry{
			{label: "2014", kind: entryModArchiveDir, url: targetURL},
			{label: "other", kind: entryModArchiveDir, url: "other/"},
		},
		albums:      []string{"2014", "other"},
		albumCursor: 0,
		focusPanel:  0,
		modArchiveItems: map[string][]modarchive.DirItem{
			targetURL: {{
				Name:      "j-61m_-_kilobyte_chillout.it.zip",
				URL:       targetURL + "j-61m_-_kilobyte_chillout.it.zip",
				Kind:      modarchive.KindFile,
				CleanName: "j-61m_-_kilobyte_chillout.it",
			}},
		},
	}
	o.SetLibAlbums(func() []player.Album { return all })
	o.SetAddCatalogAlbum(func(album player.Album) int {
		all = append(all, album)
		return len(all) - 1
	})

	if selected := o.Select(); selected {
		t.Fatal("directory selection unexpectedly started playback")
	}
	if len(o.navStack) != 2 {
		t.Fatalf("navStack depth = %d, want 2", len(o.navStack))
	}
	if o.focusPanel != 0 {
		t.Fatalf("focusPanel = %d, want left panel", o.focusPanel)
	}
	if len(o.albumEntries) != 2 || o.albumEntries[1].kind != entryCatalogTrack {
		t.Fatalf("directory files were not entered as a new level: %#v", o.albumEntries)
	}
}

func TestSelectModArchiveEmptyDirectoryEntersLevel(t *testing.T) {
	targetURL := "http://modarchive.textfiles.com/2014/IT/B/"
	o := &Overlay{
		navStack:     []navLevel{{ctx: ctxCatalog, entries: []navEntry{{label: "B/", kind: entryModArchiveDir, url: targetURL}}}},
		albumEntries: []navEntry{{label: "B/", kind: entryModArchiveDir, url: targetURL}},
		albums:       []string{"B/"},
		modArchiveItems: map[string][]modarchive.DirItem{
			targetURL: {},
		},
		albumCursor: 0,
		focusPanel:  0,
	}

	if o.Select() {
		t.Fatal("empty directory selection unexpectedly started playback")
	}
	if len(o.navStack) != 2 {
		t.Fatalf("navStack depth = %d, want 2", len(o.navStack))
	}
	if len(o.albumEntries) != 1 || o.albumEntries[0].kind != entryParent {
		t.Fatalf("empty directory level = %#v, want parent entry", o.albumEntries)
	}
}

func TestNavigateToModArchiveZipTrack(t *testing.T) {
	targetURL := "http://modarchive.textfiles.com/2014/IT/J/"
	trackURL := targetURL + "song.mod.zip"
	albums := []player.Album{{
		Name:   "ModArchive: 2014/IT/J",
		Path:   player.ModArchivePrefix + targetURL,
		Tracks: []string{player.ModArchivePrefix + trackURL},
	}}
	o := &Overlay{
		modArchiveItems: map[string][]modarchive.DirItem{
			modarchive.BaseURL: {{Name: "2014", URL: targetURL, Kind: modarchive.KindDir}},
			targetURL:          {{Name: "song.mod.zip", URL: trackURL, Kind: modarchive.KindFile}},
		},
		libAlbums: func() []player.Album { return albums },
	}
	o.refreshAlbumsCache()

	o.NavigateToTrack(player.ModArchivePrefix + trackURL)

	if len(o.navStack) < 2 || !o.isCatalog() {
		t.Fatalf("navigation stack = %#v, want ModArchive catalog", o.navStack)
	}
	if o.focusPanel != 0 {
		t.Fatalf("focus panel = %d, want left panel (cursor on track entry)", o.focusPanel)
	}
	selected := o.currentEntry()
	if selected == nil || !selected.IsCatalogTrack() || selected.trackIdx != 0 {
		t.Fatalf("selected entry = %#v, want ZIP-backed catalog track", selected)
	}
}

func TestNavigateToRestoredModArchiveTrackCreatesAlbum(t *testing.T) {
	targetURL := "http://modarchive.textfiles.com/modarchive_2014_additions/MOD/A/"
	trackURL := targetURL + "aceman_-_wonka_honk.mod.zip"
	var albums []player.Album
	o := &Overlay{
		modArchiveItems: map[string][]modarchive.DirItem{
			modarchive.BaseURL: {{Name: "2014", URL: targetURL, Kind: modarchive.KindDir}},
			targetURL: {{
				Name:      "aceman_-_wonka_honk.mod.zip",
				URL:       trackURL,
				Kind:      modarchive.KindFile,
				CleanName: "aceman_-_wonka_honk.mod",
			}},
		},
	}
	o.SetLibAlbums(func() []player.Album { return albums })
	o.SetAddCatalogAlbum(func(album player.Album) int {
		albums = append(albums, album)
		return len(albums) - 1
	})

	o.NavigateToTrack(player.ModArchivePrefix + trackURL)

	if len(albums) != 1 {
		t.Fatalf("catalog albums = %d, want restored track album to be created", len(albums))
	}
	selected := o.currentEntry()
	if selected == nil || !selected.IsCatalogTrack() || selected.trackIdx != 0 {
		t.Fatalf("selected entry = %#v, want restored ModArchive track", selected)
	}
	if name, selectedPath, _, _ := o.SelectedCatalogInfo(); name == "" || selectedPath != player.ModArchivePrefix+trackURL {
		t.Fatalf("selected catalog track = (%q, %q), want %q", name, selectedPath, player.ModArchivePrefix+trackURL)
	}
}

func TestNavigateToModlandTrack(t *testing.T) {
	albumPath := player.ModlandPrefix + "Protracker/Curt Cool"
	playingPath := albumPath + "/second.mod"
	albums := []player.Album{{
		Name: "Modland: Protracker/Curt Cool",
		Path: albumPath,
		Tracks: []string{
			albumPath + "/first.mod",
			playingPath,
		},
	}}
	o := &Overlay{libAlbums: func() []player.Album { return albums }}
	o.refreshAlbumsCache()

	o.NavigateToTrack(playingPath)

	if len(o.navStack) != 4 || !o.isCatalog() {
		t.Fatalf("navigation stack = %#v, want Modland track level", o.navStack)
	}
	if o.source != sourceModland {
		t.Fatalf("source = %v, want Modland", o.source)
	}
	selected := o.currentEntry()
	if selected == nil || !selected.IsCatalogTrack() || selected.trackIdx != 1 {
		t.Fatalf("selected entry = %#v, want second Modland track", selected)
	}
}

func TestSelectCatalogAlbumEntersTrackLevel(t *testing.T) {
	entries := []navEntry{{label: "song", kind: entryModlandAlbum, albumIdx: 0}}
	albums := []player.Album{{
		Name:   "song",
		Path:   player.ModlandPrefix + "Protracker/song",
		Tracks: []string{player.ModlandPrefix + "Protracker/song/song.mod"},
	}}
	o := &Overlay{
		navStack:     []navLevel{{ctx: ctxCatalog, entries: entries}},
		albumEntries: entries,
		libAlbums:    func() []player.Album { return albums },
		albumCursor:  0,
		focusPanel:   0,
		panelEntered: true,
	}
	o.refreshAlbumsCache()

	if o.Select() {
		t.Fatal("catalog album selection unexpectedly started playback")
	}
	if o.focusPanel != 0 {
		t.Fatalf("focusPanel = %d, want left panel", o.focusPanel)
	}
	if len(o.navStack) != 2 || len(o.albumEntries) != 2 {
		t.Fatalf("catalog track level = stack:%d entries:%d, want stack:2 entries:2", len(o.navStack), len(o.albumEntries))
	}
	if o.albumEntries[0].kind != entryParent || !o.albumEntries[1].IsCatalogTrack() {
		t.Fatalf("catalog track entries = %#v, want parent and catalog track", o.albumEntries)
	}
	o.CursorDown()
	name, path, tracks, idx := o.SelectedCatalogInfo()
	if name != "song" || path != player.ModlandPrefix+"Protracker/song/song.mod" {
		t.Fatalf("selected catalog track = (%q, %q), want song and track path", name, path)
	}
	if idx != 0 || len(tracks) != 1 {
		t.Fatalf("selected catalog info = (tracks:%d, idx:%d), want 1 track at idx 0", len(tracks), idx)
	}
}

func TestMicrophoneDeviceSelection(t *testing.T) {
	o := &Overlay{
		navStack:     []navLevel{{ctx: ctxSourceRoot}},
		focusPanel:   0,
		panelEntered: true,
	}
	o.ShowMicrophoneDevices([]string{"USB microphone", "Webcam microphone"})

	if o.topLevel().ctx != ctxMicrophone {
		t.Fatalf("top context = %v, want microphone", o.topLevel().ctx)
	}
	if len(o.albumEntries) != 3 || o.albumEntries[0].kind != entryParent {
		t.Fatalf("device entries = %#v, want parent and two devices", o.albumEntries)
	}
	if o.albumEntries[1].device != "USB microphone" || o.albumEntries[2].device != "Webcam microphone" {
		t.Fatalf("device entries = %#v, want SDL device names", o.albumEntries)
	}

	o.CursorDown()
	if o.Select() {
		t.Fatal("microphone device selection unexpectedly started playback")
	}
	device, ok := o.ConsumeMicDeviceSelection()
	if !ok || device != "USB microphone" {
		t.Fatalf("selected device = (%q, %t), want (USB microphone, true)", device, ok)
	}
}

func TestMicrophoneDeviceRefreshKeepsSourceCursor(t *testing.T) {
	o := &Overlay{navStack: []navLevel{{ctx: ctxSourceRoot}}, panelEntered: true}
	o.SetMicDevices([]string{"USB microphone", "Webcam microphone"})
	o.albumCursor = 1
	o.ShowMicrophoneDevices([]string{"USB microphone", "Webcam microphone"})
	o.albumCursor = 2 // select the second device after the parent entry
	o.navStack[len(o.navStack)-1].cursor = 2

	o.ShowMicrophoneDevices([]string{"USB microphone", "Webcam microphone"})
	if o.navStack[0].cursor != 1 {
		t.Fatalf("source cursor after device refresh = %d, want 1", o.navStack[0].cursor)
	}
	if o.albumCursor != 0 {
		t.Fatalf("device cursor after refresh = %d, want 0", o.albumCursor)
	}
	o.Back()
	if o.albumCursor != 1 || o.currentEntry().source != sourceMicrophone {
		t.Fatalf("cursor after leaving microphone menu = %d (%#v), want source microphone at 1", o.albumCursor, o.currentEntry())
	}
}

func TestMicrophoneSourceHiddenWithoutDevices(t *testing.T) {
	o := &Overlay{navStack: []navLevel{{ctx: ctxSourceRoot}}}
	o.SetMicDevices(nil)

	if len(o.albumEntries) != 1 || o.albumEntries[0].source != sourceMusic {
		t.Fatalf("source entries without devices = %#v, want music only", o.albumEntries)
	}

	o.micActive = true
	o.SetMicDevices(nil)
	if len(o.albumEntries) != 2 || o.albumEntries[1].source != sourceMicrophone {
		t.Fatalf("source entries during capture = %#v, want music and microphone", o.albumEntries)
	}
}

func TestMicrophoneSourceHiddenWhenCaptureStopsAfterDeviceRemoval(t *testing.T) {
	o := &Overlay{navStack: []navLevel{{ctx: ctxSourceRoot}}}
	o.SetMicDevices([]string{"USB microphone"})
	o.SetMicActive(true)
	o.SetMicDevices(nil)
	o.SetMicActive(false)

	if len(o.albumEntries) != 1 || o.albumEntries[0].source != sourceMusic {
		t.Fatalf("source entries after capture stops = %#v, want music only", o.albumEntries)
	}
}

// --- scrollOffset ---

func TestScrollOffset_noScrollNeeded(t *testing.T) {
	got := scrollOffset(0, 2, 5, 10)
	if got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

func TestScrollOffset_cursorBelowWindow(t *testing.T) {
	// cursor=8, visible rows 5..9 → cursor >= current+maxRows (8 >= 5+5=10? no) → stays at 5
	got := scrollOffset(5, 8, 20, 5)
	if got != 5 {
		t.Fatalf("expected 5, got %d", got)
	}
}

func TestScrollOffset_cursorPastWindow(t *testing.T) {
	// cursor=10, current=5, maxRows=5: 10 >= 5+5=10 → current = 10-5+1 = 6
	got := scrollOffset(5, 10, 20, 5)
	if got != 6 {
		t.Fatalf("expected 6, got %d", got)
	}
}

func TestScrollOffset_clampToEnd(t *testing.T) {
	// cursor=18, current=0, maxRows=5: current = 18-5+1 = 14; 14 > 20-5=15? no → 14
	got := scrollOffset(0, 18, 20, 5)
	if got != 14 {
		t.Fatalf("expected 14, got %d", got)
	}
}

func TestScrollOffset_fewerItemsThanRows(t *testing.T) {
	got := scrollOffset(0, 2, 3, 10)
	if got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

// --- Shadow: uint8 correctness ---

func TestShadow_coverageAlpha(t *testing.T) {
	rgba := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 4; y < 6; y++ {
		for x := 4; x < 6; x++ {
			rgba.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}

	applyOutlineShadow(rgba, color.RGBA{255, 255, 255, 255}, 1)

	for y := 4; y < 6; y++ {
		for x := 4; x < 6; x++ {
			r, g, b, a := rgba.At(x, y).RGBA()
			if r != 0xffff || g != 0xffff || b != 0xffff || a != 0xffff {
				t.Errorf("center pixel (%d,%d): got rgba(%d,%d,%d,%d), want white", x, y, r, g, b, a)
			}
		}
	}

	for _, pt := range []image.Point{{3, 4}, {6, 4}, {4, 3}, {4, 6}} {
		_, _, _, a := rgba.At(pt.X, pt.Y).RGBA()
		if a == 0 {
			t.Errorf("expected outline at (%d,%d), got transparent", pt.X, pt.Y)
		}
	}
}

func TestShadow_zeroRadius(t *testing.T) {
	rgba := image.NewRGBA(image.Rect(0, 0, 5, 5))
	rgba.SetRGBA(2, 2, color.RGBA{255, 255, 255, 255})

	applyOutlineShadow(rgba, color.RGBA{255, 255, 255, 255}, 0)

	_, _, _, a := rgba.At(1, 2).RGBA()
	if a != 0 {
		t.Fatalf("expected no outline with radius=0, got alpha=%d", a)
	}
}

// --- ModArchive listings resolve synchronously from the local cache ---

func TestModArchiveSyncFromCache(t *testing.T) {
	url := "http://modarchive.textfiles.com/2014/IT/"
	var all []player.Album
	o := &Overlay{
		baseDir: t.TempDir(),
		modArchiveItems: map[string][]modarchive.DirItem{
			url: {{
				Name:      "j-61m_-_kilobyte_chillout.it.zip",
				URL:       url + "j-61m_-_kilobyte_chillout.it.zip",
				Kind:      modarchive.KindFile,
				CleanName: "j-61m_-_kilobyte_chillout.it",
			}},
		},
	}
	o.SetLibAlbums(func() []player.Album { return all })
	o.SetAddCatalogAlbum(func(album player.Album) int {
		all = append(all, album)
		return len(all) - 1
	})
	defer o.Close()

	// First access resolves to a leaf album immediately — no async, no retry.
	entries := o.buildModArchiveEntries(url)
	if len(entries) != 1 || entries[0].kind != entryCatalogTrack {
		t.Fatalf("expected one catalog track, got %#v", entries)
	}
	if len(all) != 1 {
		t.Fatalf("expected album added via callback, got %d", len(all))
	}

	// Missing listings resolve to nil — empty catalog, honest entry.
	if o.buildModArchiveEntries("http://example.invalid/") != nil {
		t.Fatal("uncached listing should resolve to nil")
	}
}

func TestModArchiveSnapshotZipIsDirectory(t *testing.T) {
	archiveURL := modarchive.BaseURL + modarchive.SnapshotDir + "/A/A0.zip"
	o := &Overlay{}
	entries := o.buildModArchiveEntriesFromItems(modarchive.BaseURL+modarchive.SnapshotDir+"/A/", []modarchive.DirItem{{
		Name:      "A0.zip",
		URL:       archiveURL,
		Kind:      modarchive.KindArchive,
		CleanName: "A0",
	}})
	if len(entries) != 1 || entries[0].kind != entryModArchiveDir || entries[0].label != "A0/" || entries[0].url != archiveURL {
		t.Fatalf("snapshot archive entries = %#v, want A0 directory", entries)
	}
}

// --- NC FSM tests ---

// ncTestOverlay creates an Overlay with a real temp directory for NC testing.
func ncTestOverlay(t *testing.T) (*Overlay, string) {
	t.Helper()
	dir := t.TempDir()
	// Create structure: dir/sub1/sub2/track.it, dir/sub1/track2.mod, dir/track3.s3m
	sub1 := filepath.Join(dir, "sub1")
	sub2 := filepath.Join(sub1, "sub2")
	if err := os.MkdirAll(sub2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub2, "track.it"), []byte("IT"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub1, "track2.mod"), []byte("MOD"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "track3.s3m"), []byte("S3M"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Hidden dir should be skipped.
	if err := os.MkdirAll(filepath.Join(dir, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	o := &Overlay{
		baseDir:         dir,
		modArchiveItems: make(map[string][]modarchive.DirItem),
	}
	o.switchToNC(dir)
	return o, dir
}

func TestNCRootEntries(t *testing.T) {
	o, _ := ncTestOverlay(t)
	defer o.Close()

	if len(o.albumEntries) != 3 {
		t.Fatalf("expected parent plus 2 local entries, got %d: %v", len(o.albumEntries), o.albums)
	}
	if o.albumEntries[0].label != ".." || o.albumEntries[0].kind != entryParent {
		t.Fatalf("expected parent entry, got %#v", o.albumEntries[0])
	}
	if o.albumEntries[1].label != "sub1/" || o.albumEntries[1].kind != entryNCDir {
		t.Fatalf("expected sub1/ directory, got %#v", o.albumEntries[1])
	}
	if o.albumEntries[2].label != "track3.s3m" {
		t.Fatalf("expected track3.s3m, got %q", o.albumEntries[2].label)
	}
	if o.albumEntries[2].kind != entryNCFile {
		t.Fatalf("expected entryNCFile, got %d", o.albumEntries[2].kind)
	}
}

func TestNCMissingRootReportsFailed(t *testing.T) {
	o, dir := ncTestOverlay(t)
	defer o.Close()

	o.switchToNC(filepath.Join(dir, "missing"))
	if got := o.NCListingStatus(); got != filesystem.StatusFailed {
		t.Fatalf("NC listing status = %s, want failed", got)
	}
}

func TestNavigateToTrackSelectsLocalTrack(t *testing.T) {
	o, dir := ncTestOverlay(t)
	defer o.Close()

	trackPath := filepath.Join(dir, "sub1", "track2.mod")
	o.NavigateToTrack(trackPath)

	if !o.isNC() {
		t.Fatal("NavigateToTrack should open the local NC view")
	}
	if got := o.ncDir(); got != filepath.Dir(trackPath) {
		t.Fatalf("NC directory = %q, want %q", got, filepath.Dir(trackPath))
	}
	if o.focusPanel != 0 {
		t.Fatalf("focus panel = %d, want left panel", o.focusPanel)
	}
	if o.albumCursor < 0 || o.albumCursor >= len(o.albumEntries) {
		t.Fatalf("album cursor = %d, entries = %d", o.albumCursor, len(o.albumEntries))
	}
	selected := o.albumEntries[o.albumCursor]
	if !selected.IsNCFile() || selected.filePath != trackPath {
		t.Fatalf("selected entry = %#v, want local track %q", selected, trackPath)
	}
}

func TestNCDirectoryCounts(t *testing.T) {
	o, dir := ncTestOverlay(t)
	defer o.Close()

	files, dirs := o.ncDirectoryCounts(dir)
	if files != 1 || dirs != 1 {
		t.Fatalf("root counts = files:%d dirs:%d, want files:1 dirs:1", files, dirs)
	}

	files, dirs = o.ncDirectoryCounts(filepath.Join(dir, "sub1"))
	if files != 1 || dirs != 1 {
		t.Fatalf("sub1 counts = files:%d dirs:%d, want files:1 dirs:1", files, dirs)
	}

	files, dirs = o.ncDirectoryCounts(filepath.Join(dir, "sub1", "sub2"))
	if files != 1 || dirs != 0 {
		t.Fatalf("sub2 counts = files:%d dirs:%d, want files:1 dirs:0", files, dirs)
	}
}

func TestNCOnlineAddsProviderEntries(t *testing.T) {
	o, _ := ncTestOverlay(t)
	defer o.Close()

	o.SetOnline(true)
	if len(o.albumEntries) != 3 {
		t.Fatalf("expected parent plus local entries in NC root, got %d: %v", len(o.albumEntries), o.albums)
	}
	for _, entry := range o.albumEntries {
		if entry.kind == entrySource || entry.kind == entryParent && entry.label != ".." {
			t.Fatalf("invalid NC root entry: %#v", o.albumEntries)
		}
	}
}

func TestNCEnterDirAndBack(t *testing.T) {
	o, dir := ncTestOverlay(t)
	defer o.Close()

	// Enter sub1.
	sub1 := filepath.Join(dir, "sub1")
	o.ncEnterDir(sub1)
	if o.ncDir() != sub1 {
		t.Fatalf("ncDir = %q, want %q", o.ncDir(), sub1)
	}
	if len(o.albumEntries) != 3 { // .., sub2/, track2.mod
		t.Fatalf("expected 3 entries in sub1, got %d: %v", len(o.albumEntries), o.albums)
	}
	if o.albumEntries[0].label != ".." {
		t.Fatalf("first entry should be .., got %q", o.albumEntries[0].label)
	}
	if o.albumCursor != 0 {
		t.Fatalf("cursor should be 0 after enter, got %d", o.albumCursor)
	}
	if len(o.navStack) != 3 {
		t.Fatalf("navStack depth = %d, want 3 (source root + NC root + sub1)", len(o.navStack))
	}

	// Enter sub2.
	sub2 := filepath.Join(sub1, "sub2")
	o.ncEnterDir(sub2)
	if len(o.albumEntries) != 2 { // .., track.it
		t.Fatalf("expected 2 entries in sub2, got %d", len(o.albumEntries))
	}

	// Back to sub1.
	if !o.popLevel() {
		t.Fatal("popLevel should return true")
	}
	if o.ncDir() != sub1 {
		t.Fatalf("after back: ncDir = %q, want %q", o.ncDir(), sub1)
	}
	if len(o.navStack) != 3 {
		t.Fatalf("navStack depth = %d, want 3", len(o.navStack))
	}

	// Back to NC root.
	if !o.popLevel() {
		t.Fatal("popLevel should return true")
	}
	if o.ncDir() != dir {
		t.Fatalf("after back: ncDir = %q, want %q", o.ncDir(), dir)
	}
	if len(o.navStack) != 2 {
		t.Fatalf("navStack depth = %d, want 2 (source root + NC root)", len(o.navStack))
	}

	// Back at NC root goes to the virtual source root.
	if !o.popLevel() {
		t.Fatal("popLevel at NC root should return true")
	}
	if o.topLevel().ctx != ctxSourceRoot {
		t.Fatalf("after NC root: ctx = %v, want source root", o.topLevel().ctx)
	}

	// The virtual source root is not poppable.
	if o.popLevel() {
		t.Fatal("popLevel at source root should return false")
	}
}

func TestNCFocusMachine(t *testing.T) {
	o, _ := ncTestOverlay(t)
	defer o.Close()
	o.panelEntered = true
	o.focusPanel = 0
	o.ncRight = ncRightInfo

	// Left panel → Right = Info.
	o.FocusRight()
	if o.focusPanel != 1 || o.ncRight != ncRightInfo {
		t.Fatalf("after Right: focusPanel=%d ncRight=%d, want 1/%d", o.focusPanel, o.ncRight, ncRightInfo)
	}

	// Info → Right = Play.
	o.FocusRight()
	if o.focusPanel != 1 || o.ncRight != ncRightPlay {
		t.Fatalf("after 2nd Right: focusPanel=%d ncRight=%d, want 1/%d", o.focusPanel, o.ncRight, ncRightPlay)
	}

	// Play → Right = Delete.
	o.FocusRight()
	if o.focusPanel != 1 || o.ncRight != ncRightDelete {
		t.Fatalf("after 3rd Right: focusPanel=%d ncRight=%d, want 1/%d", o.focusPanel, o.ncRight, ncRightDelete)
	}

	// Delete → Left = Play.
	o.FocusLeft()
	if o.focusPanel != 1 || o.ncRight != ncRightPlay {
		t.Fatalf("after Left: focusPanel=%d ncRight=%d, want 1/%d", o.focusPanel, o.ncRight, ncRightPlay)
	}

	// Play → Left = Info.
	o.FocusLeft()
	if o.focusPanel != 1 || o.ncRight != ncRightInfo {
		t.Fatalf("after 2nd Left: focusPanel=%d ncRight=%d, want 1/%d", o.focusPanel, o.ncRight, ncRightInfo)
	}

	// Info → Left = left panel.
	o.FocusLeft()
	if o.focusPanel != 0 {
		t.Fatalf("after 3rd Left: should stay at 0, got %d", o.focusPanel)
	}
}

func TestNCDeleteConfirm(t *testing.T) {
	o, _ := ncTestOverlay(t)
	defer o.Close()
	o.panelEntered = true
	o.focusPanel = 1
	o.ncRight = ncRightDelete

	// First Select = activate confirm.
	if o.Select() {
		t.Fatal("Select on Delete should not return true")
	}
	if !o.ncConfirm {
		t.Fatal("ncConfirm should be true after Select on Delete")
	}

	// Backspace = cancel confirm, focus stays on Delete.
	o.Back()
	if o.ncConfirm {
		t.Fatal("Back should cancel ncConfirm")
	}
	if o.focusPanel != 1 || o.ncRight != ncRightDelete {
		t.Fatalf("focus should stay on Delete: focusPanel=%d ncRight=%d", o.focusPanel, o.ncRight)
	}

	// Re-activate confirm and consume.
	o.ncRight = ncRightDelete
	o.Select()
	if !o.NCConsumeDeleteConfirmed() {
		// Select on Delete when ncConfirm=false sets ncConfirm=true, returns false.
		// Need second Select to confirm.
		o.Select()
	}
	if !o.NCConsumeDeleteConfirmed() {
		t.Fatal("NCConsumeDeleteConfirmed should return true after confirm")
	}
	// Second call = false (one-shot).
	if o.NCConsumeDeleteConfirmed() {
		t.Fatal("NCConsumeDeleteConfirmed should return false on second call")
	}
}

func TestNCCatalogRoundTrip(t *testing.T) {
	o, _ := ncTestOverlay(t)
	defer o.Close()
	o.SetMicDevices([]string{"test microphone"})
	o.SetOnline(true)
	o.switchToSourceRoot()
	o.panelEntered = true

	if len(o.albumEntries) != 4 {
		t.Fatalf("expected four source entries, got %d: %v", len(o.albumEntries), o.albums)
	}
	modlandIdx := 2
	o.albumCursor = modlandIdx

	// Enter Modland from the virtual source root.
	if o.Select() {
		t.Fatal("catalog entry should not start playback")
	}
	if len(o.navStack) != 2 {
		t.Fatalf("navStack depth = %d, want 2 (source root + formats)", len(o.navStack))
	}
	if o.isNC() {
		t.Fatal("catalog level should not be NC")
	}
	if len(o.albumEntries) == 0 || o.albumEntries[0].kind != entryParent {
		t.Fatalf("provider root should start with parent entry: %#v", o.albumEntries)
	}

	// Selecting .. returns to the virtual source root just like Back.
	if o.Select() {
		t.Fatal("selecting parent unexpectedly started playback")
	}
	if len(o.navStack) != 1 || o.topLevel().ctx != ctxSourceRoot {
		t.Fatalf("selecting parent should return to source root, got depth=%d ctx=%v", len(o.navStack), o.topLevel().ctx)
	}

	// Back at the virtual source root closes the UI.
	o.uiVisible = true
	o.Back()
	if o.uiVisible {
		t.Fatal("Back at source root should close the UI")
	}
}

func TestNCBackFromMusicRoot(t *testing.T) {
	o, _ := ncTestOverlay(t)
	defer o.Close()
	o.panelEntered = true

	// Music root → Back → virtual source root.
	o.Back()
	if o.topLevel().ctx != ctxSourceRoot {
		t.Fatalf("Back at music root should switch to source root, got ctx=%v", o.topLevel().ctx)
	}
}

func TestBreadcrumbText(t *testing.T) {
	o := &Overlay{baseDir: "/app"}

	o.navStack = []navLevel{{ctx: ctxNC, dirPath: "/app/music"}}
	if got := o.breadcrumbText(); got != "/music" {
		t.Fatalf("NC root: got %q, want %q", got, "/music")
	}

	o.navStack = []navLevel{{ctx: ctxNC, dirPath: "/app/music"}, {ctx: ctxNC, dirPath: "/app/music/a/b"}}
	if got := o.breadcrumbText(); got != "/music/a/b" {
		t.Fatalf("NC subdir: got %q", got)
	}

	// Deep-link collapses the stack to one NC level; crumbs derive from dir.
	o.navStack = []navLevel{{ctx: ctxNC, dirPath: "/app/music/a/b"}}
	if got := o.breadcrumbText(); got != "/music/a/b" {
		t.Fatalf("NC deep-link: got %q", got)
	}

	o.navStack = []navLevel{
		{ctx: ctxNC, dirPath: "/app/music"},
		{ctx: ctxCatalog, label: "Modland"},
	}
	if got := o.breadcrumbText(); got != "/music/Modland" {
		t.Fatalf("catalog stack: got %q", got)
	}

	// Leaf album appended when its tracks panel is open.
	o.albumEntries = []navEntry{{label: "Author", kind: entryModlandAlbum, albumIdx: 0}}
	o.albumCursor = 0
	o.focusPanel = 1
	if got := o.breadcrumbText(); got != "/music/Modland/Author" {
		t.Fatalf("leaf entered: got %q", got)
	}

	// Full catalog path remains available; rendering truncates only when it
	// exceeds the available width.
	o.navStack = append(o.navStack,
		navLevel{ctx: ctxCatalog, label: "Protracker"},
		navLevel{ctx: ctxCatalog, label: "Nested"},
	)
	if got := o.breadcrumbText(); got != "/music/Modland/Protracker/Nested/Author" {
		t.Fatalf("elision: got %q", got)
	}
}

func TestModArchiveNavigationTargets(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want []modArchiveNavigationTarget
	}{
		{
			name: "snapshot archive",
			url:  modarchive.BaseURL + modarchive.SnapshotDir + "/B/B0.zip",
			want: []modArchiveNavigationTarget{
				{url: modarchive.BaseURL + modarchive.SnapshotDir + "/", label: "1987-2007"},
				{url: modarchive.BaseURL + modarchive.SnapshotDir + "/B/", label: "B"},
				{url: modarchive.BaseURL + modarchive.SnapshotDir + "/B/B0.zip", label: "B0"},
			},
		},
		{
			name: "yearly addition directory",
			url:  modarchive.BaseURL + "modarchive_2009_additions/AHX/M/",
			want: []modArchiveNavigationTarget{
				{url: modarchive.BaseURL + "modarchive_2009_additions/", label: "2009"},
				{url: modarchive.BaseURL + "modarchive_2009_additions/AHX/", label: "AHX"},
				{url: modarchive.BaseURL + "modarchive_2009_additions/AHX/M/", label: "M"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := modArchiveNavigationTargets(test.url); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("navigation targets = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestNavigateToRestoredModArchiveSnapshotTrackBreadcrumb(t *testing.T) {
	bucketURL := modarchive.BaseURL + modarchive.SnapshotDir + "/B/B0.zip"
	trackURL := bucketURL + "#boom-crash-m00.mod"
	albums := []player.Album{{
		Name:   "ModArchive: 1987-2007/B/B0.zip",
		Path:   player.ModArchivePrefix + bucketURL,
		Tracks: []string{player.ModArchivePrefix + trackURL},
	}}
	o := &Overlay{
		modArchiveItems: map[string][]modarchive.DirItem{
			modarchive.BaseURL: {{Name: modarchive.SnapshotDir, URL: modarchive.BaseURL + modarchive.SnapshotDir + "/", Kind: modarchive.KindDir}},
			modarchive.BaseURL + modarchive.SnapshotDir + "/":   {{Name: "B", URL: modarchive.BaseURL + modarchive.SnapshotDir + "/B/", Kind: modarchive.KindDir}},
			modarchive.BaseURL + modarchive.SnapshotDir + "/B/": {{Name: "B0.zip", URL: bucketURL, Kind: modarchive.KindArchive, CleanName: "B0"}},
			bucketURL: {{Name: "boom-crash-m00.mod", URL: trackURL, Kind: modarchive.KindFile}},
		},
		libAlbums: func() []player.Album { return albums },
	}
	o.refreshAlbumsCache()

	o.NavigateToTrack(player.ModArchivePrefix + trackURL)

	if got := o.breadcrumbText(); got != "/modarchive/1987-2007/B/B0" {
		t.Fatalf("restored snapshot breadcrumb = %q, want %q", got, "/modarchive/1987-2007/B/B0")
	}

	o.albumCursor = 0
	if o.Select() {
		t.Fatal("selecting restored snapshot parent unexpectedly started playback")
	}
	if got := o.breadcrumbText(); got != "/modarchive/1987-2007/B" {
		t.Fatalf("breadcrumb after parent = %q, want %q", got, "/modarchive/1987-2007/B")
	}

	o.albumCursor = 0
	o.Select()
	if got := o.breadcrumbText(); got != "/modarchive/1987-2007" {
		t.Fatalf("breadcrumb after second parent = %q, want %q", got, "/modarchive/1987-2007")
	}

	o.albumCursor = 0
	o.Select()
	if got := o.breadcrumbText(); got != "/modarchive" {
		t.Fatalf("breadcrumb after third parent = %q, want %q", got, "/modarchive")
	}
}

func TestNavigateToRestoredModArchiveAdditionTrackBreadcrumb(t *testing.T) {
	additionURL := modarchive.BaseURL + "modarchive_2009_additions/"
	formatURL := additionURL + "AHX/"
	directoryURL := formatURL + "M/"
	trackURL := directoryURL + "m0d_-_sundown.ahx.zip"
	albums := []player.Album{{
		Name:   "ModArchive: 2009/AHX/M",
		Path:   player.ModArchivePrefix + directoryURL,
		Tracks: []string{player.ModArchivePrefix + trackURL},
	}}
	o := &Overlay{
		modArchiveItems: map[string][]modarchive.DirItem{
			modarchive.BaseURL: {{Name: "modarchive_2009_additions", URL: additionURL, Kind: modarchive.KindDir}},
			additionURL:        {{Name: "AHX", URL: formatURL, Kind: modarchive.KindDir}},
			formatURL:          {{Name: "M", URL: directoryURL, Kind: modarchive.KindDir}},
			directoryURL:       {{Name: "m0d_-_sundown.ahx.zip", URL: trackURL, Kind: modarchive.KindFile}},
		},
		libAlbums: func() []player.Album { return albums },
	}
	o.refreshAlbumsCache()

	o.NavigateToTrack(player.ModArchivePrefix + trackURL)

	if got := o.breadcrumbText(); got != "/modarchive/2009/AHX/M" {
		t.Fatalf("restored addition breadcrumb = %q, want %q", got, "/modarchive/2009/AHX/M")
	}

	o.albumCursor = 0
	if o.Select() {
		t.Fatal("selecting restored addition parent unexpectedly started playback")
	}
	if got := o.breadcrumbText(); got != "/modarchive/2009/AHX" {
		t.Fatalf("breadcrumb after parent = %q, want %q", got, "/modarchive/2009/AHX")
	}
}

// --- Catalog track info panel ---

func catalogTrackInfoTestAlbums() []player.Album {
	return []player.Album{{
		Name: "ModArchive: test",
		Path: player.ModArchivePrefix + "http://example.com/test",
		Tracks: []string{
			player.ModArchivePrefix + "http://example.com/test/a.mod",
			player.ModArchivePrefix + "http://example.com/test/b.mod",
		},
	}}
}

func TestCatalogTrackInfoCachedWithEmptyMetadata(t *testing.T) {
	e := &navEntry{label: "a.mod", kind: entryCatalogTrack, albumIdx: 0, trackIdx: 0}
	lines := catalogTrackInfoLines(e, catalogTrackInfoTestAlbums(), []player.TrackInfo{
		{Path: player.ModArchivePrefix + "http://example.com/test/a.mod", Cached: true},
		{Path: player.ModArchivePrefix + "http://example.com/test/b.mod"},
	})
	// Cached track with empty metadata: panel shows title + blank line.
	if len(lines) < 2 {
		t.Fatalf("cached track with empty metadata should still show title, got %q", lines)
	}
	if lines[0] != "a.mod" {
		t.Fatalf("first line should be track title, got %q", lines[0])
	}
}

func TestCatalogTrackInfoNotCached(t *testing.T) {
	e := &navEntry{label: "a.mod", kind: entryCatalogTrack, albumIdx: 0, trackIdx: 0}
	lines := catalogTrackInfoLines(e, catalogTrackInfoTestAlbums(), []player.TrackInfo{
		{Path: player.ModArchivePrefix + "http://example.com/test/a.mod", Cached: false},
	})
	// Uncached track: no right panel.
	if lines != nil {
		t.Fatalf("uncached track must return nil, got %q", lines)
	}
}

func TestCatalogTrackInfoSelectsRightTrack(t *testing.T) {
	// Second track of the album selected; first is cached, second is not.
	e := &navEntry{label: "b.mod", kind: entryCatalogTrack, albumIdx: 0, trackIdx: 1}
	lines := catalogTrackInfoLines(e, catalogTrackInfoTestAlbums(), []player.TrackInfo{
		{Path: player.ModArchivePrefix + "http://example.com/test/a.mod", Cached: true},
		{Path: player.ModArchivePrefix + "http://example.com/test/b.mod", Cached: false},
	})
	// Second track is not cached: no right panel.
	if lines != nil {
		t.Fatalf("uncached track must return nil, got %q", lines)
	}
}

func TestCatalogTrackInfoLongComment(t *testing.T) {
	e := &navEntry{label: "a.mod", kind: entryCatalogTrack, albumIdx: 0, trackIdx: 0}
	comment := "line one\nline two\nline three\nline four"
	lines := catalogTrackInfoLines(e, catalogTrackInfoTestAlbums(), []player.TrackInfo{
		{Path: player.ModArchivePrefix + "http://example.com/test/a.mod", Cached: true, Duration: 120, Comment: comment},
	})
	// title, "", "Comment:", 4 comment lines = 7
	if len(lines) < 7 {
		t.Fatalf("comment lines missing, got %d lines: %q", len(lines), lines)
	}
	found := false
	for _, l := range lines {
		if l == "line three" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("comment line missing, got %q", lines)
	}
}

func TestCatalogTrackInfoInvalidIndexReturnsNil(t *testing.T) {
	e := &navEntry{label: "a.mod", kind: entryCatalogTrack, albumIdx: 5, trackIdx: 0}
	lines := catalogTrackInfoLines(e, catalogTrackInfoTestAlbums(), nil)
	if lines != nil {
		t.Fatalf("invalid album index must return nil, got %q", lines)
	}
}
