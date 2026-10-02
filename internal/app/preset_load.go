package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dendec/glitchscope/internal/presets"
	"github.com/dendec/glitchscope/internal/projectm"
)

// presetLoadResult is an immutable snapshot produced by the CPU-side reader.
// No projectM or OpenGL state crosses the worker boundary.
type presetLoadResult struct {
	requestID      uint64
	name           string
	data           []byte
	smooth         bool
	readStarted    time.Time
	readFinished   time.Time
	err            error
	beginStarted   time.Time
	beginDone      time.Time
	commitDuration time.Duration
}

// requestPresetLoadWithTransition replaces any older request. The worker only
// reads the archive; all native preparation stays on the main GL thread.
func (a *App) requestPresetLoadWithTransition(name string, smooth bool) {
	if name == "" {
		return
	}

	requestID := a.presetRequestID.Add(1)
	a.presetSwitch.Store(false)
	slog.Debug("preset telemetry", "phase", "begin", "request_id", requestID, "preset", name, "smooth_transition", smooth)
	if a.presetLoadCancel != nil {
		a.presetLoadCancel()
		a.presetLoadCancel = nil
	}
	if a.pm != nil {
		a.pm.CancelPresetLoad()
	}
	a.presetLoadData = nil
	a.presetLoadInFlight = true
	a.presetLoadFrames = 0

	parent := a.appCtx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	a.presetLoadCancel = cancel
	if a.presetLoadResults == nil {
		a.presetLoadResults = make(chan presetLoadResult, 4)
	}
	a.presetLoadWg.Add(1)
	go func() {
		defer a.presetLoadWg.Done()
		started := time.Now()
		data, err := presets.Read(name)
		result := presetLoadResult{
			requestID:    requestID,
			name:         name,
			data:         data,
			smooth:       smooth,
			readStarted:  started,
			readFinished: time.Now(),
			err:          err,
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case a.presetLoadResults <- result:
		case <-ctx.Done():
		}
	}()
}

func (a *App) cancelPresetLoad() {
	a.presetRequestID.Add(1)
	if a.presetLoadCancel != nil {
		a.presetLoadCancel()
		a.presetLoadCancel = nil
	}
	if a.pm != nil {
		a.pm.CancelPresetLoad()
	}
	a.presetLoadData = nil
	a.presetLoadInFlight = false
	a.presetLoadFrames = 0
}

// pollPresetLoad advances the worker/native pipeline once per main-loop
// iteration. The active preset is rendered until the native job is committed.
func (a *App) pollPresetLoad() {
	if a.presetLoadInFlight {
		a.presetLoadFrames++
	}
	var latest *presetLoadResult
	for {
		select {
		case result := <-a.presetLoadResults:
			if result.requestID == a.presetRequestID.Load() {
				copy := result
				latest = &copy
			}
		default:
			goto resultDrained
		}
	}

resultDrained:
	if latest != nil {
		if a.presetLoadCancel != nil {
			a.presetLoadCancel()
			a.presetLoadCancel = nil
		}
		if latest.err != nil {
			a.finishPresetLoadFailure(latest, latest.err)
			return
		}
		if a.pm == nil {
			a.finishPresetLoadFailure(latest, context.Canceled)
			return
		}
		slog.Debug("preset telemetry",
			"phase", "cpu_prepare",
			"request_id", latest.requestID,
			"preset", latest.name,
			"bytes", len(latest.data),
			"read_us", latest.readFinished.Sub(latest.readStarted).Microseconds())
		slog.Debug("preset telemetry", "phase", "gl_prepare_begin", "request_id", latest.requestID,
			"preset", latest.name, "parallel_shader_compile", projectm.ParallelShaderCompile(),
			"shader_compile_mode", projectm.ShaderCompileMode())
		latest.beginStarted = time.Now()
		if !a.pm.BeginPresetLoad(string(latest.data), latest.smooth) {
			errText := a.pm.PresetLoadError()
			if errText == "" {
				errText = "native preset preparation failed"
			}
			a.finishPresetLoadFailure(latest, errors.New(errText))
			return
		}
		latest.beginDone = time.Now()
		createMS, initializeMS, expressionsMS, framebuffersMS, warpShaderMS, compositeShaderMS := a.pm.PresetPrepareTimes()
		slog.Debug("preset telemetry", "phase", "gl_prepare_started", "request_id", latest.requestID, "preset", latest.name,
			"gl_prepare_us", latest.beginDone.Sub(latest.beginStarted).Microseconds(),
			"native_create_ms", createMS, "native_initialize_ms", initializeMS,
			"native_expressions_ms", expressionsMS, "native_framebuffers_ms", framebuffersMS,
			"native_warp_shader_ms", warpShaderMS, "native_composite_shader_ms", compositeShaderMS)
		a.presetLoadData = latest
	}

	data := a.presetLoadData
	if data == nil || data.requestID != a.presetRequestID.Load() || a.pm == nil {
		return
	}

	switch status := a.pm.PollPresetLoad(); status {
	case projectm.PresetLoadReady:
		slog.Debug("preset telemetry",
			"phase", "shader_compile_completed",
			"request_id", data.requestID,
			"preset", data.name,
			"shader_wait_us", time.Since(data.beginDone).Microseconds())
		commitStarted := time.Now()
		if !a.pm.CommitPresetLoad() {
			errText := a.pm.PresetLoadError()
			if errText == "" {
				errText = "native preset commit failed"
			}
			a.finishPresetLoadFailure(data, errors.New(errText))
			return
		}
		data.commitDuration = time.Since(commitStarted)
		a.finishPresetLoadCommit(data)
	case projectm.PresetLoadFailed:
		errText := a.pm.PresetLoadError()
		if errText == "" {
			errText = "native preset preparation failed"
		}
		a.finishPresetLoadFailure(data, errors.New(errText))
	}
}

