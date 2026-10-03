package app

import (
	"math"
	"testing"
	"time"

	"github.com/dendec/glitchscope/internal/config"
)

func TestFpsMeterUsesFixedWindow(t *testing.T) {
	var meter fpsMeter
	for i := 0; i < fpsWindow; i++ {
		meter.AddDuration(time.Second / time.Duration(i+1))
	}
	if !meter.Full() {
		t.Fatal("expected a full FPS window")
	}
	if got, want := meter.Average(), 5.5; math.Abs(got-want) > 0.0001 {
		t.Fatalf("average = %v, want %v", got, want)
	}
	meter.Reset()
	if meter.Average() != 0 || meter.Full() {
		t.Fatal("reset meter retained samples")
	}
}

func TestRenderCostMeterUtilization(t *testing.T) {
	var meter renderCostMeter
	for range renderCostWindow {
		meter.Add(25 * time.Millisecond)
	}
	if !meter.Full() {
		t.Fatal("expected a full render-cost window")
	}
	if got := meter.Utilization(30); math.Abs(got-0.75) > 0.0001 {
		t.Fatalf("utilization = %v, want 0.75", got)
	}
	if got := renderUtilization(25*time.Millisecond, 30); math.Abs(got-0.75) > 0.0001 {
		t.Fatalf("single-frame utilization = %v, want 0.75", got)
	}
	if got := meter.Utilization(20); math.Abs(got-0.5) > 0.0001 {
		t.Fatalf("target utilization = %v, want 0.5", got)
	}
	if meter.Count() != renderCostWindow {
		t.Fatalf("sample count = %d, want %d", meter.Count(), renderCostWindow)
	}
	meter.Reset()
	if meter.Full() || meter.Average() != 0 {
		t.Fatal("reset retained render-cost samples")
	}
}

func TestVisualizerClock(t *testing.T) {
	var clock visualizerClock
	clock.framePeriod = time.Second / 30
	now := time.Unix(100, 0)
	if !clock.Due(now) {
		t.Fatal("first visualizer frame should be due")
	}
	clock.Complete(now)
	if clock.Due(now.Add(clock.framePeriod - time.Nanosecond)) {
		t.Fatal("visualizer frame should not run before its deadline")
	}
	clock.Complete(now.Add(clock.framePeriod))
	if got := clock.meter.Average(); math.Abs(got-30) > 0.001 {
		t.Fatalf("visualizer FPS = %v, want 30", got)
	}
}

func TestVisualizerClockReschedulesAfterSlowFrame(t *testing.T) {
	var clock visualizerClock
	start := time.Unix(100, 0)
	clock.Complete(start)
	slowFrameEnd := start.Add(67 * time.Millisecond)
	clock.Complete(slowFrameEnd)
	if !clock.Due(slowFrameEnd) {
		t.Fatal("slow frame should not wait another frame period after missing its deadline")
	}
}

func TestRunStateConsumesEachVisualizerFrameOnce(t *testing.T) {
	var state runState
	var viz visualizerClock
	viz.framePeriod = time.Second / 30
	if state.consumeAdaptiveFrame(&viz) {
		t.Fatal("empty clock produced an adaptive sample")
	}
	now := time.Unix(100, 0)
	viz.Complete(now)
	if !state.consumeAdaptiveFrame(&viz) || state.consumeAdaptiveFrame(&viz) {
		t.Fatal("visualizer frame was not consumed exactly once")
	}
	viz.Complete(now.Add(viz.framePeriod))
	if !state.consumeAdaptiveFrame(&viz) {
		t.Fatal("next visualizer frame did not produce an adaptive sample")
	}
}

func TestAdaptivePolicyUsesUtilizationAndHysteresis(t *testing.T) {
	params := defaultAdaptiveParams()
	p := adaptivePolicy{params: params}
	now := time.Unix(100, 0)
	if got := p.Decide(1.05, 1, now, 0, 0, 4); got != adaptiveNone {
		t.Fatalf("first high sample = %v, want none", got)
	}
	if got := p.Decide(1.05, 1, now.Add(params.downshiftAfter-time.Nanosecond), 0, 0, 4); got != adaptiveNone {
		t.Fatalf("early high transition = %v, want none", got)
	}
	if got := p.Decide(1.05, 1, now.Add(params.downshiftAfter), 0, 0, 4); got != adaptiveResolutionDown {
		t.Fatalf("high utilization transition = %v, want resolution down", got)
	}
	if got := p.Decide(1.05, 1, now.Add(params.downshiftAfter+time.Millisecond), 0, 0, 4); got != adaptiveNone {
		t.Fatalf("cooldown transition = %v, want none", got)
	}
	if got := p.Decide(0.8, 1, now.Add(params.downshiftAfter+time.Second), 0, 0, 4); got != adaptiveNone {
		t.Fatalf("dead-band transition = %v, want none", got)
	}
}

