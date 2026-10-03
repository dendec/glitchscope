package ui

import (
	"strings"
	"testing"

	"github.com/dendec/glitchscope/internal/i18n"
	"golang.org/x/image/font/basicfont"
)

func TestPresetPackDetailsShowArchiveSizeAndPresetCount(t *testing.T) {
	for _, state := range []string{"available", "installed", "downloading", "testing", "busy"} {
		t.Run(state, func(t *testing.T) {
			item := PresetPackItem{ID: "cream", Name: "Cream", ArchiveBytes: 2 << 20, PresetCount: 40}
			item.Installed = state == "installed" || state == "testing" || state == "busy"
			item.Downloading = state == "downloading"
			item.Testing = state == "testing"
			item.Busy = state == "busy"
			o := newPresetPackOverlay(item)
			selectPresetPackRoot(t, o, item.ID)
			size, count := "2.0 MB", "40"
			if !item.Installed {
				size, count = "≈ "+size, "≈ "+count
			}
			texts := strings.Join(packRowTexts(o.buildPresetDetailRows()), "\n")
			for _, want := range []string{
				o.catalog.Format(i18n.PresetPacksArchiveSize, size),
				o.catalog.Format(i18n.PresetPacksPresetCount, count),
			} {
				if !strings.Contains(texts, want) {
					t.Fatalf("details %q missing %q", texts, want)
				}
			}
			if state == "installed" && o.presetPackActionCount(item) != 2 {
				t.Fatal("metadata changed action navigation")
			}
		})
	}
}

func TestPresetPackDetailsShowUnknownMetadata(t *testing.T) {
	item := PresetPackItem{ID: "cream", Name: "Cream"}
	o := newPresetPackOverlay(item)
	texts := strings.Join(packRowTexts(o.presetPackActionRows(item)), "\n")
	for _, key := range []i18n.Key{i18n.PresetPacksArchiveSize, i18n.PresetPacksPresetCount} {
		if !strings.Contains(texts, o.catalog.Format(key, "—")) {
			t.Fatalf("unknown metadata missing from %q", texts)
		}
	}
}

func TestPresetPackDownloadIsRightPanelAction(t *testing.T) {
	o := newPresetPackOverlay(PresetPackItem{ID: "cream", Name: "Cream"})
	selectPresetPackRoot(t, o, "cream")
	var gotAction PresetPackAction
	var calls int
	o.SetPresetPackAction(func(_ string, action PresetPackAction) { gotAction = action; calls++ })

	if o.Select() || calls != 0 {
		t.Fatal("selecting an unavailable collection should only focus its action")
	}
	if o.focusPanel != 1 || o.presetPackActionCursor != 0 {
		t.Fatalf("focus/cursor = %d/%d, want right/first action", o.focusPanel, o.presetPackActionCursor)
	}
	if o.Select() {
		t.Fatal("collection action selected a preset")
	}
	if gotAction != PresetPackInstall || calls != 1 {
		t.Fatalf("action/calls = %d/%d, want one download", gotAction, calls)
	}
	if o.presetNav.Depth() != 1 {
		t.Fatalf("action changed collection navigation depth to %d", o.presetNav.Depth())
	}
	if !strings.Contains(strings.Join(packRowTexts(o.buildPresetDetailRows()), " "), "Downloads and installs the collection ZIP") {
		t.Fatal("Download action is missing contextual help")
	}
}

func TestInstalledPresetPackActionsAreOnRightPanel(t *testing.T) {
	o := newPresetPackOverlay(PresetPackItem{ID: "cream", Name: "Cream", Installed: true})
	o.SetPresetTree([]string{"Cream/Fractal/demo.milk"})
	selectPresetPackRoot(t, o, "cream")
	if o.Select() {
		t.Fatal("opening collection selected a preset")
	}
	if o.focusPanel != 0 || o.presetNav.Depth() != 2 {
		t.Fatal("selecting an installed collection should open its tree in the left panel")
	}
	o.presetNav.current().cursor = 1 // Skip the parent entry.
	if node := o.presetNav.Selected(); node == nil || node.name != "Fractal" {
		t.Fatalf("first collection entry = %#v, want real preset folder", node)
	}
	if len(o.presetNav.current().nodes) != 1 {
		t.Fatalf("collection entries = %d, want one real folder and no inline action", len(o.presetNav.current().nodes))
	}

	o.Back()
	o.FocusRight()
	rows := o.buildPresetDetailRows()
	if len(rows) < 2 || rows[0].text != o.catalog.Text("actions.delete") || rows[1].text != o.catalog.Text("actions.test") {
		t.Fatalf("collection actions = %#v, want Delete and Test", rows)
	}
	if !strings.Contains(strings.Join(packRowTexts(rows), " "), "Removes the collection ZIP") {
		t.Fatal("Delete action is missing contextual help")
	}
	o.presetPackActionCursor = 1
	if !strings.Contains(strings.Join(packRowTexts(o.buildPresetDetailRows()), " "), "Measures every preset") {
		t.Fatal("Test action is missing contextual help")
	}
	o.presetPackActionCursor = 0
	var gotAction PresetPackAction
	calls := 0
	o.SetPresetPackAction(func(_ string, action PresetPackAction) { gotAction = action; calls++ })
	if o.Select() || !o.presetPackRemoveConfirm || calls != 0 {
		t.Fatal("first Delete selection should request confirmation without dispatch")
	}
	if o.Select() || o.presetPackRemoveConfirm || calls != 1 || gotAction != PresetPackRemove {
		t.Fatal("second Delete selection should dispatch remove")
	}
}

