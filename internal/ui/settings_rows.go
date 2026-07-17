package ui

import "github.com/dendec/mdpp/internal/config"

// BuildSettingsRows creates SettingRow entries from the current graphics
// config and window dimensions. Call on page open and on window resize.
func BuildSettingsRows(gs config.GraphicsSettings, winW, winH int) []SettingRow {
	resolutions := config.ComputeResolutions(winW, winH)
	resValues := make([]string, len(resolutions))
	resIndex := 0
	found := false
	for i, r := range resolutions {
		resValues[i] = r.String()
		if !found && r.Width == gs.RenderWidth && r.Height == gs.RenderHeight {
			resIndex = i
			found = true
		}
	}
	if !found && len(resolutions) > 0 {
		closest := config.ClosestResolution(resolutions, config.RenderResolution{Width: gs.RenderWidth, Height: gs.RenderHeight})
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
		if f == gs.UpscaleFilter {
			filterIndex = i
		}
	}

	return []SettingRow{
		{Label: "Render resolution", Values: resValues, Index: resIndex},
		{Label: "Upscale filter", Values: filterValues, Index: filterIndex},
	}
}