func TestAdaptivePolicyStopsAtResolutionFloor(t *testing.T) {
	params := defaultAdaptiveParams()
	p := adaptivePolicy{params: params}
	now := time.Unix(100, 0)
	if got := p.Decide(1.05, 1, now, 1, 0, 3); got != adaptiveNone {
		t.Fatal("first high sample should only arm hysteresis")
	}
	if got := p.Decide(1.05, 1, now.Add(params.downshiftAfter), 1, 0, 3); got != adaptiveResolutionDown {
		t.Fatalf("floor transition = %v, want resolution down", got)
	}
}

func TestAdaptivePolicyUpshiftsWhenTargetCadenceHasHeadroom(t *testing.T) {
	params := defaultAdaptiveParams()
	p := adaptivePolicy{params: params}
	now := time.Unix(100, 0)
	for i := range 3 {
		if got := p.Decide(0.9, 1, now.Add(time.Duration(i)*time.Second), 2, 0, 3); got != adaptiveNone {
			t.Fatalf("early recovery at %d = %v, want none", i, got)
		}
	}
	if got := p.Decide(0.9, 1, now.Add(3*time.Second), 2, 0, 3); got != adaptiveResolutionUp {
		t.Fatalf("target cadence did not allow resolution recovery: %v", got)
	}
}

func TestResolutionStateUsesConfiguredCeiling(t *testing.T) {
	params := defaultAdaptiveParams()
	var state resolutionState
	ceiling := config.RenderResolution{Width: 640, Height: 360}
	if !state.Reset(1280, 720, ceiling, params) {
		t.Fatal("resolution state did not initialize")
	}
	if state.index != state.ceilingIndex {
		t.Fatalf("index=%d ceiling=%d", state.index, state.ceilingIndex)
	}
	now := time.Unix(100, 0)
	resolution, action := state.Decide(1.05, 1, now)
	if action != adaptiveNone || resolution.Width != 0 {
		t.Fatal("first high sample changed quality")
	}
	resolution, action = state.Decide(1.05, 1, now.Add(params.downshiftAfter))
	if action != adaptiveResolutionDown || resolution.Width == 0 {
		t.Fatalf("high utilization did not lower resolution: %+v action=%v", resolution, action)
	}
}

func TestFrameRateAdaptivePolicyNeverChangesTarget(t *testing.T) {
	params := defaultAdaptiveParams()
	if params.utilizationHigh != 1 || params.downshiftAfter <= 0 {
		t.Fatalf("frame-rate params = %+v", params)
	}
	var state resolutionState
	state.Reset(1280, 720, config.RenderResolution{Width: 1280, Height: 720}, params)
	now := time.Unix(100, 0)
	state.Decide(1.2, 1.3, now)
	_, action := state.Decide(1.2, 1.3, now.Add(params.downshiftAfter))
	if action != adaptiveResolutionDown {
		t.Fatalf("overloaded frame-rate target = action %v", action)
	}
	if state.floorIndex != len(state.resolutions)-1 {
		t.Fatalf("adaptive floor = %d, want complete grid ending at %d", state.floorIndex, len(state.resolutions)-1)
	}
	state.index = state.floorIndex
	state.RestartForPreset()
	state.Decide(1.2, 1.3, now.Add(2*time.Second))
	_, action = state.Decide(1.2, 1.3, now.Add(3*time.Second))
	if action != adaptiveNone {
		t.Fatalf("adaptive changed target at resolution floor: action=%v", action)
	}
}

func TestResolutionUsesCompleteGridBelowSelectedCeiling(t *testing.T) {
	var state resolutionState
	if !state.Reset(1280, 720, config.RenderResolution{Width: 960, Height: 540}, defaultAdaptiveParams()) {
		t.Fatal("resolution state did not initialize")
	}
	if got, want := state.resolutions[state.floorIndex].Height, 180; got != want {
		t.Fatalf("adaptive floor = %dp, want %dp", got, want)
	}
}

