package app

import "time"

const (
	fpsWindow          = 10
	adaptiveThreshLow  = 25.0
	adaptiveThreshHigh = 30.0
	adaptiveDownCount  = 15
	adaptiveUpCount    = 30
	adaptiveCooldown   = 2 * time.Second
)

type adaptivePolicy struct {
	lowCount  int
	highCount int
	cooldown  time.Time
}

func (p *adaptivePolicy) Reset() {
	p.lowCount = 0
	p.highCount = 0
	p.cooldown = time.Time{}
}

func (p *adaptivePolicy) Decide(now time.Time, fps float64, current, resolutionCount int) (next int, changed bool) {
	switch {
	case fps < adaptiveThreshLow:
		p.lowCount++
		p.highCount = 0
	case fps > adaptiveThreshHigh:
		p.highCount++
		p.lowCount = 0
	default:
		p.lowCount = 0
		p.highCount = 0
	}

	if now.Sub(p.cooldown) <= adaptiveCooldown {
		return current, false
	}

	if p.lowCount >= adaptiveDownCount && current+1 < resolutionCount {
		p.Reset()
		p.cooldown = now
		return current + 1, true
	}
	if p.highCount >= adaptiveUpCount && current > 0 {
		p.Reset()
		p.cooldown = now
		return current - 1, true
	}
	return current, false
}
