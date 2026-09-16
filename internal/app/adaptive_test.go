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
	params := config.PerfModePerformance.Params()
	p := adaptivePolicy{params: params}
	now := time.Unix(100, 0)
	if got := p.Decide(0.95, 1, now, 30, 0, 0, 4); got != adaptiveNone {
		t.Fatalf("first high sample = %v, want none", got)
	}
	if got := p.Decide(0.95, 1, now.Add(params.DownshiftAfter-time.Nanosecond), 30, 0, 0, 4); got != adaptiveNone {
		t.Fatalf("early high transition = %v, want none", got)
	}
	if got := p.Decide(0.95, 1, now.Add(params.DownshiftAfter), 30, 0, 0, 4); got != adaptiveResolutionDown {
		t.Fatalf("high utilization transition = %v, want resolution down", got)
	}
	if got := p.Decide(0.95, 1, now.Add(params.DownshiftAfter+time.Millisecond), 25, 0, 0, 4); got != adaptiveNone {
		t.Fatalf("cooldown transition = %v, want none", got)
	}
	if got := p.Decide(0.50, 1, now.Add(params.DownshiftAfter+time.Second), 25, 0, 0, 4); got != adaptiveNone {
		t.Fatalf("dead-band transition = %v, want none", got)
	}
}

func TestAdaptivePolicyLowersResolutionBeforeFrequency(t *testing.T) {
	params := config.PerfModePerformance.Params()
	p := adaptivePolicy{params: params}
	now := time.Unix(100, 0)
	if got := p.Decide(0.95, 1, now, 30, 1, 0, 3); got != adaptiveNone {
		t.Fatal("first high sample should only arm hysteresis")
	}
	if got := p.Decide(0.95, 1, now.Add(params.DownshiftAfter), 30, 1, 0, 3); got != adaptiveResolutionDown {
		t.Fatalf("floor transition = %v, want resolution down", got)
	}
}

func TestAdaptivePolicyRestoresFrequencyBeforeResolution(t *testing.T) {
	params := config.PerfModePerformance.Params()
	p := adaptivePolicy{params: params}
	now := time.Unix(100, 0)
	if got := p.Decide(0.50, 1, now, 20, 2, 0, 3); got != adaptiveNone {
		t.Fatal("first low sample should only arm hysteresis")
	}
	if got := p.Decide(0.50, 1, now.Add(params.UpshiftAfter), 20, 2, 0, 3); got != adaptiveFrequencyUp {
		t.Fatalf("recovery transition = %v, want frequency up", got)
	}
}

func TestAdaptivePolicyRejectsFrequencyIncreaseThatWouldOverload(t *testing.T) {
	params := config.PerfModeEco.Params()
	p := adaptivePolicy{params: params}
	now := time.Unix(100, 0)
	if got := p.Decide(0.7, 1, now, 10, 1, 0, 2); got != adaptiveNone {
		t.Fatalf("first in-band recovery = %v, want none", got)
	}
	if got := p.Decide(0.7, 1, now.Add(params.UpshiftAfter), 10, 1, 0, 2); got != adaptiveNone {
		t.Fatalf("unsafe recovery = %v, want none", got)
	}
}

func TestResolutionStateUsesConfiguredCeiling(t *testing.T) {
	params := config.PerfModeBalanced.Params()
	var state resolutionState
	ceiling := config.RenderResolution{Width: 640, Height: 360}
	if !state.Reset(1280, 720, ceiling, params) {
		t.Fatal("resolution state did not initialize")
	}
	if state.index != state.ceilingIndex {
		t.Fatalf("index=%d ceiling=%d", state.index, state.ceilingIndex)
	}
	now := time.Unix(100, 0)
	resolution, _, action := state.Decide(0.80, 1, now, params.VisualizerFPS)
	if action != adaptiveNone || resolution.Width != 0 {
		t.Fatal("first high sample changed quality")
	}
	resolution, _, action = state.Decide(0.80, 1, now.Add(params.DownshiftAfter), params.VisualizerFPS)
	if action != adaptiveResolutionDown || resolution.Width == 0 {
		t.Fatalf("high utilization did not preserve frequency: %+v action=%v", resolution, action)
	}
}