func TestResolutionReconfigurePreservesWarmRenderSize(t *testing.T) {
	var state resolutionState
	params := defaultAdaptiveParams()
	if !state.Reset(1280, 720, config.RenderResolution{Width: 1280, Height: 720}, params) {
		t.Fatal("resolution state did not initialize")
	}
	warm := state.resolutions[5]
	if !state.Reconfigure(1280, 720, config.RenderResolution{Width: 960, Height: 540}, warm, params) {
		t.Fatal("resolution state did not reconfigure")
	}
	if got := state.resolutions[state.index]; got != warm {
		t.Fatalf("warm resolution=%v, want %v", got, warm)
	}
	if state.index < state.ceilingIndex || state.index > state.floorIndex {
		t.Fatalf("warm index=%d outside [%d,%d]", state.index, state.ceilingIndex, state.floorIndex)
	}
}

func TestResolutionTrialRejectsUnhelpfulReduction(t *testing.T) {
	var s resolutionState
	params := defaultAdaptiveParams()
	s.Reset(1280, 720, config.RenderResolution{Width: 640, Height: 360}, params)
	now := time.Unix(100, 0)
	s.Decide(1.05, 1, now)
	_, action := s.Decide(1.05, 1, now.Add(time.Second))
	if action != adaptiveResolutionDown {
		t.Fatal("did not try resolution first")
	}
	_, action = s.Decide(1.04, 1, now.Add(2*time.Second))
	if action != adaptiveResample || s.downBlocked {
		t.Fatal("first uncertain window should request confirmation")
	}
	_, action = s.Decide(1.04, 1, now.Add(3*time.Second))
	if action != adaptiveResolutionUp || s.index != s.ceilingIndex || !s.downBlocked {
		t.Fatal("ineffective reduction was not rolled back")
	}
	s.Decide(1.05, 1.3, now.Add(4*time.Second))
	_, action = s.Decide(1.05, 1.3, now.Add(5*time.Second))
	if action != adaptiveNone {
		t.Fatal("resolution-insensitive preset should keep the selected frequency")
	}
	s.RestartForPreset()
	if s.downBlocked {
		t.Fatal("new preset inherited resolution sensitivity")
	}
}

func TestResolutionTrialRetainsUsefulReductionAndHonorsFloor(t *testing.T) {
	var s resolutionState
	params := defaultAdaptiveParams()
	s.Reset(1280, 720, config.RenderResolution{Width: 640, Height: 360}, params)
	now := time.Unix(100, 0)
	s.Decide(1.05, 1, now)
	s.Decide(1.05, 1, now.Add(time.Second))
	s.Decide(0.6, 1, now.Add(2*time.Second))
	if s.index == s.ceilingIndex || s.downBlocked {
		t.Fatal("useful reduction was discarded")
	}
	s.index = s.floorIndex
	s.policy.Restart()
	s.Decide(1.05, 1.3, now.Add(3*time.Second))
	_, action := s.Decide(1.05, 1.3, now.Add(4*time.Second))
	if action != adaptiveNone || s.index != s.floorIndex {
		t.Fatal("floor did not protect the selected resolution policy")
	}
}

func TestFailedUpscaleIsNotRepeated(t *testing.T) {
	var s resolutionState
	params := defaultAdaptiveParams()
	s.Reset(1280, 720, config.RenderResolution{Width: 640, Height: 360}, params)
	s.index = s.floorIndex
	original := s.index
	now := time.Unix(100, 0)
	s.Decide(0.4, 1, now)
	_, action := s.Decide(0.4, 1, now.Add(params.upshiftAfter))
	if action != adaptiveResolutionUp {
		t.Fatal("did not try upscale")
	}
	s.Decide(1, 1, now.Add(5*time.Second))
	if s.index != original || s.upscaleFloor != original {
		t.Fatal("failed upscale did not restore and protect stable resolution")
	}
	s.Decide(0.4, 1, now.Add(6*time.Second))
	_, action = s.Decide(0.4, 1, now.Add(10*time.Second))
	if action != adaptiveNone {
		t.Fatal("failed upscale was retried")
	}
}

