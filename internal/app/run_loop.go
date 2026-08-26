package app

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/input"
	"github.com/dendec/glitchscope/internal/presets"
	"github.com/dendec/glitchscope/internal/projectm"
	"github.com/dendec/glitchscope/internal/ui"
	"github.com/veandco/go-sdl2/sdl"
)

const (
	lowFPSThresh    = 15.0
	softCutDuration = 2.5 // seconds, for smooth preset transitions
)

type pendingPreset struct {
	name string
	at   time.Time
}

// Run enters the main loop. Must be called after Init().
func (a *App) Run() {
	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()

	lastFrame := time.Now()
	var fpsMeter fpsMeter

	var prevW, prevH int
	lowFPSPreset := "" // last preset that triggered the warning
	lastSkippedPreset := ""

	for range ticker.C {
		if a.quit.Load() {
			return
		}
		now := time.Now()
		dt := now.Sub(lastFrame).Seconds()
		lastFrame = now

		if dt > 0 {
			fpsMeter.Add(1.0 / dt)
		}
		fpsAvg := fpsMeter.Average()

		w32, h32 := a.window.GLGetDrawableSize()
		w, h := int(w32), int(h32)

		if fpsMeter.Full() && fpsAvg < lowFPSThresh && !a.onPresetsPage {
			presetName := ""
			if a.presetIdx >= 0 && a.presetIdx < len(a.presetNames) {
				presetName = a.presetNames[a.presetIdx]
			}
			if presetName != lowFPSPreset {
				slog.Warn("low fps", "fps", fpsAvg, "threshold", lowFPSThresh, "preset", presetName, "resolution", fmt.Sprintf("%dx%d", w, h))
				lowFPSPreset = presetName
			}
		}
		if fpsMeter.Full() && fpsAvg >= lowFPSThresh {
			lowFPSPreset = "" // reset so next drop on same preset logs again
		}
		winChanged := w != prevW || h != prevH

		if winChanged && prevW > 0 && prevH > 0 {
			if a.renderScaleExplicit {
				a.applyRenderResolution(config.RenderResolution{
					Width:  scaledDim(w, a.renderScale),
					Height: scaledDim(h, a.renderScale),
				})
			} else if a.settings.Graphics.Adaptive {
				cur := config.RenderResolution{Width: a.settings.Graphics.RenderWidth, Height: a.settings.Graphics.RenderHeight}
				if a.adaptive.Configure(w, h, cur) {
					a.applyRenderResolution(a.adaptive.resolutions[a.adaptive.index])
				} else {
					slog.Warn("adaptive: empty resolution list on resize", "window", fmt.Sprintf("%dx%d", w, h))
				}
				a.resetAdaptiveCounters()
			} else {
				resolutions := config.ComputeResolutions(w, h)
				saved := config.RenderResolution{Width: a.settings.Graphics.RenderWidth, Height: a.settings.Graphics.RenderHeight}
				target := config.ClosestResolution(resolutions, saved)
				a.applyRenderResolution(target)
			}

			if a.overlay != nil && a.overlay.IsSettingsPage() {
				rows := ui.BuildSettingsRows(*a.settings, w, h, a.renderScaleExplicit)
				a.overlay.SetSettingsRows(rows, a.overlay.SettingsCursor())
			}
		}
		prevW, prevH = w, h

		if a.settings.Graphics.Adaptive && !a.renderScaleExplicit && !a.onPresetsPage && fpsMeter.Full() {
			if resolution, direction, changed, minReached := a.adaptive.Decide(fpsAvg); changed {
				a.applyRenderResolution(resolution)
				if direction > 0 {
					slog.Info("adaptive: step down", "resolution", resolution)
				} else {
					slog.Info("adaptive: step up", "resolution", resolution)
				}
			} else if minReached {
				presetName := a.currentPresetName()
				if presetName != "" && presetName != lastSkippedPreset {
					slog.Warn("preset too heavy", "fps", fpsAvg, "preset", presetName, "action", "skipping")
					lastSkippedPreset = presetName
					a.loadPreset(a.presetIdx + 1)
				}
			}
		}

		if a.overlay != nil {
			a.overlay.SetScreenSize(w, h)
		}
		if a.overlay != nil {
			selectedAlbum := a.presenter.selectedAlbumIndex()
			currentAlbum := -1
			if a.lib != nil {
				currentAlbum = a.lib.CurrentAlbumIndex()
			}
			// The snapshot resolves the target album itself (selected album
			// when the UI is active, else the playing album), so the metadata
			// gate must be evaluated for the same index.
			trackAlbumIdx := currentAlbum
			if selectedAlbum >= 0 {
				trackAlbumIdx = selectedAlbum
			}
			a.presenter.Update(fpsAvg, a.settings.Graphics.Adaptive && !a.renderScaleExplicit,
				a.settings.Graphics.RenderHeight, a.prof.ReadStats(),
				a.playbackState.snapshot(selectedAlbum, a.presenter.needsTrackInfos(trackAlbumIdx)))
		}

		for e := sdl.PollEvent(); e != nil; e = sdl.PollEvent() {
			act := a.inp.ProcessEvent(e)
			if act == input.ActionQuit {
				return
			}
			if act == input.ActionRandomPreset {
				a.randPreset()
			} else {
				a.handleAction(act, w, h)
			}
		}

		// Continuous seeking: velocity scales with stick deflection (or a held
		// ,/. key) and accelerates when held at max for long enough. The
		// discrete ±5s on a single ,/. press is still handled by the action.
		if a.pl != nil {
			state := sdl.GetKeyboardState()
			var velocity float64
			switch {
			case state[sdl.SCANCODE_PERIOD] != 0:
				velocity = seekSpeed(1.0, a.seek.UpdateHold(1.0, now))
			case state[sdl.SCANCODE_COMMA] != 0:
				velocity = seekSpeed(-1.0, a.seek.UpdateHold(1.0, now))
			default:
				rx := a.inp.RightStickX()
				velocity = seekSpeed(rx, a.seek.UpdateHold(rx, now))
			}
			if target := a.seek.Target(a.pl.Position(), velocity, dt); target >= 0 {
				if err := a.pl.Seek(target); err != nil {
					// Backend rejected the seek (e.g. unsupported source) — stop
					// trying so we don't churn the decoder every frame.
					a.seek.lastDir = 0
				}
			}
		}

		if a.pending.name != "" && now.After(a.pending.at) {
			if d, err := presets.Read(a.pending.name); err == nil {
				a.pm.LoadPresetData(string(d), true)
				a.applyPresetName(a.pending.name)
			}
			a.pending = pendingPreset{}
		}

		if a.presetTicker != nil {
			select {
			case <-a.presetTicker.C:
				a.randPreset()
			default:
			}
		}

		if a.settings.PresetInterval == config.PresetAuto && a.presetSwitch.Swap(false) {
			a.randPreset()
		}

		if a.pl != nil {
			started, failed := a.pl.CheckPending()
			if failed {
				if a.overlay != nil {
					a.overlay.ShowTrack(" playback error")
				}
				a.resumePath = ""
				a.resumeSeconds = 0
			} else {
				// A load completed successfully. For catalog tracks the file
				// may now be cached on disk, so the right panel must re-read
				// its metadata (Cached, duration, comment) — even if the load
				// finished before the presenter ever observed loading=true.
				if started {
					a.presenter.invalidateTrackInfos()
				}
				if a.resumeAttempted && a.resumePath != "" && a.pl.TrackPath() == a.resumePath && a.pl.IsValidVoice() {
					resumePath := a.resumePath
					if err := a.pl.Seek(a.resumeSeconds); err != nil {
						slog.Warn("restore playback position", "path", resumePath, "error", err)
					}
					if a.overlay != nil && a.overlay.UIVisible() {
						a.overlay.NavigateToTrack(resumePath)
					}
					a.resumePath = ""
					a.resumeSeconds = 0
				}
			}
			a.restoreSavedPosition(a.online.Load())
		}

		if a.pl != nil && a.lib != nil && a.mic == nil && a.pl.Voice() != 0 && a.pl.TrackFinished() {
			a.autoAdvance()
		}

		a.pm.SetFPS(int32(fpsAvg))

		// Detect presets page transitions.
		onPresets := a.overlay != nil && a.overlay.IsPresetsPage()
		if onPresets && !a.onPresetsPage {
			a.onPresetsPage = true
			if a.preview != nil {
				a.preview.SetThrottle(false)
			}
			slog.Info("presets page: main viz stopped")
		} else if !onPresets && a.onPresetsPage {
			a.onPresetsPage = false
			if a.preview != nil {
				a.preview.SetThrottle(true)
			}
			if a.overlay != nil {
				if key := a.overlay.SelectedPresetKey(); key != "" {
					a.transitionPreset(key)
				}
			}
			slog.Info("presets page: main viz resumed")
		}

		if a.onPresetsPage {
			// Presets page: main viz stopped, all resources to preview.
			ClearFB()
			a.rt.Capture()

			// Feed audio to preview only (skip main pm).
			var wave []float32
			if a.mic != nil {
				wave = a.mic.Read()
			} else if a.pl != nil {
				wave = a.pl.GetWave()
			}
			if len(wave) > 0 {
				a.previewFeedPCM(wave)
			} else if a.preview != nil && a.preview.isReady() {
				a.previewFeedPCM(a.generateTestSignal(512))
			}

			// Render preview as fast as possible.
			if a.preview != nil {
				tW, tH := ThumbSize(w, h)
				a.preview.Resize(tW, tH)
				a.preview.ProcessNext()
				a.preview.ProcessNext()
				a.preview.ProcessNext()
			}
		} else {
			// Normal mode: main viz + throttled preview.
			a.pm.RenderFrame()
			a.rt.Capture()

			if a.preview != nil {
				tW, tH := ThumbSize(w, h)
				a.preview.Resize(tW, tH)
				a.preview.ProcessNext()
			}

			// Feed audio to both main pm and preview.
			if a.mic != nil {
				if w := a.mic.Read(); len(w) > 0 {
					a.pm.PCMAddFloat(w, projectm.Mono)
					a.previewFeedPCM(w)
				}
			} else if a.pl != nil {
				if w := a.pl.GetWave(); w != nil {
					a.pm.PCMAddFloat(w, projectm.Mono)
					a.previewFeedPCM(w)
				}
			}
		}

		a.rt.BlitToScreen(w, h)

		if a.overlay != nil {
			rw, rh := a.rt.Size()
			a.overlay.SetControllerConnected(a.inp.HasController())
			a.overlay.Update(a.inp.DPadUpHeld(), a.inp.DPadDownHeld())
			a.overlay.Draw(w, h)
			a.pm.BindFeedbackFramebuffer()
			a.overlay.Inject(rw, rh)
		}

		a.window.GLSwap()
	}
}

