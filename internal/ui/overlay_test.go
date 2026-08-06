package ui

import (
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dendec/pmv/internal/modarchive"
	"github.com/dendec/pmv/internal/player"
)

func TestDisplayTrackPath(t *testing.T) {
	o := &Overlay{baseDir: "/opt/pmv"}

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "local file relative to application",
			path: filepath.Join("/opt/pmv", "music", "2010", "IT", "1_9.it"),
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

func TestSelectModArchiveAlbumKeepsParentEntries(t *testing.T) {
	targetURL := "http://modarchive.textfiles.com/2014/IT/J/"
	o := &Overlay{
		rootEntries: []navEntry{
			{label: "2014", kind: entryModArchiveDir, url: targetURL},
			{label: "other", kind: entryModArchiveDir, url: "other/"},
		},
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
	if o.focusPanel != 1 {
		t.Fatalf("focusPanel = %d, want tracks panel", o.focusPanel)
	}
	if len(o.albumEntries) != 2 || o.albumEntries[1].label != "other" {
		t.Fatalf("parent entries were not preserved: %#v", o.albumEntries)
	}
	if !o.albumEntries[0].IsLeafAlbum() {
		t.Fatalf("selected entry was not resolved to an album: %#v", o.albumEntries[0])
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

// --- Overlay.Close cancels goroutines ---

func TestOverlayCloseCancelsWorker(t *testing.T) {
	o := &Overlay{
		modArchivePending: make(map[string]bool),
		modArchiveResults: make(chan modArchiveResult, 8),
		closeCh:           make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		r := modArchiveResult{targetURL: "x", err: errors.New("timeout")}
		select {
		case o.modArchiveResults <- r:
		case <-o.closeCh:
		}
		close(done)
	}()

	o.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("goroutine did not exit after Close()")
	}
}

// --- Retry after failed ModArchive fetch ---

func TestModArchiveRetryAfterError(t *testing.T) {
	url := "http://modarchive.textfiles.com/2014/IT/"
	o := &Overlay{
		modArchiveItems:   make(map[string][]modarchive.DirItem),
		modArchivePending: make(map[string]bool),
		modArchiveResults: make(chan modArchiveResult, 8),
		closeCh:           make(chan struct{}),
	}
	defer o.Close()

	// Simulate a failed fetch result.
	o.modArchiveResults <- modArchiveResult{targetURL: url, err: errors.New("network error")}
	o.applyModArchiveResults()

	// Error should not be cached — items should be absent.
	if _, ok := o.modArchiveItems[url]; ok {
		t.Fatal("error result should not be cached in modArchiveItems")
	}
	if o.modArchivePending[url] {
		t.Fatal("pending flag should be cleared after result")
	}

	// Re-enter: should trigger a new async request.
	entries := o.buildModArchiveEntries(url)
	if entries != nil {
		t.Fatalf("expected nil entries before fetch completes, got %d", len(entries))
	}
	if !o.modArchivePending[url] {
		t.Fatal("retry should have set modArchivePending")
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
		baseDir:           dir,
		libMode:           libModeNC,
		ncPath:            dir,
		modArchiveItems:   make(map[string][]modarchive.DirItem),
		modArchivePending: make(map[string]bool),
		modArchiveResults: make(chan modArchiveResult, 8),
		closeCh:           make(chan struct{}),
	}
	o.albumEntries = o.buildNCRootEntries()
	o.albums = labelsOf(o.albumEntries)
	return o, dir
}

func TestNCRootEntries(t *testing.T) {
	o, _ := ncTestOverlay(t)
	defer o.Close()

	if len(o.albumEntries) != 2 {
		t.Fatalf("expected 2 entries (sub1/, track3.s3m), got %d: %v", len(o.albumEntries), o.albums)
	}
	if o.albumEntries[0].label != "sub1/" {
		t.Fatalf("expected sub1/, got %q", o.albumEntries[0].label)
	}
	if o.albumEntries[0].kind != entryNCDir {
		t.Fatalf("expected entryNCDir, got %d", o.albumEntries[0].kind)
	}
	if o.albumEntries[1].label != "track3.s3m" {
		t.Fatalf("expected track3.s3m, got %q", o.albumEntries[1].label)
	}
	if o.albumEntries[1].kind != entryNCFile {
		t.Fatalf("expected entryNCFile, got %d", o.albumEntries[1].kind)
	}
}

func TestNCEnterDirAndBack(t *testing.T) {
	o, dir := ncTestOverlay(t)
	defer o.Close()

	// Enter sub1.
	sub1 := filepath.Join(dir, "sub1")
	o.ncEnterDir(sub1)
	if o.ncPath != sub1 {
		t.Fatalf("ncPath = %q, want %q", o.ncPath, sub1)
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
	if len(o.ncStack) != 1 {
		t.Fatalf("ncStack depth = %d, want 1", len(o.ncStack))
	}

	// Enter sub2.
	sub2 := filepath.Join(sub1, "sub2")
	o.ncEnterDir(sub2)
	if len(o.albumEntries) != 2 { // .., track.it
		t.Fatalf("expected 2 entries in sub2, got %d", len(o.albumEntries))
	}

	// Back to sub1.
	if !o.ncBack() {
		t.Fatal("ncBack should return true")
	}
	if o.ncPath != sub1 {
		t.Fatalf("after back: ncPath = %q, want %q", o.ncPath, sub1)
	}
	if len(o.ncStack) != 1 {
		t.Fatalf("ncStack depth = %d, want 1", len(o.ncStack))
	}

	// Back to root.
	if !o.ncBack() {
		t.Fatal("ncBack should return true")
	}
	if o.ncPath != dir {
		t.Fatalf("after back: ncPath = %q, want %q", o.ncPath, dir)
	}
	if len(o.ncStack) != 0 {
		t.Fatalf("ncStack depth = %d, want 0", len(o.ncStack))
	}

	// Back at root = no-op.
	if o.ncBack() {
		t.Fatal("ncBack at root should return false")
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

func TestNCProviderRoundTripPreservesState(t *testing.T) {
	o, dir := ncTestOverlay(t)
	defer o.Close()
	o.online = true
	o.panelEntered = true

	// At NC root, set some scroll position.
	o.albumCursor = 0
	o.albumsScroll = 3

	// Switch to provider (simulates selecting Modland).
	o.ncSwitchToProvider()
	if o.libMode != libModeProvider {
		t.Fatal("should be in provider mode")
	}

	// Return to NC.
	o.ncSwitchToNC()
	if o.libMode != libModeNC {
		t.Fatal("should be back in NC mode")
	}
	if o.ncPath != dir {
		t.Fatalf("ncPath = %q, want %q", o.ncPath, dir)
	}
	if o.albumsScroll != 3 {
		t.Fatalf("scroll not restored: albumsScroll=%d, want 3", o.albumsScroll)
	}

	// Navigate into sub1, then back to root, switch to provider, return.
	sub1 := filepath.Join(dir, "sub1")
	o.ncEnterDir(sub1)
	o.albumCursor = 1
	o.ncBack()
	o.albumsScroll = 1
	o.ncSwitchToProvider()
	o.ncSwitchToNC()
	if o.albumsScroll != 1 {
		t.Fatalf("scroll not restored after round-trip: albumsScroll=%d, want 1", o.albumsScroll)
	}
	if len(o.ncStack) != 0 {
		t.Fatalf("ncStack should be empty at root, got depth %d", len(o.ncStack))
	}
}
