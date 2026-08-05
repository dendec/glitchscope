package app

import (
	"log/slog"
	"math/rand"
	"strings"

	"github.com/dendec/pmv/internal/config"
	"github.com/dendec/pmv/internal/input"
	"github.com/dendec/pmv/internal/player"
	"github.com/dendec/pmv/internal/presets"
	"github.com/dendec/pmv/internal/ui"
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
			a.overlay.ToggleUI() // always show/hide the UI, never cycles pages
			if !wasVisible && a.overlay.UIVisible() && a.lib != nil {
				// Opening the playlist screen: start with the cursor on the
				// track that is currently playing.
				a.overlay.FocusPlayingTrack(a.lib.CurrentAlbumIndex(), a.lib.CurrentTrackIndex())
			}
		}
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
					a.loadPresetByKey(key)
				}
			}
		} else if a.overlay.Select() && a.lib != nil && a.pl != nil {
			if a.overlay.FocusPanel() == 0 {
				if path := a.lib.SelectAlbum(a.overlay.AlbumCursor()); path != "" {
					a.playTrack(path, a.lib.CurrentAlbum().Name)
				}
			} else {
				if albumName, path := a.overlay.SelectedTrackPath(); strings.HasPrefix(path, player.ModArchivePrefix) {
					// ModArchive albums live in the overlay, not the library —
					// play straight from the overlay's track list.
					a.playTrack(path, albumName)
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
	}
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
		rows := ui.BuildSettingsRows(*a.settings, winW, winH, a.renderScaleExplicit)
		a.overlay.SetSettingsRows(rows, 0)
	}
}

func (a *App) handleNormalAction(act input.Action) {
	switch act {
	case input.ActionSelect:
		if a.overlay != nil {
			a.overlay.ToggleUI()
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
		if path, album, ok := a.nextTrack(); ok {
			a.playTrack(path, album)
		}

	case input.ActionNextPreset:
		a.loadPreset(a.presetIdx + 1)

	case input.ActionPrevPreset:
		a.loadPreset(a.presetIdx - 1)
	}
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

// loadPresetByKey loads a preset by its store key via transition.
func (a *App) loadPresetByKey(key string) {
	a.transitionPreset(key)
}

// randPreset picks a random non-transition preset.
func (a *App) randPreset() {
	if len(a.presetNames) == 0 {
		return
	}

	// Build pool of normal (non-"!") presets.
	var normals []string
	for _, n := range a.presetNames {
		if n[0] != '!' {
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
		return
	}
	a.pm.LoadPresetData(string(d), true)
	a.applyPresetName(name)
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
	if len(rows) < 7 {
		return
	}

	resIndex := rows[ui.SettingResolution].Index
	resolutions := config.ComputeResolutions(winW, winH)

	if a.renderScaleExplicit {
		if resIndex >= 0 && resIndex < len(resolutions) {
			a.applyAdaptiveResolution(resolutions[resIndex])
		}
		a.resetAdaptiveCounters()
	} else if resIndex == 0 {
		a.settings.Graphics.Adaptive = true
		a.resetAdaptiveState(winW, winH)
	} else {
		a.settings.Graphics.Adaptive = false
		fixedIdx := resIndex - 1
		if fixedIdx >= 0 && fixedIdx < len(resolutions) {
			a.applyAdaptiveResolution(resolutions[fixedIdx])
		}
		a.resetAdaptiveCounters()
	}

	filterIndex := rows[ui.SettingFilter].Index
	filters := config.AllFilters()
	if filterIndex >= 0 && filterIndex < len(filters) {
		a.settings.Graphics.UpscaleFilter = filters[filterIndex]
		a.rt.SetNearest(a.settings.Graphics.UpscaleFilter.IsNearest())
	}

	shuffleModes := config.AllShuffleModes()
	if rows[ui.SettingShuffle].Index >= 0 && rows[ui.SettingShuffle].Index < len(shuffleModes) {
		a.settings.Playback.ShuffleMode = shuffleModes[rows[ui.SettingShuffle].Index]
	}
	a.playbackState.regenerateShuffleOrder(a.settings.Playback)

	repeatModes := config.AllRepeatModes()
	if rows[ui.SettingRepeat].Index >= 0 && rows[ui.SettingRepeat].Index < len(repeatModes) {
		a.settings.Playback.Repeat = repeatModes[rows[ui.SettingRepeat].Index]
	}

	presetIntervals := config.AllPresetIntervals()
	if rows[ui.SettingPresetTimer].Index >= 0 && rows[ui.SettingPresetTimer].Index < len(presetIntervals) {
		a.settings.PresetInterval = presetIntervals[rows[ui.SettingPresetTimer].Index]
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

	a.overlay.SetTheme(a.settings.UI.Theme, int(a.settings.UI.Transparency))

	if err := config.SaveSettings(a.settingsPath, *a.settings); err != nil {
		slog.Warn("settings save", "error", err)
	} else {
		slog.Debug("settings saved", "path", a.settingsPath)
	}

	rows = ui.BuildSettingsRows(*a.settings, winW, winH, a.renderScaleExplicit)
	a.overlay.SetSettingsRows(rows, a.overlay.SettingsCursor())
}

// playTrack starts playback of a track and shows the notification.
func (a *App) playTrack(path, album string) {
	if a.overlay != nil {
		label := album
		if album != "" {
			label = album + " — " + player.TrackTitle(path)
		}
		a.overlay.ShowTrack(label)
	}

	if !a.playbackState.play(path) {
		return
	}
	slog.Info("now loading", "track", path, "album", album)
}
