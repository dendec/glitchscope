package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/dendec/glitchscope/internal/config"
)

func TestPresetProfilesRequireStableFramesAndSeparateFrameRates(t *testing.T) {
	var tuning presetTuning
	key := presetProfileKey{name: "a", frameRate: config.FrameRate20, adaptive: true, width: 1280, height: 720}
	tuning.activate(key, []byte("milk"))
	for range 29 {
		tuning.observe(4, 3, true)
	}
	if tuning.profiles[key].ready {
		t.Fatal("remembered an unproven resolution")
	}
	tuning.observe(4, 3, false)
	for range 29 {
		tuning.observe(4, 3, true)
	}
	if tuning.profiles[key].ready {
		t.Fatal("unstable interval counted toward stability")
	}
	tuning.observe(4, 3, true)
	if p := tuning.activate(key, []byte("milk")); !p.ready || p.index != 4 || p.floor != 3 {
		t.Fatalf("profile = %+v", p)
	}
	tuning.markHeavy()
	if !tuning.profiles[key].heavy {
		t.Fatal("heavy preset not marked")
	}
	if p := tuning.activate(key, []byte("edited milk")); p.ready || p.heavy {
		t.Fatal("edited preset reused stale profile")
	}
	key.targetFPS = 60
	if p := tuning.activate(key, []byte("milk")); p.ready {
		t.Fatal("resolved target FPS reused stale profile")
	}
	key.frameRate = config.FrameRate30
	if p := tuning.activate(key, []byte("milk")); p.ready {
		t.Fatal("frame-rate setting reused stale profile")
	}
	for i := range maxPresetProfiles + 10 {
		key.name = fmt.Sprint(i)
		tuning.activate(key, nil)
	}
	if len(tuning.profiles) != maxPresetProfiles {
		t.Fatalf("profile count = %d", len(tuning.profiles))
	}
}

func TestEnergySavingPresentation(t *testing.T) {
	now := time.Unix(1, 0)
	next := now.Add(time.Second / 24)
	for _, rate := range []config.FrameRate{config.FrameRate25, config.FrameRate20} {
		if presentationDue(rate, now, next, false, false, false) {
			t.Fatal("duplicate frame presented")
		}
		if !presentationDue(rate, now, next, true, false, false) {
			t.Fatal("new visualizer frame suppressed")
		}
		if !presentationDue(rate, now, next, false, true, false) {
			t.Fatal("input response suppressed")
		}
		if presentationDue(rate, now, next, false, false, true) {
			t.Fatal("UI animation ignored deadline")
		}
		if !presentationDue(rate, next, next, false, false, true) {
			t.Fatal("UI animation deadline missed")
		}
	}
	if !presentationDue(config.FrameRateMax, now, next, false, false, false) {
		t.Fatal("Max cadence changed")
	}
}

func TestNewPresetWarmStartsAtCurrentResolution(t *testing.T) {
	var state resolutionState
	params := defaultAdaptiveParams()
	state.Reset(1280, 720, config.RenderResolution{Width: 640, Height: 360}, params)
	state.index = 5
	state.downBlocked = true
	state.upscaleFloor = 5
	state.trialAction = adaptiveResolutionDown
	state.startPreset(presetProfile{}, state.index)
	if state.index != 5 || state.downBlocked || state.trialAction != adaptiveNone || state.upscaleFloor != state.ceilingIndex {
		t.Fatalf("new preset did not preserve warm start: %+v", state)
	}
	state.startPreset(presetProfile{ready: true, index: 6, floor: 6}, 5)
	if state.index != 6 {
		t.Fatal("known lower-resolution profile was not accepted")
	}
	state.startPreset(presetProfile{ready: true, index: 2, floor: 2}, 5)
	if state.index != 5 {
		t.Fatal("known higher-resolution profile caused an immediate upscale")
	}
	state.startPreset(presetProfile{}, -1)
	if state.index != state.ceilingIndex {
		t.Fatal("warm start was not clamped to the ceiling")
	}
}

func TestNewPresetWarmStartKeepsEqualProfileUpscaleFloor(t *testing.T) {
	var state resolutionState
	params := defaultAdaptiveParams()
	if !state.Reset(1280, 720, config.RenderResolution{Width: 1280, Height: 720}, params) {
		t.Fatal("resolution state did not initialize")
	}
	warmIndex := state.floorIndex
	state.startPreset(presetProfile{ready: true, index: warmIndex, floor: warmIndex}, warmIndex)
	if state.index != warmIndex || state.upscaleFloor != warmIndex {
		t.Fatalf("equal profile floor was lost: index=%d upscaleFloor=%d", state.index, state.upscaleFloor)
	}
}
