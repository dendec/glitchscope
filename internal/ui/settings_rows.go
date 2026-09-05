package ui

import (
	"fmt"

	"github.com/dendec/glitchscope/internal/config"
)

// Settings row indices — shared between BuildSettingsRows and applySettings.
// Each index points to the actual setting row (headers are skipped).
const (
	SettingShuffle         = 1
	SettingRepeat          = 2
	SettingPerformanceMode = 4
	SettingPresetTimer     = 5
	SettingResolution      = 6
	SettingFilter          = 7
	SettingBeatSensitivity = 8
	SettingTheme           = 10
	SettingTransparency    = 11
	SettingShowStats       = 12
)

// settingOpt is a setting whose String() produces a display label.
type settingOpt interface {
	String() string
}

// optionPair builds (values, selectedIndex) from a list of option values and
// the currently active value.  Used by every settings row builder to avoid
// repeating the same make-loop-findIndex pattern.
func optionPair[T settingOpt](all []T, current T) (values []string, index int) {
	currentStr := current.String()
	values = make([]string, len(all))
	for i, v := range all {
		values[i] = v.String()
		if v.String() == currentStr {
			index = i
		}
	}
	return values, index
}

// BuildSettingsRows creates SettingRow entries from the current config.
func BuildSettingsRows(s config.Settings, winW, winH int) []SettingRow {
	resolutions := config.ComputeResolutions(winW, winH)
	resValues, resIndex := buildResolutionValues(s, resolutions)

	filterValues, filterIndex := optionPair(config.AllFilters(), s.Graphics.UpscaleFilter)
	repeatValues, repeatIndex := optionPair(config.AllRepeatModes(), s.Playback.Repeat)
	presetValues, presetIndex := optionPair(config.AllPresetIntervals(), s.PresetInterval)
	shuffleValues, shuffleIndex := optionPair(config.AllShuffleModes(), s.Playback.ShuffleMode)
	themeValues, themeIndex := optionPair(config.AllThemes(), s.UI.Theme)
	transValues, transIndex := optionPair(config.AllTransparencies(), s.UI.Transparency)
	perfValues, perfIndex := optionPair(config.AllPerformanceModes(), s.Graphics.PerformanceMode)

	beatSensitivities := config.AllBeatSensitivities()
	beatValues := make([]string, len(beatSensitivities))
	beatIndex := 0
	for i, b := range beatSensitivities {
		beatValues[i] = fmt.Sprintf("%.2g", b)
		if b == s.Graphics.BeatSensitivity {
			beatIndex = i
		}
	}

	return []SettingRow{
		// Playback
		{Header: true, Label: "── Playback ────"},
		{Label: "Shuffle", Values: shuffleValues, Index: shuffleIndex},
		{Label: "Repeat", Values: repeatValues, Index: repeatIndex},
		// Visualization
		{Header: true, Label: "── Visualization ──"},
		{Label: "Performance mode", Values: perfValues, Index: perfIndex},
		{Label: "Preset auto-switch", Values: presetValues, Index: presetIndex},
		{Label: "Render resolution", Values: resValues, Index: resIndex},
		{Label: "Upscale filter", Values: filterValues, Index: filterIndex},
		{Label: "Beat sensitivity", Values: beatValues, Index: beatIndex},
		// Appearance
		{Header: true, Label: "── Appearance ───"},
		{Label: "Theme", Values: themeValues, Index: themeIndex},
		{Label: "Transparency", Values: transValues, Index: transIndex},
		{Label: "Show stats", Values: []string{"Off", "On"}, Index: boolIndex(s.UI.ShowStats)},
	}
}

// buildResolutionValues produces the resolution option list and selected index.
// "Auto" is always included as the first option so the user can switch back
// from a fixed resolution to adaptive mode.
func buildResolutionValues(s config.Settings, resolutions []config.RenderResolution) ([]string, int) {
	values := append([]string{"Auto"}, renderResolutionStrings(resolutions)...)
	if len(resolutions) == 0 {
		values = append(values, "N/A")
	}
	if s.Graphics.Adaptive {
		return values, 0
	}
	// Find the currently selected fixed resolution (index offset by 1
	// because "Auto" is always at position 0).
	for i, r := range resolutions {
		if r.Width == s.Graphics.RenderWidth && r.Height == s.Graphics.RenderHeight {
			return values, i + 1
		}
	}
	return values, 0
}

func renderResolutionStrings(resolutions []config.RenderResolution) []string {
	out := make([]string, len(resolutions))
	for i, r := range resolutions {
		out[i] = r.String()
	}
	return out
}

func boolIndex(v bool) int {
	if v {
		return 1
	}
	return 0
}