func TestTestActionHiddenAfterPresetPackIsFullyTested(t *testing.T) {
	o := newPresetPackOverlay(PresetPackItem{
		ID: "cream", Name: "Cream", Installed: true, Tested: true, TestProgress: 40, TestTotal: 40,
	})
	selectPresetPackRoot(t, o, "cream")

	item := o.selectedPresetPack()
	if item == nil {
		t.Fatal("selected collection not found")
	}
	if count := o.presetPackActionCount(*item); count != 1 {
		t.Fatalf("action count = %d, want only Delete", count)
	}
	rows := o.buildPresetDetailRows()
	if len(rows) == 0 || rows[0].text != o.catalog.Text("actions.delete") {
		t.Fatalf("first collection action = %#v, want Delete", rows)
	}
	for _, row := range rows {
		if row.text == o.catalog.Text("actions.test") {
			t.Fatalf("completed collection still shows Test: %#v", rows)
		}
	}
}

func TestInstalledPresetPackTestActionUsesSecondRightRow(t *testing.T) {
	o := newPresetPackOverlay(PresetPackItem{ID: "cream", Name: "Cream", Installed: true})
	selectPresetPackRoot(t, o, "cream")
	o.FocusRight()
	o.CursorDown()
	o.FocusRight()
	if o.presetPackActionCursor != 1 {
		t.Fatalf("repeated FocusRight reset action cursor to %d", o.presetPackActionCursor)
	}
	var gotAction PresetPackAction
	o.SetPresetPackAction(func(_ string, action PresetPackAction) { gotAction = action })
	if o.Select() || gotAction != PresetPackTest {
		t.Fatalf("Test action = %d, want test", gotAction)
	}
}

func TestDownloadingCollectionCanBeCancelledFromRightPanel(t *testing.T) {
	o := newPresetPackOverlay(PresetPackItem{ID: "cream", Name: "Cream", Downloading: true})
	selectPresetPackRoot(t, o, "cream")
	o.FocusRight()
	var gotID string
	var gotAction PresetPackAction
	o.SetPresetPackAction(func(id string, action PresetPackAction) { gotID, gotAction = id, action })
	if o.Select() || gotID != "cream" || gotAction != PresetPackCancel {
		t.Fatalf("cancel action = %q/%d, want cream/cancel", gotID, gotAction)
	}
}

func TestPresetPackDownloadProgressAppearsOnlyInTree(t *testing.T) {
	o := newPresetPackOverlay(PresetPackItem{
		ID: "cream", Name: "Cream", Downloading: true, ProgressRead: 25, ProgressTotal: 100,
	})
	selectPresetPackRoot(t, o, "cream")
	if line := o.nodeDisplayLine(o.presetNav.Selected(), ""); !strings.Contains(line, "25%") {
		t.Fatalf("collection line = %q, want progress", line)
	}
	for _, row := range o.buildPresetDetailRows() {
		if strings.Contains(row.text, "%") || strings.Contains(row.text, "Patreon") {
			t.Fatalf("right panel duplicates progress or source label: %q", row.text)
		}
	}
}

