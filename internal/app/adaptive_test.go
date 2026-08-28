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

	meter.AddDuration(time.Second / 21)
	if got, want := meter.Average(), 7.5; math.Abs(got-want) > 0.0001 {
		t.Fatalf("rolling average = %v, want %v", got, want)
	}

	meter.Reset()
	if meter.Average() != 0 || meter.Full() {
		t.Fatal("reset meter retained samples")
	}
}

func TestVisualizerClock(t *testing.T) {
	var clock visualizerClock
	now := time.Unix(100, 0)
	if !clock.Due(now) {
		t.Fatal("first visualizer frame should be due")
	}
	clock.Complete(now)
	if clock.Due(now.Add(visualizerFramePeriod - time.Nanosecond)) {
		t.Fatal("visualizer frame should not run before its deadline")
	}
	if !clock.Due(now.Add(visualizerFramePeriod)) {
		t.Fatal("visualizer frame should run at its deadline")
	}
	clock.Complete(now.Add(visualizerFramePeriod))
	if got := clock.meter.Average(); math.Abs(got-visualizerTargetFPS) > 0.001 {
		t.Fatalf("visualizer FPS = %v, want %v", got, visualizerTargetFPS)
	}
	clock.Reset()
	if !clock.Due(now) || clock.meter.Average() != 0 {
		t.Fatal("reset should make a fresh visualizer frame due without stale FPS")
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

func TestVisualizerClockKeepsDeadlineAfterFrameWork(t *testing.T) {
	var clock visualizerClock
	start := time.Unix(100, 0)
	clock.Complete(start)

	frameCompleted := start.Add(visualizerFramePeriod + 20*time.Millisecond)
	clock.Complete(frameCompleted)
	nextDeadline := start.Add(2 * visualizerFramePeriod)
	if clock.Due(nextDeadline.Add(-time.Nanosecond)) {
		t.Fatal("visualizer frame became due before the fixed 30 FPS deadline")
	}
	if !clock.Due(nextDeadline) {
		t.Fatal("visualizer frame was delayed by render work")
	}
}

func TestRunStateConsumesEachVisualizerFrameOnce(t *testing.T) {
	var state runState
	if state.consumeAdaptiveFrame() {
		t.Fatal("empty clock produced an adaptive sample")
	}

	now := time.Unix(100, 0)
	state.visualizer.Complete(now)
	if !state.consumeAdaptiveFrame() {
		t.Fatal("completed visualizer frame did not produce an adaptive sample")
	}
	if state.consumeAdaptiveFrame() {
		t.Fatal("same visualizer frame produced two adaptive samples")
	}

	state.visualizer.Complete(now.Add(visualizerFramePeriod))
	if !state.consumeAdaptiveFrame() {
		t.Fatal("next visualizer frame did not produce an adaptive sample")
	}
}

func TestPresetTransitionSuspendsAndRestartsAdaptivePolicy(t *testing.T) {
	var a App
	for range adaptiveLowFrames - 1 {
		a.adaptive.policy.Decide(adaptiveThreshLow-1, 0, 3)
	}
	a.adaptive.policy.cooldown = adaptiveCooldown

	now := time.Unix(100, 0)
	a.suspendAdaptiveForPresetTransition(now)
	if !a.adaptiveSuspended(now.Add(softCutDuration - time.Nanosecond)) {
		t.Fatal("adaptive resumed before the soft transition completed")
	}
	if a.adaptiveSuspended(now.Add(softCutDuration)) {
		t.Fatal("adaptive remained suspended after the soft transition completed")
	}
	if a.adaptive.policy.lowFrames != 0 || a.adaptive.policy.cooldown != 0 || a.adaptive.policy.fpsRing.pos != 0 {
		t.Fatal("preset transition retained adaptive policy history")
	}
}

func TestAdaptiveDownByFrames(t *testing.T) {
	var p adaptivePolicy
	// 9 frames: building counters, no trigger yet.
	for i := 0; i < 9; i++ {
		if next, changed, _ := p.Decide(15, 0, 3); changed || next != 0 {
			t.Fatalf("should not change at step %d", i)
		}
	}
	// 10th frame: lowFrames=10, ring full, all < 20 → triggers.
	if next, changed, _ := p.Decide(15, 0, 3); !changed || next != 1 {
		t.Fatalf("frame trigger = (%d, %v), want (1, true)", next, changed)
	}
}

func TestAdaptiveUpByFrames(t *testing.T) {
	var p adaptivePolicy
	for i := 0; i < 9; i++ {
		if next, changed, _ := p.Decide(30, 1, 3); changed || next != 1 {
			t.Fatalf("should not change at step %d", i)
		}
	}
	if next, changed, _ := p.Decide(30, 1, 3); !changed || next != 0 {
		t.Fatalf("frame trigger = (%d, %v), want (0, true)", next, changed)
	}
}

func TestAdaptiveTimeFallbackUltraLowFPS(t *testing.T) {
	var p adaptivePolicy
	// At 2fps: each call adds 0.5s to lowElapsed.
	// After 3 calls: lowElapsed=1.5 < 2.0, no trigger.
	for i := 0; i < 3; i++ {
		if next, changed, _ := p.Decide(2, 0, 3); changed || next != 0 {
			t.Fatalf("should not change at step %d", i)
		}
	}
	// 4th call: lowElapsed=2.0 ≥ 2.0 → time fallback triggers.
	if next, changed, _ := p.Decide(2, 0, 3); !changed || next != 1 {
		t.Fatalf("time fallback = (%d, %v), want (1, true)", next, changed)
	}
}

func TestAdaptiveTimeFallbackSingleFPS(t *testing.T) {
	var p adaptivePolicy
	// At 1fps: each call adds 1.0s. 1st call: lowElapsed=1.0 < 2.0.
	if next, changed, _ := p.Decide(1, 0, 3); changed || next != 0 {
		t.Fatalf("should not change at step 0")
	}
	// 2nd call: lowElapsed=2.0 ≥ 2.0 → time fallback triggers.
	if next, changed, _ := p.Decide(1, 0, 3); !changed || next != 1 {
		t.Fatalf("time fallback = (%d, %v), want (1, true)", next, changed)
	}
}

func TestAdaptiveUnstableFPSBlocksTrigger(t *testing.T) {
	var p adaptivePolicy
	// Alternate between 19 and 21 — oscillating around threshold.
	for i := 0; i < 20; i++ {
		fps := 19.0
		if i%2 == 0 {
			fps = 21.0
		}
		p.Decide(fps, 0, 3)
	}
	// Should NOT trigger — fpsRing has samples > 20 (unstable).
	if next, changed, _ := p.Decide(19, 0, 3); changed || next != 0 {
		t.Fatalf("unstable should not trigger: next=%d changed=%v", next, changed)
	}
}

func TestAdaptiveDeadZoneResetsCounters(t *testing.T) {
	var p adaptivePolicy
	for i := 0; i < 8; i++ {
		p.Decide(15, 0, 3)
	}
	// Dead zone (22) resets everything.
	p.Decide(22, 0, 3)
	// 2 low frames — should NOT trigger.
	if next, changed, _ := p.Decide(15, 0, 3); changed || next != 0 {
		t.Fatalf("after dead zone: next=%d changed=%v", next, changed)
	}
}

func TestAdaptiveCooldown(t *testing.T) {
	var p adaptivePolicy
	// 10 calls: 10th triggers cooldown=10.
	for i := 0; i < 10; i++ {
		p.Decide(15, 0, 3)
	}
	// 11th call: cooldown active → blocked.
	if next, changed, _ := p.Decide(15, 0, 3); changed || next != 0 {
		t.Fatalf("cooldown should block: next=%d changed=%v", next, changed)
	}
}

func TestAdaptiveMinReached(t *testing.T) {
	var p adaptivePolicy
	// 9 calls building up, 10th triggers with min resolution.
	for i := 0; i < 9; i++ {
		p.Decide(15, 2, 3)
	}
	if next, changed, minReached := p.Decide(15, 2, 3); changed || next != 2 || !minReached {
		t.Fatalf("min reached = (%d, %v, %v), want (2, false, true)", next, changed, minReached)
	}
}

func TestAdaptiveBounds(t *testing.T) {
	var p adaptivePolicy
	// Already at highest resolution: can't upscale.
	for i := 0; i < 10; i++ {
		p.Decide(30, 0, 3)
	}
	if next, changed, _ := p.Decide(30, 0, 3); changed || next != 0 {
		t.Fatalf("at upper bound: next=%d changed=%v", next, changed)
	}
}

func TestResolutionStateDecidesAndUpdatesIndex(t *testing.T) {
	state := resolutionState{
		resolutions: []config.RenderResolution{{Width: 320, Height: 180}, {Width: 480, Height: 270}},
	}
	for i := 0; i < 10; i++ {
		state.Decide(adaptiveThreshLow - 1)
	}
	if state.index != 1 {
		t.Fatalf("index = %d, want 1", state.index)
	}
	if got := state.resolutions[state.index]; got.Width != 480 || got.Height != 270 {
		t.Fatalf("resolution = %v, want 480x270", got)
	}
}

func TestResolutionStateBlocksFailedUpscaleForPreset(t *testing.T) {
	state := resolutionState{
		resolutions: []config.RenderResolution{
			{Width: 800, Height: 450},
			{Width: 640, Height: 360},
		},
		index:        1,
		upscaleTrial: -1,
	}

	for range adaptiveHighFrames {
		state.Decide(adaptiveThreshHigh + 1)
	}
	if state.index != 0 {
		t.Fatalf("first upscale index = %d, want 0", state.index)
	}
	for range adaptiveCooldown + adaptiveLowFrames {
		state.Decide(adaptiveThreshLow - 1)
	}
	if state.index != 1 || state.upscaleFloor != 1 {
		t.Fatalf("failed upscale state = (index %d, floor %d), want (1, 1)", state.index, state.upscaleFloor)
	}
	for range adaptiveCooldown + adaptiveHighFrames*2 {
		state.Decide(adaptiveThreshHigh + 1)
	}
	if state.index != 1 {
		t.Fatalf("blocked resolution was retried: index = %d, want 1", state.index)
	}

	state.RestartForPreset()
	for range adaptiveHighFrames {
		state.Decide(adaptiveThreshHigh + 1)
	}
	if state.index != 0 {
		t.Fatalf("new preset did not clear upscale block: index = %d, want 0", state.index)
	}
}

func TestFpsRingMinMax(t *testing.T) {
	var r fpsRing
	r.Add(10)
	r.Add(30)
	r.Add(5)
	r.Add(20)
	if r.min() != 5 {
		t.Fatalf("min = %v, want 5", r.min())
	}
	if r.max() != 30 {
		t.Fatalf("max = %v, want 30", r.max())
	}
}
