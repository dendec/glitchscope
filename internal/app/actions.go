package app

import (
	"log/slog"
	"math/rand"

	"github.com/dendec/mdpp/internal/config"
	"github.com/dendec/mdpp/internal/input"
	"github.com/dendec/mdpp/internal/player"
	"github.com/dendec/mdpp/internal/presets"
	"github.com/dendec/mdpp/internal/ui"
)

// handleAction dispatches an input action.
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
				a.lib.SelectAlbum(a.overlay.AlbumCursor())
				if path := a.lib.SelectTrack(a.overlay.TrackCursor()); path != "" {
					a.playTrack(path, a.lib.CurrentAlbum().Name)
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

// switchScreen moves the overlay to the next/previous page and, when landing
// on the settings page, (re)builds its rows from the current config.
func (a *App) switchScreen(winW, winH int, forward bool) {
	if forward {
		a.overlay.NextScreen()
	} else {
		a.overlay.PrevScreen()
	}
	if a.overlay.IsSettingsPage() {
		rows := ui.BuildSettingsRows(*a.settings, winW, winH)
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
		if a.lib == nil || a.pl == nil {
			return
		}
		if path := a.lib.AlbumPrev(); path != "" {
			a.playTrack(path, a.lib.CurrentAlbum().Name)
		}

	case input.ActionCursorDown, input.ActionNextAlbum:
		if a.lib == nil || a.pl == nil {
			return
		}
		if path := a.lib.AlbumNext(); path != "" {
			a.playTrack(path, a.lib.CurrentAlbum().Name)
		}

	case input.ActionFocusLeft, input.ActionPrevTrack:
		if a.lib == nil || a.pl == nil {
			return
		}
		if path := a.lib.TrackPrev(); path != "" {
			a.playTrack(path, a.lib.CurrentAlbum().Name)
		}

	case input.ActionFocusRight, input.ActionNextTrack:
		if a.lib == nil || a.pl == nil {
			return
		}
		if path := a.lib.TrackNext(); path != "" {
			a.playTrack(path, a.lib.CurrentAlbum().Name)
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

// randPreset picks a random non-transition preset and loads it via transition.
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

// transitionPreset loads the selected preset immediately.
// All preset changes route through here — DRY single path.
func (a *App) transitionPreset(name string) {
	d, err := presets.Read(name)
	if err != nil {
		return
	}
	a.pm.LoadPresetData(string(d), true)
	a.applyPresetName(name)
}

// applyPresetName updates the preset index and overlay after a preset is loaded.
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
	if len(rows) < 5 {
		return
	}

	// Row 0: Render resolution.
	resIndex := rows[0].Index
	resolutions := config.ComputeResolutions(winW, winH)
	if resIndex >= 0 && resIndex < len(resolutions) {
		r := resolutions[resIndex]
		a.settings.Graphics.RenderWidth = r.Width
		a.settings.Graphics.RenderHeight = r.Height
		a.rt.Resize(r.Width, r.Height)
		a.pm.SetWindowSize(r.Width, r.Height)
	}

	// Row 1: Upscale filter.
	filterIndex := rows[1].Index
	filters := config.AllFilters()
	if filterIndex >= 0 && filterIndex < len(filters) {
		a.settings.Graphics.UpscaleFilter = filters[filterIndex]
		a.rt.SetNearest(a.settings.Graphics.UpscaleFilter.IsNearest())
	}

	// Row 2: Shuffle.
	a.settings.Playback.Shuffle = rows[2].Index == 1

	// Row 3: Repeat.
	repeatModes := config.AllRepeatModes()
	if rows[3].Index >= 0 && rows[3].Index < len(repeatModes) {
		a.settings.Playback.Repeat = repeatModes[rows[3].Index]
	}

	// Row 4: Preset auto-switch.
	presetIntervals := config.AllPresetIntervals()
	if rows[4].Index >= 0 && rows[4].Index < len(presetIntervals) {
		a.settings.PresetInterval = presetIntervals[rows[4].Index]
		a.resetPresetTicker()
	}

	if err := config.SaveSettings(a.settingsPath, *a.settings); err != nil {
		slog.Warn("settings save", "error", err)
	} else {
		slog.Debug("settings saved", "path", a.settingsPath)
	}

	rows = ui.BuildSettingsRows(*a.settings, winW, winH)
	a.overlay.SetSettingsRows(rows, a.overlay.SettingsCursor())
}

// playTrack starts playback of a track and shows the notification.
func (a *App) playTrack(path, album string) {
	// Show track name immediately, then start async load.
	if a.overlay != nil {
		label := album
		if album != "" {
			label = album + " — " + player.TrackTitle(path)
		}
		a.overlay.ShowTrack(label)
	}

	a.pl.PlayFileAsync(path)
	slog.Info("now loading", "track", path, "album", album)
}
