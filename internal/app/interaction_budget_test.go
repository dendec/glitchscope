package app

import (
	"testing"
	"time"
)

func TestInteractionBudgetFastAndSlowFrames(t *testing.T) {
	now := time.Unix(100, 0)
	for _, base := range []time.Duration{time.Second / 144, time.Second / 60, time.Second / 30, time.Second / 24} {
		var b interactionBudget
		b.Complete(now, now.Add(time.Millisecond))
		if got := b.Period(base, true); got != base {
			t.Fatalf("fast frame: got %v, want %v", got, base)
		}
		if !b.Due(now.Add(base), base, true) {
			t.Fatal("fast frame lost its mode cadence")
		}
		b.Complete(now, now.Add(25*time.Millisecond))
		if got := b.Period(base, true); got != 100*time.Millisecond {
			t.Fatalf("slow frame period = %v", got)
		}
		if got := b.Period(base, false); got != base {
			t.Fatalf("idle period = %v", got)
		}
	}
}

func TestInteractionBudgetSustainedInputCannotStarve(t *testing.T) {
	now := time.Unix(100, 0)
	var b interactionBudget
	b.Complete(now, now.Add(40*time.Millisecond))
	base := time.Second / 30
	for step := 1; step < 125; step++ {
		if b.Due(now.Add(time.Duration(step)*time.Millisecond), base, true) {
			t.Fatalf("early frame at %d ms", step)
		}
	}
	if !b.Due(now.Add(interactionMaxPeriod), base, true) {
		t.Fatal("continuous input starved animation")
	}
	// An overrun cannot trigger immediate catch-up work.
	b.Complete(now, now.Add(200*time.Millisecond))
	if b.Due(now.Add(200*time.Millisecond), base, true) {
		t.Fatal("overrun immediately scheduled another render")
	}
	if !b.Due(now.Add(200*time.Millisecond+mainFramePeriod), base, true) {
		t.Fatal("overrun failed to recover")
	}
}

func TestInteractionBudgetPresentationPressureAndRecovery(t *testing.T) {
	now := time.Unix(100, 0)
	var b interactionBudget
	b.Complete(now, now.Add(2*time.Millisecond))
	b.ObservePresentation(mainFramePeriod, mainFramePeriod)
	if b.cost != 2*time.Millisecond {
		t.Fatal("vsync was counted as rendering")
	}
	b.Complete(now, now.Add(2*time.Millisecond))
	b.ObservePresentation(mainFramePeriod+20*time.Millisecond, mainFramePeriod)
	if b.cost != 22*time.Millisecond {
		t.Fatalf("deferred cost = %v", b.cost)
	}
	b.ObservePresentation(time.Second, mainFramePeriod)
	if b.cost != 22*time.Millisecond {
		t.Fatal("UI-only frame was attributed to previous render twice")
	}
	b.Complete(now, now.Add(2*time.Millisecond))
	if b.cost >= 22*time.Millisecond || b.cost <= 2*time.Millisecond {
		t.Fatalf("recovery not gradual: %v", b.cost)
	}
	if !b.Due(now, time.Second/30, false) {
		t.Fatal("idle rendering still throttled")
	}
}

func TestVisualizerAdaptivePauseState(t *testing.T) {
	var c visualizerClock
	if c.adaptivePaused {
		t.Fatal("new clock unexpectedly paused")
	}
	c.PauseAdaptive(true)
	if !c.adaptivePaused {
		t.Fatal("pause state was not set")
	}
	c.PauseAdaptive(false)
	if c.adaptivePaused {
		t.Fatal("pause state was not cleared")
	}
}
