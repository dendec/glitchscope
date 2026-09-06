package ui

import (
	"slices"
	"strings"
	"testing"
)

func TestHelpTopicsLoadFromAsset(t *testing.T) {
	if len(helpTopics) != 10 {
		t.Fatalf("loaded %d Help topics, want 10", len(helpTopics))
	}
	if helpTopics[HelpQuickStart].Title != "Getting Started" || len(helpTopics[HelpQuickStart].Lines) == 0 {
		t.Fatalf("Getting Started topic was not loaded from asset: %+v", helpTopics[HelpQuickStart])
	}
	if len(helpTopics[HelpFormats].Children) != 3 || len(helpTopics[HelpCatalogs].Children) != 2 {
		t.Fatalf("hierarchical Help entries were not loaded: formats=%d catalogs=%d", len(helpTopics[HelpFormats].Children), len(helpTopics[HelpCatalogs].Children))
	}
	if len(helpTopics[HelpLicenses].Children) != 23 {
		t.Fatalf(" Licenses children = %d, want 23", len(helpTopics[HelpLicenses].Children))
	}
}

func TestHelpUsesSharedControlLabels(t *testing.T) {
	for _, gamepad := range []bool{false, true} {
		o := &Overlay{controllerConnected: gamepad}
		lines := o.helpLines(helpTopic(HelpQuickStart))
		joined := strings.Join(lines, "\n")
		if !strings.Contains(joined, o.controlLabel(hintMenu)) {
			t.Fatalf("missing menu control in %s", joined)
		}
		if strings.Contains(joined, "{") {
			t.Fatalf("unexpanded controls: %s", joined)
		}
		if len(lines) > 10 {
			t.Fatal("Getting Started is too long")
		}
	}
}

func TestPageCycleIncludesHelp(t *testing.T) {
	o := &Overlay{uiPage: PageLibrary}
	for _, want := range []UIPage{PagePresets, PageSettings, PageHelp, PageLibrary} {
		o.NextScreen()
		if o.uiPage != want {
			t.Fatalf("NextScreen() page = %d, want %d", o.uiPage, want)
		}
	}

	for _, want := range []UIPage{PageHelp, PageSettings, PagePresets, PageLibrary} {
		o.PrevScreen()
		if o.uiPage != want {
			t.Fatalf("PrevScreen() page = %d, want %d", o.uiPage, want)
		}
	}
}

func TestHelpTopicChangeResetsContentPosition(t *testing.T) {
	o := &Overlay{helpView: HelpViewState{TopicCursor: int(HelpQuickStart), ContentTop: 4}}
	o.helpMoveTopic(1)
	if o.helpView.TopicCursor != int(HelpControls) || o.helpView.ContentTop != 0 {
		t.Fatalf("help state = %+v, want topic %d at top", o.helpView, HelpControls)
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
			ContentTop:  5,
		},
		uiPage: PageHelp,
	}
	o.CursorDown()
	if o.helpView.ContentTop != 6 {
		t.Fatalf("ContentTop = %d, want 6", o.helpView.ContentTop)
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

func TestCatalogHelpUsesRuntimeCounts(t *testing.T) {
	o := &Overlay{catalogInfo: CatalogInfo{
		ModlandTracks:         123456,
		ModlandDirectories:    789,
		ModArchiveTracks:      4567,
		ModArchiveDirectories: 89,
	}}
	text := strings.Join(o.helpLines(HelpTopic{Lines: helpTopic(HelpCatalogs).Children[0].Lines}), "\n")
	if !strings.Contains(text, "123,456") || !strings.Contains(text, "789") {
		t.Fatalf("runtime catalog counts missing from %q", text)
	}
}

func TestLongHelpTextGetsParagraphBreaks(t *testing.T) {
	line := strings.Repeat("First sentence has useful details. Second sentence adds context. ", 5)
	lines := readableHelpLines(line)
	if len(lines) < 3 || !slices.Contains(lines, "") {
		t.Fatalf("long help line was not split into paragraphs: %#v", lines)
	}
}
