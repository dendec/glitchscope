package ui

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
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

func TestSelectModArchiveDirectoryShowsFiles(t *testing.T) {
	targetURL := "http://modarchive.textfiles.com/2014/IT/J/"
	entries := []navEntry{
		{label: "2014", kind: entryModArchiveDir, url: targetURL},
		{label: "other", kind: entryModArchiveDir, url: "other/"},
	}
	o := &Overlay{
		navStack: []navLevel{{ctx: ctxCatalog, entries: entries}},
		albumEntries: []navEntry{
			{label: "2014", kind: entryModArchiveDir, url: targetURL},
			{label: "other", kind: entryModArchiveDir, url: "other/"},
		},
		albums:      []string{"2014", "other"},
		albumCursor: 0,
		focusPanel:  0,
		allAlbums:   []player.Album{},
		modArchiveItems: map[string][]modarchive.DirItem{
			targetURL: {{
				Name:      "j-61m_-_kilobyte_chillout.it.zip",
				URL:       targetURL + "j-61m_-_kilobyte_chillout.it.zip",
				Kind:      modarchive.KindFile,
				CleanName: "j-61m_-_kilobyte_chillout.it",
			}},
		},
	}

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
		allAlbums:   []player.Album{},
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

func TestSelectCatalogAlbumEntersTrackLevel(t *testing.T) {
	entries := []navEntry{{label: "song", kind: entryModlandAlbum, albumIdx: 0}}
	o := &Overlay{
		navStack:     []navLevel{{ctx: ctxCatalog, entries: entries}},
		albumEntries: entries,
		allAlbums: []player.Album{{
			Name:   "song",
			Path:   player.ModlandPrefix + "Protracker/song",
			Tracks: []string{player.ModlandPrefix + "Protracker/song/song.mod"},
		}},
		albumCursor:  0,
		focusPanel:   0,
		panelEntered: true,
	}

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
	name, path := o.SelectedCatalogTrack()
	if name != "song" || path != player.ModlandPrefix+"Protracker/song/song.mod" {
		t.Fatalf("selected catalog track = (%q, %q), want song and track path", name, path)
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
	defer o.Close()

	// First access resolves to a leaf album immediately — no async, no retry.
	entries := o.buildModArchiveEntries(url)
	if len(entries) != 1 || entries[0].kind != entryCatalogTrack {
		t.Fatalf("expected one catalog track, got %#v", entries)
	}
	if len(o.allAlbums) != 1 {
		t.Fatalf("expected album appended to allAlbums, got %d", len(o.allAlbums))
	}

	// Missing listings resolve to nil — empty catalog, honest entry.
	if o.buildModArchiveEntries("http://example.invalid/") != nil {
		t.Fatal("uncached listing should resolve to nil")
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

	// Left panel → Right = Play.
	o.FocusRight()
	if o.focusPanel != 1 || o.ncRight != ncRightPlay {
		t.Fatalf("after Right: focusPanel=%d ncRight=%d, want 1/%d", o.focusPanel, o.ncRight, ncRightPlay)
	}

	// Play → Right = Delete.
	o.FocusRight()
	if o.focusPanel != 1 || o.ncRight != ncRightDelete {
		t.Fatalf("after 2nd Right: focusPanel=%d ncRight=%d, want 1/%d", o.focusPanel, o.ncRight, ncRightDelete)
	}

	// Delete → Right = no change (rightmost).
	o.FocusRight()
	if o.focusPanel != 1 || o.ncRight != ncRightDelete {
		t.Fatalf("after 3rd Right: should stay at Delete, got focusPanel=%d ncRight=%d", o.focusPanel, o.ncRight)
	}

	// Delete → Left = Play.
	o.FocusLeft()
	if o.focusPanel != 1 || o.ncRight != ncRightPlay {
		t.Fatalf("after Left: focusPanel=%d ncRight=%d, want 1/%d", o.focusPanel, o.ncRight, ncRightPlay)
	}

	// Play → Left = left panel.
	o.FocusLeft()
	if o.focusPanel != 0 {
		t.Fatalf("after 2nd Left: focusPanel=%d, want 0", o.focusPanel)
	}

	// Left → Left = no change.
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
