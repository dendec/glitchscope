package app

import "time"

const (
	fpsWindow          = 10
	adaptiveThreshLow  = 20.0            // fps below this triggers downscale
	adaptiveThreshHigh = 30.0            // fps above this triggers upscale
	adaptiveLowHold    = 2 * time.Second // must stay low this long to downscale
	adaptiveHighHold   = 5 * time.Second // must stay high this long to upscale
	adaptiveCooldown   = 2 * time.Second // min time between resolution changes
)

type adaptivePolicy struct {
	lowSince  time.Time // when fps first dropped below low threshold
	highSince time.Time // when fps first rose above high threshold
	cooldown  time.Time
}

func (p *adaptivePolicy) Reset() {
	p.lowSince = time.Time{}
	p.highSince = time.Time{}
}

// Decide returns the next resolution index, whether it changed, and whether
// the resolution is already at minimum but fps is still critically low
// (meaning the preset is too heavy for this device).
func (p *adaptivePolicy) Decide(now time.Time, fps float64, current, resolutionCount int) (next int, changed bool, minReached bool) {
	// Cooldown: don't change resolution too frequently.
	if now.Sub(p.cooldown) <= adaptiveCooldown {
		return current, false, false
	}

	switch {
	case fps < adaptiveThreshLow:
		if p.lowSince.IsZero() {
			p.lowSince = now
		}
		p.highSince = time.Time{}
		if now.Sub(p.lowSince) >= adaptiveLowHold {
			if current+1 < resolutionCount {
				p.Reset()
				p.cooldown = now
				return current + 1, true, false
			}
			// Already at minimum resolution but fps still low.
			return current, false, true
		}
	case fps > adaptiveThreshHigh:
		if p.highSince.IsZero() {
			p.highSince = now
		}
		p.lowSince = time.Time{}
		if now.Sub(p.highSince) >= adaptiveHighHold && current > 0 {
			p.Reset()
			p.cooldown = now
			return current - 1, true, false
		}
	default:
		// In the dead zone (20-30): reset both timers.
		p.Reset()
	}
	return current, false, false
}