func TestPerformanceModeTargets(t *testing.T) {
	want := map[config.PerformanceMode]int32{
		config.PerfModePerformance: 30,
		config.PerfModeBalanced:    25,
		config.PerfModeEco:         20,
	}
	for mode, fps := range want {
		if got := mode.Params().VisualizerFPS; got != fps {
			t.Fatalf("%v target FPS=%d, want %d", mode, got, fps)
		}
	}
}

func TestResolutionLimitsRelativeToSelectedCeiling(t *testing.T) {
	for _, tc := range []struct {
		mode           config.PerformanceMode
		ceiling, floor int
	}{
		{config.PerfModeUltra, 720, 720},
		{config.PerfModePerformance, 720, 360},
		{config.PerfModeBalanced, 540, 270},
		{config.PerfModeEco, 720, 180},
		{config.PerfModeEco, 540, 135},
		{config.PerfModeEco, 360, 90},
		{config.PerfModeEco, 90, 90},
	} {
		var s resolutionState
		s.Reset(1280, 720, config.RenderResolution{Width: tc.ceiling * 16 / 9, Height: tc.ceiling}, tc.mode.Params())
		if got := s.resolutions[s.floorIndex].Height; got != tc.floor {
			t.Fatalf("%v ceiling %dp: floor %dp, want %dp", tc.mode, tc.ceiling, got, tc.floor)
		}
	}
}

func TestResolutionTrialRejectsUnhelpfulReduction(t *testing.T) {
	var s resolutionState
	params := config.PerfModeEco.Params()
	s.Reset(1280, 720, config.RenderResolution{Width: 640, Height: 360}, params)
	now := time.Unix(100, 0)
	s.Decide(0.8, 1, now, 20)
	_, _, action := s.Decide(0.8, 1, now.Add(time.Second), 20)
	if action != adaptiveResolutionDown {
		t.Fatal("did not try resolution first")
	}
	_, _, action = s.Decide(0.79, 1, now.Add(2*time.Second), 20)
	if action != adaptiveResample || s.downBlocked {
		t.Fatal("first uncertain window should request confirmation")
	}
	_, _, action = s.Decide(0.79, 1, now.Add(3*time.Second), 20)
	if action != adaptiveResolutionUp || s.index != s.ceilingIndex || !s.downBlocked {
		t.Fatal("ineffective reduction was not rolled back")
	}
	s.Decide(0.8, 1.3, now.Add(4*time.Second), 20)
	_, fps, action := s.Decide(0.8, 1.3, now.Add(5*time.Second), 20)
	if action != adaptiveFrequencyDown || fps != 15 {
		t.Fatal("resolution-insensitive preset did not fall back to frequency")
	}
	s.RestartForPreset()
	if s.downBlocked {
		t.Fatal("new preset inherited resolution sensitivity")
	}
}

func TestResolutionTrialRetainsUsefulReductionAndHonorsFloor(t *testing.T) {
	var s resolutionState
	params := config.PerfModeBalanced.Params()
	s.Reset(1280, 720, config.RenderResolution{Width: 640, Height: 360}, params)
	now := time.Unix(100, 0)
	s.Decide(0.9, 1, now, 25)
	s.Decide(0.9, 1, now.Add(time.Second), 25)
	s.Decide(0.6, 1, now.Add(2*time.Second), 25)
	if s.index == s.ceilingIndex || s.downBlocked {
		t.Fatal("useful reduction was discarded")
	}
	s.index = s.floorIndex
	s.policy.Restart()
	s.Decide(0.9, 1.3, now.Add(3*time.Second), 25)
	_, fps, action := s.Decide(0.9, 1.3, now.Add(4*time.Second), 25)
	if action != adaptiveFrequencyDown || fps != 20 || s.index != s.floorIndex {
		t.Fatal("floor did not protect resolution")
	}
}

func TestFailedUpscaleIsNotRepeated(t *testing.T) {
	var s resolutionState
	params := config.PerfModeBalanced.Params()
	s.Reset(1280, 720, config.RenderResolution{Width: 640, Height: 360}, params)
	s.index = s.floorIndex
	original := s.index
	now := time.Unix(100, 0)
	s.Decide(0.4, 1, now, 25)
	_, _, action := s.Decide(0.4, 1, now.Add(params.UpshiftAfter), 25)
	if action != adaptiveResolutionUp {
		t.Fatal("did not try upscale")
	}
	s.Decide(0.8, 1, now.Add(5*time.Second), 25)
	if s.index != original || s.upscaleFloor != original {
		t.Fatal("failed upscale did not restore and protect stable resolution")
	}
	s.Decide(0.4, 1, now.Add(6*time.Second), 25)
	_, _, action = s.Decide(0.4, 1, now.Add(10*time.Second), 25)
	if action != adaptiveNone {
		t.Fatal("failed upscale was retried")
	}
}