func TestPresetPackTestProgressAppearsOnlyInRightPanel(t *testing.T) {
	o := newPresetPackOverlay(PresetPackItem{
		ID: "cream", Name: "Cream", Installed: true, Testing: true, TestProgress: 12, TestTotal: 40, TestFPS: 47,
	})
	selectPresetPackRoot(t, o, "cream")
	if line := o.nodeDisplayLine(o.presetNav.Selected(), ""); strings.Contains(line, "12") {
		t.Fatalf("collection tree line contains test progress: %q", line)
	}
	rows := o.buildPresetDetailRows()
	if len(rows) < 5 || !strings.Contains(rows[len(rows)-2].text, "12") || !strings.Contains(rows[len(rows)-2].text, "40") || rows[len(rows)-1].text != "FPS: 47" {
		t.Fatalf("test details = %#v, want cancel, progress 12/40, and FPS 47", rows)
	}
	if !strings.Contains(strings.Join(packRowTexts(rows), " "), "Stops the current download or preset test.") {
		t.Fatalf("test action is missing contextual help: %#v", rows)
	}
}

func packRowTexts(rows []listRow) []string {
	texts := make([]string, len(rows))
	for i := range rows {
		texts[i] = rows[i].text
	}
	return texts
}

func TestRunningPresetPackTestAllowsPageNavigation(t *testing.T) {
	for _, forward := range []bool{true, false} {
		o := &Overlay{uiPage: PagePresets, panelEntered: true, focusPanel: 1}
		o.SetPresetPacks([]PresetPackItem{{ID: "cream", Name: "Cream", Testing: true}})
		var actions int
		o.SetPresetPackAction(func(string, PresetPackAction) { actions++ })
		pages := []UIPage{PageSettings, PageHelp, PageLibrary, PagePresets}
		if !forward {
			pages = []UIPage{PageLibrary, PageHelp, PageSettings, PagePresets}
		}
		for _, want := range pages {
			if forward {
				o.NextScreen()
			} else {
				o.PrevScreen()
			}
			if o.uiPage != want || o.focusPanel != 0 || !o.panelEntered {
				t.Fatalf("forward=%t: page=%d focus=%d entered=%t, want page=%d with left panel focused", forward, o.uiPage, o.focusPanel, o.panelEntered, want)
			}
			if !o.presetPackTestRunning() || actions != 0 {
				t.Fatal("page navigation modified the running preset test")
			}
		}
	}
}

func TestPresetPackCalibrationKeepsCollectionDetailsVisible(t *testing.T) {
	o := newPresetPackOverlay(PresetPackItem{ID: "cream", Name: "Cream", Installed: true, Testing: true, TestFPS: 10})
	o.SetPresetTree([]string{"Cream/first.milk", "Cream/second.milk"})
	selectPresetPackRoot(t, o, "cream")
	o.FocusRight()
	o.presetPreviewFPSNow = 25
	for _, name := range []string{"Cream/first.milk", "Cream/second.milk"} {
		o.SetPresetName(name)
		if node := o.presetNav.Selected(); node == nil || node.packID != "cream" || o.focusPanel != 1 {
			t.Fatal("test preset change moved focus away from collection actions")
		}
		rows := o.buildPresetDetailRows()
		if rows[len(rows)-1].text != "FPS: 10" {
			t.Fatalf("collection details = %#v, want current test FPS", rows)
		}
	}
	o.SetPresetPacks([]PresetPackItem{{ID: "cream", Name: "Cream", Installed: true, Testing: true, TestFPS: 5}})
	rows := o.buildPresetDetailRows()
	if !o.presetsDetailDirty || rows[len(rows)-1].text != "FPS: 5" {
		t.Fatal("new test FPS did not update the collection detail panel")
	}
}

func TestBackCancelsRunningPresetPackTest(t *testing.T) {
	o := &Overlay{uiPage: PagePresets, panelEntered: true}
	o.SetPresetPacks([]PresetPackItem{
		{ID: "cream", Name: "Cream", Installed: true, Testing: true},
		{ID: "mashups", Name: "Mashups", Installed: true},
	})
	o.SetPresetTree(nil)
	selectPresetPackRoot(t, o, "mashups")
	var gotID string
	var gotAction PresetPackAction
	o.SetPresetPackAction(func(id string, action PresetPackAction) {
		gotID, gotAction = id, action
	})

	o.Back()

	if gotID != "cream" || gotAction != PresetPackCancel || o.uiPage != PagePresets {
		t.Fatalf("Back dispatched %q/%d and moved to page %d; want cancel active test cream and stay on Presets", gotID, gotAction, o.uiPage)
	}
}

