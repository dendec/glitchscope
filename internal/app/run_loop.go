package app

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/input"
	"github.com/dendec/glitchscope/internal/player"
	"github.com/dendec/glitchscope/internal/presets"
	"github.com/dendec/glitchscope/internal/projectm"
	"github.com/dendec/glitchscope/internal/ui"
	"github.com/veandco/go-sdl2/sdl"
)

const (
	softCutDuration = 2500 * time.Millisecond
	mainFramePeriod = time.Second / 60
)

type pendingPreset struct {
	name string
	at   time.Time
}

type visualizerClock struct {
	lastFrame   time.Time
	nextFrame   time.Time
	meter       fpsMeter
	frames      uint64
	framePeriod time.Duration // per-mode visualizer frame period
}

type runState struct {
	lastLoop          time.Time
	adaptiveFrame     uint64
	prevW             int
	prevH             int
	lowFPSPreset      string
	lastSkippedPreset string
}

func (c *visualizerClock) Due(now time.Time) bool {
	return c.nextFrame.IsZero() || !now.Before(c.nextFrame)
}

func (c *visualizerClock) Complete(now time.Time) {
	if !c.lastFrame.IsZero() {
		c.meter.AddDuration(now.Sub(c.lastFrame))
	}
	c.lastFrame = now
	c.frames++
	if c.framePeriod == 0 {
		c.framePeriod = time.Second / 30
	}
	if c.nextFrame.IsZero() {
		c.nextFrame = now.Add(c.framePeriod)
		return
	}
	c.nextFrame = c.nextFrame.Add(c.framePeriod)
	if now.After(c.nextFrame) {
		c.nextFrame = now
	}
}

func (c *visualizerClock) Reset() {
	*c = visualizerClock{framePeriod: c.framePeriod}
}

func (s *runState) consumeAdaptiveFrame(viz *visualizerClock) bool {
	if s.adaptiveFrame == viz.frames {
		return false
	}
	s.adaptiveFrame = viz.frames
	return true
}

// Run enters the main loop. Must be called after Init().
func (a *App) Run() {
	// Extract textures on the first frame — avoids blocking startup.
	a.ensureTextures(a.pm)

	ticker := time.NewTicker(mainFramePeriod)
	defer ticker.Stop()

	state := runState{lastLoop: time.Now()}
	var nextDisplayCheck time.Time
	refreshRate := int32(60)
	loopPeriod := mainFramePeriod
	a.vizClock.framePeriod = time.Second / time.Duration(a.settings.Graphics.PerformanceMode.Params().VisualizerFPS)

	for range ticker.C {
		if a.quit.Load() {
			return
		}
		now := time.Now()
		if !now.Before(nextDisplayCheck) {
			refreshRate = a.displayRefreshRate()
			nextDisplayCheck = now.Add(time.Second)
		}
		period, fps := frameTiming(a.settings.Graphics.PerformanceMode, refreshRate)
		if period != loopPeriod {
			ticker.Reset(period)
			loopPeriod = period
		}
		a.vizClock.framePeriod = time.Second / time.Duration(fps)
		dt := now.Sub(state.lastLoop).Seconds()
		state.lastLoop = now
		fpsAvg := a.vizClock.meter.Average()

		w32, h32 := a.window.GLGetDrawableSize()
		w, h := int(w32), int(h32)
		a.prepareFrame(&state, now, w, h, fpsAvg)
		if !a.handleFrameInput(&state, now, dt, w, h) {
			return
		}
		a.updateFramePlayback(now)
		a.renderFrame(now, w, h)
	}
}

