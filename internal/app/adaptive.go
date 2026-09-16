package app

import (
	"time"

	"github.com/dendec/glitchscope/internal/config"
)

type adaptiveAction uint8

const (
	adaptiveNone adaptiveAction = iota
	adaptiveFrequencyDown
	adaptiveFrequencyUp
	adaptiveResolutionDown
	adaptiveResolutionUp
	// adaptiveResample requests a disjoint measurement window without changing quality.
	adaptiveResample
)

// Allow small scheduling jitter without treating it as sustained overload.
const cadenceTolerance = 1.10

// adaptivePolicy preserves cadence before optimizing the soft work budget.
// The state bounds and validates resolution trials.
type adaptivePolicy struct {
	params        config.ModeParams
	highSince     time.Time
	upSince       time.Time
	lowSince      time.Time
	cooldownUntil time.Time
}

func (p *adaptivePolicy) Reset() {
	p.highSince = time.Time{}
	p.upSince = time.Time{}
	p.lowSince = time.Time{}
}

func (p *adaptivePolicy) Restart() {
	*p = adaptivePolicy{params: p.params}
}

func (p *adaptivePolicy) Decide(utilization, cadence float64, now time.Time, currentFPS, currentIndex, ceilingIndex, resolutionCount int) adaptiveAction {
	if !p.cooldownUntil.IsZero() && now.Before(p.cooldownUntil) {
		return adaptiveNone
	}

	// Frequency recovery uses the whole frame period, not the soft energy band.
	nextFPS := min(currentFPS+int(p.params.FrequencyStep), int(p.params.VisualizerFPS))
	if currentFPS < nextFPS && cadence <= cadenceTolerance &&
		utilization*float64(nextFPS)/float64(max(currentFPS, 1)) <= 0.90 {
		p.highSince = time.Time{}
		p.lowSince = time.Time{}
		if p.upSince.IsZero() {
			p.upSince = now
			return adaptiveNone
		}
		if now.Sub(p.upSince) >= p.params.UpshiftAfter {
			p.resetAfterChange(now)
			return adaptiveFrequencyUp
		}
		return adaptiveNone
	}
	p.upSince = time.Time{}

	switch {
	case utilization > p.params.UtilizationHigh || cadence > cadenceTolerance:
		p.lowSince = time.Time{}
		if p.highSince.IsZero() {
			p.highSince = now
			return adaptiveNone
		}
		if now.Sub(p.highSince) < p.params.DownshiftAfter {
			return adaptiveNone
		}
		p.resetAfterChange(now)
		if currentIndex+1 < resolutionCount {
			return adaptiveResolutionDown
		}
		// Exceeding an energy target alone must never sacrifice sustained FPS.
		if cadence > cadenceTolerance && currentFPS > int(p.params.MinVisualizerFPS) {
			return adaptiveFrequencyDown
		}
	case utilization < p.params.UtilizationHigh:
		p.highSince = time.Time{}
		p.upSince = time.Time{}
		if utilization >= p.params.UtilizationLow {
			p.lowSince = time.Time{}
			return adaptiveNone
		}
		if p.lowSince.IsZero() {
			p.lowSince = now
			return adaptiveNone
		}
		if now.Sub(p.lowSince) < p.params.UpshiftAfter {
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
