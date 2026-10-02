package app

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/presets"
)

type presetPackTestState struct {
	id                 string
	names              []string
	index              int
	current            string
	restoreName        string
	originalPreset     string
	originalResolution config.RenderResolution
	adaptiveBefore     resolutionState
	renderFPS          fpsMeter
	complete           bool
	restoring          bool
}

func presetPackPresetNames(names []string, packName string) []string {
	prefix := packName + "/"
	result := make([]string, 0)
	for _, name := range names {
		if strings.HasPrefix(name, prefix) {
			result = append(result, name)
		}
	}
	return result
}

func presetProfileFullyTested(profile presetProfile, exists bool) bool {
	return exists && (profile.heavy || profile.screened && profile.ready)
}

func (a *App) presetPackCalibrationActive() bool {
	return a.presetPackTest != nil
}

func (a *App) presetPackCalibrationMeasuring() bool {
	return a.presetPackTest != nil && !a.presetPackTest.restoring
}

func (a *App) adaptiveTuningEnabled() bool {
	return a.settings != nil && (a.settings.Graphics.Adaptive || a.presetPackCalibrationMeasuring())
}

func (a *App) adaptiveMeasurementReady() bool {
	if a.presetPackCalibrationMeasuring() {
		return a.renderCost.CalibrationFull(a.targetVisualizerFPS)
	}
	return a.renderCost.Full()
}

func (a *App) startPresetPackTest(id string) {
	if a.presetPackTest != nil || a.presetPackCancel != nil || a.settings == nil || a.window == nil || a.pm == nil || a.rt == nil {
		return
	}
	var packName string
	for _, pack := range presets.AvailablePacks() {
		if pack.ID == id {
			packName = pack.Name
			break
		}
	}
	if packName == "" {
		return
	}
	installed := false
	for _, item := range a.presetPackItems {
		if item.ID == id {
			installed = item.Installed
			break
		}
	}
	if !installed {
		return
	}
	names := presetPackPresetNames(a.presetNames, packName)
	if len(names) == 0 {
		a.setPresetPackTestError(id, "No presets found in this collection")
		return
	}
	width, height := a.window.GLGetDrawableSize()
	adaptiveBefore := a.adaptive
	originalResolution := currentRenderResolution(a.rt)
	if !a.adaptive.Configure(int(width), int(height), a.configuredResolution, defaultAdaptiveParams()) {
		a.setPresetPackTestError(id, "No render resolutions available for testing")
		return
	}

	a.presetPackTest = &presetPackTestState{
		id:                 id,
		names:              names,
		originalPreset:     a.currentPresetName(),
		originalResolution: originalResolution,
		adaptiveBefore:     adaptiveBefore,
		complete:           true,
	}
	a.pending = pendingPreset{}
	a.presetSwitch.Store(false)
	a.presetProbe.Stop()
	if a.presetTicker != nil {
		a.presetTicker.Stop()
		a.presetTicker = nil
	}
	a.pm.SetHardCutEnabled(false)
	for i := range a.presetPackItems {
		if a.presetPackItems[i].ID == id {
			a.presetPackItems[i].Error = ""
			a.presetPackItems[i].Testing = true
			a.presetPackItems[i].Tested = false
			a.presetPackItems[i].TestProgress = 0
			a.presetPackItems[i].TestTotal = len(names)
			break
		}
	}
	a.publishPresetPackItems()
	a.startNextPresetPackTest()
	slog.Info("preset pack test started", "pack", id, "presets", len(names))
}

func (a *App) startNextPresetPackTest() {
	state := a.presetPackTest
	if state == nil || state.restoring {
		return
	}
	previousIndex := state.index
	for state.index < len(state.names) {
		name := state.names[state.index]
		key := a.presetProfileKey(name)
		profile, exists := a.presetTuning.profiles[key]
		if !presetProfileFullyTested(profile, exists) {
			break
		}
		state.index++
	}
	if state.index != previousIndex {
		a.publishPresetPackTestProgress()
	}
	if state.index < len(state.names) {
		state.current = state.names[state.index]
		state.renderFPS.Reset()
		a.requestPresetLoadWithTransition(state.current, false)
		return
	}
	a.restoreAfterPresetPackTest()
}

