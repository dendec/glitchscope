package ui

import (
	"testing"

	"github.com/dendec/glitchscope/internal/i18n"
	"github.com/dendec/glitchscope/internal/input"
	"github.com/dendec/glitchscope/internal/player"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
)

func testPointerOverlay() *Overlay {
	return &Overlay{
		face:         basicfont.Face7x13,
		fontSize:     13,
		screenW:      640,
		screenH:      480,
		uiVisible:    true,
		panelEntered: true,
		navStack:     []navLevel{{ctx: ctxSourceRoot}},
	}
}

func TestPointerRightClickCopiesLinkAndPrimaryClickOpensIt(t *testing.T) {
	const url = "https://example.test/stream"
	o := testPointerOverlay()
	o.SetPointerCapabilities(false, true, false)
	layout := o.pointerLayout(640, 480)
	var row pointerTextRow
	found := false
	for _, target := range layout.targets {
		if target.target.kind == pointerTargetPanel && target.target.panel == 1 {
			row = pointerTextRow{text: "Homepage: " + url, link: url, x: target.rect.x, y: target.rect.y, w: target.rect.w, h: target.rect.h}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("right panel target missing")
	}
	o.pointerTextRows = []pointerTextRow{row}
	x := row.x + float32(font.MeasureString(o.face, "Homepage: ").Ceil()+4)
	y := row.y + row.h/2
	down := input.PointerEvent{Device: input.PointerMouse, Phase: input.PointerDown, X: x, Y: y, Button: input.PointerButtonSecondary}
	o.HandlePointer(down, 640, 480)
	if got := o.ConsumePointerCopy(); got != url {
		t.Fatalf("right-click copy = %q, want %q", got, url)
	}
	down.Button = input.PointerButtonPrimary
	o.HandlePointer(down, 640, 480)
	down.Phase = input.PointerUp
	o.HandlePointer(down, 640, 480)
	if got := o.ConsumePointerURL(); got != url {
		t.Fatalf("primary click URL = %q, want %q", got, url)
	}
}

func pointerTap(o *Overlay, x, y float32) PointerResult {
	o.HandlePointer(input.PointerEvent{
		Device:    input.PointerTouch,
		Phase:     input.PointerDown,
		PointerID: 1,
		X:         x,
		Y:         y,
		Button:    input.PointerButtonPrimary,
	}, 640, 480)
	return o.HandlePointer(input.PointerEvent{
		Device:    input.PointerTouch,
		Phase:     input.PointerUp,
		PointerID: 1,
		X:         x,
		Y:         y,
		Button:    input.PointerButtonPrimary,
	}, 640, 480)
}

func TestPointerCloseRequiresMouseOrTouchCapability(t *testing.T) {
	o := testPointerOverlay()
	o.SetPointerCapabilities(false, false, false)
	if o.pointerCloseVisible() {
		t.Fatal("close affordance visible without a pointer device")
	}

	o.SetPointerCapabilities(false, true, false)
	l := o.pointerLayout(640, 480)
	var close pointerRect
	found := false
	for _, item := range l.targets {
		if item.target.kind == pointerTargetClose {
			close, found = item.rect, true
			break
		}
	}
	if !found {
		t.Fatal("close target missing with mouse capability")
	}
	if got := pointerTap(o, close.x+close.w/2, close.y+close.h/2); got != PointerResultClose {
		t.Fatalf("close tap result = %v, want %v", got, PointerResultClose)
	}
}

func TestPointerSettingsValueTapAppliesDirectly(t *testing.T) {
	o := testPointerOverlay()
	o.uiPage = PageSettings
	o.settingsRows = []SettingRow{{Label: "Theme", Values: []string{"Dark", "Light"}, Index: 0}}
	o.settingsCursor = 0
	o.focusPanel = 0

	l := o.pointerLayout(640, 480)
	var value pointerRect
	found := false
	for _, item := range l.targets {
		if item.target.kind == pointerTargetRow && item.target.panel == 1 && item.target.index == 1 {
			value, found = item.rect, true
			break
		}
	}
	if !found {
		t.Fatal("settings value target missing")
	}
	x, y := value.x+value.w/2, value.y+value.h/2
	if got := pointerTap(o, x, y); got != PointerResultSelect {
		t.Fatalf("settings value tap = %v, want select", got)
	}
	if !o.Select() || o.settingsRows[0].Index != 1 {
		t.Fatalf("settings value was not applied: %#v", o.settingsRows[0])
	}
}

func TestPointerMouseMotionDoesNotMoveSelection(t *testing.T) {
	o := testPointerOverlay()
	o.albums = []string{"first", "second"}
	o.albumEntries = []navEntry{{label: "first"}, {label: "second"}}
	o.albumCursor = 0
	o.focusPanel = 0

	l := o.pointerLayout(640, 480)
	var second pointerRect
	for _, item := range l.targets {
		if item.target.kind == pointerTargetRow && item.target.panel == 0 && item.target.index == 1 {
			second = item.rect
			break
		}
	}
	if second.w == 0 {
		t.Fatal("second row target missing")
	}
	o.HandlePointer(input.PointerEvent{
		Device: input.PointerMouse,
		Phase:  input.PointerMove,
		X:      second.x + second.w/2,
		Y:      second.y + second.h/2,
	}, 640, 480)
	if o.albumCursor != 0 || o.focusPanel != 0 {
		t.Fatalf("mouse hover changed selection: cursor=%d focus=%d", o.albumCursor, o.focusPanel)
	}
}

func TestPointerTapSelectsRowWhenItChangesPanel(t *testing.T) {
	o := testPointerOverlay()
	o.navStack = []navLevel{{ctx: ctxCatalog}}
	o.albums = []string{"album"}
	o.albumEntries = []navEntry{{label: "album"}}
	o.trackInfos = []player.TrackInfo{{Path: "track.mod"}}
	o.focusPanel = 0

	l := o.pointerLayout(640, 480)
	var track pointerRect
	for _, item := range l.targets {
		if item.target.kind == pointerTargetRow && item.target.panel == 1 {
			track = item.rect
			break
		}
	}
	if track.w == 0 {
		t.Fatal("track row target missing")
	}
	if got := pointerTap(o, track.x+track.w/2, track.y+track.h/2); got != PointerResultSelect {
		t.Fatalf("track tap result = %v, want %v", got, PointerResultSelect)
	}
	if o.focusPanel != 1 || o.trackCursor != 0 {
		t.Fatalf("track tap focus = %d cursor = %d, want panel 1 cursor 0", o.focusPanel, o.trackCursor)
	}
}

func TestPointerBreadcrumbHitboxUsesNavigationRow(t *testing.T) {
	o := testPointerOverlay()
	o.navStack = []navLevel{
		{ctx: ctxSourceRoot},
		{ctx: ctxCatalog, label: "Modland"},
	}
	o.source = sourceModland

	l := o.pointerLayout(640, 480)
	var root pointerRect
	for _, item := range l.targets {
		if item.target.kind == pointerTargetBreadcrumb && item.target.navStackDepth == 1 {
			root = item.rect
			break
		}
	}
	if root.h <= 1 {
		t.Fatalf("breadcrumb hitbox height = %.0f, want full navigation row", root.h)
	}
	if got := pointerTap(o, root.x+root.w/2, root.y+root.h/2); got != PointerResultNone {
		t.Fatalf("breadcrumb tap result = %v, want no app action", got)
	}
	if len(o.navStack) != 1 {
		t.Fatalf("breadcrumb root did not return to source root: depth=%d", len(o.navStack))
	}
}

func TestPointerWheelUsesFocusedPanelWithoutMousePosition(t *testing.T) {
	o := testPointerOverlay()
	o.uiPage = PageSettings
	o.settingsRows = make([]SettingRow, 40)
	for i := range o.settingsRows {
		o.settingsRows[i] = SettingRow{Label: "Setting", Values: []string{"0"}, Index: 0}
	}
	o.settingsCursor = 0
	o.focusPanel = 0
	o.HandlePointer(input.PointerEvent{
		Device:        input.PointerMouse,
		Phase:         input.PointerWheel,
		ScrollY:       -1,
		PositionValid: false,
	}, 640, 480)
	if o.settingsCursor != 0 || o.albumsScroll != 1 {
		t.Fatalf("focused-panel wheel state = cursor %d scroll %d, want cursor 0 scroll 1", o.settingsCursor, o.albumsScroll)
	}
}

func TestPointerTouchSwipeMovesLibraryScroll(t *testing.T) {
	o := testPointerOverlay()
	o.navStack = []navLevel{{ctx: ctxCatalog}}
	o.albums = make([]string, 60)
	o.albumEntries = make([]navEntry, len(o.albums))
	o.albumCursor = 0
	o.focusPanel = 0
	l := o.pointerLayout(640, 480)
	downY := l.panelY + l.lineH/2

	o.HandlePointer(input.PointerEvent{
		Device:    input.PointerTouch,
		Phase:     input.PointerDown,
		PointerID: 1,
		X:         10,
		Y:         downY,
		Button:    input.PointerButtonPrimary,
	}, 640, 480)
	for _, y := range []float32{downY - 5, downY - 10, downY - 15, downY - 20} {
		o.HandlePointer(input.PointerEvent{
			Device:    input.PointerTouch,
			Phase:     input.PointerMove,
			PointerID: 1,
			X:         10,
			Y:         y,
		}, 640, 480)
	}
	if got := o.HandlePointer(input.PointerEvent{
		Device:    input.PointerTouch,
		Phase:     input.PointerUp,
		PointerID: 1,
		X:         10,
		Y:         downY - 20,
		Button:    input.PointerButtonPrimary,
	}, 640, 480); got != PointerResultNone {
		t.Fatalf("swipe result = %v, want no selection", got)
	}
	if o.albumCursor != 0 || o.albumsScroll == 0 {
		t.Fatalf("touch swipe state = cursor %d scroll %d, want cursor 0 and non-zero scroll", o.albumCursor, o.albumsScroll)
	}
}

func TestPointerHelpParentRowBacksOutOneLevel(t *testing.T) {
	o := testPointerOverlay()
	o.uiPage = PageHelp
	o.helpView.InChildren = true
	o.helpView.TopicCursor = int(HelpQuickStart)
	o.focusPanel = 0

	l := o.pointerLayout(640, 480)
	var parent pointerRect
	found := false
	for _, item := range l.targets {
		if item.target.kind == pointerTargetBreadcrumb && item.target.helpBack {
			parent, found = item.rect, true
			break
		}
	}
	if !found {
		t.Fatal("Help parent row target missing inside a topic")
	}
	if parent.y != l.panelY {
		t.Fatalf("Help parent row y = %.0f, want panel y %.0f", parent.y, l.panelY)
	}
	if got := pointerTap(o, parent.x+parent.w/2, parent.y+parent.h/2); got != PointerResultNone {
		t.Fatalf("help breadcrumb result = %v, want no app action", got)
	}
	if o.helpView.InChildren || o.uiPage != PageHelp {
		t.Fatalf("Help parent row did not back out: page=%d view=%+v", o.uiPage, o.helpView)
	}
}

func TestPointerRepeatOnSelectedHelpRowKeepsSelection(t *testing.T) {
	o := testPointerOverlay()
	o.uiPage = PageHelp
	o.panelEntered = true
	o.focusPanel = 1
	o.helpTopics = []HelpTopic{{ID: HelpQuickStart, Children: []HelpEntry{{Title: "Music"}}}}
	o.helpView.InChildren = true
	o.helpView.TopicCursor = int(HelpQuickStart)
	o.helpView.EntryCursor = 0

	l := o.pointerLayout(640, 480)
	var row pointerRect
	for _, item := range l.targets {
		if item.target.kind == pointerTargetRow && item.target.panel == 0 && item.target.index == 0 {
			row = item.rect
			break
		}
	}
	if row.w == 0 {
		t.Fatal("selected Help row target missing")
	}
	if got := pointerTap(o, row.x+row.w/2, row.y+row.h/2); got != PointerResultNone {
		t.Fatalf("repeat Help tap result = %v, want no navigation action", got)
	}
	if o.focusPanel != 0 || o.helpView.EntryCursor != 0 {
		t.Fatalf("repeat Help tap focus=%d cursor=%d, want left focus and cursor 0", o.focusPanel, o.helpView.EntryCursor)
	}
}

func TestPointerHelpSelectRestoresPanelUnderPointer(t *testing.T) {
	o := testPointerOverlay()
	o.uiPage = PageHelp
	o.panelEntered = true
	o.focusPanel = 0
	o.helpTopics = []HelpTopic{{ID: HelpQuickStart, Children: []HelpEntry{{Title: "Music"}}}}
	o.helpView.InChildren = true
	o.helpView.TopicCursor = int(HelpQuickStart)
	o.helpView.EntryCursor = 0

	l := o.pointerLayout(640, 480)
	var row pointerRect
	for _, item := range l.targets {
		if item.target.kind == pointerTargetRow && item.target.panel == 0 && item.target.index == 0 {
			row = item.rect
			break
		}
	}
	if row.w == 0 {
		t.Fatal("Help row target missing")
	}
	if got := pointerTap(o, row.x+row.w/2, row.y+row.h/2); got != PointerResultSelect {
		t.Fatalf("Help row tap result = %v, want %v", got, PointerResultSelect)
	}

	// The app routes PointerResultSelect through the normal Select action.
	// Help's keyboard-oriented transition moves to the content panel; the
	// pointer path must restore the panel where the tap happened.
	pointerPanel := o.FocusPanel()
	o.Select()
	if o.FocusPanel() != 1 {
		t.Fatalf("Help Select focus = %d, want content panel before restore", o.FocusPanel())
	}
	o.RestorePointerFocus(pointerPanel)
	if o.FocusPanel() != 0 {
		t.Fatalf("restored Help pointer focus = %d, want left panel", o.FocusPanel())
	}
}

func TestPointerHelpRootHasNoParentRow(t *testing.T) {
	o := testPointerOverlay()
	o.uiPage = PageHelp
	l := o.pointerLayout(640, 480)
	for _, item := range l.targets {
		if item.target.kind == pointerTargetBreadcrumb && item.target.helpBack {
			t.Fatal("Help parent row is visible at the root")
		}
	}
	if l.panelY != float32(o.headerHeight(o.lineHeight())) {
		t.Fatalf("Help panel y = %.0f, want header height %.0f", l.panelY, float32(o.headerHeight(o.lineHeight())))
	}
}

func TestHelpParentRowIsKeyboardSelectable(t *testing.T) {
	o := testPointerOverlay()
	o.uiPage = PageHelp
	o.panelEntered = true
	o.focusPanel = 0
	o.helpTopics = []HelpTopic{{ID: HelpQuickStart, Children: []HelpEntry{{Title: "Controls"}}}}
	o.helpView.InChildren = true

	o.helpMoveEntry(-1)
	if !o.helpView.ParentSelected {
		t.Fatal("moving above the first Help entry did not select the parent row")
	}
	if o.Select() {
		t.Fatal("selecting Help parent row unexpectedly returned an app action")
	}
	if o.helpView.InChildren || o.helpView.ParentSelected {
		t.Fatalf("keyboard parent selection did not go back: view=%+v", o.helpView)
	}
}

func TestHelpParentRowHintsBackOnly(t *testing.T) {
	o := testPointerOverlay()
	o.uiPage = PageHelp
	o.helpView.InChildren = true
	o.helpView.ParentSelected = true
	o.focusPanel = 0

	hints := o.helpHints()
	if len(hints) != 1 || hints[0].Label != o.catalog.Text(i18n.ActionBack) {
		t.Fatalf("parent row hints = %#v, want Back only", hints)
	}
}

func TestPointerOnlyDeviceHidesKeyMappingFooter(t *testing.T) {
	o := testPointerOverlay()
	o.SetPointerCapabilities(false, true, false)
	if got := o.ActionHints(); got != nil {
		t.Fatalf("pointer-only action hints = %v, want nil", got)
	}
	if got := o.hintRowHeight(); got != 0 {
		t.Fatalf("pointer-only hint row height = %d, want 0", got)
	}

	o.SetPointerCapabilities(true, true, false)
	if got := o.hintRowHeight(); got == 0 {
		t.Fatal("keyboard capability did not restore hint row")
	}
}

func TestPresetBreadcrumbRootCollapsesPresetTree(t *testing.T) {
	o := testPointerOverlay()
	o.uiPage = PagePresets
	o.presetNav.stack = []presetNavLevel{
		{},
		{label: "Collection"},
	}
	segments := o.breadcrumbSegments()
	if len(segments) != 2 || segments[0].target.presetDepth != 1 {
		t.Fatalf("preset breadcrumb segments = %#v", segments)
	}
	o.pointerNavigateBreadcrumb(segments[0].target)
	if got := len(o.presetNav.stack); got != 1 {
		t.Fatalf("preset stack depth after root breadcrumb = %d, want 1", got)
	}
}

func TestCloseUICancelsTransientSettingsEdit(t *testing.T) {
	o := testPointerOverlay()
	o.settingsRows = []SettingRow{{Label: "Theme", Values: []string{"Dark", "Light"}, Index: 0}}
	o.settingsCursor = 0
	o.settingsValueCursor = 1
	o.settingsEditing = true
	o.CloseUI()
	if o.uiVisible {
		t.Fatal("CloseUI left the overlay visible")
	}
	if o.settingsEditing || o.settingsValueCursor != 0 {
		t.Fatalf("CloseUI did not cancel settings edit: editing=%t value=%d", o.settingsEditing, o.settingsValueCursor)
	}
}
