package ui

import (
	"fmt"

	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/i18n"
)

// Settings row indices — shared between BuildSettingsRows and applySettings.
// Each index points to the actual setting row (headers are skipped).
const (
	SettingShuffle         = 1
	SettingRepeat          = 2
	SettingPerformanceMode = 4
	SettingVisualizer      = 5
	SettingRotation        = 6
	SettingResolution      = 7
	SettingFilter          = 8
	SettingBeatSensitivity = 9
	SettingLanguage        = 11
	SettingTheme           = 12
	SettingTransparency    = 13
	SettingShowStats       = 14
	SettingCacheSize       = 16
	SettingCacheRetention  = 17
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
	return BuildSettingsRowsWithCatalog(s, winW, winH, i18n.MustLoad(i18n.English))
}

// BuildSettingsRowsWithCatalog creates localized rows while preserving enum indices.
func BuildSettingsRowsWithCatalog(s config.Settings, winW, winH int, catalog i18n.Catalog) []SettingRow {
	resolutions := config.ComputeResolutions(winW, winH)
	resValues, resIndex := buildResolutionValuesWithLabels(s, resolutions, catalog.Text(i18n.ValueAuto), catalog.Text(i18n.ValueUnavailable))

	filterValues := []string{catalog.Text(i18n.ValueSmooth), catalog.Text(i18n.ValuePixel)}
	filterIndex := comparableIndex(config.AllFilters(), s.Graphics.UpscaleFilter)
	repeatValues := []string{catalog.Text(i18n.ValueOff), catalog.Text(i18n.ValueRepeatOne), catalog.Text(i18n.ValueRepeatAll)}
	repeatIndex := comparableIndex(config.AllRepeatModes(), s.Playback.Repeat)
	presetValues, presetIndex := localizedPresetIntervals(catalog, s.PresetInterval)
	shuffleValues := []string{catalog.Text(i18n.ValueOff), catalog.Text(i18n.ValueShuffleAlbum), catalog.Text(i18n.ValueShuffleSource), catalog.Text(i18n.ValueShuffleAll)}
	shuffleIndex := comparableIndex(config.AllShuffleModes(), s.Playback.ShuffleMode)
	themeValues, themeIndex := optionPair(config.AllThemes(), s.UI.Theme)
	transValues, transIndex := optionPair(config.AllTransparencies(), s.UI.Transparency)
	perfValues := []string{catalog.Text(i18n.ValuePerformance), catalog.Text(i18n.ValueBalanced), catalog.Text(i18n.ValueEco)}
	perfIndex := comparableIndex(config.AllPerformanceModes(), s.Graphics.PerformanceMode)
	cacheRetentionValues, cacheRetentionIndex := optionPair(config.AllCacheRetentions(), s.TrackCache.Retention)
	cacheSizeValues, cacheSizeIndex := optionPair(config.AllCacheSizeLimits(), s.TrackCache.MaxBytes)
	languages := config.AllLanguages()
	languageValues := make([]string, len(languages))
	languageIndex := 0
	for i, language := range languages {
		languageValues[i] = language.String()
		if language == s.UI.Language {
			languageIndex = i
		}
	}

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
		{Header: true, Label: "── " + catalog.Text(i18n.SettingsPlayback) + " ────"},
		{Label: catalog.Text(i18n.SettingsShuffle), Values: shuffleValues, Index: shuffleIndex},
		{Label: catalog.Text(i18n.SettingsRepeat), Values: repeatValues, Index: repeatIndex},
		// Visualization
		{Header: true, Label: "── " + catalog.Text(i18n.SettingsVisualization) + " ──"},
		{Label: catalog.Text(i18n.SettingsPerformance), Values: perfValues, Index: perfIndex},
		{Label: catalog.Text(i18n.SettingsVisualizer), Values: []string{catalog.Text(i18n.ValueOff), catalog.Text(i18n.ValueOn)}, Index: boolIndex(!s.Graphics.VisualizerOff)},
		{Label: catalog.Text(i18n.SettingsRotation), Values: presetValues, Index: presetIndex},
		{Label: catalog.Text(i18n.SettingsResolution), Values: resValues, Index: resIndex},
		{Label: catalog.Text(i18n.SettingsFilter), Values: filterValues, Index: filterIndex},
		{Label: catalog.Text(i18n.SettingsSensitivity), Values: beatValues, Index: beatIndex},
		// Appearance
		{Header: true, Label: "── " + catalog.Text(i18n.SettingsAppearance) + " ───"},
		{Label: catalog.Text(i18n.SettingsLanguage), Values: languageValues, Index: languageIndex},
		{Label: catalog.Text(i18n.SettingsTheme), Values: themeValues, Index: themeIndex},
		{Label: catalog.Text(i18n.SettingsTransparency), Values: transValues, Index: transIndex},
		{Label: catalog.Text(i18n.SettingsStats), Values: []string{catalog.Text(i18n.ValueOff), catalog.Text(i18n.ValueOn)}, Index: boolIndex(s.UI.ShowStats)},
		{Header: true, Label: "── " + catalog.Text(i18n.SettingsCache) + " ───────"},
		{Label: catalog.Text(i18n.SettingsSize), Values: cacheSizeValues, Index: cacheSizeIndex},
		{Label: catalog.Text(i18n.SettingsLifetime), Values: cacheRetentionValues, Index: cacheRetentionIndex},
	}
}

