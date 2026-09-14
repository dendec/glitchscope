package app

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/filesystem"
	"github.com/dendec/glitchscope/internal/i18n"
	"github.com/dendec/glitchscope/internal/input"
	"github.com/dendec/glitchscope/internal/mic"
	"github.com/dendec/glitchscope/internal/player"
	"github.com/dendec/glitchscope/internal/presets"
	"github.com/dendec/glitchscope/internal/radio"
	"github.com/dendec/glitchscope/internal/ui"
)

func (a *App) handleAction(act input.Action, winW, winH int) {
	// Common actions — same regardless of UI mode.
	switch act {
	case input.ActionPlayPause:
		if a.pl != nil {
			a.pl.TogglePause()
		}
		return
	case input.ActionToggleOverlay:
		if a.overlay != nil {
			a.overlay.ToggleVisibility()
		}
		return
	case input.ActionToggleUI:
		if a.overlay != nil {
			wasVisible := a.overlay.UIVisible()
			a.overlay.ToggleUI()
			if !wasVisible {
				a.rememberMenuOpened()
				a.requestConnectivity()
			}
			// When opening the UI, navigate to the currently playing track.
			if !wasVisible && a.pl != nil {
				a.navigateOverlayToPlayingTrack()
			}
		}
		return
	case input.ActionSeekForward:
		if a.pl != nil && !player.IsRadio(a.pl.TrackPath()) {
			if err := a.pl.Seek(min(a.pl.Position()+5, a.pl.Duration())); err != nil {
				slog.Warn("seek forward failed", "error", err)
			}
		}
		return
	case input.ActionSeekBackward:
		if a.pl != nil && !player.IsRadio(a.pl.TrackPath()) {
			if err := a.pl.Seek(max(a.pl.Position()-5, 0)); err != nil {
				slog.Warn("seek backward failed", "error", err)
			}
		}
		return
	case input.ActionFavorite:
		a.handleFavorite()
		return
	case input.ActionFavoriteRemove:
		a.handleFavoriteRemove()
		return
	}

	if a.overlay != nil && a.overlay.UIVisible() {
		a.handleUIAction(act, winW, winH)
	} else {
		a.handleNormalAction(act)
	}
}

func (a *App) handleUIAction(act input.Action, winW, winH int) {
	switch act {
	case input.ActionCursorUp:
		a.overlay.CursorUp()

	case input.ActionCursorDown:
		a.overlay.CursorDown()

	case input.ActionFocusLeft:
		a.overlay.FocusLeft()

	case input.ActionFocusRight:
		a.overlay.FocusRight()

	case input.ActionSelect:
		if a.overlay.IsSettingsPage() {
			if a.overlay.Select() {
				a.applySettings(winW, winH)
			}
		} else if a.overlay.IsPresetsPage() {
			if a.overlay.Select() {
				if key := a.overlay.SelectedPresetKey(); key != "" {
					a.selectPreset(key)
				}
			}
		} else if a.overlay.IsNCMode() {
			if a.overlay.Select() && a.pl != nil {
				path := a.overlay.NCPlaySelected()
				if path != "" {
					if radio.IsPlaylistPath(path) {
						a.playRadioPlaylist(path)
						return
					}
					if fi, err := os.Stat(path); err == nil && fi.IsDir() {
						a.playDirectory(path)
					} else {
						a.playFile(path)
					}
				}
			}
			if a.overlay.NCConsumeDeleteConfirmed() {
				path := a.overlay.NCDeletePath()
				if path != "" {
					a.deleteNCPath(path)
				}
			}
		} else if a.overlay.IsFavoritesMode() {
			if a.overlay.Select() && a.pl != nil {
				path := a.overlay.SelectedTrackPath()
				if path != "" {
					a.playFavoriteFile(path)
				}
			}
		} else {
			selected := a.overlay.Select()
			if path := a.overlay.CatalogConsumeDeleteConfirmed(); path != "" {
				a.deleteCachedTrack(path)
				return
			}
			if a.overlay.ConsumeMicMenuRequest() {
				devices := mic.InputDevices()
				a.overlay.SetMicDevices(devices)
				a.overlay.ShowMicrophoneDevices(devices)
			} else if device, ok := a.overlay.ConsumeMicDeviceSelection(); ok {
				a.startMicCapture(device)
			} else if a.overlay.ConsumeMicStopRequest() {
				a.stopMicCapture()
			} else if kind, filter, ok := a.overlay.ConsumeRadioPageRequest(); ok {
				a.requestMoreRadio(kind, filter)
			} else if kind, filter, ok := a.overlay.ConsumeRadioBrowseRequest(); ok {
				a.requestRadio(kind, filter)
			} else if station, stations, kind, filter, ok := a.overlay.ConsumeRadioStationSelection(); ok {
				a.radioBrowseKind, a.radioBrowseFilter = kind, filter
				a.startRadioStation(station, stations)
			} else if selected && a.lib != nil && a.pl != nil {
				if a.overlay.IsCatalogMode() {
					albumName, path, tracks, idx := a.overlay.SelectedCatalogInfo()
					slog.Debug("Select: catalog mode", "albumName", albumName, "path", path)
					if path != "" {
						// Start playback first, then commit the playlist: a
						// failed start must not leave a stale catalog playlist
						// behind for next/prev to navigate.
						a.playTrack(path, albumName)
						a.playbackState.setPlaylist(tracks, idx, albumName)
					}
				} else if a.overlay.FocusPanel() == 0 {
					if path := a.lib.SelectAlbum(a.overlay.AlbumCursor()); path != "" {
						a.playTrack(path, a.lib.CurrentAlbum().Name)
					}
				} else {
					a.lib.SelectAlbum(a.overlay.AlbumCursor())
					if path := a.lib.SelectTrack(a.overlay.TrackCursor()); path != "" {
						a.playTrack(path, a.lib.CurrentAlbum().Name)
					}
				}
			}
		}
	case input.ActionBack:
		a.overlay.Back()

	case input.ActionNextPreset:
		// While the UI is open, L1/R1 page through Library/Settings/Presets
		// instead of quick-switching presets (Presets page covers that now).
		a.switchScreen(winW, winH, true)

	case input.ActionPrevPreset:
		a.switchScreen(winW, winH, false)

	case input.ActionFavorite:
		a.handleFavorite()

	case input.ActionFavoriteRemove:
		a.handleFavoriteRemove()
	}
}