func TestAdaptivePreservesStableCadence(t *testing.T) {
	params := defaultAdaptiveParams()
	for _, utilization := range []float64{0.8, 0.95, 1} {
		p := adaptivePolicy{params: params}
		now := time.Unix(100, 0)
		// Cadence is within tolerance, so a full budget alone does not cause
		// downward resolution changes once the policy has reached its floor.
		for i := range 20 {
			if action := p.Decide(utilization, 1.04, now.Add(time.Duration(i)*time.Second), 2, 2, 3); action != adaptiveNone {
				t.Fatalf("stable cadence at resolution floor: utilization=%v action=%v", utilization, action)
			}
		}
	}
}

func TestResolutionTrialRetainsCadenceImprovement(t *testing.T) {
	var state resolutionState
	params := defaultAdaptiveParams()
	state.Reset(1280, 720, config.RenderResolution{Width: 1280, Height: 720}, params)
	now := time.Unix(100, 0)
	// glass corridor: render-only cost barely changed, but lowering resolution
	// restored actual cadence from about 11 to 20 FPS. Cadence is independent
	// evidence of benefit even if work timing is noisy.
	state.Decide(1.05, 1.8, now)
	_, action := state.Decide(1.05, 1.8, now.Add(time.Second))
	if action != adaptiveResolutionDown {
		t.Fatal("did not try lower resolution")
	}
	_, action = state.Decide(0.64, 1, now.Add(2*time.Second))
	if action != adaptiveNone || state.index != 1 || state.downBlocked || state.trialAction != adaptiveNone {
		t.Fatalf("cadence improvement discarded: %+v action=%v", state, action)
	}
}

func TestResolutionTrialConfirmsNoisyFirstWindow(t *testing.T) {
	var state resolutionState
	params := defaultAdaptiveParams()
	state.Reset(1280, 720, config.RenderResolution{Width: 1280, Height: 720}, params)
	now := time.Unix(100, 0)
	state.Decide(1.05, 1.5, now)
	state.Decide(1.05, 1.5, now.Add(time.Second))
	_, action := state.Decide(1.04, 1.5, now.Add(2*time.Second))
	if action != adaptiveResample {
		t.Fatal("uncertain first window was not confirmed")
	}
	_, action = state.Decide(0.6, 1, now.Add(3*time.Second))
	if action != adaptiveNone || state.index != 1 || state.downBlocked || state.trialAction != adaptiveNone {
		t.Fatal("fresh confirmation showing improvement was not retained")
	}
}

func TestResolutionSearchFindsHighestPassingLevelWithBinaryProbes(t *testing.T) {
	var state resolutionState
	if !state.Reset(1280, 720, config.RenderResolution{Width: 1280, Height: 720}, defaultAdaptiveParams()) {
		t.Fatal("resolution state did not initialize")
	}
	state.index = state.floorIndex
	now := time.Unix(100, 0)
	if got, action := state.startResolutionSearch(0.5, 1); action != adaptiveResolutionUp || got != state.resolutions[0] {
		t.Fatalf("first ceiling probe = (%v, %v), want (%v, up)", got, action, state.resolutions[0])
	}
	if _, action := state.Decide(1.1, 1.2, now); action != adaptiveResample {
		t.Fatalf("first failing ceiling window = %v, want confirmation", action)
	}
	if _, action := state.Decide(1.1, 1.2, now); action != adaptiveResolutionDown || state.index != 3 {
		t.Fatalf("confirmed ceiling failure did not probe 450p: index=%d action=%v", state.index, action)
	}
	if _, action := state.Decide(0.7, 1, now); action != adaptiveResolutionUp || state.index != 1 {
		t.Fatalf("passing midpoint did not probe 630p: index=%d action=%v", state.index, action)
	}
	if _, action := state.Decide(1.1, 1.2, now); action != adaptiveResample {
		t.Fatalf("first failing 630p window = %v, want confirmation", action)
	}
	if _, action := state.Decide(1.1, 1.2, now); action != adaptiveResolutionDown || state.index != 2 {
		t.Fatalf("confirmed 630p failure did not probe 540p: index=%d action=%v", state.index, action)
	}
	if _, action := state.Decide(0.7, 1, now); action != adaptiveNone || state.searchActive {
		t.Fatalf("passing 540p did not finish search: index=%d action=%v", state.index, action)
	}
	if state.index != 2 || state.upscaleFloor != 2 {
		t.Fatalf("best resolution boundary = index %d, floor %d; want 2, 2", state.index, state.upscaleFloor)
	}
}