// autoAdvance picks the next track based on shuffle/repeat settings.
func (a *App) autoAdvance() {
	if track, ok := a.playbackState.advance(a.settings.Playback); ok {
		a.playTrack(track.path, track.album)
		if a.overlay != nil {
			a.overlay.NavigateToTrack(track.path)
			a.presenter.invalidateTrackInfos()
		}
	}
}

func (a *App) startPresetTicker() {
	interval := a.settings.PresetInterval
	if interval <= config.PresetOff {
		return
	}
	a.presetTicker = time.NewTicker(time.Duration(interval) * time.Second)
}

func (a *App) resetPresetTicker() {
	if a.presetTicker != nil {
		a.presetTicker.Stop()
		a.presetTicker = nil
	}
	a.startPresetTicker()
}

func (a *App) applyRenderResolution(r config.RenderResolution) {
	a.settings.Graphics.RenderWidth = r.Width
	a.settings.Graphics.RenderHeight = r.Height
	if currentW, currentH := a.rt.Size(); currentW == r.Width && currentH == r.Height {
		return
	}
	a.rt.Resize(r.Width, r.Height)
	a.pm.SetWindowSize(r.Width, r.Height)
}

func (a *App) resetAdaptiveCounters() {
	a.adaptive.policy.Reset()
}

func (a *App) resetAdaptiveState(winW, winH int) {
	if !a.adaptive.Reset(winW, winH) {
		slog.Warn("adaptive: empty resolution list", "window", fmt.Sprintf("%dx%d", winW, winH))
		return
	}
	a.applyRenderResolution(a.adaptive.resolutions[0])
}