func (a *App) deleteCachedTrack(path string) {
	if a.trackCache == nil || !a.trackCache.IsCached(path) {
		return
	}
	if a.favorites != nil && a.favorites.GetPlaylist(path) != "" {
		if a.overlay != nil {
			a.overlay.ShowMessage(i18n.InfoFavoritesStayOffline)
		}
		return
	}
	if a.pl != nil && a.pl.TrackPath() == path {
		a.pl.Stop()
	}
	if err := a.trackCache.Delete(path); err != nil {
		slog.Warn("delete cached track", "path", path, "error", err)
		// The file removal itself is authoritative. A manifest/index write
		// failure must not leave the UI and offline projection claiming that
		// a successfully deleted file is still available.
		a.refreshOfflineProjection()
		if !a.trackCache.IsCached(path) {
			if a.overlay != nil {
				a.overlay.ShowTrack("removed from cache")
				a.overlay.RefreshTrackCache()
			}
			return
		}
		if a.overlay != nil {
			a.overlay.ShowTrack("cache delete failed")
		}
		return
	}
	if a.overlay != nil {
		a.overlay.ShowTrack("removed from cache")
		a.overlay.RefreshTrackCache()
	}
	a.refreshOfflineProjection()
}

// switchScreen moves the overlay to the next/previous page and rebuilds
// settings rows when landing on the settings page.
func (a *App) switchScreen(winW, winH int, forward bool) {
	if forward {
		a.overlay.NextScreen()
	} else {
		a.overlay.PrevScreen()
	}
	if a.overlay.IsSettingsPage() {
		rows := ui.BuildSettingsRowsWithCatalog(*a.settings, winW, winH, a.overlay.Catalog())
		a.overlay.SetSettingsRows(rows, 0)
	}
	if a.overlay.IsLibraryPage() {
		a.overlay.RefreshTrackCache()
		a.requestConnectivity()
	}
}

func (a *App) handleNormalAction(act input.Action) {
	switch act {
	case input.ActionSelect:
		if a.overlay != nil {
			wasVisible := a.overlay.UIVisible()
			a.overlay.ToggleUI()
			if !wasVisible && a.pl != nil {
				a.navigateOverlayToPlayingTrack()
			}
		}

	case input.ActionBack:
		// ignored in normal mode

	case input.ActionCursorUp, input.ActionPrevAlbum:
		if path, album, ok := a.previousAlbum(); ok {
			a.playTrack(path, album)
		}

	case input.ActionCursorDown, input.ActionNextAlbum:
		if path, album, ok := a.nextAlbum(); ok {
			a.playTrack(path, album)
		}

	case input.ActionFocusLeft, input.ActionPrevTrack:
		if path, album, ok := a.previousTrack(); ok {
			a.playTrack(path, album)
		}

	case input.ActionFocusRight, input.ActionNextTrack:
		if path, album, ok := a.manualNext(a.settings.Playback); ok {
			a.playTrack(path, album)
		}

	case input.ActionNextPreset:
		a.loadPreset(a.presetIdx + 1)

	case input.ActionPrevPreset:
		a.loadPreset(a.presetIdx - 1)
	}
}

