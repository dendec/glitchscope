package ui

import (
	"strings"
	"testing"

	"github.com/dendec/glitchscope/internal/config"
)

func TestSettingsRowsUseCompactSectionsAndLabels(t *testing.T) {
	rows := BuildSettingsRows(config.DefaultSettings(), 640, 480)
	want := []string{
		"── Playback ────", "Shuffle", "Repeat",
		"── Visualization ──", "Performance", "Visualizer", "Rotation", "Resolution", "Filter", "Sensitivity",
		"── Appearance ───", "Theme", "Transparency", "Stats",
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

func TestRotationDescriptionsExplainBehavior(t *testing.T) {
	for value, want := range []string{"manually", "visualizer decide", "selected interval"} {
		if got := settingDescription(SettingRotation, value); !strings.Contains(got, want) {
			t.Errorf("Rotation description %d = %q, want %q", value, got, want)
		}
	}
}
