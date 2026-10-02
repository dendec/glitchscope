package ui

import (
	"strings"
	"testing"

	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/i18n"
)

func TestSettingsRowsUseCompactSectionsAndLabels(t *testing.T) {
	rows := BuildSettingsRows(config.DefaultSettings(), 640, 480)
	want := []string{
		"── Playback ────", "Shuffle", "Repeat", "Sort",
		"── Visualization ──", "Visualizer", "Frame rate", "Adaptive resolution", "Resolution", "Upscale filter", "Sensitivity", "Rotation",
		"── Appearance ───", "Language", "Theme", "Transparency", "Stats", "Player bar",
		"── Downloads ───────", "Size", "Keep for",
	}
	if len(rows) != len(want) {
		t.Fatalf("settings rows = %d, want %d", len(rows), len(want))
	}
	for i := range rows {
		if rows[i].Label != want[i] {
			t.Errorf("row %d label = %q, want %q", i, rows[i].Label, want[i])
		}
	}
}

func TestSettingsRowsUseLocalizedLabelsWithoutChangingSelection(t *testing.T) {
	settings := config.DefaultSettings()
	settings.UI.Language = config.Russian
	settings.Playback.ShuffleMode = config.ShuffleSource
	rows := BuildSettingsRowsWithCatalog(settings, 640, 480, i18n.MustLoad(i18n.Russian))
	if rows[SettingLanguage].Values[rows[SettingLanguage].Index] != "Русский" {
		t.Fatal("language self-name is not selected")
	}
	if rows[SettingShuffle].Index != 2 || rows[SettingShuffle].Values[2] != "Источник" {
		t.Fatalf("localized shuffle selection = %+v", rows[SettingShuffle])
	}
}

func TestEverySettingHasAContextDescriptionInEveryLanguage(t *testing.T) {
	for _, language := range config.AllLanguages() {
		catalog := i18n.MustLoad(i18n.Language(language))
		for index, row := range BuildSettingsRowsWithCatalog(config.DefaultSettings(), 640, 480, catalog) {
			if row.Header {
				continue
			}
			if got := settingDescription(catalog, index, row.Index); got == "" {
				t.Errorf("%s setting %q has no context description", language, row.Label)
			}
		}
	}
}

func TestSortSettingUsesPersistedGlobalOrder(t *testing.T) {
	settings := config.DefaultSettings()
	settings.UI.SortOrder = config.SortZA
	row := BuildSettingsRowsWithCatalog(settings, 640, 480, i18n.MustLoad(i18n.English))[SettingSort]
	if row.Label != "Sort" || row.Index != 2 || !equalStrings(row.Values, []string{"Source order", "A–Z", "Z–A"}) {
		t.Fatalf("sort row = %+v", row)
	}
}

func TestRotationDescriptionsExplainBehavior(t *testing.T) {
	for value, want := range []string{"manually", "visualizer decide", "selected interval"} {
		if got := settingDescription(i18n.MustLoad(i18n.English), SettingRotation, value); !strings.Contains(got, want) {
			t.Errorf("Rotation description %d = %q, want %q", value, got, want)
		}
	}
}

func TestLanguageSwitchPreservesUIStateAndInvalidatesText(t *testing.T) {
	o := &Overlay{
		catalog:        i18n.MustLoad(i18n.English),
		uiPage:         PageHelp,
		focusPanel:     1,
		settingsCursor: SettingTheme,
		helpView:       HelpViewState{TopicCursor: 3, EntryCursor: 1, InChildren: true},
		playingPath:    "/music/example.mod",
	}
	if err := o.SetLanguage(config.Russian); err != nil {
		t.Fatal(err)
	}
	if o.uiPage != PageHelp || o.focusPanel != 1 || o.settingsCursor != SettingTheme || o.helpView.TopicCursor != 3 || o.playingPath != "/music/example.mod" {
		t.Fatalf("language switch changed semantic state: %+v", o)
	}
	if !o.settingsDirty || !o.helpDirty || !o.pageIndicatorDirty || !o.breadcrumbDirty {
		t.Fatal("language switch did not invalidate all text projections")
	}
	if got := o.helpTopic(HelpQuickStart).Title; got != "Начало работы" {
		t.Fatalf("Russian Help title = %q", got)
	}
}

func TestMaxFrameRateSettingsRows(t *testing.T) {
	s := config.DefaultSettings()
	s.Graphics.FrameRate = config.FrameRateMax
	s.Graphics.Adaptive = false
	s.Graphics.RenderWidth, s.Graphics.RenderHeight = 1920, 1080
	rows := BuildSettingsRows(s, 1920, 1080)
	rate := rows[SettingFrameRate]
	if rate.Values[rate.Index] != "Max (60 FPS)" {
		t.Fatalf("frame rate row: %+v", rate)
	}
	resolution := rows[SettingResolution]
	if len(resolution.Values) <= 1 || resolution.Values[resolution.Index] != "1920x1080" {
		t.Fatalf("fixed resolution: %+v", resolution)
	}
}

func TestFrameRateChoicesAndDescriptions(t *testing.T) {
	rates := config.AllFrameRates()
	for _, language := range config.AllLanguages() {
		catalog := i18n.MustLoad(i18n.Language(language))
		for index, rate := range rates {
			settings := config.DefaultSettings()
			settings.Graphics.FrameRate = rate
			row := BuildSettingsRowsWithCatalogForRefresh(settings, 640, 480, 120, catalog)[SettingFrameRate]
			if len(row.Values) != len(rates) || row.Index != index {
				t.Fatalf("%v %v: %+v", language, rate, row)
			}
			if got := settingDescription(catalog, SettingFrameRate, index); got != catalog.Text(i18n.DescriptionFrameRate) {
				t.Fatalf("%v %v: mismatched description %q", language, rate, got)
			}
		}
	}
}

