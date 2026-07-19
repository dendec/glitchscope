package ui

import "github.com/dendec/mdpp/internal/config"

// BuildSettingsRows creates SettingRow entries from the current
// config and window dimensions. Call on page open and on window resize.
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

	shuffleValues := []string{"Off", "On"}
	shuffleIndex := 0
	if s.Playback.Shuffle {
		shuffleIndex = 1
	}

	return []SettingRow{
		{Label: "Render resolution", Values: resValues, Index: resIndex},
		{Label: "Upscale filter", Values: filterValues, Index: filterIndex},
		{Label: "Shuffle", Values: shuffleValues, Index: shuffleIndex},
		{Label: "Repeat", Values: repeatValues, Index: repeatIndex},
		{Label: "Preset auto-switch", Values: presetValues, Index: presetIndex},
	}
}
