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
		"── Visualization ──", "Performance", "Visualizer", "Rotation", "Resolution", "Filter", "Sensitivity",
		"── Appearance ───", "Language", "Theme", "Transparency", "Stats",
		"── Cache ───────", "Size", "Lifetime",
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