func (a *App) prepareFrame(state *runState, now time.Time, w, h int, fpsAvg float64) {
	newVisualizerFrame := state.consumeAdaptiveFrame(&a.vizClock)
	lowThresh := a.settings.Graphics.PerformanceMode.Params().LowFPSThresh
	if a.vizClock.meter.Full() && fpsAvg < lowThresh && !a.onPresetsPage {
		presetName := ""
		if a.presetIdx >= 0 && a.presetIdx < len(a.presetNames) {
			presetName = a.presetNames[a.presetIdx]
		}
		if presetName != state.lowFPSPreset {
			renderW, renderH := a.rt.Size()
			slog.Warn("low fps", "fps", fpsAvg, "threshold", lowThresh, "preset", presetName, "resolution", fmt.Sprintf("%dx%d", renderW, renderH))
			state.lowFPSPreset = presetName
		}
	}
	if a.vizClock.meter.Full() && fpsAvg >= lowThresh {
		state.lowFPSPreset = "" // reset so next drop on same preset logs again
	}
	winChanged := w != state.prevW || h != state.prevH
	if winChanged {
		a.presentRequested = true
	}

	if winChanged && state.prevW > 0 && state.prevH > 0 {
		if a.settings.Graphics.PerformanceMode == config.PerfModeUltra {
			a.applyRenderResolution(config.RenderResolution{Width: w, Height: h})
		} else if a.settings.Graphics.Adaptive {
			cur := config.RenderResolution{Width: a.settings.Graphics.RenderWidth, Height: a.settings.Graphics.RenderHeight}
			if a.adaptive.Configure(w, h, cur, a.settings.Graphics.PerformanceMode.Params()) {
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
			rows := ui.BuildSettingsRowsWithCatalog(*a.settings, w, h, a.overlay.Catalog())
			a.overlay.SetSettingsRows(rows, a.overlay.SettingsCursor())
		}
	}
	state.prevW, state.prevH = w, h

	if a.settings.Graphics.PerformanceMode != config.PerfModeUltra && a.settings.Graphics.Adaptive && !a.settings.Graphics.VisualizerOff && !a.onPresetsPage &&
		a.vizClock.meter.Full() && newVisualizerFrame && !a.adaptiveSuspended(now) {
		// Reconfigure the active profile after mode/window changes without
		// reusing an incompatible measurement.
		key := presetProfileKey{name: a.currentPresetName(), mode: a.settings.Graphics.PerformanceMode, width: w, height: h}
		if key != a.presetTuning.active {
			if data, err := presets.Read(key.name); err == nil {
				a.presetTuning.activate(key, data)
			} else {
				slog.Debug("preset profile read", "error", err)
			}
		}
		a.presetTuning.observe(a.adaptive.index, a.adaptive.upscaleFloor, fpsAvg >= a.settings.Graphics.PerformanceMode.Params().AdaptiveThreshLow)
		if resolution, direction, changed, minReached := a.adaptive.Decide(fpsAvg); changed {
			a.applyRenderResolution(resolution)
			if direction > 0 {
				slog.Info("adaptive: step down", "resolution", resolution)
			} else {
				slog.Info("adaptive: step up", "resolution", resolution)
			}
		} else if minReached {
			presetName := a.currentPresetName()
			if presetName != "" && presetName != state.lastSkippedPreset {
				slog.Warn("preset too heavy", "fps", fpsAvg, "preset", presetName, "action", "skipping")
				state.lastSkippedPreset = presetName
				a.presetTuning.markHeavy()
				a.loadPreset(a.presetIdx + 1)
			}
		}
	}

	if a.overlay != nil {
		a.overlay.SetScreenSize(w, h)
		a.overlay.SetOnline(a.online.Load())
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
		playback := a.playbackState.snapshot(selectedAlbum, a.presenter.needsTrackInfos(trackAlbumIdx))
		playback = a.resolveRadioPlayingInfo(playback)
		a.presenter.Update(fpsAvg, a.settings.Graphics.Adaptive && a.settings.Graphics.PerformanceMode != config.PerfModeUltra,
			a.settings.Graphics.RenderHeight, a.prof.ReadStats(), playback)
	}
}

func (a *App) handleFrameInput(state *runState, now time.Time, dt float64, w, h int) bool {
	favoriteMode := a.favoriteMode()
	for e := sdl.PollEvent(); e != nil; e = sdl.PollEvent() {
		act := a.inp.ProcessEvent(e, favoriteMode, now)
		if act != input.ActionNone {
			a.presentRequested = true
		}
		if act == input.ActionQuit {
			return false
		}
		if act == input.ActionRandomPreset {
			presetsOpen := a.overlay != nil && a.overlay.UIVisible() && a.overlay.IsPresetsPage()
			if !presetsOpen {
				a.randPreset()
			}
		} else {
			a.handleAction(act, w, h)
		}
	}

	if hold := a.inp.PollFavoriteHold(favoriteMode, now); hold != input.ActionNone {
		a.handleAction(hold, w, h)
	}

	// Apply page transitions before timers so automatic preset changes are
	// suspended on the same frame that the Presets page opens.
	onPresets := a.overlay != nil && a.overlay.UIVisible() && a.overlay.IsPresetsPage()
	if onPresets && !a.onPresetsPage {
		a.enterPresetsPage()
		a.vizClock.Reset()
	} else if !onPresets && a.onPresetsPage {
		a.leavePresetsPage()
		a.vizClock.Reset()
	}

	// Continuous seeking: velocity scales with stick deflection (or a held
	// ,/. key) and accelerates when held at max for long enough. The
	// discrete ±5s on a single ,/. press is still handled by the action.
	if a.pl != nil && !player.IsRadio(a.pl.TrackPath()) {
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
	return true
}

func (a *App) updateFramePlayback(now time.Time) {
	a.pollRadio()
	if !a.settings.Graphics.VisualizerOff && !a.onPresetsPage && a.pending.name != "" && now.After(a.pending.at) {
		a.transitionPreset(a.pending.name)
		a.pending = pendingPreset{}
	}

	if a.presetTicker != nil && !a.settings.Graphics.VisualizerOff && !a.onPresetsPage {
		select {
		case <-a.presetTicker.C:
			a.randPreset()
		default:
		}
	}

	if !a.settings.Graphics.VisualizerOff && !a.onPresetsPage && a.settings.PresetInterval == config.PresetAuto && a.presetSwitch.Swap(false) {
		a.randPreset()
	}

	if a.pl != nil {
		loadingPath := a.pl.TrackPath()
		started, failed := a.pl.CheckPending()
		if failed {
			a.recoverPlaybackFailure(loadingPath)
		} else {
			// A load completed successfully. For catalog tracks the file
			// may now be cached on disk, so the right panel must re-read
			// its metadata (Cached, duration, comment) — even if the load
			// finished before the presenter ever observed loading=true.
			if started {
				a.notifyRadioStarted(loadingPath)
				a.failedTracks = nil
				a.presenter.invalidateTrackInfos()
				if !player.IsRadio(loadingPath) {
					a.pruneTrackCache()
				}
			}
			if a.resumeAttempted && a.resumePath != "" && a.pl.TrackPath() == a.resumePath && a.pl.IsValidVoice() {
				resumePath := a.resumePath
				if !player.IsRadio(resumePath) {
					if err := a.pl.Seek(a.resumeSeconds); err != nil {
						slog.Warn("restore playback position", "path", resumePath, "error", err)
					}
				}
				if a.overlay != nil && a.overlay.UIVisible() {
					a.navigateOverlayToTrack(resumePath)
				}
				a.resumePath = ""
				a.resumeSeconds = 0
			}
		}
		a.restoreSavedPosition(a.online.Load())
	}

	if a.pl != nil && a.lib != nil && a.mic == nil && a.pl.Voice() != 0 && a.pl.TrackFinished() {
		if path := a.pl.TrackPath(); player.IsRadio(path) {
			a.pl.Stop()
			a.recoverPlaybackFailure(path)
		} else {
			a.autoAdvance()
		}
	}
}

func (a *App) renderFrame(now time.Time, w, h int) {
	a.trackCacheOnce.Do(a.startTrackCacheCleanup)
	uiVisible := a.overlay != nil && a.overlay.UIVisible()
	uiOpaque := uiVisible && a.settings.UI.Transparency == 0
	uiInteracting := uiVisible && a.overlay != nil && a.overlay.InteractionActive(now)
	if a.overlay != nil {
		if a.trackCacheReady.Swap(false) {
			a.overlay.RefreshTrackCache()
		}
		a.overlay.SetControllerConnected(a.inp.HasController())
		a.overlay.Update(a.inp.DPadUpHeld(), a.inp.DPadDownHeld())
		uiInteracting = uiVisible && a.overlay.InteractionActive(time.Now())
		if uiInteracting {
			a.presentRequested = true
		}
	}
	rendered := false
	if a.onPresetsPage {
		// Presets page: one preview render at thumbnail size. The thumb
		// texture is used both for the page background and for the selected
		// preset details, so it must keep running even with an opaque UI.
		// During active navigation keep the last result and give the cursor
		// the whole frame; rendering resumes as soon as navigation settles.
		if !uiInteracting {
			if wave := a.readAudio(); len(wave) > 0 {
				a.previewFeedPCM(wave)
			} else if a.preview != nil && a.preview.isReady() {
				a.previewFeedPCM(a.testSignal())
			}

			if a.preview != nil && a.preview.isReady() {
				tW, tH := ui.PresetPreviewSize(w, h)
				a.preview.Resize(tW, tH)
				rendered = a.preview.ProcessNext()
				tex, _ := a.preview.Result()
				a.overlay.SetPreviewBackground(tex)
			}
		}
	} else {
		// Normal mode: clear preview background, restore thumbnail size.
		if a.overlay != nil {
			a.overlay.SetPreviewBackground(0)
		}
		if a.preview != nil {
			tW, tH := ui.PresetPreviewSize(w, h)
			a.preview.Resize(tW, tH)
		}

		// Schedule projectM independently from input/UI outside Ultra. GL
		// work remains on this thread; between visualizer frames, keep
		// presenting the last captured texture.
		if !a.settings.Graphics.VisualizerOff && !uiOpaque && !uiInteracting &&
			(a.settings.Graphics.PerformanceMode == config.PerfModeUltra || a.vizClock.Due(now)) {
			fps := int32(time.Second / a.vizClock.framePeriod)
			a.pm.SetFPS(fps)
			a.pm.RenderFrame()
			a.rt.Capture()

			// Feed audio to the main projectM instance.
			if wave := a.readAudio(); len(wave) > 0 {
				a.pm.PCMAddFloat(wave, projectm.Mono)
			}

			a.vizClock.Complete(time.Now())
			rendered = true
		}

	}
	audioOnly := a.settings.Graphics.VisualizerOff && !a.onPresetsPage
	due := presentationDue(a.settings.Graphics.PerformanceMode, now, a.nextPresent, rendered, a.presentRequested, uiVisible)
	if audioOnly {
		due = a.presentRequested || !now.Before(a.nextPresent)
	}
	if !due {
		return
	}
	a.presentRequested = false
	a.nextPresent = now.Add(a.vizClock.framePeriod)
	// Preview rendering and feedback injection may leave a different FBO
	// bound. Every presets-page presentation starts on a clean screen,
	// including iterations where the preview renderer is throttled.
	if a.onPresetsPage || uiOpaque {
		ui.ClearBackground(w, h)
	} else if audioOnly {
		ui.ClearBackground(w, h)
		if !uiVisible {
			a.nextPresent = now.Add(250 * time.Millisecond)
		}
	} else if !a.onPresetsPage {
		a.rt.BlitToScreen(w, h)
	}
	if a.overlay != nil {
		rw, rh := a.rt.Size()
		a.overlay.Draw(w, h)
		if !audioOnly && !a.onPresetsPage && !uiOpaque {
			a.pm.BindFeedbackFramebuffer()
			a.overlay.Inject(rw, rh)
		}
	}

	a.window.GLSwap()
}

func (a *App) enterPresetsPage() {
	a.onPresetsPage = true
	a.selectedPreset = ""
	a.presetSwitch.Store(false)
	if a.presetTicker != nil {
		a.presetTicker.Stop()
		a.presetTicker = nil
	}
	if a.pm != nil {
		a.pm.SetHardCutEnabled(false)
	}
	// Force an immediate preview render on the next frame so the
	// background appears without a black flash.
	if a.preview != nil {
		a.preview.SkipThrottle()
	}
	slog.Info("presets page: main viz and preset timers stopped")
}

func (a *App) leavePresetsPage() {
	a.onPresetsPage = false
	if a.pm != nil {
		a.pm.SetHardCutEnabled(!a.settings.Graphics.VisualizerOff && a.settings.PresetInterval == config.PresetAuto)
	}
	a.presetSwitch.Store(false)
	a.startPresetTicker()

	selected := a.selectedPreset
	a.selectedPreset = ""
	if selected != "" {
		a.transitionPreset(selected)
	}
	slog.Info("presets page: main viz and preset timers resumed")
}

// autoAdvance picks the next track based on shuffle/repeat settings.
func (a *App) autoAdvance() {
	if track, ok := a.playbackState.advance(a.settings.Playback); ok {
		a.playNextTrack(track)
	}
}

// playNextTrack keeps the overlay synchronized with every automatic transition,
// whether the previous track ended normally or failed to load. This is shared
// by all sources so the cursor and right panel follow the newly selected track.
func (a *App) playNextTrack(track trackRef) {
	a.startTrack(track.path, track.album)
	if a.overlay != nil {
		if player.IsRadio(track.path) && a.radio != nil {
			if station, ok := a.radio.Lookup(track.path); ok {
				a.overlay.SetPlayingInfo(station.DisplayName(), track.path)
			}
		} else {
			a.overlay.SetPlayingInfo(track.album, track.path)
		}
		a.navigateOverlayToTrack(track.path)
		a.presenter.invalidateTrackInfos()
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
	if a.settings.Graphics.PerformanceMode == config.PerfModeUltra {
		w, h := a.window.GLGetDrawableSize()
		r = config.RenderResolution{Width: int(w), Height: int(h)}
	}
	a.settings.Graphics.RenderWidth = r.Width
	a.settings.Graphics.RenderHeight = r.Height
	a.resizeRenderTarget(r)
}

func (a *App) resizeRenderTarget(r config.RenderResolution) {
	if currentW, currentH := a.rt.Size(); currentW == r.Width && currentH == r.Height {
		return
	}
	a.rt.Resize(r.Width, r.Height)
	a.pm.SetWindowSize(r.Width, r.Height)
	a.pm.BindFeedbackFramebuffer()
	a.rt.SeedFeedback()
}

func (a *App) suspendAdaptiveForPresetTransition(now time.Time) {
	a.adaptiveResumeAt = now.Add(softCutDuration)
	a.adaptive.RestartForPreset()
}

func (a *App) adaptiveSuspended(now time.Time) bool {
	return now.Before(a.adaptiveResumeAt)
}

func (a *App) resetAdaptiveCounters() {
	a.adaptive.policy.Reset()
}

func (a *App) resetAdaptiveState(winW, winH int) {
	if !a.adaptive.Reset(winW, winH, a.settings.Graphics.PerformanceMode.Params()) {
		slog.Warn("adaptive: empty resolution list", "window", fmt.Sprintf("%dx%d", winW, winH))
		return
	}
	a.applyRenderResolution(a.adaptive.resolutions[a.adaptive.index])
}

// Energy-saving modes avoid swapping duplicate backgrounds;
// UI animation has its own deadline and input can request immediate presentation.
func presentationDue(mode config.PerformanceMode, now, deadline time.Time, rendered, requested, uiVisible bool) bool {
	return mode == config.PerfModeUltra || mode == config.PerfModePerformance || rendered || requested || (uiVisible && !now.Before(deadline))
}

// frameTiming keeps existing modes at 60 Hz and lets Ultra follow the display.
func frameTiming(mode config.PerformanceMode, refreshRate int32) (time.Duration, int32) {
	if mode != config.PerfModeUltra {
		return mainFramePeriod, mode.Params().VisualizerFPS
	}
	if refreshRate <= 0 {
		refreshRate = 60
	}
	return time.Second / time.Duration(refreshRate), refreshRate
}

func (a *App) displayRefreshRate() int32 {
	index, err := a.window.GetDisplayIndex()
	if err != nil {
		slog.Debug("display index unavailable", "error", err)
		return 60
	}
	mode, err := sdl.GetCurrentDisplayMode(index)
	if err != nil {
		slog.Debug("display mode unavailable", "error", err)
		return 60
	}
	if mode.RefreshRate <= 0 {
		return 60
	}
	return mode.RefreshRate
}

// recoverPlaybackFailure also handles a live stream ending after startup;
// Repeat One must not trap the listener on a failed radio source.
func (a *App) recoverPlaybackFailure(path string) {
	if player.IsRadio(path) && a.radio != nil && a.online.Load() {
		a.radio.MarkDead(path)
	} else if player.IsRadio(path) {
		// A failed connection while offline is not evidence that the station is
		// dead. Keep its descriptor and cached listing for the next retry.
		slog.Debug("radio failure kept in cache while offline", "path", path)
	}
	if !player.IsRadio(path) {
		a.pruneTrackCache()
	}
	if a.overlay != nil {
		a.overlay.ShowTrack(" playback error")
	}
	a.resumePath, a.resumeSeconds = "", 0
	if track, ok := a.nextAfterFailure(path, a.settings.Playback); ok {
		a.playNextTrack(track)
	} else {
		slog.Warn("playback recovery stopped", "reason", "no next candidate or failure limit reached")
		if a.overlay != nil {
			a.overlay.ShowTrack("no playable track; select another track")
		}
	}
}
