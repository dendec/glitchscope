package app

import "time"

// interactionMaxPeriod prevents sustained input from starving animation.
// It bounds scheduling delay, not the duration of a native render call.
const interactionMaxPeriod = 125 * time.Millisecond

// interactionBudget is main-thread scheduling policy, independent of GL and UI.
// During input, reserve roughly three quarters of time for the rest of the loop.
// Fast presets retain their mode cadence. Slow presets back off immediately and
// recover gradually, avoiding oscillation after a single inexpensive frame.
type interactionBudget struct {
	lastStart   time.Time
	lastEnd     time.Time
	cost        time.Duration
	pendingCost time.Duration
}

func (b *interactionBudget) Period(base time.Duration, interacting bool) time.Duration {
	if !interacting {
		return base
	}
	return max(base, min(interactionMaxPeriod, 4*b.cost))
}

func (b *interactionBudget) Due(now time.Time, base time.Duration, interacting bool) bool {
	if !interacting || b.lastStart.IsZero() {
		return true
	}
	period := b.Period(base, true)
	deadline := b.lastStart.Add(period)
	// After an overrun, leave a UI-only tick instead of catching up renders.
	if b.lastEnd.Sub(b.lastStart) >= period {
		deadline = b.lastEnd.Add(mainFramePeriod)
	}
	return !now.Before(deadline)
}

func (b *interactionBudget) Complete(start, end time.Time) {
	b.lastStart, b.lastEnd = start, end
	b.pendingCost = end.Sub(start)
	b.observe(b.pendingCost)
}

func (b *interactionBudget) ObservePresentation(elapsed, displayPeriod time.Duration) {
	if b.pendingCost == 0 {
		return
	}
	if displayPeriod <= 0 {
		displayPeriod = mainFramePeriod
	}
	if excess := elapsed - displayPeriod; excess > 0 {
		b.observe(b.pendingCost + excess)
	}
	b.pendingCost = 0
}

func (b *interactionBudget) observe(cost time.Duration) {
	if cost >= b.cost {
		b.cost = cost
	} else {
		b.cost -= (b.cost - cost) / 8
	}
}
