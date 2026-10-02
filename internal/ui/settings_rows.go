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
	SettingSort            = 3
	SettingVisualizer      = 5
	SettingFrameRate       = 6
	SettingAdaptive        = 7
	SettingResolution      = 8
	SettingFilter          = 9
	SettingBeatSensitivity = 10
	SettingRotation        = 11
	SettingLanguage        = 13
	SettingTheme           = 14
	SettingTransparency    = 15
	SettingShowStats       = 16
	SettingPlayerBar       = 17
	SettingCacheSize       = 19
	SettingCacheRetention  = 20
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
	return BuildSettingsRowsWithCatalogForRefresh(s, winW, winH, 60, i18n.MustLoad(i18n.English))
}

// BuildSettingsRowsWithCatalog creates localized rows while preserving enum indices.
func BuildSettingsRowsWithCatalog(s config.Settings, winW, winH int, catalog i18n.Catalog) []SettingRow {
	return BuildSettingsRowsWithCatalogForRefresh(s, winW, winH, 60, catalog)
}

// BuildSettingsRowsForRefresh creates settings rows using the active display
// refresh rate for the dynamic frame-rate choices.
func BuildSettingsRowsForRefresh(s config.Settings, winW, winH int, refreshRate int32) []SettingRow {
	return BuildSettingsRowsWithCatalogForRefresh(s, winW, winH, refreshRate, i18n.MustLoad(i18n.English))
}

