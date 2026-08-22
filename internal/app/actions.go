package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/filesystem"
	"github.com/dendec/glitchscope/internal/input"
	"github.com/dendec/glitchscope/internal/mic"
	"github.com/dendec/glitchscope/internal/player"
	"github.com/dendec/glitchscope/internal/presets"
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
			a.overlay.ToggleUI() // always show/hide the UI, never cycles pages
		}
		return
	case input.ActionSeekForward:
		if a.pl != nil {
			if err := a.pl.Seek(min(a.pl.Position()+5, a.pl.Duration())); err != nil {
				slog.Debug("seek forward", "error", err)
			}
		}
		return
	case input.ActionSeekBackward:
		if a.pl != nil {
			if err := a.pl.Seek(max(a.pl.Position()-5, 0)); err != nil {
				slog.Debug("seek backward", "error", err)
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
		} else if a.overlay.IsNCMode() {
			if a.overlay.Select() && a.pl != nil {
				path := a.overlay.NCPlaySelected()
				if path != "" {
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
		} else {
			selected := a.overlay.Select()
			if a.overlay.ConsumeMicMenuRequest() {
				devices := mic.InputDevices()
				a.overlay.SetMicDevices(devices)
				a.overlay.ShowMicrophoneDevices(devices)
			} else if device, ok := a.overlay.ConsumeMicDeviceSelection(); ok {
				a.startMicCapture(device)
			} else if a.overlay.ConsumeMicStopRequest() {
				a.stopMicCapture()
			} else if selected && a.lib != nil && a.pl != nil {
				if a.overlay.IsCatalogMode() {
					if albumName, path := a.overlay.SelectedCatalogTrack(); path != "" {
						a.playTrack(path, albumName)
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
	if len(rows) < 8 {
		return
	}

	resIndex := rows[ui.SettingResolution].Index
	resolutions := config.ComputeResolutions(winW, winH)

	if a.renderScaleExplicit {
		if resIndex >= 0 && resIndex < len(resolutions) {
			a.applyRenderResolution(resolutions[resIndex])
		}
		a.resetAdaptiveCounters()
	} else if resIndex == 0 {
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
		a.pm.SetHardCutEnabled(a.settings.PresetInterval == config.PresetAuto)
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

	rows = ui.BuildSettingsRows(*a.settings, winW, winH, a.renderScaleExplicit)
	a.overlay.SetSettingsRows(rows, a.overlay.SettingsCursor())
}

// playTrack starts playback of a track and shows the notification.
func (a *App) playTrack(path, album string) {
	a.stopMicCapture() // any playback wins over microphone input

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

// startMicCapture starts capture from one selected input device. Capture feeds the
// visualizer directly — the mic stream is not played through the speakers
// (feedback) and SoLoud playback is stopped while capturing.
func (a *App) startMicCapture(device string) {
	a.stopMicCapture()
	if a.pl != nil {
		a.pl.Stop()
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