func TestResolutionSearchAcceptsPreconfirmedSevereFailure(t *testing.T) {
	var state resolutionState
	if !state.Reset(1280, 720, config.RenderResolution{Width: 1280, Height: 720}, defaultAdaptiveParams()) {
		t.Fatal("resolution state did not initialize")
	}
	state.index = state.floorIndex
	if _, action := state.startResolutionSearch(0.5, 1); action != adaptiveResolutionUp {
		t.Fatalf("first ceiling probe action = %v, want up", action)
	}
	state.confirmSearchTrial()
	if _, action := state.Decide(10, 10, time.Unix(100, 0)); action != adaptiveResolutionDown || state.index != 3 {
		t.Fatalf("preconfirmed ceiling failure did not bisect immediately: index=%d action=%v", state.index, action)
	}
}

func TestResolutionSearchFinishesWhenCeilingPasses(t *testing.T) {
	var state resolutionState
	state.Reset(1280, 720, config.RenderResolution{Width: 1280, Height: 720}, defaultAdaptiveParams())
	state.index = state.floorIndex
	resolution, action := state.startResolutionSearch(0.5, 1)
	if action != adaptiveResolutionUp || resolution != state.resolutions[state.ceilingIndex] {
		t.Fatalf("first trial = (%v, %v), want configured ceiling and up", resolution, action)
	}
	if resolution, action = state.Decide(0.7, 1, time.Unix(100, 0)); action != adaptiveNone || state.searchActive {
		t.Fatalf("passing ceiling did not finish search: resolution=%v action=%v active=%t", resolution, action, state.searchActive)
	}
	if state.index != state.ceilingIndex || state.upscaleFloor != state.ceilingIndex {
		t.Fatalf("passing ceiling result = index %d, floor %d; want %d", state.index, state.upscaleFloor, state.ceilingIndex)
	}
}

func TestResolutionSearchReturnsToBestPassingLevel(t *testing.T) {
	var state resolutionState
	state.Reset(1280, 720, config.RenderResolution{Width: 1280, Height: 720}, defaultAdaptiveParams())
	state.index = state.floorIndex
	state.startResolutionSearch(0.5, 1)
	now := time.Unix(100, 0)
	state.Decide(1.1, 1.2, now) // fail at ceiling; request confirmation.
	state.Decide(1.1, 1.2, now) // confirmed failure; probe midpoint 3.
	state.Decide(0.7, 1, now)   // midpoint passes; probe index 1.
	state.Decide(1.1, 1.2, now) // request confirmation at index 1.
	state.Decide(1.1, 1.2, now) // confirmed failure; probe index 2.
	state.Decide(1.1, 1.2, now) // request confirmation at index 2.
	resolution, action := state.Decide(1.1, 1.2, now)
	if action != adaptiveResolutionDown || state.searchActive || state.index != 3 || resolution != state.resolutions[3] {
		t.Fatalf("search did not restore best passing level: index=%d active=%t resolution=%v action=%v", state.index, state.searchActive, resolution, action)
	}
}

func TestResolutionSearchWaitsForTargetHeadroomAtMinimum(t *testing.T) {
	var state resolutionState
	state.Reset(1280, 720, config.RenderResolution{Width: 1280, Height: 720}, defaultAdaptiveParams())
	state.index = state.floorIndex
	if _, action := state.startResolutionSearch(1.1, 1.2); action != adaptiveNone || state.searchActive {
		t.Fatalf("search started without target headroom: active=%t action=%v", state.searchActive, action)
	}
	if state.upscaleFloor != state.ceilingIndex {
		t.Fatal("regular adaptive policy was not retained for a noisy baseline")
	}
}

func TestResolutionSearchHonorsConfiguredCeiling(t *testing.T) {
	var state resolutionState
	if !state.Reset(1280, 720, config.RenderResolution{Width: 640, Height: 360}, defaultAdaptiveParams()) {
		t.Fatal("resolution state did not initialize")
	}
	state.index = state.floorIndex
	resolution, action := state.startResolutionSearch(0.5, 1)
	if action != adaptiveResolutionUp || resolution != state.resolutions[state.ceilingIndex] {
		t.Fatalf("first probe = (%v, %v), want configured ceiling %v and up", resolution, action, state.resolutions[state.ceilingIndex])
	}
}
