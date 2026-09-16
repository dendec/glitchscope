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
		"── Playback ────", "Shuffle", "Repeat",
		"── Visualization ──", "Quality", "Visualizer", "Rotation", "Resolution", "Filter", "Sensitivity",
		"── Appearance ───", "Language", "Theme", "Transparency", "Stats",
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

func TestUltraSettingsRows(t *testing.T) {
	s := config.DefaultSettings()
	s.Graphics.PerformanceMode = config.PerfModeUltra
	s.Graphics.RenderWidth, s.Graphics.RenderHeight = 1920, 1080
	rows := BuildSettingsRows(s, 1920, 1080)
	perf := rows[SettingPerformanceMode]
	if perf.Values[perf.Index] != "Ultra" {
		t.Fatalf("performance row: %+v", perf)
	}
	resolution := rows[SettingResolution]
	if len(resolution.Values) <= 1 || resolution.Values[resolution.Index] != "1920x1080" {
		t.Fatalf("Ultra resolution: %+v", resolution)
	}
}

func TestPerformanceChoicesDescendAndKeepDescriptions(t *testing.T) {
	modes := []config.PerformanceMode{config.PerfModeUltra, config.PerfModePerformance, config.PerfModeBalanced, config.PerfModeEco}
	labels := []i18n.Key{i18n.ValueUltra, i18n.ValuePerformance, i18n.ValueBalanced, i18n.ValueEco}
	descriptions := []i18n.Key{i18n.DescriptionUltra, i18n.DescriptionPerformance, i18n.DescriptionBalanced, i18n.DescriptionEco}
	for _, language := range config.AllLanguages() {
		catalog := i18n.MustLoad(i18n.Language(language))
		for index, mode := range modes {
			settings := config.DefaultSettings()
			settings.Graphics.PerformanceMode = mode
			row := BuildSettingsRowsWithCatalog(settings, 640, 480, catalog)[SettingPerformanceMode]
			if len(row.Values) != len(modes) || row.Index != index || row.Values[index] != catalog.Text(labels[index]) {
				t.Fatalf("%v %v: %+v", language, mode, row)
			}
			if got := settingDescription(catalog, SettingPerformanceMode, index); got != catalog.Text(descriptions[index]) {
				t.Fatalf("%v %v: mismatched description %q", language, mode, got)
			}
		}
	}
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

func TestUltraResolutionDescriptionIsFixed(t *testing.T) {
	settings := config.DefaultSettings()
	settings.Graphics.PerformanceMode = config.PerfModeUltra
	o := &Overlay{
		catalog:      i18n.MustLoad(i18n.English),
		settingsRows: BuildSettingsRows(settings, 640, 480),
	}
	if got := o.selectedSettingDescription(SettingResolution, 0); got != o.catalog.Text(i18n.DescriptionResolutionFixed) {
		t.Fatalf("Ultra resolution description = %q", got)
	}
}
