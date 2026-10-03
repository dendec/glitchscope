package app

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/i18n"
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

// presetLoadProbe records the first few operations after a preset load. The
// load itself happens synchronously in projectM, so these samples distinguish
// preset construction from deferred driver work in rendering or presentation.
type presetLoadProbe struct {
	name           string
	loadedAt       time.Time
	renderSamples  int
	presentSamples int
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

// displaySnapshot identifies the active display mode used by the window.
// Display identity and mode dimensions matter even when the refresh rate does
// not change: adaptive measurements are tied to the active display.
type displaySnapshot struct {
	index         int
	width, height int32
	refreshRate   int32
	format        uint32
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
	display := a.displaySnapshot()
	refreshRate := display.refreshRate
	loopPeriod := mainFramePeriod
	for range ticker.C {
		if a.quit.Load() {
			return
		}
		now := time.Now()
		displayChanged := false
		if !now.Before(nextDisplayCheck) {
			newDisplay := a.displaySnapshot()
			displayChanged = newDisplay != display
			display = newDisplay
			refreshRate = display.refreshRate
			nextDisplayCheck = now.Add(time.Second)
		}
		w32, h32 := a.window.GLGetDrawableSize()
		w, h := int(w32), int(h32)
		period, fps := frameTiming(a.settings.Graphics.FrameRate, refreshRate)
		if period != loopPeriod {
			ticker.Reset(period)
			loopPeriod = period
		}
		if a.targetVisualizerFPS != fps {
			a.setVisualizerFPS(fps)
			if a.adaptiveTuningEnabled() && a.rt != nil {
				a.presetProbe.Stop()
				a.resetAdaptiveState(int(w32), int(h32))
				a.renderCost.Reset()
			}
		} else if displayChanged && a.adaptiveTuningEnabled() && a.rt != nil {
			a.presetProbe.Stop()
			a.resetAdaptiveState(int(w32), int(h32))
			a.renderCost.Reset()
		}
		if displayChanged && a.overlay != nil && a.overlay.IsSettingsPage() {
			a.refreshSettingsRows(w, h, refreshRate, a.overlay.SettingsCursor())
		}
		a.uiFramePeriod = period
		dt := now.Sub(state.lastLoop).Seconds()
		state.lastLoop = now
		fpsAvg := a.visualizerTelemetryFPS()

		a.prepareFrame(&state, now, w, h, fpsAvg, refreshRate)
		if !a.handleFrameInput(&state, now, dt, w, h) {
			return
		}
		a.pollPresetLoad()
		a.pollPresetPackOperation(now)
		a.updateFramePlayback(now)
		a.renderFrame(now, w, h)
	}
}

func (a *App) prepareFrame(state *runState, now time.Time, w, h int, fpsAvg float64, refreshRate int32) {
	newVisualizerFrame := state.consumeAdaptiveFrame(&a.vizClock)
	adaptiveReady := !a.vizClock.adaptivePaused && a.adaptiveMeasurementReady()
	params := defaultAdaptiveParams()
	// Use the scheduled cadence so fewer renders actually release budget.
	utilization := a.renderCost.Utilization(a.targetVisualizerFPS)
	cadence := a.renderCost.Cadence(a.targetVisualizerFPS)
	if adaptiveReady && utilization > params.utilizationHigh && !a.onPresetsPage {
		presetName := ""
		if a.presetIdx >= 0 && a.presetIdx < len(a.presetNames) {
			presetName = a.presetNames[a.presetIdx]
		}
		if presetName != state.lowFPSPreset {
			renderW, renderH := a.rt.Size()
			slog.Debug("soft frame budget exceeded", "utilization", utilization, "threshold", params.utilizationHigh, "preset", presetName, "resolution", fmt.Sprintf("%dx%d", renderW, renderH))
			state.lowFPSPreset = presetName
		}
	}
	if adaptiveReady && utilization <= params.utilizationHigh {
		state.lowFPSPreset = "" // reset so next drop on same preset logs again
	}
	winChanged := w != state.prevW || h != state.prevH
	if winChanged {
		a.renderCost.Reset()
		a.presetProbe.Stop()
		adaptiveReady = false
	}
	if winChanged {
		a.presentRequested = true
	}

	if winChanged && state.prevW > 0 && state.prevH > 0 {
		ceiling := config.ResolutionAtMost(config.ComputeResolutions(w, h), a.configuredResolution)
		if !a.adaptiveTuningEnabled() {
			a.applyRenderResolution(ceiling)
		} else {
			warm := currentRenderResolution(a.rt)
			if a.adaptive.Reconfigure(w, h, ceiling, warm, params) {
				a.applyRenderResolution(a.adaptive.resolutions[a.adaptive.index])
			} else {
				slog.Warn("adaptive: empty resolution list on resize", "window", fmt.Sprintf("%dx%d", w, h))
			}
			if a.overlay != nil && a.overlay.IsSettingsPage() {
				a.refreshSettingsRows(w, h, refreshRate, a.overlay.SettingsCursor())
			}
		}
	}
	state.prevW, state.prevH = w, h

	if a.adaptiveTuningEnabled() && (!a.settings.Graphics.VisualizerOff || a.presetPackCalibrationActive()) && !a.onPresetsPage &&
		adaptiveReady && newVisualizerFrame && !a.adaptiveSuspended(now) &&
		(!a.presetPackCalibrationActive() || !a.presetLoadInFlight) {
		// Reconfigure the active profile after setting/window changes without
		// reusing an incompatible measurement.
		key := a.presetProfileKeyAt(a.currentPresetName(), w, h)
		profileChanged := false
		if key != a.presetTuning.active {
			profileChanged = true
			if data, err := presets.Read(key.name); err == nil {
				a.activatePresetProfile(key.name, data)
			} else {
				slog.Debug("preset profile read", "error", err)
			}
		}
		if !profileChanged {
			a.presetTuning.observe(a.adaptive.index, a.adaptive.upscaleFloor,
				a.adaptive.trialAction == adaptiveNone && !a.adaptive.searchActive && cadence <= cadenceTolerance)
			searching := a.adaptive.searchActive
			if searching && a.presetPackCalibrationMeasuring() &&
				a.renderCost.CalibrationSeverelyOverBudget(a.targetVisualizerFPS) {
				a.adaptive.confirmSearchTrial()
			}
			if resolution, action := a.adaptive.Decide(utilization, cadence, now); action != adaptiveNone {
				a.renderCost.Reset()
				switch action {
				case adaptiveResample:
					slog.Debug("adaptive: confirming resolution trial", "cadence_ratio", cadence)
				case adaptiveResolutionDown, adaptiveResolutionUp:
					a.applyRenderResolution(resolution)
					slog.Info("adaptive: resolution step", "resolution", resolution, "utilization", utilization, "cadence_ratio", cadence)
				}
			}
			if searching && !a.adaptive.searchActive {
				a.presetTuning.recordResolution(a.adaptive.index, a.adaptive.upscaleFloor)
				slog.Info("adaptive: resolution search complete",
					"resolution", currentRenderResolution(a.rt),
					"upscale_floor", a.adaptive.upscaleFloor)
				if a.presetPackCalibrationMeasuring() {
					a.finishPresetPackTestPreset(a.currentPresetName())
				}
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
		a.presenter.Update(fpsAvg, a.settings.Graphics.Adaptive,
			effectiveRenderHeight, a.prof.ReadStats(), playback)
	}
}

func (a *App) handleFrameInput(state *runState, now time.Time, dt float64, w, h int) bool {
	favoriteMode := a.favoriteMode()
	windowW, windowH := a.window.GetSize()
	a.inp.SetPointerSpace(input.PointerSpace{
		WindowW:   int(windowW),
		WindowH:   int(windowH),
		DrawableW: w,
		DrawableH: h,
	})
	for e := sdl.PollEvent(); e != nil; e = sdl.PollEvent() {
		if isPrimaryWindowClose(e, a.windowID) {
			slog.Info("window close requested")
			return false
		}
		act := a.inp.ProcessEvent(e, favoriteMode, now)
		pointerEventSeen := false
		for pointerEvent, ok := a.inp.PollPointerEvent(); ok; pointerEvent, ok = a.inp.PollPointerEvent() {
			pointerEventSeen = true
			a.handlePointerEvent(pointerEvent, w, h)
		}
		if pointerEventSeen {
			a.presentRequested = true
		}
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
	onPresets := a.overlay != nil && a.overlay.UIVisible() && a.overlay.IsPresetsPage() && !a.presetPackCalibrationActive()
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
			velocity = a.seek.Speed(1.0, now)
		case state[sdl.SCANCODE_COMMA] != 0:
			velocity = a.seek.Speed(-1.0, now)
		default:
			velocity = a.seek.Speed(a.inp.RightStickX(), now)
		}
		if target := a.seek.Target(a.pl.Position(), velocity, dt); target >= 0 {
			if err := a.pl.Seek(target); err != nil {
				// Backend rejected the seek (e.g. unsupported source) — stop
				// trying so we don't churn the decoder every frame.
				a.seek.lastDir = 0
			}
		}
	} else {
		a.seek.drive.Reset()
	}
	return true
}

func (a *App) updateFramePlayback(now time.Time) {
	a.pollRadio()
	if !a.presetPackCalibrationActive() && !a.settings.Graphics.VisualizerOff && !a.onPresetsPage && a.pending.name != "" && now.After(a.pending.at) {
		a.transitionPreset(a.pending.name)
		a.pending = pendingPreset{}
	}

	if !a.presetPackCalibrationActive() && a.presetTicker != nil && !a.settings.Graphics.VisualizerOff && !a.onPresetsPage {
		select {
		case <-a.presetTicker.C:
			if a.presetLoadInFlight {
				a.presetSwitch.Store(true)
			} else {
				a.randPreset()
			}
		default:
		}
	}

	if !a.presetPackCalibrationActive() && !a.settings.Graphics.VisualizerOff && !a.onPresetsPage && a.settings.PresetInterval == config.PresetAuto && a.presetSwitch.Swap(false) {
		if a.presetLoadInFlight {
			a.presetSwitch.Store(true)
		} else {
			a.randPreset()
		}
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
				if a.radio != nil && player.IsRadio(loadingPath) {
					a.radio.RememberPlayed(loadingPath)
					if a.overlay != nil {
						a.overlay.SetRadioHistory(a.radio.History())
					}
				}
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
	testingPack := a.presetPackCalibrationActive()
	uiOpaque := uiVisible && a.settings.UI.Transparency == 0 && !testingPack
	uiInteracting := uiVisible && a.overlay != nil && a.overlay.InteractionActive(now)
	if a.overlay != nil {
		if a.trackCacheReady.Swap(false) {
			a.overlay.RefreshTrackCache()
		}
		caps := a.inp.PointerCapabilities()
		a.overlay.SetControllerConnected(caps.Controller)
		a.overlay.SetPointerCapabilities(caps.Keyboard, caps.Mouse, caps.Touch)
		a.overlay.SetLeftStickY(a.inp.LeftStickY())
		a.overlay.Update(a.inp.DPadUpHeld(), a.inp.DPadDownHeld())
		uiInteracting = uiVisible && a.overlay.InteractionActive(time.Now())
		if uiInteracting {
			a.presentRequested = true
		}
	}
	paused := (uiVisible && !testingPack) || (a.settings.Graphics.VisualizerOff && !testingPack) || a.onPresetsPage
	if paused != a.vizClock.adaptivePaused {
		a.resetAdaptiveCounters()
		a.renderCost.Reset()
		a.presetProbe.resetWindow()
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
	var presentationCost time.Duration
	if uiInteracting {
		presentationCost = a.presentFrame(time.Now(), w, h, false, uiVisible, uiOpaque)
	}
	started := time.Now()
	rendered := a.renderVisualization(started, w, h, uiOpaque, uiInteracting, budget)
	renderCost := time.Since(started)
	if rendered {
		budget.Complete(started, started.Add(renderCost))
	}
	if uiInteracting {
		if rendered {
			a.recordPresetPackRender(renderCost + presentationCost)
			a.presentRequested = true
		}
		return
	}
	presentationCost = a.presentFrame(time.Now(), w, h, rendered, uiVisible, uiOpaque)
	presented := time.Now()
	if rendered && !a.onPresetsPage {
		a.recordPresetPackRender(renderCost + presentationCost)
		if !paused && !a.adaptiveSuspended(presented) {
			frameWork := renderCost + presentationCost
			a.renderCost.AddFrame(renderCost, presentationCost, presented)
			if a.adaptiveTuningEnabled() {
				if a.presetLoadInFlight {
					a.presetProbe.resetWindow()
				} else {
					a.observePresetProbe(frameWork, presented)
				}
			} else {
				a.presetProbe.Stop()
			}
		}
		if frameWork := renderCost + presentationCost; frameWork >= 100*time.Millisecond {
			renderW, renderH := a.rt.Size()
			slog.Warn("slow visualizer frame",
				"frame_rate", a.settings.Graphics.FrameRate,
				"render_ms", renderCost.Seconds()*1000,
				"presentation_ms", presentationCost.Seconds()*1000,
				"frame_work_ms", frameWork.Seconds()*1000,
				"target_fps", a.targetVisualizerFPS,
				"resolution", fmt.Sprintf("%dx%d", renderW, renderH),
				"adaptive_paused", paused || a.adaptiveSuspended(presented))
		}
	}
}

func (a *App) observePresetProbe(frameWork time.Duration, now time.Time) {
	switch a.presetProbe.Observe(frameWork, now) {
	case presetProbePassed:
		a.presetTuning.markScreened()
		a.adaptive.policy.params = defaultAdaptiveParams()
		a.adaptive.policy.params.upshiftAfter = presetFastUpshiftAfter
		a.adaptive.policy.Restart()
		if a.presetPackCalibrationMeasuring() && !a.adaptiveMeasurementReady() {
			a.presetProbe.Start()
			return
		}
		searchStarted := false
		if a.adaptiveMeasurementReady() {
			resolution, action := a.adaptive.startResolutionSearch(
				a.renderCost.Utilization(a.targetVisualizerFPS),
				a.renderCost.Cadence(a.targetVisualizerFPS),
			)
			if action != adaptiveNone {
				searchStarted = true
				a.renderCost.Reset()
				a.applyRenderResolution(resolution)
				slog.Info("adaptive: resolution search probe", "resolution", resolution)
			} else {
				a.presetTuning.recordResolution(a.adaptive.index, a.adaptive.upscaleFloor)
			}
		} else {
			a.presetTuning.recordResolution(a.adaptive.index, a.adaptive.upscaleFloor)
		}
		if a.presetPackCalibrationMeasuring() && !searchStarted {
			a.finishPresetPackTestPreset(a.currentPresetName())
		}
		slog.Info("preset performance probe passed",
			"preset", a.currentPresetName(),
			"minimum_fps", time.Second.Seconds()/heavyPresetFrameBudget.Seconds(),
			"resolution", currentRenderResolution(a.rt))
	case presetProbeHeavy:
		name := a.currentPresetName()
		a.presetTuning.markHeavy()
		a.refreshPresetTree()
		slog.Warn("preset excluded for low performance",
			"preset", name,
			"max_fps", time.Second.Seconds()/heavyPresetFrameBudget.Seconds(),
			"resolution", currentRenderResolution(a.rt))
		if a.presetPackCalibrationMeasuring() {
			a.finishPresetPackTestPreset(name)
			return
		}
		if !a.loadPresetHard(a.presetIdx + 1) {
			a.loadBuiltInPreset()
		}
	}
}

func (a *App) loadBuiltInPreset() {
	if a.pm != nil {
		a.pm.LoadPresetData(string(presets.DefaultPreset()), false)
	}
	a.presetIdx = -1
	a.presetTuning.active.name = ""
	a.presetProbe.Stop()
	a.presetLoadProbe = nil
	a.adaptive.policy.params = defaultAdaptiveParams()
	a.adaptive.RestartForPreset()
	if len(a.adaptive.resolutions) > 0 {
		a.adaptive.index = a.adaptive.ceilingIndex
		a.applyRenderResolution(a.adaptive.resolutions[a.adaptive.index])
	}
	a.suspendAdaptiveForPresetTransition(time.Now())
	a.adaptiveResumeAt = time.Now()
	if a.overlay != nil {
		a.overlay.SetPresetName("")
	}
	slog.Warn("no playable presets remain; using built-in preset")
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
	if (a.settings.Graphics.VisualizerOff && !a.presetPackCalibrationActive()) || opaque ||
		!budget.Due(now, a.vizClock.framePeriod, interacting) ||
		!a.vizClock.Due(now) {
		return false
	}
	a.pm.SetFPS(int32(time.Second / budget.Period(a.vizClock.framePeriod, interacting)))
	renderStarted := time.Now()
	a.pm.RenderFrame()
	renderFinished := time.Now()
	captureStarted := renderFinished
	a.rt.Capture()
	captureFinished := time.Now()
	a.recordPresetRender(renderStarted, renderFinished, captureStarted, captureFinished)
	if wave := a.readAudio(); len(wave) > 0 {
		a.pm.PCMAddFloat(wave, projectm.Mono)
	}
	a.vizClock.Complete(time.Now())
	return true
}

func (a *App) presentFrame(now time.Time, w, h int, rendered, uiVisible, uiOpaque bool) time.Duration {
	audioOnly := a.settings.Graphics.VisualizerOff && !a.onPresetsPage && !a.presetPackCalibrationActive()
	due := presentationDue(a.settings.Graphics.FrameRate, now, a.nextPresent, rendered, a.presentRequested, uiVisible)
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

	swapStarted := time.Now()
	a.window.GLSwap()
	swapFinished := time.Now()
	budget := &a.vizBudget
	if a.onPresetsPage {
		budget = &a.previewBudget
	}
	// Blit/draw/swap can expose deferred GPU work and UI pressure. Exclude
	// one UI period, including normal vsync waiting, from the cost estimate.
	elapsed := time.Since(presentStart)
	a.recordPresetPresentation(presentStart, elapsed, swapStarted, swapFinished)
	budget.ObservePresentation(elapsed, a.uiFramePeriod)
	return elapsed
}

func (a *App) recordPresetRender(renderStarted, renderFinished, captureStarted, captureFinished time.Time) {
	p := a.presetLoadProbe
	if p == nil || p.renderSamples >= 5 {
		return
	}
	p.renderSamples++
	slog.Debug("preset telemetry",
		"phase", "render",
		"preset", p.name,
		"sample", p.renderSamples,
		"since_load_us", renderStarted.Sub(p.loadedAt).Microseconds(),
		"render_us", renderFinished.Sub(renderStarted).Microseconds(),
		"capture_us", captureFinished.Sub(captureStarted).Microseconds())
	a.finishPresetProbeIfComplete()
}

func (a *App) recordPresetPresentation(presentStarted time.Time, presentDuration time.Duration, swapStarted, swapFinished time.Time) {
	p := a.presetLoadProbe
	if p == nil || p.presentSamples >= 5 {
		return
	}
	p.presentSamples++
	slog.Debug("preset telemetry",
		"phase", "present",
		"preset", p.name,
		"sample", p.presentSamples,
		"since_load_us", presentStarted.Sub(p.loadedAt).Microseconds(),
		"present_us", presentDuration.Microseconds(),
		"swap_us", swapFinished.Sub(swapStarted).Microseconds())
	a.finishPresetProbeIfComplete()
}

func (a *App) finishPresetProbeIfComplete() {
	p := a.presetLoadProbe
	if p != nil && p.renderSamples >= 5 && p.presentSamples >= 5 {
		a.presetLoadProbe = nil
	}
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
		a.pm.SetHardCutEnabled(!a.presetPackCalibrationActive() && !a.settings.Graphics.VisualizerOff && a.settings.PresetInterval == config.PresetAuto)
	}
	a.presetSwitch.Store(false)
	if !a.presetPackCalibrationActive() {
		a.startPresetTicker()
	}

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
	if a.presetPackCalibrationActive() {
		return
	}
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

func currentRenderResolution(rt *projectm.RenderTarget) config.RenderResolution {
	if rt == nil {
		return config.RenderResolution{}
	}
	w, h := rt.Size()
	return config.RenderResolution{Width: w, Height: h}
}

func (a *App) resizeRenderTarget(r config.RenderResolution) {
	if currentW, currentH := a.rt.Size(); currentW == r.Width && currentH == r.Height {
		return
	}
	a.resetPresetPackRender()
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
	if !a.adaptive.Reconfigure(winW, winH, a.configuredResolution, currentRenderResolution(a.rt), defaultAdaptiveParams()) {
		slog.Warn("adaptive: empty resolution list", "window", fmt.Sprintf("%dx%d", winW, winH))
		return
	}
	a.applyRenderResolution(a.adaptive.resolutions[a.adaptive.index])
}

func (a *App) setVisualizerFPS(fps int32) {
	if fps <= 0 {
		return
	}
	a.targetVisualizerFPS = fps
	a.vizClock.framePeriod = time.Second / time.Duration(fps)
	a.vizClock.nextFrame = time.Time{}
}

// Numeric frame-rate limits avoid swapping duplicate backgrounds; UI animation
// has its own deadline and input can request immediate presentation.
func presentationDue(rate config.FrameRate, now, deadline time.Time, rendered, requested, uiVisible bool) bool {
	// A newly rendered frame or explicit input is always presented. Max also
	// requests every display tick, matching the display-refresh setting.
	return rate.IsMax() || rendered || requested || (uiVisible && !now.Before(deadline))
}

// frameTiming keeps the UI ticker responsive while the visualizer cadence is
// controlled independently by the selected frame-rate setting.
func frameTiming(rate config.FrameRate, refreshRate int32) (time.Duration, int32) {
	refreshRate = config.NormalizeRefreshRate(refreshRate)
	rate = config.NormalizeFrameRate(rate, refreshRate)
	if rate.IsMax() {
		return time.Second / time.Duration(refreshRate), refreshRate
	}
	return mainFramePeriod, rate.Target(refreshRate)
}

func (a *App) displaySnapshot() displaySnapshot {
	snapshot := displaySnapshot{index: -1, refreshRate: 60}
	if a.window == nil {
		return snapshot
	}
	index, err := a.window.GetDisplayIndex()
	if err != nil {
		slog.Debug("display index unavailable", "error", err)
		return snapshot
	}
	snapshot.index = index
	mode, err := sdl.GetCurrentDisplayMode(index)
	if err != nil {
		slog.Debug("display mode unavailable", "error", err)
		return snapshot
	}
	snapshot.width, snapshot.height = mode.W, mode.H
	snapshot.refreshRate = config.NormalizeRefreshRate(mode.RefreshRate)
	snapshot.format = mode.Format
	return snapshot
}

func (a *App) displayRefreshRate() int32 {
	return a.displaySnapshot().refreshRate
}

// recoverPlaybackFailure also handles a live stream ending after startup;
// Repeat One must not trap the listener on a failed radio source.
func (a *App) recoverPlaybackFailure(path string) {
	// A local track can disappear after the favorites preflight but before the
	// asynchronous player opens it. Remove only that confirmed-missing entry.
	a.pruneMissingLocalFavorite(path)
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
	if a.overlay != nil && !player.IsRadio(path) {
		a.overlay.ShowTrack(" playback error")
	}
	a.resumePath, a.resumeSeconds = "", 0
	if track, ok := a.nextAfterFailure(path, a.settings.Playback); ok {
		a.playNextTrack(track)
	} else {
		slog.Warn("playback recovery stopped", "reason", "no next candidate or failure limit reached")
		if a.overlay != nil && !player.IsRadio(path) {
			a.overlay.ShowTrack("no playable track; select another track")
		}
	}
	if a.overlay != nil && player.IsRadio(path) {
		a.overlay.ShowMessage(i18n.InfoRadioPlaybackFailed)
	}
}