func (a *App) finishPresetLoadCommit(data *presetLoadResult) {
	committedAt := time.Now()
	if a.presetPackCalibrationActive() {
		a.adaptive.RestartForPreset()
		a.renderCost.Reset()
		a.adaptiveResumeAt = committedAt
	} else {
		a.suspendAdaptiveForPresetTransition(committedAt)
	}
	if !data.smooth && !a.presetPackCalibrationActive() {
		a.adaptiveResumeAt = committedAt
	}
	a.presetLoadProbe = &presetLoadProbe{name: data.name, loadedAt: committedAt}

	meta := presets.ParseMeta(data.data)
	randomTextures := 0
	for _, reference := range presets.TextureReferences(data.data) {
		if reference.Kind == "random" {
			randomTextures++
		}
	}
	renderW, renderH := a.rt.Size()
	slog.Debug("preset telemetry",
		"phase", "commit",
		"request_id", data.requestID,
		"preset", data.name,
		"bytes", len(data.data),
		"per_frame_eqs", meta.PerFrameEqs,
		"per_pixel_eqs", meta.PerPixelEqs,
		"shapes", meta.Shapes,
		"waves", meta.Waves,
		"external_textures", meta.Textures,
		"random_textures", randomTextures,
		"smooth_transition", data.smooth,
		"read_us", data.readFinished.Sub(data.readStarted).Microseconds(),
		"gl_prepare_us", data.beginDone.Sub(data.beginStarted).Microseconds(),
		"shader_wait_us", (committedAt.Sub(data.beginDone) - data.commitDuration).Microseconds(),
		"commit_us", data.commitDuration.Microseconds(),
		"total_us", committedAt.Sub(data.readStarted).Microseconds(),
		"frames_while_loading", a.presetLoadFrames,
		"resolution", fmt.Sprintf("%dx%d", renderW, renderH))
	a.applyPresetName(data.name)
	a.activatePresetProfile(data.name, data.data)
	a.presetLoadData = nil
	a.presetLoadInFlight = false
	a.presetLoadFrames = 0
	if state := a.presetPackTest; state != nil && state.restoring && state.restoreName == data.name {
		a.finishPresetPackTestRestore()
	}
}

func (a *App) finishPresetLoadFailure(data *presetLoadResult, err error) {
	if data == nil {
		slog.Warn("preset load failed", "error", err)
	} else {
		slog.Warn("preset load failed", "request_id", data.requestID, "preset", data.name, "error", err)
	}
	a.presetLoadData = nil
	a.presetLoadInFlight = false
	a.presetLoadFrames = 0
	if data != nil {
		if state := a.presetPackTest; state != nil {
			if state.restoring && state.restoreName == data.name {
				a.loadBuiltInPreset()
				a.adaptive = state.adaptiveBefore
				a.applyRenderResolution(state.originalResolution)
				a.finishPresetPackTestRestore()
				return
			}
			if !state.restoring && state.current == data.name {
				a.recordPresetPackTestFailure(data.name, err)
			}
		}
	}
}
