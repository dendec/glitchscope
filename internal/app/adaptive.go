package app

import "github.com/dendec/glitchscope/internal/config"

const (
	fpsWindow = 10
)

// fpsRing is a fixed-size ring buffer of FPS samples for stability checks.
type fpsRing struct {
	data [fpsWindow]float64
	pos  int
	full bool
}

func (r *fpsRing) Add(v float64) {
	r.data[r.pos] = v
	r.pos++
	if r.pos >= fpsWindow {
		r.pos = 0
		r.full = true
	}
}

// min/max return the extremum over valid samples.
func (r *fpsRing) min() float64 {
	n := fpsWindow
	if !r.full {
		n = r.pos
	}
	if n == 0 {
		return 1e9
	}
	m := r.data[0]
	for i := 1; i < n; i++ {
		if r.data[i] < m {
			m = r.data[i]
		}
	}
	return m
}

func (r *fpsRing) max() float64 {
	n := fpsWindow
	if !r.full {
		n = r.pos
	}
	if n == 0 {
		return -1
	}
	m := r.data[0]
	for i := 1; i < n; i++ {
		if r.data[i] > m {
			m = r.data[i]
		}
	}
	return m
}

type adaptivePolicy struct {
	fpsRing     fpsRing
	lowFrames   int     // consecutive frames below threshold
	highFrames  int     // consecutive frames above threshold
	lowElapsed  float64 // seconds below threshold
	highElapsed float64 // seconds above threshold
	cooldown    int     // frames until next change allowed
	params      config.ModeParams
}

func (p *adaptivePolicy) Reset() {
	p.lowFrames = 0
	p.highFrames = 0
	p.lowElapsed = 0
	p.highElapsed = 0
}

func (p *adaptivePolicy) Restart() {
	*p = adaptivePolicy{params: p.params}
}

func (p *adaptivePolicy) triggerDown(current, resolutionCount int) (int, bool, bool) {
	p.Reset()
	p.cooldown = p.params.AdaptiveCooldown
	if current+1 < resolutionCount {
		return current + 1, true, false
	}
	return current, false, true
}

func (p *adaptivePolicy) triggerUp(current int) (int, bool, bool) {
	p.Reset()
	p.cooldown = p.params.AdaptiveCooldown
	return current - 1, true, false
}

// Decide evaluates whether to change render resolution.
//
// Two trigger modes (whichever fires first):
//   - Frame mode: N consecutive frames + ring buffer stability check
//   - Time mode: elapsed time at ultra-low fps (bypasses ring buffer)
//
// Stability: frame mode requires all fpsWindow samples on the same side of
// the threshold. This prevents oscillation when fps hovers near the boundary.
func (p *adaptivePolicy) Decide(fps float64, current, resolutionCount int) (next int, changed bool, minReached bool) {
	if p.cooldown > 0 {
		p.cooldown--
		p.fpsRing.Add(fps)
		return current, false, false
	}

	p.fpsRing.Add(fps)

	switch {
	case fps < p.params.AdaptiveThreshLow:
		p.lowFrames++
		p.lowElapsed += 1.0 / max(fps, 0.1)
		p.highFrames = 0
		p.highElapsed = 0

		if p.lowFrames >= p.params.AdaptiveLowFrames && p.fpsRing.full && p.fpsRing.max() < p.params.AdaptiveThreshLow {
			return p.triggerDown(current, resolutionCount)
		}
		if p.lowElapsed >= p.params.AdaptiveLowSec {
			return p.triggerDown(current, resolutionCount)
		}

	case fps > p.params.AdaptiveThreshHigh:
		p.highFrames++
		p.highElapsed += 1.0 / max(fps, 0.1)
		p.lowFrames = 0
		p.lowElapsed = 0

		if p.highFrames >= p.params.AdaptiveHighFrames && p.fpsRing.full && p.fpsRing.min() > p.params.AdaptiveThreshHigh && current > 0 {
			return p.triggerUp(current)
		}
		if p.highElapsed >= p.params.AdaptiveHighSec && current > 0 {
			return p.triggerUp(current)
		}

	default:
		p.Reset()
	}

	return current, false, false
}
