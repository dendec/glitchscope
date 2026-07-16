package app

import (
	"log/slog"
	"math/rand"
	"time"

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
			a.overlay.ToggleUI()
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
		// Initialize settings rows on first Select after opening the page.
		// Select() sets panelEntered=true, so this only runs once (when
		// OpenSettingsPage() just reset panelEntered to false).
		if a.overlay.IsSettingsPage() && !a.overlay.IsSettingsEditing() && !a.overlay.SelectEntered() {
			rows := ui.BuildSettingsRows(*a.gs, winW, winH)
			a.overlay.SetSettingsRows(rows, 0)
		}
	case input.ActionBack:
		a.overlay.Back()

	case input.ActionNextPreset:
		a.loadPreset(a.presetIdx + 1)

	case input.ActionPrevPreset:
		a.loadPreset(a.presetIdx - 1)
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

// loadPreset loads a preset by index (with wrapping) and sets it on projectM.
func (a *App) loadPreset(idx int) {
	if len(a.presetNames) == 0 {
		return
	}
	n := len(a.presetNames)
	a.presetIdx = ((idx % n) + n) % n
	d, err := presets.Read(a.presetNames[a.presetIdx])
	if err != nil {
		return
	}
	a.pm.LoadPresetData(string(d), true)
	if a.overlay != nil {
		a.overlay.SetPresetName(a.presetNames[a.presetIdx])
	}
	slog.Info("preset", "name", a.presetNames[a.presetIdx])
}

// randPreset picks a random non-transition preset, optionally transitions
// through a random "!" preset, and loads the target.
func (a *App) randPreset() {
	if len(a.presetNames) == 0 {
		return
	}

	var normal, trans []string
	for _, n := range a.presetNames {
		if n[0] == '!' {
			trans = append(trans, n)
		} else {
			normal = append(normal, n)
		}
	}
	if len(normal) == 0 {
		normal = a.presetNames
	}

	target := normal[rand.Intn(len(normal))]
	if len(normal) > 1 {
		for target == a.presetNames[a.presetIdx] {
			target = normal[rand.Intn(len(normal))]
		}
	}

	if len(trans) > 0 {
		t := trans[rand.Intn(len(trans))]
		if d, err := presets.Read(t); err == nil {
			a.pm.LoadPresetData(string(d), false)
			slog.Info("transition", "name", t)
		}
		a.pending = pendingPreset{name: target, at: time.Now().Add(transitionDelay)}
		return
	}

	d, err := presets.Read(target)
	if err != nil {
		return
	}
	a.pm.LoadPresetData(string(d), true)
	for i, n := range a.presetNames {
		if n == target {
			a.presetIdx = i
			break
		}
	}
	if a.overlay != nil {
		a.overlay.SetPresetName(target)
	}
	slog.Info("preset", "name", target)
}

// applySettings reads confirmed settings rows and applies changes.
func (a *App) applySettings(winW, winH int) {
	rows := a.overlay.SettingsRows()
	if len(rows) < 2 {
		return
	}

	// Row 0: Render resolution.
	resIndex := rows[0].Index
	resolutions := config.ComputeResolutions(winW, winH)
	if resIndex >= 0 && resIndex < len(resolutions) {
		r := resolutions[resIndex]
		a.gs.RenderWidth = r.Width
		a.gs.RenderHeight = r.Height
		a.rt.Resize(r.Width, r.Height)
		a.pm.SetWindowSize(r.Width, r.Height)
	}

	// Row 1: Upscale filter.
	filterIndex := rows[1].Index
	filters := config.AllFilters()
	if filterIndex >= 0 && filterIndex < len(filters) {
		a.gs.UpscaleFilter = filters[filterIndex]
		a.rt.SetNearest(a.gs.UpscaleFilter.IsNearest())
	}

	if err := config.SaveSettings(a.settingsPath, *a.gs); err != nil {
		slog.Warn("settings save", "error", err)
	} else {
		slog.Debug("settings saved", "path", a.settingsPath)
	}

	rows = ui.BuildSettingsRows(*a.gs, winW, winH)
	a.overlay.SetSettingsRows(rows, a.overlay.SettingsCursor())
}

// playTrack starts playback of a track and shows the notification.
func (a *App) playTrack(path, album string) {
	if err := a.pl.PlayFile(path); err != nil {
		slog.Error("play", "path", path, "error", err)
		return
	}
	if a.overlay != nil {
		label := album
		if album != "" {
			label = album + " — " + player.TrackTitle(path)
		}
		a.overlay.ShowTrack(label)
	}
	slog.Info("now playing", "track", path, "album", album)
}