func (a *App) navigateOverlayToPlayingTrack() {
	if a.overlay == nil || a.pl == nil {
		return
	}
	path := a.pl.TrackPath()
	if player.IsRadio(path) && a.radio != nil {
		if station, ok := a.radio.Lookup(path); ok {
			a.overlay.SetPlayingInfo(station.DisplayName(), path)
			a.navigateOverlayToRadioStation(station)
			return
		}
	}
	a.overlay.SetPlayingInfo(a.currentAlbumName(), path)
	a.overlay.NavigateToTrack(path)
}

func (a *App) navigateOverlayToTrack(path string) {
	if a.overlay == nil {
		return
	}
	if player.IsRadio(path) && a.radio != nil {
		if station, ok := a.radio.Lookup(path); ok {
			a.navigateOverlayToRadioStation(station)
			return
		}
	}
	a.overlay.NavigateToTrack(path)
}

func (a *App) navigateOverlayToRadioStation(station radio.Station) {
	if a.radioBrowseKind != "" {
		a.overlay.NavigateToRadioStationInBrowse(station, a.radioBrowseKind, a.radioBrowseFilter)
		return
	}
	a.overlay.NavigateToRadioStation(station)
}

// currentPresetName returns the name of the current preset, or "" if none.
func (a *App) currentPresetName() string {
	if a.presetIdx >= 0 && a.presetIdx < len(a.presetNames) {
		return a.presetNames[a.presetIdx]
	}
	return ""
}

// loadPreset loads a preset by index (with wrapping) via transition.
func (a *App) loadPreset(idx int) {
	if len(a.presetNames) == 0 {
		return
	}
	n := len(a.presetNames)
	name := a.presetNames[((idx%n)+n)%n]
	a.transitionPreset(name)
}

// selectPreset confirms a preset on the Presets page without mutating the
// running projectM instance. It is loaded when the page or menu closes.
func (a *App) selectPreset(key string) {
	a.selectedPreset = key
}

// randPreset picks a random non-transition preset.
func (a *App) randPreset() {
	if len(a.presetNames) == 0 {
		return
	}

	// Build pool of normal (non-"!") presets.
	key := a.presetProfileKey("")
	var normals []string
	for _, n := range a.presetNames {
		key.name = n
		if n[0] != '!' && !a.presetTuning.profiles[key].heavy {
			normals = append(normals, n)
		}
	}
	if len(normals) == 0 {
		normals = a.presetNames
	}

	// Pick random target, avoid repeating current.
	target := normals[rand.Intn(len(normals))]
	if len(normals) > 1 {
		for target == a.presetNames[a.presetIdx] {
			target = normals[rand.Intn(len(normals))]
		}
	}

	a.transitionPreset(target)
}

// transitionPreset loads a preset immediately — single entry point for all
// preset changes (DRY).
func (a *App) transitionPreset(name string) {
	d, err := presets.Read(name)
	if err != nil {
		slog.Warn("preset transition read failed", "preset", name, "error", err)
		return
	}
	a.pm.LoadPresetData(string(d), true)
	a.suspendAdaptiveForPresetTransition(time.Now())
	a.applyPresetName(name)
	a.activatePresetProfile(name, d)
}

// applyPresetName updates the preset index and overlay after a load.
func (a *App) applyPresetName(name string) {
	for i, n := range a.presetNames {
		if n == name {
			a.presetIdx = i
			break
		}
	}
	if a.overlay != nil {
		a.overlay.SetPresetName(name)
	}
	slog.Info("preset", "name", name)
}

