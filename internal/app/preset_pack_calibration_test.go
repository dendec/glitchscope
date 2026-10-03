package app

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/ui"
)

func TestPresetPackPresetNamesUsesCollectionRootBoundary(t *testing.T) {
	names := []string{
		"Cream of the Crop/Transitions/!crossfade.milk",
		"Cream of the Crop/Fractal/demo.milk",
		"Cream of the Crop Extras/demo.milk",
		"Isosceles Mashups 2020/Loops/loop.milk",
	}
	want := []string{
		"Cream of the Crop/Transitions/!crossfade.milk",
		"Cream of the Crop/Fractal/demo.milk",
	}
	if got := presetPackPresetNames(names, "Cream of the Crop"); !reflect.DeepEqual(got, want) {
		t.Fatalf("presetPackPresetNames() = %v, want %v", got, want)
	}
}

func TestPresetPackTestFailureKeepsCollectionIncomplete(t *testing.T) {
	state := &presetPackTestState{id: "cream", complete: true, restoring: true}
	a := &App{
		presetPackTest:  state,
		presetPackItems: []ui.PresetPackItem{{ID: "cream", Testing: true}},
	}
	a.recordPresetPackTestFailure("Cream/broken.milk", errors.New("invalid preset"))
	if state.complete {
		t.Fatal("failed preset was counted as a completed collection test")
	}
}

func TestAdaptiveTuningIsEnabledForCollectionCalibration(t *testing.T) {
	a := &App{settings: &config.Settings{}}
	if a.adaptiveTuningEnabled() {
		t.Fatal("adaptive tuning should be disabled outside calibration")
	}
	a.presetPackTest = &presetPackTestState{}
	if !a.adaptiveTuningEnabled() {
		t.Fatal("calibration should enable adaptive tuning when the setting is off")
	}
	a.presetPackTest.restoring = true
	if a.adaptiveTuningEnabled() {
		t.Fatal("restore phase should not run a second calibration")
	}
	a.settings.Graphics.Adaptive = true
	if !a.adaptiveTuningEnabled() {
		t.Fatal("adaptive setting should remain enabled during restore")
	}
}

func TestRecordPresetPackRenderPublishesCurrentPresetFPS(t *testing.T) {
	a := &App{
		presetPackTest:  &presetPackTestState{id: "cream"},
		presetPackItems: []ui.PresetPackItem{{ID: "cream", Testing: true}},
	}
	a.recordPresetPackRender(time.Second / 30)
	if got := a.presetPackItems[0].TestFPS; got != 30 {
		t.Fatalf("first measured FPS = %d, want 30", got)
	}
	a.recordPresetPackRender(time.Second / 60)
	if got := a.presetPackItems[0].TestFPS; got != 40 {
		t.Fatalf("averaged measured FPS = %d, want 40", got)
	}
	a.presetPackTest.restoring = true
	a.recordPresetPackRender(time.Second / 20)
	if got := a.presetPackItems[0].TestFPS; got != 40 {
		t.Fatalf("restore-phase FPS = %d, want unchanged 40", got)
	}
}

func TestPresetPackTelemetryUsesFrameCostAndResetsMeasurements(t *testing.T) {
	a := &App{
		presetNames:    []string{"Cream/test.milk"},
		presetPackTest: &presetPackTestState{id: "cream", current: "Cream/test.milk"},
		presetPackItems: []ui.PresetPackItem{{
			ID: "cream", Testing: true,
		}},
	}
	a.vizClock.meter.AddDuration(time.Second / 30)
	a.recordPresetPackRender(100 * time.Millisecond)
	if got := a.visualizerTelemetryFPS(); got != 10 || a.presetPackItems[0].TestFPS != 10 {
		t.Fatalf("telemetry=%f panel=%d, want measured 10 FPS rather than 30 FPS cadence", got, a.presetPackItems[0].TestFPS)
	}
	a.resetPresetPackRender()
	if a.visualizerTelemetryFPS() != 0 || a.presetPackItems[0].TestFPS != 0 {
		t.Fatal("measurement reset retained FPS from the previous preset or resolution")
	}
	a.presetLoadInFlight = true
	a.recordPresetPackRender(time.Millisecond)
	a.presetLoadInFlight = false
	a.presetPackTest.current = "Cream/next.milk"
	a.recordPresetPackRender(time.Millisecond)
	if a.visualizerTelemetryFPS() != 0 {
		t.Fatal("loading frames or previous-preset frames contaminated test FPS")
	}
	a.presetPackTest.current = a.currentPresetName()
	a.recordPresetPackRender(200 * time.Millisecond)
	if got := a.visualizerTelemetryFPS(); got != 5 || a.presetPackItems[0].TestFPS != 5 {
		t.Fatalf("new measurement=%f panel=%d, want 5 FPS", got, a.presetPackItems[0].TestFPS)
	}
	a.presetPackTest = nil
	if got := int(a.visualizerTelemetryFPS() + 0.5); got != 30 {
		t.Fatalf("normal telemetry=%d, want restored 30 FPS cadence", got)
	}
}
