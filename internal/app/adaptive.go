package app

import (
	"time"
)

type adaptiveAction uint8

const (
	adaptiveNone adaptiveAction = iota
	adaptiveResolutionDown
	adaptiveResolutionUp
	// adaptiveResample requests a disjoint measurement window without changing resolution.
	adaptiveResample
)

// Allow small scheduling jitter without treating it as sustained overload.
const cadenceTolerance = 1.10

// adaptiveParams is runtime policy, not persisted user configuration.
type adaptiveParams struct {
	utilizationHigh float64
	downshiftAfter  time.Duration
	upshiftAfter    time.Duration
}

func defaultAdaptiveParams() adaptiveParams {
	return adaptiveParams{
		utilizationHigh: 1,
		downshiftAfter:  time.Second,
		upshiftAfter:    3 * time.Second,
	}
}

// adaptivePolicy preserves cadence before optimizing the soft work budget.
// The state bounds and validates resolution trials.
type adaptivePolicy struct {
	params        adaptiveParams
	highSince     time.Time
	lowSince      time.Time
	cooldownUntil time.Time
}

func (p *adaptivePolicy) Reset() {
	p.highSince = time.Time{}
	p.lowSince = time.Time{}
}

func (p *adaptivePolicy) Restart() {
	*p = adaptivePolicy{params: p.params}
}

func (p *adaptivePolicy) Decide(utilization, cadence float64, now time.Time, currentIndex, ceilingIndex, resolutionCount int) adaptiveAction {
	if !p.cooldownUntil.IsZero() && now.Before(p.cooldownUntil) {
		return adaptiveNone
	}

	switch {
	case utilization > p.params.utilizationHigh || cadence > cadenceTolerance:
		p.lowSince = time.Time{}
		if p.highSince.IsZero() {
			p.highSince = now
			return adaptiveNone
		}
		if now.Sub(p.highSince) < p.params.downshiftAfter {
			return adaptiveNone
		}
		p.resetAfterChange(now)
		if currentIndex+1 < resolutionCount {
			return adaptiveResolutionDown
		}
	case utilization <= p.params.utilizationHigh:
		p.highSince = time.Time{}
		if p.lowSince.IsZero() {
			p.lowSince = now
			return adaptiveNone
		}
		if now.Sub(p.lowSince) < p.params.upshiftAfter {
			return adaptiveNone
		}
		p.resetAfterChange(now)
		if currentIndex > ceilingIndex {
			return adaptiveResolutionUp
		}
	default:
		p.Reset()
	}

	return adaptiveNone
}

func (p *adaptivePolicy) resetAfterChange(now time.Time) {
	p.Reset()
	p.cooldownUntil = now.Add(250 * time.Millisecond)
}
