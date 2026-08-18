package ui

import "testing"

func TestHelpTopicsLoadFromAsset(t *testing.T) {
	if len(helpTopics) != 10 {
		t.Fatalf("loaded %d Help topics, want 10", len(helpTopics))
	}
	if helpTopics[HelpLibrary].Title != "Library" || len(helpTopics[HelpLibrary].Lines) == 0 {
		t.Fatalf("Library topic was not loaded from asset: %+v", helpTopics[HelpLibrary])
	}
	if len(helpTopics[HelpFormats].Children) == 0 || len(helpTopics[HelpCatalogs].Children) != 2 {
		t.Fatalf("hierarchical Help entries were not loaded: formats=%d catalogs=%d", len(helpTopics[HelpFormats].Children), len(helpTopics[HelpCatalogs].Children))
	}
}

func TestHelpLinesUseKeyboardMappingByDefault(t *testing.T) {
	o := &Overlay{}
	lines := o.helpLines(helpTopic(HelpGettingStarted))
	if lines[2] != "Press Enter to open or play." || lines[4] != "Press Backspace to return or cancel." {
		t.Fatalf("keyboard Help mapping = %v", lines)
	}
}

func TestHelpLinesUseGamepadMappingWhenConnected(t *testing.T) {
	o := &Overlay{controllerConnected: true}
	lines := o.helpLines(helpTopic(HelpControls))
	if lines[0] != "B selects, opens, or plays the focused item." || lines[1] != "A returns, cancels, or closes the current panel." {
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
	o := &Overlay{helpView: HelpViewState{TopicCursor: int(HelpLibrary), ContentTop: 4}}
	o.helpMoveTopic(1)
	if o.helpView.TopicCursor != int(HelpDelete) || o.helpView.ContentTop != 0 {
		t.Fatalf("help state = %+v, want topic %d at top", o.helpView, HelpDelete)
	}
}

func TestHelpEntryChangeResetsContentPosition(t *testing.T) {
	o := &Overlay{helpView: HelpViewState{
		TopicCursor: int(HelpFormats),
		EntryCursor: 20,
		ContentTop:  8,
		InChildren:  true,
	}}
	o.helpMoveEntry(1)
	if o.helpView.EntryCursor != 21 || o.helpView.ContentTop != 0 {
		t.Fatalf("help state = %+v, want entry 21 at top", o.helpView)
	}
}

func TestHelpContentScrollStopsAtLastVisiblePage(t *testing.T) {
	o := &Overlay{
		focusPanel:      1,
		helpVisibleRows: 3,
		panelEntered:    true,
		helpView: HelpViewState{
			TopicCursor: int(HelpGettingStarted),
			ContentTop:  3,
		},
		uiPage: PageHelp,
	}
	o.CursorDown()
	if o.helpView.ContentTop != 3 {
		t.Fatalf("ContentTop = %d, want 3", o.helpView.ContentTop)
	}
	for range 3 {
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