// BuildSettingsRowsWithCatalogForRefresh creates localized settings rows for
// a specific active display refresh rate.
func BuildSettingsRowsWithCatalogForRefresh(s config.Settings, winW, winH int, refreshRate int32, catalog i18n.Catalog) []SettingRow {
	resolutions := config.ComputeResolutions(winW, winH)
	resValues, resIndex := buildResolutionValuesWithLabels(s, resolutions, catalog.Text(i18n.ValueUnavailable))

	filterValues := []string{catalog.Text(i18n.ValueSmooth), catalog.Text(i18n.ValuePixel)}
	filterIndex := comparableIndex(config.AllFilters(), s.Graphics.UpscaleFilter)
	repeatValues := []string{catalog.Text(i18n.ValueOff), catalog.Text(i18n.ValueRepeatOne), catalog.Text(i18n.ValueRepeatAll)}
	repeatIndex := comparableIndex(config.AllRepeatModes(), s.Playback.Repeat)
	presetValues, presetIndex := localizedPresetIntervals(catalog, s.PresetInterval)
	shuffleValues := []string{catalog.Text(i18n.ValueOff), catalog.Text(i18n.ValueShuffleAlbum), catalog.Text(i18n.ValueShuffleSource), catalog.Text(i18n.ValueShuffleAll)}
	shuffleIndex := comparableIndex(config.AllShuffleModes(), s.Playback.ShuffleMode)
	sortValues := []string{catalog.Text(i18n.ValueSourceOrder), catalog.Text(i18n.ValueAZ), catalog.Text(i18n.ValueZA)}
	sortIndex := comparableIndex(config.AllSortOrders(), s.UI.SortOrder)
	themeValues, themeIndex := optionPair(config.AllThemes(), s.UI.Theme)
	transValues, transIndex := optionPair(config.AllTransparencies(), s.UI.Transparency)
	refreshRate = config.NormalizeRefreshRate(refreshRate)
	frameRates := config.FrameRateChoices(refreshRate)
	frameValues := make([]string, len(frameRates))
	for i, rate := range frameRates {
		if rate.IsMax() {
			frameValues[i] = fmt.Sprintf("%s (%d FPS)", catalog.Text(i18n.ValueMax), rate.Target(refreshRate))
		} else {
			frameValues[i] = rate.String()
		}
	}
	// A fixed cap equal to the current refresh is hidden from the values list,
	// so select the equivalent Max row while preserving the persisted cap until
	// the user confirms the settings.
	selectedFrameRate := s.Graphics.FrameRate
	if !selectedFrameRate.IsMax() && int32(selectedFrameRate) >= refreshRate {
		selectedFrameRate = config.FrameRateMax
	}
	frameIndex := comparableIndex(frameRates, selectedFrameRate)
	cacheRetentionValues, cacheRetentionIndex := localizedCacheRetentions(catalog, s.TrackCache.Retention)
	cacheSizes := config.AllCacheSizeLimits()
	cacheSizeValues, cacheSizeIndex := optionPair(cacheSizes, s.TrackCache.MaxBytes)
	for i, size := range cacheSizes {
		if size == config.CacheUnlimited {
			cacheSizeValues[i] = catalog.Text(i18n.CacheUnlimited)
		}
	}
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
		{Label: catalog.Text(i18n.SettingsSort), Values: sortValues, Index: sortIndex},
		// Visualization
		{Header: true, Label: "── " + catalog.Text(i18n.SettingsVisualization) + " ──"},
		{Label: catalog.Text(i18n.SettingsVisualizer), Values: []string{catalog.Text(i18n.ValueOff), catalog.Text(i18n.ValueOn)}, Index: boolIndex(!s.Graphics.VisualizerOff)},
		{Label: catalog.Text(i18n.SettingsFrameRate), Values: frameValues, Index: frameIndex},
		{Label: catalog.Text(i18n.SettingsAdaptiveResolution), Values: []string{catalog.Text(i18n.ValueOff), catalog.Text(i18n.ValueOn)}, Index: boolIndex(s.Graphics.Adaptive)},
		{Label: catalog.Text(i18n.SettingsResolution), Values: resValues, Index: resIndex},
		{Label: catalog.Text(i18n.SettingsFilter), Values: filterValues, Index: filterIndex},
		{Label: catalog.Text(i18n.SettingsSensitivity), Values: beatValues, Index: beatIndex},
		{Label: catalog.Text(i18n.SettingsRotation), Values: presetValues, Index: presetIndex},
		// Appearance
		{Header: true, Label: "── " + catalog.Text(i18n.SettingsAppearance) + " ───"},
		{Label: catalog.Text(i18n.SettingsLanguage), Values: languageValues, Index: languageIndex},
		{Label: catalog.Text(i18n.SettingsTheme), Values: themeValues, Index: themeIndex},
		{Label: catalog.Text(i18n.SettingsTransparency), Values: transValues, Index: transIndex},
		{Label: catalog.Text(i18n.SettingsStats), Values: []string{catalog.Text(i18n.ValueOff), catalog.Text(i18n.ValueOn)}, Index: boolIndex(s.UI.ShowStats)},
		{Label: catalog.Text(i18n.SettingsPlayerBar), Values: []string{catalog.Text(i18n.ValueOff), catalog.Text(i18n.ValueOn)}, Index: boolIndex(s.UI.ShowPlayerBar)},
		{Header: true, Label: "── " + catalog.Text(i18n.SourceDownloads) + " ───────"},
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

// buildResolutionValues produces the configured resolution list and selection.
// The selected value is always the user's fixed resolution or adaptive ceiling.
func buildResolutionValuesWithLabels(s config.Settings, resolutions []config.RenderResolution, unavailable string) ([]string, int) {
	values := renderResolutionStrings(resolutions)
	if len(resolutions) == 0 {
		values = append(values, unavailable)
	}
	for i, r := range resolutions {
		if r.Width == s.Graphics.RenderWidth && r.Height == s.Graphics.RenderHeight {
			return values, i
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
		return catalog.Text(i18n.DescriptionResolution)
	case SettingFrameRate:
		return catalog.Text(i18n.DescriptionFrameRate)
	case SettingAdaptive:
		if value == 0 {
			return catalog.Text(i18n.DescriptionAdaptiveOff)
		}
		return catalog.Text(i18n.DescriptionAdaptiveOn)
	case SettingFilter:
		return catalog.Text(i18n.DescriptionFilter)
	case SettingPlayerBar:
		return catalog.Text(i18n.DescriptionPlayerBar)
	case SettingSort:
		return catalog.Text(i18n.DescriptionSort)
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
	case SettingRepeat:
		return catalog.Text(i18n.DescriptionRepeat)
	case SettingBeatSensitivity:
		return catalog.Text(i18n.DescriptionSensitivity)
	case SettingLanguage:
		return catalog.Text(i18n.DescriptionLanguage)
	case SettingTheme:
		return catalog.Text(i18n.DescriptionTheme)
	case SettingTransparency:
		return catalog.Text(i18n.DescriptionTransparency)
	case SettingShowStats:
		return catalog.Text(i18n.DescriptionStats)
	case SettingCacheRetention:
		if value == 0 {
			return downloadsDescription(catalog, i18n.DescriptionCacheSession)
		}
		return downloadsDescription(catalog, i18n.DescriptionCacheRetention)
	case SettingCacheSize:
		return downloadsDescription(catalog, i18n.DescriptionCacheSize)
	}
	return ""
}

func downloadsDescription(catalog i18n.Catalog, policyKey i18n.Key) string {
	return catalog.Text(policyKey) + " " + catalog.Text(i18n.InfoFavoritesStayOffline)
}

// selectedSettingDescription keeps all visualizer controls independent: the
// resolution explanation describes the adaptive toggle separately.
func (o *Overlay) selectedSettingDescription(setting, value int) string {
	return settingDescription(o.catalog, setting, value)
}

func localizedCacheRetentions(catalog i18n.Catalog, current config.CacheRetention) ([]string, int) {
	all := config.AllCacheRetentions()
	keys := map[config.CacheRetention]i18n.Key{
		config.CacheDoNotKeep:  i18n.CacheNone,
		config.CacheOneDay:     i18n.CacheOneDay,
		config.CacheSevenDays:  i18n.CacheSevenDays,
		config.CacheThirtyDays: i18n.CacheThirtyDays,
		config.CacheNinetyDays: i18n.CacheNinetyDays,
		config.CacheSixMonths:  i18n.CacheSixMonths,
		config.CacheForever:    i18n.CacheForever,
	}
	values := make([]string, len(all))
	for i, retention := range all {
		values[i] = catalog.Text(keys[retention])
	}
	return values, comparableIndex(all, current)
}