func comparableIndex[T comparable](all []T, current T) int {
	for i, value := range all {
		if value == current {
			return i
		}
	}
	return 0
}

func localizedPresetIntervals(catalog i18n.Catalog, current config.PresetInterval) ([]string, int) {
	all := config.AllPresetIntervals()
	values := make([]string, len(all))
	for i, interval := range all {
		switch interval {
		case config.PresetOff:
			values[i] = catalog.Text(i18n.ValueOff)
		case config.PresetAuto:
			values[i] = catalog.Text(i18n.ValueAuto)
		default:
			values[i] = interval.String()
		}
	}
	return values, comparableIndex(all, current)
}

// buildResolutionValues produces the resolution option list and selected index.
// "Auto" is always included as the first option so the user can switch back
// from a fixed resolution to adaptive mode.
func buildResolutionValuesWithLabels(s config.Settings, resolutions []config.RenderResolution, auto, unavailable string) ([]string, int) {
	values := append([]string{auto}, renderResolutionStrings(resolutions)...)
	if len(resolutions) == 0 {
		values = append(values, unavailable)
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

// settingDescription explains the selected value without exposing implementation details.
func settingDescription(catalog i18n.Catalog, setting, value int) string {
	switch setting {
	case SettingPerformanceMode:
		switch value {
		case 0:
			return catalog.Text(i18n.DescriptionPerformance)
		case 1:
			return catalog.Text(i18n.DescriptionBalanced)
		case 2:
			return catalog.Text(i18n.DescriptionEco)
		}
	case SettingVisualizer:
		if value == 0 {
			return catalog.Text(i18n.DescriptionVisualizerOff)
		}
		return catalog.Text(i18n.DescriptionVisualizerOn)
	case SettingRotation:
		if value == 0 {
			return catalog.Text(i18n.DescriptionRotationOff)
		}
		if value == 1 {
			return catalog.Text(i18n.DescriptionRotationAuto)
		}
		return catalog.Text(i18n.DescriptionRotationInterval)
	case SettingResolution:
		if value == 0 {
			return catalog.Text(i18n.DescriptionResolutionAuto)
		}
		return catalog.Text(i18n.DescriptionResolutionFixed)
	case SettingShuffle:
		switch value {
		case 0:
			return catalog.Text(i18n.DescriptionShuffleOff)
		case 1:
			return catalog.Text(i18n.DescriptionShuffleAlbum)
		case 2:
			return catalog.Text(i18n.DescriptionShuffleSource)
		case 3:
			return catalog.Text(i18n.DescriptionShuffleAll)
		}
	case SettingCacheRetention:
		if value == 0 {
			return catalog.Text(i18n.DescriptionCacheSession)
		}
		return catalog.Text(i18n.DescriptionCacheRetention)
	case SettingCacheSize:
		return catalog.Text(i18n.DescriptionCacheSize)
	}
	return ""
}
