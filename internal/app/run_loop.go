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
	lastFrame      time.Time
	nextFrame      time.Time
	meter          fpsMeter
	adaptivePaused bool
	frames         uint64
	framePeriod    time.Duration // effective visualizer frame period
}

type runState struct {
	lastLoop      time.Time
	adaptiveFrame uint64
	prevW         int
	prevH         int
	lowFPSPreset  string
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

// PauseAdaptive excludes deliberate throttling and the first recovery interval.
func (c *visualizerClock) PauseAdaptive(paused bool) {
	c.adaptivePaused = paused
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
		if a.targetVisualizerFPS != fps {
			a.targetVisualizerFPS = fps
			a.setEffectiveVisualizerFPS(fps)
		}
		a.uiFramePeriod = period
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
	adaptiveReady := !a.vizClock.adaptivePaused && a.renderCost.Full()
	params := a.settings.Graphics.PerformanceMode.Params()
	// Use the scheduled cadence so fewer renders actually release budget.
	utilization := a.renderCost.Utilization(a.effectiveVisualizerFPS)
	cadence := a.renderCost.Cadence(a.effectiveVisualizerFPS)
	if adaptiveReady && utilization > params.UtilizationHigh && !a.onPresetsPage {
		presetName := ""
		if a.presetIdx >= 0 && a.presetIdx < len(a.presetNames) {
			presetName = a.presetNames[a.presetIdx]
		}
		if presetName != state.lowFPSPreset {
			renderW, renderH := a.rt.Size()
			slog.Debug("soft frame budget exceeded", "utilization", utilization, "threshold", params.UtilizationHigh, "preset", presetName, "resolution", fmt.Sprintf("%dx%d", renderW, renderH))
			state.lowFPSPreset = presetName
		}
	}
	if adaptiveReady && utilization <= params.UtilizationHigh {
		state.lowFPSPreset = "" // reset so next drop on same preset logs again
	}
	winChanged := w != state.prevW || h != state.prevH
	if winChanged {
		a.renderCost.Reset()
		adaptiveReady = false
	}
	if winChanged {
		a.presentRequested = true
	}

	if winChanged && state.prevW > 0 && state.prevH > 0 {
		ceiling := config.ResolutionAtMost(config.ComputeResolutions(w, h), a.configuredResolution)
		if a.settings.Graphics.PerformanceMode == config.PerfModeUltra {
			a.applyRenderResolution(ceiling)
		} else {
			if a.adaptive.Configure(w, h, ceiling, params) {
				a.applyRenderResolution(a.adaptive.resolutions[a.adaptive.index])
			} else {
				slog.Warn("adaptive: empty resolution list on resize", "window", fmt.Sprintf("%dx%d", w, h))
			}
			a.resetAdaptiveCounters()
			if a.overlay != nil && a.overlay.IsSettingsPage() {
				rows := ui.BuildSettingsRowsWithCatalog(*a.settings, w, h, a.overlay.Catalog())
				a.overlay.SetSettingsRows(rows, a.overlay.SettingsCursor())
			}
		}
	}
	state.prevW, state.prevH = w, h

	if a.settings.Graphics.PerformanceMode != config.PerfModeUltra && !a.settings.Graphics.VisualizerOff && !a.onPresetsPage &&
		adaptiveReady && newVisualizerFrame && !a.adaptiveSuspended(now) {
		renderW, renderH := a.rt.Size()
		highFor := time.Duration(0)
		if !a.adaptive.policy.highSince.IsZero() {
			highFor = now.Sub(a.adaptive.policy.highSince)
		}
		upFor := time.Duration(0)
		if !a.adaptive.policy.upSince.IsZero() {
			upFor = now.Sub(a.adaptive.policy.upSince)
		}
		lowFor := time.Duration(0)
		if !a.adaptive.policy.lowSince.IsZero() {
			lowFor = now.Sub(a.adaptive.policy.lowSince)
		}
		cooldownFor := time.Duration(0)
		if now.Before(a.adaptive.policy.cooldownUntil) {
			cooldownFor = a.adaptive.policy.cooldownUntil.Sub(now)
		}
		slog.Debug("adaptive render sample",
			"mode", a.settings.Graphics.PerformanceMode,
			"frame_work_avg_ms", a.renderCost.Average().Seconds()*1000,
			"cadence_ratio", cadence,
			"present_interval_avg_ms", a.renderCost.intervals.Average().Seconds()*1000,
			"budget_utilization_pct", utilization*100,
			"target_budget_utilization_pct", a.renderCost.Utilization(params.VisualizerFPS)*100,
			"effective_fps", a.effectiveVisualizerFPS,
			"target_fps", params.VisualizerFPS,
			"min_fps", params.MinVisualizerFPS,
			"resolution", fmt.Sprintf("%dx%d", renderW, renderH),
			"resolution_index", a.adaptive.index,
			"ceiling_index", a.adaptive.ceilingIndex,
			"resolution_count", len(a.adaptive.resolutions),
			"samples", a.renderCost.Count(),
			"high_for_ms", highFor.Milliseconds(),
			"up_for_ms", upFor.Milliseconds(),
			"resolution_up_for_ms", lowFor.Milliseconds(),
			"cooldown_for_ms", cooldownFor.Milliseconds())
		// Reconfigure the active profile after mode/window changes without
		// reusing an incompatible measurement.
		key := presetProfileKey{name: a.currentPresetName(), mode: a.settings.Graphics.PerformanceMode, width: w, height: h, ceilingIndex: a.adaptive.ceilingIndex}
		if key != a.presetTuning.active {
			if data, err := presets.Read(key.name); err == nil {
				a.presetTuning.activate(key, data)
			} else {
				slog.Debug("preset profile read", "error", err)
			}
		}
		a.presetTuning.observe(a.adaptive.index, a.adaptive.upscaleFloor, a.effectiveVisualizerFPS, a.adaptive.trialAction == adaptiveNone && cadence <= cadenceTolerance)
		if resolution, fps, action := a.adaptive.Decide(utilization, cadence, now, a.effectiveVisualizerFPS); action != adaptiveNone {
			a.renderCost.Reset()
			switch action {
			case adaptiveResample:
				slog.Debug("adaptive: confirming resolution trial", "cadence_ratio", cadence)
			case adaptiveFrequencyDown, adaptiveFrequencyUp:
				a.setEffectiveVisualizerFPS(fps)
				slog.Info("adaptive: frequency step", "fps", fps, "utilization", utilization, "cadence_ratio", cadence)
			case adaptiveResolutionDown, adaptiveResolutionUp:
				a.applyRenderResolution(resolution)
				slog.Info("adaptive: resolution step", "resolution", resolution, "utilization", utilization, "cadence_ratio", cadence)
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
		refresh := a.presenter.needsTrackInfos(trackAlbumIdx)
		playback := a.playbackState.snapshot(selectedAlbum, refresh && a.overlay.UIVisible())
		a.updateMetadata(&playback, trackAlbumIdx, refresh)
		playback = a.resolveRadioPlayingInfo(playback)
		_, effectiveRenderHeight := a.rt.Size()
		a.presenter.Update(fpsAvg, a.settings.Graphics.PerformanceMode != config.PerfModeUltra,
			effectiveRenderHeight, a.prof.ReadStats(), playback)
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
	paused := uiVisible || a.settings.Graphics.VisualizerOff || a.onPresetsPage
	if paused != a.vizClock.adaptivePaused {
		a.resetAdaptiveCounters()
		a.renderCost.Reset()
		a.adaptive.policy.Restart()
	}
	// Service time belongs to UI/playback orchestration, not the visualizer.
	// Reset the interval baseline before recording this frame, preserving genuine
	// render/swap stalls which happen below and still need adaptation.
	if a.renderCost.ExcludeServiceDelay(time.Since(now)) {
		a.adaptive.policy.Restart()
	}
	a.vizClock.PauseAdaptive(paused)
	budget := &a.vizBudget
	if a.onPresetsPage {
		budget = &a.previewBudget
	}
	if !a.onPresetsPage && a.overlay != nil {
		a.overlay.SetPreviewBackground(0)
	}
	// Publish input before entering a potentially slow, non-preemptible GL call.
	// The newly captured visualizer texture is presented on the next UI tick.
	if uiInteracting {
		a.presentFrame(time.Now(), w, h, false, uiVisible, uiOpaque)
	}
	started := time.Now()
	rendered := a.renderVisualization(started, w, h, uiOpaque, uiInteracting, budget)
	renderCost := time.Since(started)
	if rendered {
		budget.Complete(started, started.Add(renderCost))
	}
	if uiInteracting {
		if rendered {
			a.presentRequested = true
		}
		return
	}
	presentationCost := a.presentFrame(time.Now(), w, h, rendered, uiVisible, uiOpaque)
	presented := time.Now()
	if rendered && !a.onPresetsPage {
		if !paused && !a.adaptiveSuspended(presented) {
			a.renderCost.AddFrame(renderCost, presentationCost, presented)
		}
		params := a.settings.Graphics.PerformanceMode.Params()
		renderW, renderH := a.rt.Size()
		slog.Debug("visualizer frame",
			"mode", a.settings.Graphics.PerformanceMode,
			"render_ms", renderCost.Seconds()*1000,
			"presentation_ms", presentationCost.Seconds()*1000,
			"frame_work_ms", (renderCost+presentationCost).Seconds()*1000,
			"budget_ms", 1000/float64(max(a.effectiveVisualizerFPS, 1)),
			"rolling_budget_utilization_pct", a.renderCost.Utilization(a.effectiveVisualizerFPS)*100,
			"cadence_ratio", a.renderCost.Cadence(a.effectiveVisualizerFPS),
			"present_interval_avg_ms", a.renderCost.intervals.Average().Seconds()*1000,
			"effective_fps", a.effectiveVisualizerFPS,
			"target_fps", params.VisualizerFPS,
			"resolution", fmt.Sprintf("%dx%d", renderW, renderH),
			"samples", a.renderCost.Count(),
			"adaptive_paused", paused || a.adaptiveSuspended(presented))
	}
}

func (a *App) renderVisualization(now time.Time, w, h int, opaque, interacting bool, budget *interactionBudget) bool {
	if a.onPresetsPage {
		if a.preview == nil || !a.preview.isReady() || !budget.Due(now, a.preview.framePeriod, interacting) {
			return false
		}
		if wave := a.readAudio(); len(wave) > 0 {
			a.previewFeedPCM(wave)
		} else {
			a.previewFeedPCM(a.testSignal())
		}
		tW, tH := ui.PresetPreviewSize(w, h)
		a.preview.Resize(tW, tH)
		a.preview.pm.SetFPS(int32(time.Second / budget.Period(a.preview.framePeriod, interacting)))
		rendered := a.preview.ProcessNext()
		tex, _ := a.preview.Result()
		a.overlay.SetPreviewBackground(tex)
		return rendered
	}
	if a.preview != nil {
		tW, tH := ui.PresetPreviewSize(w, h)
		a.preview.Resize(tW, tH)
	}
	if a.settings.Graphics.VisualizerOff || opaque ||
		!budget.Due(now, a.vizClock.framePeriod, interacting) ||
		(a.settings.Graphics.PerformanceMode != config.PerfModeUltra && !a.vizClock.Due(now)) {
		return false
	}
	a.pm.SetFPS(int32(time.Second / budget.Period(a.vizClock.framePeriod, interacting)))
	a.pm.RenderFrame()
	a.rt.Capture()
	if wave := a.readAudio(); len(wave) > 0 {
		a.pm.PCMAddFloat(wave, projectm.Mono)
	}
	a.vizClock.Complete(time.Now())
	return true
}

func (a *App) presentFrame(now time.Time, w, h int, rendered, uiVisible, uiOpaque bool) time.Duration {
	audioOnly := a.settings.Graphics.VisualizerOff && !a.onPresetsPage
	due := presentationDue(a.settings.Graphics.PerformanceMode, now, a.nextPresent, rendered, a.presentRequested, uiVisible)
	if audioOnly {
		due = a.presentRequested || !now.Before(a.nextPresent)
	}
	if !due {
		return 0
	}
	presentStart := time.Now()
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
	budget := &a.vizBudget
	if a.onPresetsPage {
		budget = &a.previewBudget
	}
	// Blit/draw/swap can expose deferred GPU work and UI pressure. Exclude
	// one UI period, including normal vsync waiting, from the cost estimate.
	elapsed := time.Since(presentStart)
	budget.ObservePresentation(elapsed, a.uiFramePeriod)
	return elapsed
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
	a.renderCost.Reset()
}

func (a *App) adaptiveSuspended(now time.Time) bool {
	return now.Before(a.adaptiveResumeAt)
}

func (a *App) resetAdaptiveCounters() {
	a.adaptive.policy.Reset()
}

func (a *App) resetAdaptiveState(winW, winH int) {
	if !a.adaptive.Reset(winW, winH, a.configuredResolution, a.settings.Graphics.PerformanceMode.Params()) {
		slog.Warn("adaptive: empty resolution list", "window", fmt.Sprintf("%dx%d", winW, winH))
		return
	}
	a.applyRenderResolution(a.adaptive.resolutions[a.adaptive.index])
}

func (a *App) setEffectiveVisualizerFPS(fps int32) {
	if fps <= 0 {
		return
	}
	a.effectiveVisualizerFPS = fps
	a.vizClock.framePeriod = time.Second / time.Duration(fps)
	a.vizClock.nextFrame = time.Time{}
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
