package ui

import (
	"reflect"
	"testing"
)

// srcRoot builds an Overlay parked on the virtual source root (stack bottom).
func srcRoot() *Overlay {
	return &Overlay{
		navStack:   []navLevel{{ctx: ctxSourceRoot}},
		focusPanel: 0,
	}
}

// nc builds an Overlay in NC browsing mode with the given right-panel focus.
func nc(right ncRightPanel) *Overlay {
	return &Overlay{
		navStack:   []navLevel{{ctx: ctxSourceRoot}, {ctx: ctxNC, dirPath: "/music"}},
		focusPanel: 1,
		ncRight:    right,
		uiPage:     PageLibrary,
	}
}

func hints(o *Overlay) []UIHint { return o.ActionHints() }

func TestControlLabelKeyboard(t *testing.T) {
	o := srcRoot() // controllerConnected defaults false
	cases := map[string]string{
		hintSelect: "Enter",
		hintBack:   "Backspace",
		hintFocus:  "Left/Right",
		hintMove:   "Up/Down",
		hintPages:  "P",
		hintPlay:   "Space",
	}
	for action, want := range cases {
		if got := o.controlLabel(action); got != want {
			t.Errorf("controlLabel(keyboard, %q) = %q, want %q", action, got, want)
		}
	}
}

func TestControlLabelGamepad(t *testing.T) {
	o := srcRoot()
	o.controllerConnected = true
	cases := map[string]string{
		hintSelect: "A", // Nintendo A = select
		hintBack:   "B", // Nintendo B = back
		hintFocus:  "D-pad",
		hintMove:   "D-pad",
		hintPages:  "L1/R1",
		hintPlay:   "X",
	}
	for action, want := range cases {
		if got := o.controlLabel(action); got != want {
			t.Errorf("controlLabel(gamepad, %q) = %q, want %q", action, got, want)
		}
	}
}

func TestActionHintsSourceRoot(t *testing.T) {
	o := srcRoot()
	o.uiPage = PageLibrary
	got := hints(o)
	want := []UIHint{
		{Key: "Enter", Label: "Open"},
		{Key: "Backspace", Label: "Close"},
		{Key: "Up/Down", Label: "Item"},
	}
	// Pages + (no play/pause) are appended as common hints.
	want = append(want, UIHint{Key: "P", Label: "Screens"})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ActionHints(source root) = %v, want %v", got, want)
	}
}

func TestActionHintsNCLeftPanel(t *testing.T) {
	o := &Overlay{
		navStack:   []navLevel{{ctx: ctxSourceRoot}, {ctx: ctxNC, dirPath: "/music/sub"}},
		focusPanel: 0,
		uiPage:     PageLibrary,
	}
	got := hints(o)
	want := []UIHint{
		{Key: "Enter", Label: "Open"},
		{Key: "Backspace", Label: "Up"},
		{Key: "Up/Down", Label: "Item"},
		{Key: "P", Label: "Screens"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ActionHints(NC left) = %v, want %v", got, want)
	}
}

func TestActionHintsNCRightPlay(t *testing.T) {
	o := nc(ncRightPlay)
	got := hints(o)
	want := []UIHint{
		{Key: "Enter", Label: "Play"},
		{Key: "Backspace", Label: "Left panel"},
		{Key: "P", Label: "Screens"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ActionHints(NC Play) = %v, want %v", got, want)
	}
}

func TestActionHintsNCRightDelete(t *testing.T) {
	o := nc(ncRightDelete)
	got := hints(o)
	want := []UIHint{
		{Key: "Enter", Label: "Delete"},
		{Key: "Backspace", Label: "Left panel"},
		{Key: "P", Label: "Screens"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ActionHints(NC Delete) = %v, want %v", got, want)
	}
}

func TestActionHintsDeleteConfirm(t *testing.T) {
	o := nc(ncRightDelete)
	o.ncConfirm = true
	got := hints(o)
	want := []UIHint{
		{Key: "Enter", Label: "Confirm"},
		{Key: "Backspace", Label: "Cancel"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ActionHints(delete confirm) = %v, want %v", got, want)
	}
}

func TestActionHintsSettingsEditing(t *testing.T) {
	o := srcRoot()
	o.uiPage = PageSettings
	o.settingsEditing = true
	got := hints(o)
	want := []UIHint{
		{Key: "Enter", Label: "Apply"},
		{Key: "Backspace", Label: "Cancel"},
		{Key: "Up/Down", Label: "Value"},
		{Key: "P", Label: "Screens"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ActionHints(settings editing) = %v, want %v", got, want)
	}
}

func TestActionHintsSettingsBrowse(t *testing.T) {
	o := srcRoot()
	o.uiPage = PageSettings
	got := hints(o)
	want := []UIHint{
		{Key: "Enter", Label: "Edit"},
		{Key: "Backspace", Label: "Exit"},
		{Key: "Up/Down", Label: "Setting"},
		{Key: "P", Label: "Screens"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ActionHints(settings browse) = %v, want %v", got, want)
	}
}

func TestActionHintsHelpContent(t *testing.T) {
	o := srcRoot()
	o.uiPage = PageHelp
	o.focusPanel = 1
	got := hints(o)
	want := []UIHint{
		{Key: "Backspace", Label: "Topics"},
		{Key: "Up/Down", Label: "Scroll"},
		{Key: "P", Label: "Screens"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ActionHints(help content) = %v, want %v", got, want)
	}
}

func TestActionHintsHelpTopics(t *testing.T) {
	o := srcRoot()
	o.uiPage = PageHelp
	o.focusPanel = 0
	got := hints(o)
	want := []UIHint{
		{Key: "Enter", Label: "Open"},
		{Key: "Backspace", Label: "Back"},
		{Key: "Up/Down", Label: "Topic"},
		{Key: "P", Label: "Screens"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ActionHints(help topics) = %v, want %v", got, want)
	}
}

func TestActionHintsGamepadMapping(t *testing.T) {
	o := srcRoot()
	o.controllerConnected = true
	o.uiPage = PagePresets
	o.focusPanel = 1
	got := hints(o)
	want := []UIHint{
		{Key: "A", Label: "Select"},
		{Key: "B", Label: "Back"},
		{Key: "D-pad", Label: "Preset"},
		{Key: "L1/R1", Label: "Screens"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ActionHints(gamepad presets) = %v, want %v", got, want)
	}
}

func TestActionHintsPlayPauseShown(t *testing.T) {
	o := srcRoot()
	o.uiPage = PageLibrary
	o.playingTrack = "/music/track.it"
	got := hints(o)
	found := false
	for _, h := range got {
		if h.Label == "Play/Pause" {
			found = true
			if h.Key != "Space" {
				t.Fatalf("Play/Pause key = %q, want Space", h.Key)
			}
		}
	}
	if !found {
		t.Fatalf("expected a Play/Pause hint when playing, got %v", got)
	}
}

func TestJoinHints(t *testing.T) {
	text := joinHints([]UIHint{{Key: "Enter", Label: "Open"}, {Key: "Backspace", Label: "Back"}}, "   ")
	want := "[Enter] Open   [Backspace] Back"
	if text != want {
		t.Fatalf("joinHints = %q, want %q", text, want)
	}
}

func TestBuildHintsTextNilFace(t *testing.T) {
	// Without a face there is no way to measure width — return "" rather than
	// risk an oversized/clipped footer.
	o := srcRoot()
	if got := o.buildHintsText([]UIHint{{Key: "Enter", Label: "Open"}}, 640); got != "" {
		t.Fatalf("buildHintsText(nil face) = %q, want empty", got)
	}
}