func (a *App) finishPresetPackTestPreset(name string) {
	state := a.presetPackTest
	if state == nil || state.restoring || state.current != name {
		return
	}
	state.index++
	a.publishPresetPackTestProgress()
	a.startNextPresetPackTest()
}

func (a *App) publishPresetPackTestProgress() {
	state := a.presetPackTest
	if state == nil {
		return
	}
	for i := range a.presetPackItems {
		if a.presetPackItems[i].ID == state.id {
			a.presetPackItems[i].TestProgress = state.index
			a.presetPackItems[i].TestFPS = 0
			break
		}
	}
	a.publishPresetPackItems()
}

func (a *App) recordPresetPackRender(duration time.Duration) {
	state := a.presetPackTest
	if state == nil || state.restoring || duration <= 0 {
		return
	}
	state.renderFPS.AddDuration(duration)
	fps := int(state.renderFPS.Average() + 0.5)
	for i := range a.presetPackItems {
		if a.presetPackItems[i].ID == state.id && a.presetPackItems[i].TestFPS != fps {
			a.presetPackItems[i].TestFPS = fps
			a.publishPresetPackItems()
			return
		}
	}
}

func (a *App) cancelPresetPackTest(id string) {
	state := a.presetPackTest
	if state == nil || state.id != id || state.restoring {
		return
	}
	state.complete = false
	a.restoreAfterPresetPackTest()
}

func (a *App) restoreAfterPresetPackTest() {
	state := a.presetPackTest
	if state == nil || state.restoring {
		return
	}
	state.restoring = true
	a.presetProbe.Stop()

	restoreName := state.originalPreset
	if restoreName != "" && a.presetHeavy(restoreName) {
		restoreName = ""
		if index, ok := nextAvailablePresetIndex(a.presetNames, a.presetIdx+1, a.presetHeavy); ok {
			restoreName = a.presetNames[index]
		}
	}
	state.restoreName = restoreName
	a.adaptive = state.adaptiveBefore
	if restoreName == "" {
		a.loadBuiltInPreset()
		a.adaptive = state.adaptiveBefore
		a.applyRenderResolution(state.originalResolution)
		a.finishPresetPackTestRestore()
		return
	}
	a.requestPresetLoadWithTransition(restoreName, false)
}

func (a *App) finishPresetPackTestRestore() {
	state := a.presetPackTest
	if state == nil || !state.restoring {
		return
	}
	if !a.settings.Graphics.Adaptive {
		a.adaptive = state.adaptiveBefore
		a.applyRenderResolution(state.originalResolution)
	}
	for i := range a.presetPackItems {
		if a.presetPackItems[i].ID == state.id {
			a.presetPackItems[i].Testing = false
			a.presetPackItems[i].Tested = state.complete
			a.presetPackItems[i].TestProgress = state.index
			a.presetPackItems[i].TestFPS = 0
			break
		}
	}
	a.presetPackTest = nil
	a.publishPresetPackItems()
	if a.overlay == nil || !a.overlay.UIVisible() {
		if a.pm != nil {
			a.pm.SetHardCutEnabled(!a.settings.Graphics.VisualizerOff && a.settings.PresetInterval == config.PresetAuto)
		}
		a.resetPresetTicker()
	}
	slog.Info("preset pack test finished", "pack", state.id, "tested", state.index, "total", len(state.names), "complete", state.complete)
}

func (a *App) setPresetPackTestError(id, message string) {
	for i := range a.presetPackItems {
		if a.presetPackItems[i].ID == id {
			a.presetPackItems[i].Error = message
			break
		}
	}
	a.publishPresetPackItems()
}

func (a *App) recordPresetPackTestFailure(name string, err error) {
	if err != nil {
		a.presetPackTest.complete = false
		a.setPresetPackTestError(a.presetPackTest.id, fmt.Sprintf("Skipped %s: %v", name, err))
		slog.Warn("preset pack test skipped preset", "preset", name, "error", err)
	}
	a.finishPresetPackTestPreset(name)
}
