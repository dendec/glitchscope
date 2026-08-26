package app

const (
	fpsWindow          = 10
	adaptiveThreshLow  = 20.0
	adaptiveThreshHigh = 24.0
	adaptiveLowFrames  = 10  // frames below threshold to downscale
	adaptiveHighFrames = 10  // frames above threshold to upscale
	adaptiveLowSec     = 2.0 // time fallback for ultra-low fps (1-2 fps)
	adaptiveHighSec    = 4.0 // time fallback for upscale
	adaptiveCooldown   = 10  // min frames between resolution changes
)

// fpsRing is a fixed-size ring buffer of recent FPS samples.
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
	lowElapsed  float64 // seconds below threshold (accumulated per-frame)
	highElapsed float64 // seconds above threshold
	cooldown    int     // frames until next change allowed
}

func (p *adaptivePolicy) Reset() {
	p.lowFrames = 0
	p.highFrames = 0
	p.lowElapsed = 0
	p.highElapsed = 0
}

// Decide evaluates whether to change render resolution.
//
// Two trigger modes (whichever fires first):
//   - Frame mode: 10 consecutive frames + ring buffer stability check
//   - Time mode: 2s elapsed at ultra-low fps (bypasses ring buffer requirement)
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
	case fps < adaptiveThreshLow:
		p.lowFrames++
		p.lowElapsed += 1.0 / max(fps, 0.1)
		p.highFrames = 0
		p.highElapsed = 0

		// Frame trigger: requires stable low fps across all ring samples.
		if p.lowFrames >= adaptiveLowFrames && p.fpsRing.full && p.fpsRing.max() < adaptiveThreshLow {
			p.Reset()
			p.cooldown = adaptiveCooldown
			if current+1 < resolutionCount {
				return current + 1, true, false
			}
			return current, false, true
		}
		// Time fallback: at 1-2 fps, ring takes too long to fill.
		if p.lowElapsed >= adaptiveLowSec {
			p.Reset()
			p.cooldown = adaptiveCooldown
			if current+1 < resolutionCount {
				return current + 1, true, false
			}
			return current, false, true
		}

	case fps > adaptiveThreshHigh:
		p.highFrames++
		p.highElapsed += 1.0 / max(fps, 0.1)
		p.lowFrames = 0
		p.lowElapsed = 0

		// Frame trigger: requires stable high fps across all ring samples.
		if p.highFrames >= adaptiveHighFrames && p.fpsRing.full && p.fpsRing.min() > adaptiveThreshHigh && current > 0 {
			p.Reset()
			p.cooldown = adaptiveCooldown
			return current - 1, true, false
		}
		// Time fallback.
		if p.highElapsed >= adaptiveHighSec && current > 0 {
			p.Reset()
			p.cooldown = adaptiveCooldown
			return current - 1, true, false
		}

	default:
		// Dead zone (20-24): reset all counters.
		p.Reset()
	}

	return current, false, false
}