func TestEcoPreservesCadenceAboveSoftBudget(t *testing.T) {
	params := config.PerfModeEco.Params()
	for _, utilization := range []float64{0.55, 0.8, 0.95} {
		p := adaptivePolicy{params: params}
		now := time.Unix(100, 0)
		// Resolution is exhausted or known to be ineffective. A 52 ms interval
		// is about 19 FPS: it must not trigger the device's 20 -> 15 -> 10 cascade.
		for i := range 20 {
			if action := p.Decide(utilization, 1.04, now.Add(time.Duration(i)*time.Second), 20, 2, 0, 3); action != adaptiveNone {
				t.Fatalf("stable cadence above soft budget: utilization=%v action=%v", utilization, action)
			}
		}
	}
}

func TestEcoRestoresFrequencyAboveSoftBudget(t *testing.T) {
	params := config.PerfModeEco.Params()
	p := adaptivePolicy{params: params}
	now := time.Unix(100, 0)
	// 40 ms of full frame work at 15 FPS is 60% utilization; 20 FPS needs
	// 80%. Both exceed Eco's energy band, but the target cadence is feasible.
	p.Decide(0.6, 1, now, 15, 2, 0, 3)
	if action := p.Decide(0.6, 1, now.Add(params.UpshiftAfter), 15, 2, 0, 3); action != adaptiveFrequencyUp {
		t.Fatalf("feasible target cadence was not restored: %v", action)
	}
}

func TestEcoReducesFrequencyOnlyAfterRealCadenceLoss(t *testing.T) {
	params := config.PerfModeEco.Params()
	p := adaptivePolicy{params: params}
	now := time.Unix(100, 0)
	p.Decide(1.4, 1.4, now, 20, 2, 0, 3)
	if action := p.Decide(1.4, 1.4, now.Add(params.DownshiftAfter), 20, 2, 0, 3); action != adaptiveFrequencyDown {
		t.Fatalf("sustained overload at resolution floor did not lower FPS: %v", action)
	}
}

func TestResolutionTrialRetainsCadenceImprovement(t *testing.T) {
	var state resolutionState
	params := config.PerfModeEco.Params()
	state.Reset(1280, 720, config.RenderResolution{Width: 1280, Height: 720}, params)
	now := time.Unix(100, 0)
	// glass corridor: render-only cost barely changed, but lowering resolution
	// restored actual cadence from about 11 to 20 FPS. Cadence is independent
	// evidence of benefit even if work timing is noisy.
	state.Decide(0.67, 1.8, now, 20)
	_, _, action := state.Decide(0.67, 1.8, now.Add(time.Second), 20)
	if action != adaptiveResolutionDown {
		t.Fatal("did not try lower resolution")
	}
	_, _, action = state.Decide(0.64, 1, now.Add(2*time.Second), 20)
	if action != adaptiveNone || state.index != 1 || state.downBlocked || state.trialAction != adaptiveNone {
		t.Fatalf("cadence improvement discarded: %+v action=%v", state, action)
	}
}

func TestResolutionTrialConfirmsNoisyFirstWindow(t *testing.T) {
	var state resolutionState
	params := config.PerfModeEco.Params()
	state.Reset(1280, 720, config.RenderResolution{Width: 1280, Height: 720}, params)
	now := time.Unix(100, 0)
	state.Decide(0.8, 1.5, now, 20)
	state.Decide(0.8, 1.5, now.Add(time.Second), 20)
	_, _, action := state.Decide(0.79, 1.5, now.Add(2*time.Second), 20)
	if action != adaptiveResample {
		t.Fatal("uncertain first window was not confirmed")
	}
	_, _, action = state.Decide(0.6, 1, now.Add(3*time.Second), 20)
	if action != adaptiveNone || state.index != 1 || state.downBlocked || state.trialAction != adaptiveNone {
		t.Fatal("fresh confirmation showing improvement was not retained")
	}
}
