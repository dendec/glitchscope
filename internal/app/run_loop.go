package app

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/dendec/pmv/internal/config"
	"github.com/dendec/pmv/internal/input"
	"github.com/dendec/pmv/internal/presets"
	"github.com/dendec/pmv/internal/projectm"
	"github.com/dendec/pmv/internal/ui"
	"github.com/veandco/go-sdl2/sdl"
)

const (
	lowFPSThresh    = 30.0
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
	lowFPSWarned := false

	lastAlbumIdx := -1
	var prevW, prevH int

	for range ticker.C {
		now := time.Now()
		dt := now.Sub(lastFrame).Seconds()
		lastFrame = now

		if dt > 0 {
			fpsMeter.Add(1.0 / dt)
		}
		fpsAvg := fpsMeter.Average()

		if fpsMeter.Full() {
			if fpsAvg < lowFPSThresh && !lowFPSWarned {
				presetName := ""
				if a.presetIdx >= 0 && a.presetIdx < len(a.presetNames) {
					presetName = a.presetNames[a.presetIdx]
				}
				slog.Warn("low fps", "fps", fpsAvg, "threshold", lowFPSThresh, "preset", presetName)
				lowFPSWarned = true
			} else if fpsAvg >= lowFPSThresh {
				lowFPSWarned = false
			}
		}

		w32, h32 := a.window.GLGetDrawableSize()
		w, h := int(w32), int(h32)
		winChanged := w != prevW || h != prevH

		if winChanged && prevW > 0 && prevH > 0 {
			if a.renderScaleExplicit {
				a.settings.Graphics.RenderWidth = scaledDim(w, a.renderScale)
				a.settings.Graphics.RenderHeight = scaledDim(h, a.renderScale)
			} else if a.settings.Graphics.Adaptive {
				cur := config.RenderResolution{Width: a.settings.Graphics.RenderWidth, Height: a.settings.Graphics.RenderHeight}
				if a.adaptive.Configure(w, h, cur) {
					a.applyAdaptiveResolution(a.adaptive.resolutions[a.adaptive.index])
				} else {
					slog.Warn("adaptive: empty resolution list on resize", "window", fmt.Sprintf("%dx%d", w, h))
				}
				a.resetAdaptiveCounters()
			} else {
				resolutions := config.ComputeResolutions(w, h)
				saved := config.RenderResolution{Width: a.settings.Graphics.RenderWidth, Height: a.settings.Graphics.RenderHeight}
				target := config.ClosestResolution(resolutions, saved)
				a.settings.Graphics.RenderWidth, a.settings.Graphics.RenderHeight = target.Width, target.Height
			}

			if a.overlay != nil && a.overlay.IsSettingsPage() {
				rows := ui.BuildSettingsRows(*a.settings, w, h, a.renderScaleExplicit)
				a.overlay.SetSettingsRows(rows, a.overlay.SettingsCursor())
			}
		}
		prevW, prevH = w, h

		renderW, renderH := a.settings.Graphics.RenderWidth, a.settings.Graphics.RenderHeight
		if rw, rh := a.rt.Size(); rw != renderW || rh != renderH {
			a.rt.Resize(renderW, renderH)
			a.pm.SetWindowSize(renderW, renderH)
		}

		if a.settings.Graphics.Adaptive && !a.renderScaleExplicit && fpsMeter.Full() {
			if resolution, direction, changed := a.adaptive.Decide(now, fpsAvg); changed {
				a.applyAdaptiveResolution(resolution)
				if direction > 0 {
					slog.Info("adaptive: step down", "resolution", resolution)
				} else {
					slog.Info("adaptive: step up", "resolution", resolution)
				}
			}
		}

		if a.overlay != nil {
			a.overlay.SetScreenSize(w, h)
		}

		if a.overlay != nil {
			s := a.prof.ReadStats()
			line := fmt.Sprintf("FPS:%.0f MEM:%.0fM CPU:%.0f%%", fpsAvg, s.MemKB/1024, s.CPUPct)
			if s.GPUOK {
				line += fmt.Sprintf(" GPU:%.0fM %.0f%%", s.GPUMemKB/1024, s.GPUUtilPct)
			}
			if a.settings.Graphics.Adaptive && !a.renderScaleExplicit {
				line += fmt.Sprintf(" %dp", a.settings.Graphics.RenderHeight)
			}
			a.overlay.SetStats(line)
			if a.pl != nil {
				a.overlay.SetPlayback(a.pl.Position(), a.pl.Duration(), a.pl.SampleRate(), a.pl.Bitrate(), a.pl.BPM(), a.pl.Channels(), a.pl.IsPaused(), a.pl.IsTracker())
			}
			if a.lib != nil {
				a.overlay.SetAlbums(a.lib.Albums, a.lib.CurrentAlbumIndex())

				trackAlbumIdx := a.lib.CurrentAlbumIndex()
				if a.overlay.UIVisible() {
					trackAlbumIdx = a.overlay.AlbumCursor()
				}
				if trackAlbumIdx != lastAlbumIdx {
					trackCursor := 0
					if trackAlbumIdx == a.lib.CurrentAlbumIndex() {
						trackCursor = a.lib.CurrentTrackIndex()
					}
					a.overlay.SetTrackInfos(a.lib.GetAlbumTracks(trackAlbumIdx), trackCursor)
					lastAlbumIdx = trackAlbumIdx
				}

				if a.pl != nil {
					curTrack := a.pl.TrackPath()
					if !a.pl.IsValidVoice() && !a.pl.Loading() {
						curTrack = ""
					}
					curAlbum := a.lib.CurrentAlbum().Name
					a.overlay.SetPlaying(curAlbum, curTrack)
					a.overlay.SetLoading(a.pl.LoadProgress())
				}
			}
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

		if a.pl != nil {
			if wave := a.pl.GetWave(); wave != nil {
				a.pm.PCMAddFloat(wave, projectm.Mono)
			}
		}

		if a.pl != nil {
			if _, failed := a.pl.CheckPending(); failed {
				if a.overlay != nil {
					a.overlay.ShowTrack(" playback error")
				}
			}
		}

		if a.pl != nil && a.lib != nil && a.pl.Voice() != 0 && a.pl.TrackFinished() {
			a.autoAdvance()
		}

		a.pm.SetFPS(int32(fpsAvg))

		a.pm.RenderFrame()
		a.rt.Capture()
		a.rt.BlitToScreen(w, h)

		if a.overlay != nil {
			rw, rh := a.rt.Size()
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
	}
}

func (a *App) startPresetTicker() {
	interval := a.settings.PresetInterval
	if interval == config.PresetOff {
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

func (a *App) applyAdaptiveResolution(r config.RenderResolution) {
	a.settings.Graphics.RenderWidth = r.Width
	a.settings.Graphics.RenderHeight = r.Height
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
	a.applyAdaptiveResolution(a.adaptive.resolutions[0])
}