// applySettings reads confirmed settings rows and applies changes.
func (a *App) applySettings(winW, winH int) {
	rows := a.overlay.SettingsRows()
	if len(rows) <= ui.SettingCacheRetention {
		return
	}

	languages := config.AllLanguages()
	if idx := rows[ui.SettingLanguage].Index; idx >= 0 && idx < len(languages) {
		language := languages[idx]
		if language != a.settings.UI.Language {
			if err := a.overlay.SetLanguage(language); err != nil {
				slog.Warn("apply UI language", "language", language, "error", err)
			} else {
				a.settings.UI.Language = language
			}
		}
	}

	resIndex := rows[ui.SettingResolution].Index
	resolutions := config.ComputeResolutions(winW, winH)

	if resIndex == 0 {
		a.settings.Graphics.Adaptive = true
		a.resetAdaptiveState(winW, winH)
	} else {
		a.settings.Graphics.Adaptive = false
		fixedIdx := resIndex - 1
		if fixedIdx >= 0 && fixedIdx < len(resolutions) {
			a.applyRenderResolution(resolutions[fixedIdx])
		}
		a.resetAdaptiveCounters()
	}

	filterIndex := rows[ui.SettingFilter].Index
	filters := config.AllFilters()
	if filterIndex >= 0 && filterIndex < len(filters) {
		a.settings.Graphics.UpscaleFilter = filters[filterIndex]
		a.rt.SetNearest(a.settings.Graphics.UpscaleFilter.IsNearest())
	}

	perfModes := config.AllPerformanceModes()
	if idx := rows[ui.SettingPerformanceMode].Index; idx >= 0 && idx < len(perfModes) {
		newMode := perfModes[idx]
		if newMode != a.settings.Graphics.PerformanceMode {
			a.settings.Graphics.PerformanceMode = newMode
			params := newMode.Params()
			a.vizClock.framePeriod = time.Second / time.Duration(params.VisualizerFPS)
			if a.preview != nil {
				a.preview.SetFPS(params.VisualizerFPS)
			}
			a.resetAdaptiveState(winW, winH)
		}
	}

	visualizerOff := rows[ui.SettingVisualizer].Index == 0
	if visualizerOff != a.settings.Graphics.VisualizerOff {
		a.settings.Graphics.VisualizerOff = visualizerOff
		a.vizClock.Reset()
		a.resetAdaptiveCounters()
		a.presentRequested = true
	}
	cacheRetentions := config.AllCacheRetentions()
	if idx := rows[ui.SettingCacheRetention].Index; idx >= 0 && idx < len(cacheRetentions) {
		a.settings.TrackCache.Retention = cacheRetentions[idx]
	}
	cacheSizes := config.AllCacheSizeLimits()
	if idx := rows[ui.SettingCacheSize].Index; idx >= 0 && idx < len(cacheSizes) {
		a.settings.TrackCache.MaxBytes = cacheSizes[idx]
	}
	a.pruneTrackCache()

	shuffleModes := config.AllShuffleModes()
	if rows[ui.SettingShuffle].Index >= 0 && rows[ui.SettingShuffle].Index < len(shuffleModes) {
		newMode := shuffleModes[rows[ui.SettingShuffle].Index]
		if newMode != a.settings.Playback.ShuffleMode {
			a.settings.Playback.ShuffleMode = newMode
			a.playbackState.shuffle.reset()
			// Lazy-build the shuffle catalog if it was skipped at startup.
			if newMode != config.ShuffleOff {
				a.ensureShuffleCatalog()
			}
		}
	}

	repeatModes := config.AllRepeatModes()
	if rows[ui.SettingRepeat].Index >= 0 && rows[ui.SettingRepeat].Index < len(repeatModes) {
		a.settings.Playback.Repeat = repeatModes[rows[ui.SettingRepeat].Index]
	}

	presetIntervals := config.AllPresetIntervals()
	if rows[ui.SettingRotation].Index >= 0 && rows[ui.SettingRotation].Index < len(presetIntervals) {
		a.settings.PresetInterval = presetIntervals[rows[ui.SettingRotation].Index]
		a.pm.SetHardCutEnabled(!a.settings.Graphics.VisualizerOff && a.settings.PresetInterval == config.PresetAuto)
		if a.settings.PresetInterval != config.PresetAuto {
			a.presetSwitch.Store(false)
		}
		a.resetPresetTicker()
	}

	themes := config.AllThemes()
	if rows[ui.SettingTheme].Index >= 0 && rows[ui.SettingTheme].Index < len(themes) {
		a.settings.UI.Theme = themes[rows[ui.SettingTheme].Index]
	}

	transparencies := config.AllTransparencies()
	if rows[ui.SettingTransparency].Index >= 0 && rows[ui.SettingTransparency].Index < len(transparencies) {
		a.settings.UI.Transparency = transparencies[rows[ui.SettingTransparency].Index]
	}

	newShowStats := rows[ui.SettingShowStats].Index == 1
	if newShowStats != a.settings.UI.ShowStats {
		a.settings.UI.ShowStats = newShowStats
		a.overlay.SetShowFPS(newShowStats)
	}

	beatSensitivities := config.AllBeatSensitivities()
	if index := rows[ui.SettingBeatSensitivity].Index; index >= 0 && index < len(beatSensitivities) {
		a.settings.Graphics.BeatSensitivity = beatSensitivities[index]
		a.pm.SetBeatSensitivity(a.settings.Graphics.BeatSensitivity)
	}

	a.overlay.SetTheme(a.settings.UI.Theme, int(a.settings.UI.Transparency))

	if err := config.SaveSettings(a.settingsPath, *a.settings); err != nil {
		slog.Warn("settings save", "error", err)
	} else {
		slog.Debug("settings saved", "path", a.settingsPath)
	}

	rows = ui.BuildSettingsRowsWithCatalog(*a.settings, winW, winH, a.overlay.Catalog())
	a.overlay.SetSettingsRows(rows, a.overlay.SettingsCursor())
}

