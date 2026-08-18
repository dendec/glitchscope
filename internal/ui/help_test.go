package ui

import "testing"

func TestHelpTopicsLoadFromAsset(t *testing.T) {
	if len(helpTopics) != 6 {
		t.Fatalf("loaded %d Help topics, want 6", len(helpTopics))
	}
	if helpTopics[HelpQuickStart].Title != "Quick Start" || len(helpTopics[HelpQuickStart].Lines) == 0 {
		t.Fatalf("Quick Start topic was not loaded from asset: %+v", helpTopics[HelpQuickStart])
	}
	if len(helpTopics[HelpFormats].Children) != 3 || len(helpTopics[HelpCatalogs].Children) != 2 {
		t.Fatalf("hierarchical Help entries were not loaded: formats=%d catalogs=%d", len(helpTopics[HelpFormats].Children), len(helpTopics[HelpCatalogs].Children))
	}
	if len(helpTopics[HelpLicenses].Children) != 14 {
		t.Fatalf(" Licenses children = %d, want 14", len(helpTopics[HelpLicenses].Children))
	}
}

func TestHelpLinesUseKeyboardMappingByDefault(t *testing.T) {
	o := &Overlay{}
	lines := o.helpLines(helpTopic(HelpQuickStart))
	if lines[3] != "Enter confirms or plays the focused item." || lines[5] != "Arrows move between panels or scroll." {
		t.Fatalf("keyboard Help mapping = %v", lines)
	}
}

func TestHelpLinesUseGamepadMappingWhenConnected(t *testing.T) {
	o := &Overlay{controllerConnected: true}
	lines := o.helpLines(helpTopic(HelpQuickStart))
	if lines[3] != "B confirms. A returns." || lines[5] != "Right stick seeks. L1 and R1 switch presets. X picks a random preset." {
		t.Fatalf("gamepad Help mapping = %v", lines)
	}
}

func TestPageCycleIncludesHelp(t *testing.T) {
	o := &Overlay{uiPage: PageLibrary}
	for _, want := range []UIPage{PageSettings, PagePresets, PageHelp, PageLibrary} {
		o.NextScreen()
		if o.uiPage != want {
			t.Fatalf("NextScreen() page = %d, want %d", o.uiPage, want)
		}
	}

	for _, want := range []UIPage{PageHelp, PagePresets, PageSettings, PageLibrary} {
		o.PrevScreen()
		if o.uiPage != want {
			t.Fatalf("PrevScreen() page = %d, want %d", o.uiPage, want)
		}
	}
}

func TestHelpTopicChangeResetsContentPosition(t *testing.T) {
	o := &Overlay{helpView: HelpViewState{TopicCursor: int(HelpQuickStart), ContentTop: 4}}
	o.helpMoveTopic(1)
	if o.helpView.TopicCursor != int(HelpFormats) || o.helpView.ContentTop != 0 {
		t.Fatalf("help state = %+v, want topic %d at top", o.helpView, HelpFormats)
	}
}

func TestHelpEntryChangeResetsContentPosition(t *testing.T) {
	o := &Overlay{helpView: HelpViewState{
		TopicCursor: int(HelpFormats),
		EntryCursor: 0,
		ContentTop:  8,
		InChildren:  true,
	}}
	o.helpMoveEntry(1)
	if o.helpView.EntryCursor != 1 || o.helpView.ContentTop != 0 {
		t.Fatalf("help state = %+v, want entry 1 at top", o.helpView)
	}
}

func TestHelpContentScrollStopsAtLastVisiblePage(t *testing.T) {
	o := &Overlay{
		focusPanel:      1,
		helpVisibleRows: 3,
		panelEntered:    true,
		helpView: HelpViewState{
			TopicCursor: int(HelpQuickStart),
			ContentTop:  12,
		},
		uiPage: PageHelp,
	}
	o.CursorDown()
	if o.helpView.ContentTop != 12 {
		t.Fatalf("ContentTop = %d, want 12", o.helpView.ContentTop)
	}
	for range 12 {
		o.CursorUp()
	}
	if o.helpView.ContentTop != 0 {
		t.Fatalf("ContentTop after scrolling up = %d, want 0", o.helpView.ContentTop)
	}
}

func TestHelpBackReturnsToLibrary(t *testing.T) {
	o := &Overlay{uiPage: PageHelp, panelEntered: true, focusPanel: 0}
	o.Back()
	if o.uiPage != PageLibrary || o.panelEntered || o.focusPanel != 0 {
		t.Fatalf("Back() state = page %d, entered %t, focus %d", o.uiPage, o.panelEntered, o.focusPanel)
	}
}
