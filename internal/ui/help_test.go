package ui

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/formats"
	"github.com/dendec/glitchscope/internal/i18n"
)

var helpPlaceholderRE = regexp.MustCompile(`\{[a-z0-9_]+\}`)

func TestHelpTopicsLoadFromAsset(t *testing.T) {
	if len(helpTopics) != 10 {
		t.Fatalf("loaded %d Help topics, want 10", len(helpTopics))
	}
	if helpTopics[HelpQuickStart].Title != "Getting Started" || len(helpTopics[HelpQuickStart].Lines) == 0 {
		t.Fatalf("Getting Started topic was not loaded from asset: %+v", helpTopics[HelpQuickStart])
	}
	if len(helpTopics[HelpFormats].Children) != 3 || len(helpTopics[HelpSources].Children) != 7 || len(helpTopics[HelpFrameRate].Children) != 5 {
		t.Fatalf("hierarchical Help entries were not loaded: formats=%d sources=%d frame rate=%d", len(helpTopics[HelpFormats].Children), len(helpTopics[HelpSources].Children), len(helpTopics[HelpFrameRate].Children))
	}
	wantSources := []string{"Local Music", "Favorites", "Downloads", "Microphone", "Radio", "Modland", "ModArchive"}
	for i, want := range wantSources {
		if got := helpTopics[HelpSources].Children[i].Title; got != want {
			t.Fatalf("source Help entry %d = %q, want %q", i, got, want)
		}
	}
	if len(helpTopics[HelpLicenses].Children) != 23 {
		t.Fatalf(" Licenses children = %d, want 23", len(helpTopics[HelpLicenses].Children))
	}
}

func TestHelpDocumentsEverySupportedFormat(t *testing.T) {
	documented := make(map[string]bool, len(formats.SupportedExts))
	for _, category := range helpTopics[HelpFormats].Children {
		for _, entry := range category.Children {
			if documented[entry.Title] {
				t.Errorf("format %s is documented more than once", entry.Title)
			}
			documented[entry.Title] = true
			if len(entry.Lines) != 1 || entry.Lines[0] == "" {
				t.Errorf("format %s must have one concise description", entry.Title)
			}
			if len(entry.Lines) == 1 && len(entry.Lines[0]) > 200 {
				t.Errorf("format %s description is too long: %d bytes", entry.Title, len(entry.Lines[0]))
			}
		}
	}
	for extension := range formats.SupportedExts {
		if !documented[extension] {
			t.Errorf("supported format %s is missing from Help", extension)
		}
	}
	for extension := range documented {
		if !formats.SupportedExts[extension] {
			t.Errorf("Help documents unsupported format %s", extension)
		}
	}
}

func TestTranslatedHelpPreservesStructureAndLicenses(t *testing.T) {
	for _, language := range config.AllLanguages() {
		if language == config.English {
			continue
		}
		o := &Overlay{catalog: i18n.MustLoad(i18n.English), helpTopics: helpTopics}
		if err := o.SetLanguage(language); err != nil {
			t.Errorf("%s: %v", language, err)
			continue
		}
		if !sameHelpStructure(helpTopics, o.helpTopics) {
			t.Errorf("%s Help structure differs from English", language)
		}
		if !sameHelpPlaceholders(helpTopics, o.helpTopics) {
			t.Errorf("%s Help placeholders differ from English", language)
		}
		for _, ref := range []string{"preset_pack.download", "preset_pack.remove", "preset_pack.test", "preset_pack.cancel"} {
			if got := o.helpDescription(ref); got == "" {
				t.Errorf("%s Help is missing inline description %q", language, ref)
			}
		}
		if !reflect.DeepEqual(helpTopics[HelpLicenses].Children, o.helpTopics[HelpLicenses].Children) {
			t.Errorf("%s translated license metadata or text", language)
		}
	}
}

func TestPresetActionHelpDescriptionsAreShortAndLocalized(t *testing.T) {
	refs := []string{"preset_pack.download", "preset_pack.remove", "preset_pack.test", "preset_pack.cancel"}
	for _, language := range config.AllLanguages() {
		catalog := i18n.MustLoad(i18n.Language(language))
		o := &Overlay{catalog: catalog}
		if language != config.English {
			if err := o.SetLanguage(language); err != nil {
				t.Fatalf("load %s Help: %v", language, err)
			}
		}
		for _, ref := range refs {
			if got := o.helpDescription(ref); got == "" || len([]rune(got)) > 140 {
				t.Errorf("%s Help description %q has unsuitable text %q", language, ref, got)
			}
		}
	}
}

func sameHelpPlaceholders(left, right []HelpTopic) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !reflect.DeepEqual(helpPlaceholders(left[i].Title, left[i].Lines), helpPlaceholders(right[i].Title, right[i].Lines)) ||
			!sameHelpEntryPlaceholders(left[i].Children, right[i].Children) {
			return false
		}
	}
	return true
}

func sameHelpEntryPlaceholders(left, right []HelpEntry) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !reflect.DeepEqual(helpPlaceholders(left[i].Title, left[i].Lines), helpPlaceholders(right[i].Title, right[i].Lines)) ||
			!sameHelpEntryPlaceholders(left[i].Children, right[i].Children) {
			return false
		}
	}
	return true
}

func helpPlaceholders(title string, lines []string) []string {
	values := helpPlaceholderRE.FindAllString(title, -1)
	for _, line := range lines {
		values = append(values, helpPlaceholderRE.FindAllString(line, -1)...)
	}
	return values
}

func sameHelpStructure(left, right []HelpTopic) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].ID != right[i].ID || !sameHelpEntries(left[i].Children, right[i].Children) {
			return false
		}
	}
	return true
}

func sameHelpEntries(left, right []HelpEntry) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].Ref != right[i].Ref || !sameHelpEntries(left[i].Children, right[i].Children) {
			return false
		}
	}
	return true
}

func TestHelpMenuTitleMarksOnlySubmenus(t *testing.T) {
	if got := helpMenuTitle("Formats", true); got != "Formats/" {
		t.Fatalf("submenu title = %q", got)
	}
	if got := helpMenuTitle("Quick start", false); got != "Quick start" {
		t.Fatalf("leaf title = %q", got)
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

func TestSourcesHelpUsesRuntimeCounts(t *testing.T) {
	o := &Overlay{catalogInfo: CatalogInfo{
		ModlandTracks:         123456,
		ModlandDirectories:    789,
		ModArchiveTracks:      4567,
		ModArchiveDirectories: 89,
	}}
	text := strings.Join(o.helpLines(HelpTopic{Lines: helpTopic(HelpSources).Children[5].Lines}), "\n")
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
