package ui

import (
	"fmt"

	"github.com/dendec/glitchscope/internal/config"
)

// Settings row indices — shared between BuildSettingsRows and applySettings.
// Headers occupy even slots; these indices point to the actual setting rows.
const (
	SettingShuffle         = 1
	SettingRepeat          = 2
	SettingPerformanceMode = 3
	SettingPresetTimer     = 4
	SettingResolution      = 5
	SettingFilter          = 6
	SettingTheme           = 7
	SettingTransparency    = 8
	SettingShowStats       = 9
	SettingBeatSensitivity = 10
)

// BuildSettingsRows creates SettingRow entries from the current config.
func BuildSettingsRows(s config.Settings, winW, winH int, renderScaleExplicit bool) []SettingRow {
	resolutions := config.ComputeResolutions(winW, winH)

	var resValues []string
	var resIndex int

	if !renderScaleExplicit {
		resValues = append(resValues, "Auto")
		if s.Graphics.Adaptive {
			resIndex = 0
		} else {
			resIndex = 1
			for i, r := range resolutions {
				if r.Width == s.Graphics.RenderWidth && r.Height == s.Graphics.RenderHeight {
					resIndex = i + 1
					break
				}
			}
		}
	} else {
		resIndex = 0
		for i, r := range resolutions {
			if r.Width == s.Graphics.RenderWidth && r.Height == s.Graphics.RenderHeight {
				resIndex = i
				break
			}
		}
	}

	for _, r := range resolutions {
		resValues = append(resValues, r.String())
	}

	if len(resolutions) == 0 && !renderScaleExplicit && len(resValues) == 1 {
		resValues = append(resValues, "N/A")
		resIndex = 0
	}

	filters := config.AllFilters()
	filterValues := make([]string, len(filters))
	filterIndex := 0
	for i, f := range filters {
		filterValues[i] = f.String()
		if f == s.Graphics.UpscaleFilter {
			filterIndex = i
		}
	}

	repeatValues := make([]string, len(config.AllRepeatModes()))
	repeatIndex := 0
	for i, m := range config.AllRepeatModes() {
		repeatValues[i] = m.String()
		if m == s.Playback.Repeat {
			repeatIndex = i
		}
	}

	presetValues := make([]string, len(config.AllPresetIntervals()))
	presetIndex := 0
	for i, p := range config.AllPresetIntervals() {
		presetValues[i] = p.String()
		if p == s.PresetInterval {
			presetIndex = i
		}
	}

	shuffleValues := make([]string, len(config.AllShuffleModes()))
	shuffleIndex := 0
	for i, m := range config.AllShuffleModes() {
		shuffleValues[i] = m.String()
		if m == s.Playback.ShuffleMode {
			shuffleIndex = i
		}
	}

	themeValues := make([]string, len(config.AllThemes()))
	themeIndex := 0
	for i, t := range config.AllThemes() {
		themeValues[i] = t.String()
		if t == s.UI.Theme {
			themeIndex = i
		}
	}

	transValues := make([]string, len(config.AllTransparencies()))
	transIndex := 0
	for i, t := range config.AllTransparencies() {
		transValues[i] = t.String()
		if t == s.UI.Transparency {
			transIndex = i
		}
	}

	beatSensitivities := config.AllBeatSensitivities()
	beatValues := make([]string, len(beatSensitivities))
	beatIndex := 0
	for i, sensitivity := range beatSensitivities {
		beatValues[i] = fmt.Sprintf("%.2g", sensitivity)
		if sensitivity == s.Graphics.BeatSensitivity {
			beatIndex = i
		}
	}

	perfModes := config.AllPerformanceModes()
	perfValues := make([]string, len(perfModes))
	perfIndex := 0
	for i, m := range perfModes {
		perfValues[i] = m.String()
		if m == s.Graphics.PerformanceMode {
			perfIndex = i
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
		// Appearance
		{Header: true, Label: "── Appearance ───"},
		{Label: "Theme", Values: themeValues, Index: themeIndex},
		{Label: "Transparency", Values: transValues, Index: transIndex},
		{Label: "Show stats", Values: []string{"Off", "On"}, Index: boolIndex(s.UI.ShowStats)},
		// Audio
		{Header: true, Label: "── Audio ───────"},
		{Label: "Beat sensitivity", Values: beatValues, Index: beatIndex},
	}
}

func boolIndex(v bool) int {
	if v {
		return 1
	}
	return 0
}
