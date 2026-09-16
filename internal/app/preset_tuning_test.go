package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/dendec/glitchscope/internal/config"
)

func TestPresetProfilesRequireStableFramesAndSeparateModes(t *testing.T) {
	var tuning presetTuning
	key := presetProfileKey{name: "a", mode: config.PerfModeEco, width: 1280, height: 720}
	tuning.activate(key, []byte("milk"))
	for range 29 {
		tuning.observe(4, 3, 15, true)
	}
	if tuning.profiles[key].ready {
		t.Fatal("remembered an unproven resolution")
	}
	tuning.observe(4, 3, 15, false)
	for range 29 {
		tuning.observe(4, 3, 15, true)
	}
	if tuning.profiles[key].ready {
		t.Fatal("unstable interval counted toward stability")
	}
	tuning.observe(4, 3, 15, true)
	if p := tuning.activate(key, []byte("milk")); !p.ready || p.index != 4 || p.floor != 3 || p.fps != 15 {
		t.Fatalf("profile = %+v", p)
	}
	tuning.markHeavy()
	if !tuning.profiles[key].heavy {
		t.Fatal("heavy preset not marked")
	}
	if p := tuning.activate(key, []byte("edited milk")); p.ready || p.heavy {
		t.Fatal("edited preset reused stale profile")
	}
	key.mode = config.PerfModePerformance
	if p := tuning.activate(key, []byte("milk")); p.ready {
		t.Fatal("mode reused stale profile")
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
	for _, mode := range []config.PerformanceMode{config.PerfModeBalanced, config.PerfModeEco} {
		if presentationDue(mode, now, next, false, false, false) {
			t.Fatal("duplicate frame presented")
		}
		if !presentationDue(mode, now, next, true, false, false) {
			t.Fatal("new visualizer frame suppressed")
		}
		if !presentationDue(mode, now, next, false, true, false) {
			t.Fatal("input response suppressed")
		}
		if presentationDue(mode, now, next, false, false, true) {
			t.Fatal("UI animation ignored deadline")
		}
		if !presentationDue(mode, next, next, false, false, true) {
			t.Fatal("UI animation deadline missed")
		}
	}
	if !presentationDue(config.PerfModePerformance, now, next, false, false, false) {
		t.Fatal("performance mode cadence changed")
	}
}

func TestNewPresetImmediatelyRestoresQuality(t *testing.T) {
	var state resolutionState
	params := config.PerfModeEco.Params()
	state.Reset(1280, 720, config.RenderResolution{Width: 640, Height: 360}, params)
	state.index = state.floorIndex
	state.downBlocked = true
	state.upscaleFloor = state.floorIndex
	state.trialAction = adaptiveResolutionDown
	fps := state.startPreset(presetProfile{})
	if state.index != state.ceilingIndex || fps != 20 || state.downBlocked || state.trialAction != adaptiveNone || state.upscaleFloor != state.ceilingIndex {
		t.Fatalf("new preset retained degradation: %+v, fps=%d", state, fps)
	}
	fps = state.startPreset(presetProfile{ready: true, index: state.floorIndex, floor: state.floorIndex, fps: 15})
	if state.index != state.floorIndex || fps != 15 {
		t.Fatal("known preset did not restore measured cadence and resolution")
	}
}