// playTrack starts playback of a track and shows the notification.
func (a *App) playTrack(path, album string) {
	a.failedTracks = nil
	a.startTrack(path, album)
}

// startTrack preserves the failure budget while recovering from a load error.
func (a *App) startTrack(path, album string) {
	a.stopMicCapture() // any playback wins over microphone input
	a.configureTrackerRenderBudget()
	if player.IsRadio(path) && a.radio != nil {
		if station, ok := a.radio.Lookup(path); ok {
			a.beginRadioMetadata(station)
		}
	}

	if a.overlay != nil {
		label := player.TrackTitle(path)
		if player.IsRadio(path) && a.radio != nil {
			if station, ok := a.radio.Lookup(path); ok {
				label = station.DisplayName()
			}
		}
		if album != "" {
			label = album + " — " + label
		}
		a.overlay.ShowTrack(label)
	}

	if !a.playbackState.play(path) {
		return
	}
	slog.Info("now loading", "track", path, "album", album)
}

func (a *App) requestRadio(kind radio.BrowseKind, filter string) {
	if a.radio == nil {
		return
	}
	if kind == radio.BrowseRandom {
		a.requestConnectivity()
		a.radio.Begin(a.appCtx, kind, filter)
		return
	}
	if kind == radio.BrowseTag || kind == radio.BrowseLanguage || kind == radio.BrowseCountry {
		if filter == "" {
			values, stale := a.radio.Values(kind)
			if values != nil {
				a.overlay.SetRadioValues(kind, values, a.radio.ValueCounts(kind))
			}
			if !stale {
				return
			}
		} else if stations, stale := a.radio.Snapshot(kind, filter); stations != nil {
			a.overlay.SetRadioStations(kind, filter, stations, a.radio.HasMore(kind, filter))
			if !stale {
				return
			}
		}
	} else if stations, stale := a.radio.Snapshot(kind, filter); stations != nil {
		a.overlay.SetRadioStations(kind, filter, stations, a.radio.HasMore(kind, filter))
		if !stale {
			return
		}
	}
	a.radio.Begin(a.appCtx, kind, filter)
}

func (a *App) requestMoreRadio(kind radio.BrowseKind, filter string) {
	if a.radio == nil {
		return
	}
	offset, ok := a.radio.NextPage(kind, filter)
	if !ok {
		return
	}
	a.radio.BeginPage(a.appCtx, kind, filter, offset)
}

func (a *App) startRadioStation(station radio.Station, queue ...[]radio.Station) {
	if a.radio == nil || strings.TrimSpace(station.StationUUID) == "" || station.StreamURL() == "" {
		return
	}
	a.requestConnectivity()
	stations := []radio.Station{station}
	if len(queue) > 0 && len(queue[0]) > 0 {
		stations = append([]radio.Station(nil), queue[0]...)
		if slices.IndexFunc(stations, func(candidate radio.Station) bool { return candidate.Path() == station.Path() }) < 0 {
			stations = append(stations, station)
		}
	}
	a.radio.AddTransientStations(stations)
	paths := make([]string, len(stations))
	for i := range stations {
		paths[i] = stations[i].Path()
	}
	idx := slices.Index(paths, station.Path())
	if idx < 0 {
		return
	}
	a.playbackState.setPlaylist(paths, idx, "Radio")
	path := station.Path()
	if a.overlay != nil {
		a.overlay.SetPlayingInfo(station.DisplayName(), path)
	}
	a.playTrack(path, "Radio")
}

func (a *App) beginRadioMetadata(station radio.Station) {
	if a.overlay != nil {
		a.overlay.SetRadioNowPlaying(station.Path(), "")
	}
	a.beginRadioFavicon(station)
}

