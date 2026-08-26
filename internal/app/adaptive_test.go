package app

import (
	"testing"

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
	var p adaptivePolicy

	// Before the hold duration: no change.
	for i := 0; i < adaptiveLowFrames-1; i++ {
		if next, changed, minReached := p.Decide(adaptiveThreshLow-1, 0, 3); changed || next != 0 || minReached {
			t.Fatalf("changed too early at step %d: next=%d changed=%v minReached=%v", i, next, changed, minReached)
		}
	}

	// At hold duration: should step down.
	if next, changed, minReached := p.Decide(adaptiveThreshLow-1, 0, 3); !changed || next != 1 || minReached {
		t.Fatalf("step down = (%d, %v, %v), want (1, true, false)", next, changed, minReached)
	}
}

func TestAdaptivePolicyUpAfterHighFPS(t *testing.T) {
	var p adaptivePolicy

	// Before the hold duration: no change.
	for i := 0; i < adaptiveHighFrames-1; i++ {
		if next, changed, minReached := p.Decide(adaptiveThreshHigh+1, 1, 3); changed || next != 1 || minReached {
			t.Fatalf("changed too early at step %d: next=%d changed=%v minReached=%v", i, next, changed, minReached)
		}
	}

	// At hold duration: should step up.
	if next, changed, minReached := p.Decide(adaptiveThreshHigh+1, 1, 3); !changed || next != 0 || minReached {
		t.Fatalf("step up = (%d, %v, %v), want (0, true, false)", next, changed, minReached)
	}
}

func TestAdaptivePolicyDeadZoneResetsCounters(t *testing.T) {
	var p adaptivePolicy

	// Start low for a while.
	for i := 0; i < adaptiveLowFrames-1; i++ {
		p.Decide(adaptiveThreshLow-1, 0, 3)
	}
	// Then jump into dead zone — counters reset.
	p.Decide(adaptiveThreshLow+2, 0, 3)
	// Then back low — needs full hold again.
	if next, changed, minReached := p.Decide(adaptiveThreshLow-1, 0, 3); changed || next != 0 || minReached {
		t.Fatalf("dead zone should reset low counter: next=%d changed=%v minReached=%v", next, changed, minReached)
	}
}

func TestAdaptivePolicyCooldown(t *testing.T) {
	var p adaptivePolicy

	// Trigger first downscale.
	for i := 0; i < adaptiveLowFrames; i++ {
		p.Decide(adaptiveThreshLow-1, 0, 3)
	}
	p.Decide(adaptiveThreshLow-1, 0, 3) // triggers

	// Immediately try again — cooldown blocks it.
	p.Reset()
	if next, changed, minReached := p.Decide(adaptiveThreshLow-1, 1, 3); changed || next != 1 || minReached {
		t.Fatalf("cooldown should block: next=%d changed=%v minReached=%v", next, changed, minReached)
	}
}

func TestAdaptivePolicyMinReached(t *testing.T) {
	var p adaptivePolicy

	// At minimum resolution (current == resolutionCount-1) and still low fps.
	for i := 0; i < adaptiveLowFrames-1; i++ {
		p.Decide(adaptiveThreshLow-1, 2, 3)
	}
	if next, changed, minReached := p.Decide(adaptiveThreshLow-1, 2, 3); changed || next != 2 || !minReached {
		t.Fatalf("min reached = (%d, %v, %v), want (2, false, true)", next, changed, minReached)
	}
}

func TestAdaptivePolicyBounds(t *testing.T) {
	var p adaptivePolicy

	// Already at highest resolution (current == 0): can't go higher.
	for i := 0; i < adaptiveHighFrames; i++ {
		p.Decide(adaptiveThreshHigh+1, 0, 3)
	}
	if next, changed, minReached := p.Decide(adaptiveThreshHigh+1, 0, 3); changed || next != 0 || minReached {
		t.Fatalf("at upper bound: next=%d changed=%v minReached=%v", next, changed, minReached)
	}
}

func TestResolutionStateDecidesAndUpdatesIndex(t *testing.T) {
	state := resolutionState{
		resolutions: []config.RenderResolution{{Width: 320, Height: 180}, {Width: 480, Height: 270}},
	}
	// Simulate sustained low fps.
	for i := 0; i <= adaptiveLowFrames; i++ {
		state.Decide(adaptiveThreshLow - 1)
	}

	if state.index != 1 {
		t.Fatalf("index = %d, want 1", state.index)
	}
	if got := state.resolutions[state.index]; got.Width != 480 || got.Height != 270 {
		t.Fatalf("resolution = %v, want 480x270", got)
	}
}
