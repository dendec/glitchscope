package app

import (
	"testing"
	"time"

	"github.com/dendec/glitchscope/internal/config"
)

func TestFpsMeterUsesFixedWindow(t *testing.T) {
	var meter fpsMeter
	for i := 0; i < fpsWindow; i++ {
		meter.Add(float64(i + 1))
	}
	if !meter.Full() {
		t.Fatal("expected a full FPS window")
	}
	if got, want := meter.Average(), 5.5; got != want {
		t.Fatalf("average = %v, want %v", got, want)
	}

	meter.Add(21)
	if got, want := meter.Average(), 7.5; got != want {
		t.Fatalf("rolling average = %v, want %v", got, want)
	}
}

func TestAdaptivePolicyDownAfterLowFPS(t *testing.T) {
	var policy adaptivePolicy
	now := time.Now()

	// Before the hold duration: no change.
	for i := 0; i < 10; i++ {
		if next, changed, minReached := policy.Decide(now.Add(time.Duration(i)*100*time.Millisecond), adaptiveThreshLow-1, 0, 3); changed || next != 0 || minReached {
			t.Fatalf("changed too early at step %d: next=%d changed=%v minReached=%v", i, next, changed, minReached)
		}
	}

	// After adaptiveLowHold: should step down.
	if next, changed, minReached := policy.Decide(now.Add(adaptiveLowHold), adaptiveThreshLow-1, 0, 3); !changed || next != 1 || minReached {
		t.Fatalf("step down = (%d, %v, %v), want (1, true, false)", next, changed, minReached)
	}
}

func TestAdaptivePolicyUpAfterHighFPS(t *testing.T) {
	var policy adaptivePolicy
	now := time.Now()

	// Before the hold duration: no change.
	for i := 0; i < 20; i++ {
		if next, changed, minReached := policy.Decide(now.Add(time.Duration(i)*100*time.Millisecond), adaptiveThreshHigh+1, 1, 3); changed || next != 1 || minReached {
			t.Fatalf("changed too early at step %d: next=%d changed=%v minReached=%v", i, next, changed, minReached)
		}
	}

	// After adaptiveHighHold: should step up.
	if next, changed, minReached := policy.Decide(now.Add(adaptiveHighHold), adaptiveThreshHigh+1, 1, 3); !changed || next != 0 || minReached {
		t.Fatalf("step up = (%d, %v, %v), want (0, true, false)", next, changed, minReached)
	}
}

func TestAdaptivePolicyDeadZoneResetsTimers(t *testing.T) {
	var policy adaptivePolicy
	now := time.Now()

	// Start low for a while.
	policy.Decide(now, adaptiveThreshLow-1, 0, 3)
	// Then jump into dead zone.
	policy.Decide(now.Add(1*time.Second), adaptiveThreshLow+5, 0, 3)
	// Then back low — timer should have reset, needs full hold again.
	if next, changed, minReached := policy.Decide(now.Add(2*time.Second), adaptiveThreshLow-1, 0, 3); changed || next != 0 || minReached {
		t.Fatalf("dead zone should reset low timer: next=%d changed=%v minReached=%v", next, changed, minReached)
	}
}

func TestAdaptivePolicyCooldown(t *testing.T) {
	var policy adaptivePolicy
	now := time.Now()

	// Trigger first downscale.
	policy.Decide(now.Add(0), adaptiveThreshLow-1, 0, 3)
	policy.Decide(now.Add(adaptiveLowHold), adaptiveThreshLow-1, 0, 3) // triggers

	// Immediately try again — cooldown blocks it.
	policy.Reset()
	if next, changed, minReached := policy.Decide(now.Add(adaptiveLowHold+time.Nanosecond), adaptiveThreshLow-1, 1, 3); changed || next != 1 || minReached {
		t.Fatalf("cooldown should block: next=%d changed=%v minReached=%v", next, changed, minReached)
	}
}

func TestAdaptivePolicyMinReached(t *testing.T) {
	var policy adaptivePolicy
	now := time.Now()

	// At minimum resolution (current == resolutionCount-1) and still low fps.
	policy.Decide(now, adaptiveThreshLow-1, 2, 3)
	if next, changed, minReached := policy.Decide(now.Add(adaptiveLowHold), adaptiveThreshLow-1, 2, 3); changed || next != 2 || !minReached {
		t.Fatalf("min reached = (%d, %v, %v), want (2, false, true)", next, changed, minReached)
	}
}

func TestAdaptivePolicyBounds(t *testing.T) {
	var policy adaptivePolicy
	now := time.Now()

	// Already at highest resolution (current == 0): can't go higher.
	if next, changed, minReached := policy.Decide(now.Add(adaptiveHighHold), adaptiveThreshHigh+1, 0, 3); changed || next != 0 || minReached {
		t.Fatalf("at upper bound: next=%d changed=%v minReached=%v", next, changed, minReached)
	}
}

func TestResolutionStateDecidesAndUpdatesIndex(t *testing.T) {
	state := resolutionState{
		resolutions: []config.RenderResolution{{Width: 320, Height: 180}, {Width: 480, Height: 270}},
	}
	now := time.Now()
	// Simulate sustained low fps for longer than the hold duration.
	for i := 0; i < 20; i++ {
		state.Decide(now.Add(time.Duration(i)*200*time.Millisecond), adaptiveThreshLow-1)
	}

	if state.index != 1 {
		t.Fatalf("index = %d, want 1", state.index)
	}
	if got := state.resolutions[state.index]; got.Width != 480 || got.Height != 270 {
		t.Fatalf("resolution = %v, want 480x270", got)
	}
}