func (a *App) beginRadioFavicon(station radio.Station) {
	if a.radio == nil || strings.TrimSpace(station.Favicon) == "" {
		return
	}
	path := station.Path()
	if a.radioFaviconRequested[path] {
		return
	}
	select {
	case a.radioFaviconSlots <- struct{}{}:
	default:
		return
	}
	a.radioFaviconRequested[path] = true
	a.radioFaviconWG.Add(1)
	go func() {
		defer a.radioFaviconWG.Done()
		defer func() { <-a.radioFaviconSlots }()
		data, err := a.radio.FetchFavicon(a.appCtx, station.Favicon)
		var bitmap *image.RGBA
		if err == nil && len(data) > 0 {
			bitmap, err = ui.DecodeRadioFavicon(data)
		}
		select {
		case a.radioFavicons <- radioFaviconEvent{path: path, data: bitmap, err: err}:
		case <-a.appCtx.Done():
		}
	}()
}

func (a *App) playRadioPlaylist(path string) {
	stations, err := radio.ParsePlaylist(path)
	if err != nil || len(stations) == 0 {
		if err != nil {
			slog.Warn("radio playlist", "path", path, "error", err)
		}
		if a.overlay != nil {
			a.overlay.ShowTrack(a.overlay.Catalog().Text(i18n.RadioEmpty))
		}
		return
	}
	a.radioBrowseKind, a.radioBrowseFilter = "", ""
	a.radio.AddStations(stations)
	a.startRadioStation(stations[0], stations)
	a.playbackState.playlistAlbum = filepath.Base(path)
}

// currentAlbumName returns the display name of the currently playing album,
// or "" when no album is loaded.
func (a *App) currentAlbumName() string {
	if a.lib == nil {
		return ""
	}
	return a.lib.CurrentAlbum().Name
}

// startMicCapture starts capture from one selected input device. Capture feeds the
// visualizer directly — the mic stream is not played through the speakers
// (feedback) and SoLoud playback is stopped while capturing.
func (a *App) startMicCapture(device string) {
	a.stopMicCapture()
	if a.pl != nil {
		a.pl.Stop()
		a.pruneTrackCache()
	}
	c, err := mic.OpenDevice(device)
	if err != nil {
		slog.Warn("mic open failed", "device", device, "error", err)
		if a.overlay != nil {
			a.overlay.ShowTrack("microphone unavailable")
		}
		return
	}
	a.mic = c
	if a.overlay != nil {
		devices := mic.InputDevices()
		a.overlay.SetMicDevices(devices)
		a.overlay.SetMicActive(true)
		a.overlay.ShowMicrophoneDevices(devices)
		a.overlay.ShowTrack("microphone: " + device)
	}
	slog.Info("mic capture started", "device", device, "backend", c.Backend(), "rate", c.Rate(), "channels", c.Channels())
}

func (a *App) stopMicCapture() {
	if a.mic == nil {
		return
	}
	a.mic.Close()
	a.mic = nil
	if a.overlay != nil {
		devices := mic.InputDevices()
		a.overlay.SetMicDevices(devices)
		a.overlay.SetMicActive(false)
		a.overlay.ShowMicrophoneDevices(devices)
	}
	slog.Info("mic capture stopped")
}

// playDirectory recursively walks dir, collects supported audio files,
// replaces the playlist, and starts playback from the first track.
func (a *App) playDirectory(dirPath string) {
	files, err := walkAudioFiles(dirPath)
	if err != nil {
		slog.Warn("walk directory failed", "path", dirPath, "error", err)
		if a.overlay != nil {
			a.overlay.ShowTrack(audioScanMessage(err))
		}
		return
	}
	if len(files) == 0 {
		if a.overlay != nil {
			a.overlay.ShowTrack("no playable files")
		}
		return
	}
	album := filepath.Base(dirPath)
	if a.playbackState.setPlaylist(files, 0, album) {
		a.playTrack(files[0], album)
	}
}

// playFile walks the parent directory of path, builds a sorted playlist,
// positions the cursor on the selected file, and starts playback.
func (a *App) playFile(path string) {
	dir := filepath.Dir(path)
	files, err := walkAudioFiles(dir)
	if err != nil {
		slog.Warn("walk directory failed", "path", dir, "error", err)
		if a.overlay != nil {
			a.overlay.ShowTrack(audioScanMessage(err))
		}
		return
	}
	if len(files) == 0 {
		if a.overlay != nil {
			a.overlay.ShowTrack("no playable files")
		}
		return
	}
	idx := slices.Index(files, path)
	if idx < 0 {
		idx = 0
	}
	album := filepath.Base(dir)
	if a.playbackState.setPlaylist(files, idx, album) {
		a.playTrack(files[idx], album)
	}
}

