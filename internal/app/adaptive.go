package app

const (
	fpsWindow          = 10
	adaptiveThreshLow  = 20.0 // fps below this triggers downscale
	adaptiveThreshHigh = 24.0 // fps above this triggers upscale
	adaptiveLowFrames  = 120  // frames below threshold to downscale (~2s @60fps)
	adaptiveHighFrames = 300  // frames above threshold to upscale  (~5s @60fps)
	adaptiveCooldown   = 120  // min frames between resolution changes
)

type adaptivePolicy struct {
	lowCount  int // consecutive frames below threshold
	highCount int // consecutive frames above threshold
	cooldown  int // frames until next change allowed
}

func (p *adaptivePolicy) Reset() {
	p.lowCount = 0
	p.highCount = 0
}

// Decide returns the next resolution index, whether it changed, and whether
// the resolution is already at minimum but fps is still critically low.
func (p *adaptivePolicy) Decide(fps float64, current, resolutionCount int) (next int, changed bool, minReached bool) {
	if p.cooldown > 0 {
		p.cooldown--
		return current, false, false
	}

	switch {
	case fps < adaptiveThreshLow:
		p.lowCount++
		p.highCount = 0
		if p.lowCount >= adaptiveLowFrames {
			p.Reset()
			p.cooldown = adaptiveCooldown
			if current+1 < resolutionCount {
				return current + 1, true, false
			}
			return current, false, true
		}
	case fps > adaptiveThreshHigh:
		p.highCount++
		p.lowCount = 0
		if p.highCount >= adaptiveHighFrames && current > 0 {
			p.Reset()
			p.cooldown = adaptiveCooldown
			return current - 1, true, false
		}
	default:
		p.Reset()
	}
	return current, false, false
}
