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
	if !tuning.profiles[key].heavy || !tuning.profiles[key].screened {
		t.Fatal("heavy preset not marked")
	}
	for range 30 {
		tuning.observe(4, 3, true)
	}
	if !tuning.profiles[key].heavy {
		t.Fatal("stable resolution learning cleared the heavy mark")
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

func TestPresetProbeIgnoresOneLongFrame(t *testing.T) {
	var probe presetPerformanceProbe
	probe.Start()
	now := time.Unix(100, 0)
	for range presetProbeWarmup {
		probe.Observe(10*time.Millisecond, now)
		now = now.Add(50 * time.Millisecond)
	}
	for _, cost := range []time.Duration{10 * time.Millisecond, time.Second, 10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond} {
		if got := probe.Observe(cost, now); got == presetProbeHeavy {
			t.Fatal("one stalled frame marked the preset heavy")
		}
		now = now.Add(50 * time.Millisecond)
	}
}

func TestPresetProbeQuicklyMarksExtremelySlowPreset(t *testing.T) {
	var probe presetPerformanceProbe
	probe.Start()
	now := time.Unix(100, 0)
	for range presetProbeSamples - 1 {
		if got := probe.Observe(time.Second, now); got != presetProbePending {
			t.Fatalf("early slow-frame result = %v, want pending", got)
		}
		now = now.Add(time.Second)
	}
	if got := probe.Observe(time.Second, now); got != presetProbeHeavy {
		t.Fatalf("three 1 FPS frames = %v, want heavy", got)
	}
}

func TestPresetProbeQuickPathIgnoresSingleSevereStall(t *testing.T) {
	var probe presetPerformanceProbe
	probe.Start()
	now := time.Unix(100, 0)
	for _, cost := range []time.Duration{time.Second, 16 * time.Millisecond, 16 * time.Millisecond, 16 * time.Millisecond} {
		if got := probe.Observe(cost, now); got == presetProbeHeavy {
			t.Fatal("one severe stall marked the preset heavy")
		}
		now = now.Add(cost)
	}
}

func TestPresetProbeRestartsAfterLongFrameGap(t *testing.T) {
	var probe presetPerformanceProbe
	probe.Start()
	now := time.Unix(100, 0)
	for range presetProbeWarmup {
		probe.Observe(10*time.Millisecond, now)
		now = now.Add(50 * time.Millisecond)
	}
	for range 3 {
		probe.Observe(60*time.Millisecond, now)
		now = now.Add(100 * time.Millisecond)
	}
	if got := probe.Observe(10*time.Millisecond, now.Add(time.Second)); got == presetProbeHeavy {
		t.Fatal("a long pause completed a heavy classification")
	}
}

func TestPresetProbeRequiresSustainedSub20FPS(t *testing.T) {
	var probe presetPerformanceProbe
	probe.Start()
	now := time.Unix(100, 0)
	for range presetProbeWarmup {
		probe.Observe(10*time.Millisecond, now)
		now = now.Add(50 * time.Millisecond)
	}
	for range 3 {
		if got := probe.Observe(60*time.Millisecond, now); got != presetProbePending {
			t.Fatalf("probe classified too early: %v", got)
		}
		now = now.Add(100 * time.Millisecond)
	}
	if got := probe.Observe(60*time.Millisecond, now.Add(400*time.Millisecond)); got != presetProbeHeavy {
		t.Fatalf("sustained 16.7 FPS result = %v, want heavy", got)
	}
}

func TestPresetProbePassesAt20FPSOrBetter(t *testing.T) {
	var probe presetPerformanceProbe
	probe.Start()
	now := time.Unix(100, 0)
	for range presetProbeWarmup {
		probe.Observe(10*time.Millisecond, now)
		now = now.Add(50 * time.Millisecond)
	}
	for range 3 {
		if got := probe.Observe(50*time.Millisecond, now); got != presetProbePending {
			t.Fatalf("probe classified too early: %v", got)
		}
		now = now.Add(100 * time.Millisecond)
	}
	if got := probe.Observe(50*time.Millisecond, now.Add(400*time.Millisecond)); got != presetProbePassed {
		t.Fatalf("20 FPS result = %v, want passed", got)
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

func TestNewPresetRestoresCompatibleLearnedResolution(t *testing.T) {
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
	state.startPreset(presetProfile{ready: true, index: 5, floor: 5}, 5)
	if state.index != 5 || state.upscaleFloor != 5 {
		t.Fatal("known lower-resolution profile was not restored")
	}
	state.startPreset(presetProfile{ready: true, index: 4, floor: 4}, 5)
	if state.index != 4 || state.upscaleFloor != 4 {
		t.Fatal("known higher-resolution profile was not restored")
	}
	state.startPreset(presetProfile{}, -1)
	if state.index != state.ceilingIndex {
		t.Fatal("warm start was not clamped to the ceiling")
	}
}

func TestUnknownPresetStartsAtMinimumForProbe(t *testing.T) {
	var state resolutionState
	if !state.Reset(1280, 720, config.RenderResolution{Width: 1280, Height: 720}, defaultAdaptiveParams()) {
		t.Fatal("resolution state did not initialize")
	}
	state.index = state.ceilingIndex
	state.startPresetProbe()
	if state.index != state.floorIndex || state.upscaleFloor != state.ceilingIndex || state.downBlocked || state.trialAction != adaptiveNone {
		t.Fatalf("probe did not start at minimum: %+v", state)
	}
}

func TestPresetNeedsPerformanceProbeBeforeSmoothTransition(t *testing.T) {
	if !presetNeedsPerformanceProbe(true, 4, presetProfile{}, false) {
		t.Fatal("unknown adaptive preset should be screened")
	}
	if presetNeedsPerformanceProbe(false, 4, presetProfile{}, false) {
		t.Fatal("adaptive-off preset should not be screened")
	}
	if presetNeedsPerformanceProbe(true, 4, presetProfile{screened: true}, true) {
		t.Fatal("screened preset should not be probed again")
	}
	if presetNeedsPerformanceProbe(true, 0, presetProfile{}, false) {
		t.Fatal("preset without adaptive resolutions should not be probed")
	}
}

func TestPresetProfileFullyTested(t *testing.T) {
	for _, test := range []struct {
		name    string
		profile presetProfile
		exists  bool
		want    bool
	}{
		{name: "missing", want: false},
		{name: "unknown", exists: true, want: false},
		{name: "screened without resolution", profile: presetProfile{screened: true}, exists: true, want: false},
		{name: "passed and tuned", profile: presetProfile{screened: true, ready: true}, exists: true, want: true},
		{name: "heavy", profile: presetProfile{screened: true, heavy: true}, exists: true, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := presetProfileFullyTested(test.profile, test.exists); got != test.want {
				t.Fatalf("presetProfileFullyTested() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestPresetHeavyUsesInitializedProfileContext(t *testing.T) {
	context := presetProfileKey{
		frameRate:    config.FrameRate30,
		targetFPS:    30,
		adaptive:     true,
		width:        1280,
		height:       720,
		ceilingIndex: 2,
	}
	key := context
	key.name = "slow.milk"
	a := &App{presetTuning: presetTuning{
		profiles: map[presetProfileKey]presetProfile{key: {heavy: true}},
		active:   context,
	}}
	if !a.presetHeavy(key.name) {
		t.Fatal("heavy preset was not excluded from the initialized profile context")
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