// playFavoriteFile plays a track from a favorites playlist. The queue becomes
// a snapshot of the entire playlist in order; subsequent favorites changes
// don't affect the running queue. Missing local files are auto-skipped and
// removed from favorites.
func (a *App) playFavoriteFile(path string) {
	if a.favorites == nil {
		return
	}
	if player.IsRadio(path) && a.radio != nil {
		if station, ok := a.radio.Lookup(path); ok {
			a.radioBrowseKind, a.radioBrowseFilter = "", ""
			a.startRadioStation(station)
			return
		}
	}
	// Determine which playlist this track belongs to.
	kind := a.favorites.GetPlaylist(path)
	if kind == "" {
		// Not in any playlist — fall back to single-file playback.
		if player.IsLocalPath(path) {
			if _, err := os.Stat(path); err != nil {
				slog.Warn("favorites: track missing, removing", "path", path)
				_ = a.favorites.Remove(path)
				return
			}
		}
		a.playFile(path)
		return
	}
	// Snapshot the entire playlist as the playback queue.
	tracks := a.favorites.Tracks(kind)
	if len(tracks) == 0 {
		return
	}
	// Auto-skip missing local files and remove them from favorites.
	for idx := slices.Index(tracks, path); idx < len(tracks); idx++ {
		if player.IsLocalPath(tracks[idx]) {
			if _, err := os.Stat(tracks[idx]); err != nil {
				slog.Warn("favorites: track missing, skipping", "path", tracks[idx])
				_ = a.favorites.Remove(tracks[idx])
				if idx < len(tracks)-1 {
					continue
				}
				return // last track is missing, nothing to play
			}
		}
		// Found an available track.
		if a.playbackState.setPlaylist(tracks, idx, kind.String()) {
			a.playTrack(tracks[idx], "")
		}
		return
	}
}

type audioScanError struct {
	status filesystem.Status
	err    error
}

func (e *audioScanError) Error() string { return fmt.Sprintf("audio scan %s: %v", e.status, e.err) }
func (e *audioScanError) Unwrap() error { return e.err }

func audioScanMessage(err error) string {
	var scanErr *audioScanError
	if errors.As(err, &scanErr) && scanErr.status == filesystem.StatusPartial {
		return "directory scan incomplete"
	}
	return "directory scan failed"
}

// deleteNCPath validates and deletes a path, then handles post-delete effects:
// stop playback if playing track is inside, prune playlist, rescan library, sync overlay.
func (a *App) deleteNCPath(path string) {
	if a.deleteSvc == nil {
		return
	}
	cleaned := filepath.Clean(path)

	// Stat before deletion to know if it was a file.
	info, statErr := os.Stat(cleaned)
	isFile := statErr == nil && !info.IsDir()
	parentDir := filepath.Dir(cleaned)

	if err := a.deleteSvc.Delete(path); err != nil {
		slog.Warn("delete failed", "path", path, "error", err)
		if a.overlay != nil {
			a.overlay.ShowTrack("delete failed")
		}
		return
	}

	// Stop playback if playing track is inside deleted path.
	if a.pl != nil {
		playing := a.pl.TrackPath()
		if playing != "" && (playing == cleaned || strings.HasPrefix(playing, cleaned+string(filepath.Separator))) {
			a.pl.Stop()
			a.playbackState.clearPlaylist()
		} else {
			a.prunePlaylist(cleaned)
		}
	}

	// Remove parent's .gsa_meta.json for file deletion (stale cache).
	if isFile {
		os.Remove(filepath.Join(parentDir, ".gsa_meta.json"))
	}

	// Rescan library.
	if a.lib != nil {
		if err := a.lib.Rescan(a.findMusicDir()); err != nil {
			slog.Error("library rescan failed after delete", "path", cleaned, "error", err)
			if a.overlay != nil {
				a.overlay.ShowTrack("library rescan failed")
			}
			return
		}
		// Invalidate shuffle order — the library content has changed.
		a.playbackState.shuffle.reset()
		// Rescan succeeded (Library.Rescan only mutates Albums on an OK
		// scan), so the local shuffle source may be safely rebuilt too.
		a.updateLocalShuffleSource(a.lib.Albums, filesystem.StatusOK)
		a.buildShuffleCatalog()
	}

	// Sync NC overlay.
	if a.overlay != nil {
		a.overlay.NCSync()
	}

	slog.Info("deleted", "path", cleaned)
	if a.overlay != nil {
		a.overlay.ShowTrack("deleted")
	}
}

