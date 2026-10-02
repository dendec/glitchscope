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
	if got := a.presetPackItems[0].TestFPS; got != 45 {
		t.Fatalf("averaged measured FPS = %d, want 45", got)
	}
	a.presetPackTest.restoring = true
	a.recordPresetPackRender(time.Second / 20)
	if got := a.presetPackItems[0].TestFPS; got != 45 {
		t.Fatalf("restore-phase FPS = %d, want unchanged 45", got)
	}
}