func TestDynamicFrameRateRowsFollowDisplayRefresh(t *testing.T) {
	settings := config.DefaultSettings()
	settings.Graphics.FrameRate = config.FrameRateMax

	rows60 := BuildSettingsRowsForRefresh(settings, 640, 480, 60)
	rate60 := rows60[SettingFrameRate]
	if got, want := rate60.Values, []string{"15 FPS", "20 FPS", "25 FPS", "30 FPS", "40 FPS", "50 FPS", "Max (60 FPS)"}; !equalStrings(got, want) {
		t.Fatalf("60 Hz values=%v, want %v", got, want)
	}
	if rate60.Index != len(rate60.Values)-1 {
		t.Fatalf("60 Hz index=%d, want Max index %d", rate60.Index, len(rate60.Values)-1)
	}
	settings.Graphics.FrameRate = config.FrameRate60
	rate60 = BuildSettingsRowsForRefresh(settings, 640, 480, 60)[SettingFrameRate]
	if rate60.Index != len(rate60.Values)-1 {
		t.Fatalf("equal fixed cap selected index=%d, want Max index %d", rate60.Index, len(rate60.Values)-1)
	}

	rows120 := BuildSettingsRowsForRefresh(settings, 640, 480, 120)
	rate120 := rows120[SettingFrameRate]
	if got, want := rate120.Values, []string{"15 FPS", "20 FPS", "25 FPS", "30 FPS", "40 FPS", "50 FPS", "60 FPS", "Max (120 FPS)"}; !equalStrings(got, want) {
		t.Fatalf("120 Hz values=%v, want %v", got, want)
	}
	if rate120.Index != 6 {
		t.Fatalf("120 Hz index=%d, want fixed 60 index 6", rate120.Index)
	}
}

func TestSettingsRowsPreserveEditingValueWhenRefreshChanges(t *testing.T) {
	settings := config.DefaultSettings()
	settings.Graphics.FrameRate = config.FrameRateMax
	o := &Overlay{
		settingsRows:    BuildSettingsRowsForRefresh(settings, 640, 480, 60),
		settingsCursor:  SettingFrameRate,
		settingsEditing: true,
	}
	o.settingsValueCursor = len(o.settingsRows[SettingFrameRate].Values) - 1
	o.SetSettingsRows(BuildSettingsRowsForRefresh(settings, 640, 480, 120), SettingFrameRate)
	row := o.settingsRows[SettingFrameRate]
	if o.settingsCursor != SettingFrameRate || o.settingsValueCursor != len(row.Values)-1 || row.Values[o.settingsValueCursor] != "Max (120 FPS)" {
		t.Fatalf("refresh change lost Max selection: cursor=%d value=%d row=%+v", o.settingsCursor, o.settingsValueCursor, row)
	}
	settings.Graphics.FrameRate = config.FrameRate60
	o = &Overlay{
		settingsRows:    BuildSettingsRowsForRefresh(settings, 640, 480, 120),
		settingsCursor:  SettingFrameRate,
		settingsEditing: true,
	}
	o.settingsValueCursor = 6 // fixed 60 FPS on a 120 Hz display
	o.SetSettingsRows(BuildSettingsRowsForRefresh(settings, 640, 480, 50), SettingFrameRate)
	row = o.settingsRows[SettingFrameRate]
	if o.settingsValueCursor != len(row.Values)-1 || row.Values[o.settingsValueCursor] != "Max (50 FPS)" {
		t.Fatalf("slower refresh did not map fixed cap to Max: cursor=%d row=%+v", o.settingsValueCursor, row)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLocalizedCacheChoicesPreserveSelection(t *testing.T) {
	catalog := i18n.MustLoad(i18n.Russian)
	settings := config.DefaultSettings()
	settings.TrackCache.Retention = config.CacheThirtyDays
	settings.TrackCache.MaxBytes = config.CacheUnlimited
	rows := BuildSettingsRowsWithCatalog(settings, 640, 480, catalog)
	retention, size := rows[SettingCacheRetention], rows[SettingCacheSize]
	if retention.Values[retention.Index] != "30 дней" || size.Values[size.Index] != "Без ограничений" {
		t.Fatalf("cache choices: %+v, %+v", retention, size)
	}
}

func TestResolutionDescriptionReflectsIndependentControls(t *testing.T) {
	settings := config.DefaultSettings()
	settings.Graphics.FrameRate = config.FrameRateMax
	settings.Graphics.Adaptive = false
	o := &Overlay{
		catalog:      i18n.MustLoad(i18n.English),
		settingsRows: BuildSettingsRows(settings, 640, 480),
	}
	if got := o.selectedSettingDescription(SettingResolution, 0); got != o.catalog.Text(i18n.DescriptionResolution) {
		t.Fatalf("resolution description = %q", got)
	}
}

func TestFilterDescriptionExplainsAlgorithms(t *testing.T) {
	got := settingDescription(i18n.MustLoad(i18n.English), SettingFilter, 0)
	if !strings.Contains(got, "Bilinear") || !strings.Contains(got, "Nearest neighbor") {
		t.Fatalf("filter description = %q, want both algorithms", got)
	}
}