func TestPresetPackStateUpdateRetainsSelectedCollection(t *testing.T) {
	o := &Overlay{}
	o.SetPresetPacks([]PresetPackItem{{ID: "cream", Name: "Cream"}, {ID: "2020", Name: "Mashups"}})
	o.SetPresetTree(nil)
	selectPresetPackRoot(t, o, "2020")
	o.SetPresetPacks([]PresetPackItem{{ID: "cream", Name: "Cream", Installed: true}, {ID: "2020", Name: "Mashups", Downloading: true}})
	if node := o.presetNav.Selected(); node == nil || node.packID != "2020" {
		t.Fatalf("selected node after status update = %#v, want collection 2020", node)
	}
	o.SetPresetTree([]string{"Cream/Fractal/demo.milk"})
	if node := o.presetNav.Selected(); node == nil || node.packID != "2020" {
		t.Fatalf("selected node after catalog update = %#v, want collection 2020", node)
	}
}

func newPresetPackOverlay(item PresetPackItem) *Overlay {
	o := &Overlay{uiPage: PagePresets, panelEntered: true}
	o.SetPresetPacks([]PresetPackItem{item})
	o.SetPresetTree(nil)
	return o
}

func selectPresetPackRoot(t *testing.T, o *Overlay, id string) {
	t.Helper()
	for i := range o.presetTreeRoot {
		if o.presetTreeRoot[i].packID == id {
			o.presetNav.stack[0].cursor = i
			return
		}
	}
	t.Fatalf("preset pack %q not found in tree", id)
}

func TestPresetPackInstallationStatusAndCancel(t *testing.T) {
	item := PresetPackItem{ID: "butterchurn", Name: "Butterchurn", Downloading: true, Installing: true}
	o := newPresetPackOverlay(item)
	selectPresetPackRoot(t, o, item.ID)
	if got := o.presetPackStatusByID(item.ID); got != o.catalog.Text(i18n.PresetPacksInstalling) || strings.Contains(got, "%") {
		t.Fatalf("installation status = %q", got)
	}
	if o.presetPackActionCount(item) != 1 {
		t.Fatal("installation must retain Cancel")
	}
	o.FocusRight()
	var action PresetPackAction
	o.SetPresetPackAction(func(_ string, got PresetPackAction) { action = got })
	o.Select()
	if action != PresetPackCancel {
		t.Fatal("installation cannot be cancelled")
	}
}

func TestPresetPackButtonsUseLibraryLayoutAndIcons(t *testing.T) {
	for _, test := range []struct {
		item  PresetPackItem
		icons []string
	}{
		{PresetPackItem{}, []string{iconDownloads}},
		{PresetPackItem{Installed: true}, []string{iconDelete, iconTest}},
		{PresetPackItem{Installed: true, Tested: true}, []string{iconDelete}},
		{PresetPackItem{Downloading: true}, []string{iconClose}},
		{PresetPackItem{Testing: true}, []string{iconClose}},
		{PresetPackItem{Busy: true}, nil},
	} {
		o := newPresetPackOverlay(test.item)
		actions := o.presetPackButtons(test.item)
		if len(actions) != len(test.icons) {
			t.Fatalf("buttons for %+v: %v", test.item, actions)
		}
		for i, icon := range test.icons {
			if actions[i].icon != icon {
				t.Fatalf("button %d icon = %s, want %s", i, actions[i].icon, icon)
			}
		}
	}
}

func TestPresetPackBottomButtonsPointerAndHorizontalNavigation(t *testing.T) {
	item := PresetPackItem{ID: "cream", Name: "Cream", Installed: true}
	o := newPresetPackOverlay(item)
	o.face = basicfont.Face7x13
	o.fontSize = 13
	o.screenW, o.screenH = 640, 480
	selectPresetPackRoot(t, o, item.ID)
	o.FocusRight()
	o.FocusRight()
	if o.presetPackActionCursor != 1 {
		t.Fatal("Right did not select the next button")
	}
	o.FocusLeft()
	if o.focusPanel != 1 || o.presetPackActionCursor != 0 {
		t.Fatal("Left did not select the previous button")
	}
	o.presetPackRemoveConfirm = true
	placements := o.pointerActionPlacements()
	if placements[0].label != "[Delete?]" {
		t.Fatalf("confirmation label = %q", placements[0].label)
	}
	layout := o.overlayLayout(640, 480)
	targets := o.pointerActionTargets(layout)
	if len(targets) != 2 || targets[0].rect.y != float32(layout.panelY+layout.panelH-o.actionBarHeight(layout.lineH)) || targets[1].rect.x <= targets[0].rect.x {
		t.Fatalf("bottom horizontal button targets = %+v", targets)
	}
	o.applyPointerActionFocus(1)
	if o.presetPackActionCursor != 1 || o.presetPackRemoveConfirm {
		t.Fatal("pointer did not select Test and clear deletion confirmation")
	}
}
