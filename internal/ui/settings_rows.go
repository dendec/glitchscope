package ui

import "github.com/dendec/pmv/internal/config"

// BuildSettingsRows creates SettingRow entries from the current config.
func BuildSettingsRows(s config.Settings, winW, winH int) []SettingRow {
	resolutions := config.ComputeResolutions(winW, winH)
	resValues := make([]string, len(resolutions))
	resIndex := 0
	found := false
	for i, r := range resolutions {
		resValues[i] = r.String()
		if !found && r.Width == s.Graphics.RenderWidth && r.Height == s.Graphics.RenderHeight {
			resIndex = i
			found = true
		}
	}
	if !found && len(resolutions) > 0 {
		closest := config.ClosestResolution(resolutions, config.RenderResolution{Width: s.Graphics.RenderWidth, Height: s.Graphics.RenderHeight})
		for i, r := range resolutions {
			if r == closest {
				resIndex = i
				break
			}
		}
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

	return []SettingRow{
		{Label: "Render resolution", Values: resValues, Index: resIndex},
		{Label: "Upscale filter", Values: filterValues, Index: filterIndex},
		{Label: "Shuffle", Values: shuffleValues, Index: shuffleIndex},
		{Label: "Repeat", Values: repeatValues, Index: repeatIndex},
		{Label: "Preset auto-switch", Values: presetValues, Index: presetIndex},
		{Label: "Theme", Values: themeValues, Index: themeIndex},
		{Label: "Transparency", Values: transValues, Index: transIndex},
	}
}