// walkAudioFiles recursively collects supported audio files under root,
// sorted lexically by full path. Skips symlinks, dotfiles, artwork, metadata.
func walkAudioFiles(root string) ([]string, error) {
	var files []string
	report := filesystem.Walk(context.Background(), root, filesystem.Options{
		Include: filesystem.IsAudioFile,
		Descend: func(entry filesystem.Entry) bool {
			return !strings.HasPrefix(entry.Name, ".")
		},
	}, func(entry filesystem.Entry) {
		files = append(files, entry.Path)
	})
	if report.Status != filesystem.StatusOK {
		for _, issue := range report.Issues {
			slog.Warn("audio scan issue", "path", issue.Path, "error", issue.Err, "status", report.Status)
		}
		return nil, &audioScanError{status: report.Status, err: fmt.Errorf("walk %s: %w", root, report.Err())}
	}
	return files, nil
}

// handleFavorite cycles the focused track through the favorites playlists.
func (a *App) handleFavorite() {
	if a.favorites == nil {
		slog.Debug("favorites: toggle ignored", "reason", "store unavailable")
		return
	}
	if !a.favorites.Writable() {
		slog.Warn("favorites: toggle ignored", "reason", "store read-only")
		return
	}
	// Do nothing if browsing inside a favorites playlist.
	if a.overlay != nil && a.overlay.IsFavoritesMode() {
		slog.Debug("favorites: toggle ignored", "reason", "inside playlist")
		return
	}
	path := ""
	if a.overlay != nil && a.overlay.UIVisible() {
		path = a.overlay.SelectedTrackPath()
	}
	if path == "" && a.pl != nil {
		path = a.pl.TrackPath()
	}
	if path == "" {
		slog.Debug("favorites: toggle ignored", "reason", "no track selected")
		return
	}
	playlist, err := a.favorites.Cycle(path)
	if err != nil {
		slog.Warn("favorites: cycle failed", "path", path, "error", err)
		return
	}
	// A transient Radio Browser/random station is intentionally absent from
	// query-imported.json. Once the user explicitly favorites it, persist its
	// descriptor so the Favorites view can resolve it after a restart.
	if playlist != "" && player.IsRadio(path) && a.radio != nil {
		if station, ok := a.radio.Lookup(path); ok {
			a.radio.AddStations([]radio.Station{station})
		}
	}
	slog.Info("favorites: track updated", "path", path, "playlist", playlist)
	if a.overlay != nil {
		a.overlay.RefreshFavorites()
	}
}

// handleFavoriteRemove removes the focused track from any favorites playlist.
func (a *App) handleFavoriteRemove() {
	if a.favorites == nil {
		slog.Debug("favorites: remove ignored", "reason", "store unavailable")
		return
	}
	if !a.favorites.Writable() {
		slog.Warn("favorites: remove ignored", "reason", "store read-only")
		return
	}
	// Do nothing if browsing inside a favorites playlist.
	if a.overlay != nil && a.overlay.IsFavoritesMode() {
		slog.Debug("favorites: remove ignored", "reason", "inside playlist")
		return
	}
	path := ""
	if a.overlay != nil && a.overlay.UIVisible() {
		path = a.overlay.SelectedTrackPath()
	}
	if path == "" && a.pl != nil {
		path = a.pl.TrackPath()
	}
	if path == "" {
		slog.Debug("favorites: remove ignored", "reason", "no track selected")
		return
	}
	if err := a.favorites.Remove(path); err != nil {
		slog.Warn("favorites: remove failed", "path", path, "error", err)
		return
	}
	slog.Info("favorites: track removed", "path", path)
	if a.overlay != nil {
		a.overlay.RefreshFavorites()
	}
}

func (a *App) rememberMenuOpened() {
	if a.settings == nil || a.settings.UI.MenuOpened {
		return
	}
	a.settings.UI.MenuOpened = true
	if err := config.SaveSettings(a.settingsPath, *a.settings); err != nil {
		slog.Warn("save menu acknowledgement", "error", err)
	}
}

func (a *App) notifyRadioStarted(path string) {
	if a.radio == nil || !player.IsRadio(path) {
		return
	}
	station, ok := a.radio.Lookup(path)
	if !ok || strings.HasPrefix(station.StationUUID, "local-") {
		return
	}
	if a.radioClickCancel != nil {
		a.radioClickCancel()
	}
	ctx, cancel := context.WithCancel(a.appCtx)
	a.radioClickCancel = cancel
	a.radioClickWG.Add(1)
	go func() {
		defer a.radioClickWG.Done()
		defer cancel()
		if err := a.radio.Click(ctx, station.StationUUID); err != nil && ctx.Err() == nil {
			slog.Warn("radio click counter failed", "station", station.StationUUID, "error", err)
		}
	}()
}
