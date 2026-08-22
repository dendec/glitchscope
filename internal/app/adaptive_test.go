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

func TestAdaptivePolicyStepsDownAfterLowFPS(t *testing.T) {
	var policy adaptivePolicy
	now := time.Now()
	for i := 0; i < adaptiveDownCount-1; i++ {
		if next, changed := policy.Decide(now, adaptiveThreshLow-1, 0, 3); changed || next != 0 {
			t.Fatalf("changed too early at sample %d: next=%d changed=%v", i, next, changed)
		}
	}
	next, changed := policy.Decide(now, adaptiveThreshLow-1, 0, 3)
	if !changed || next != 1 {
		t.Fatalf("step down = (%d, %v), want (1, true)", next, changed)
	}
}

func TestAdaptivePolicyHonorsCooldownAndBounds(t *testing.T) {
	var policy adaptivePolicy
	now := time.Now()
	for i := 0; i < adaptiveUpCount; i++ {
		policy.Decide(now, adaptiveThreshHigh+1, 1, 3)
	}
	if next, changed := policy.Decide(now, adaptiveThreshHigh+1, 1, 3); changed || next != 1 {
		t.Fatalf("cooldown result = (%d, %v), want (1, false)", next, changed)
	}

	policy.Reset()
	for i := 0; i < adaptiveUpCount; i++ {
		policy.Decide(now.Add(adaptiveCooldown+time.Nanosecond), adaptiveThreshHigh+1, 0, 3)
	}
	if next, changed := policy.Decide(now.Add(2*adaptiveCooldown+time.Nanosecond), adaptiveThreshHigh+1, 0, 3); changed || next != 0 {
		t.Fatalf("upper bound result = (%d, %v), want (0, false)", next, changed)
	}
}

func TestResolutionStateDecidesAndUpdatesIndex(t *testing.T) {
	state := resolutionState{
		resolutions: []config.RenderResolution{{Width: 320, Height: 180}, {Width: 480, Height: 270}},
	}
	now := time.Now()
	for i := 0; i < adaptiveDownCount; i++ {
		state.Decide(now, adaptiveThreshLow-1)
	}

	if state.index != 1 {
		t.Fatalf("index = %d, want 1", state.index)
	}
	if got := state.resolutions[state.index]; got.Width != 480 || got.Height != 270 {
		t.Fatalf("resolution = %v, want 480x270", got)
	}
}
