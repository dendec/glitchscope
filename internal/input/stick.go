package input

import (
	"math"
	"time"
)

// Shared analog-stick response curve used by UI navigation and playback seek.
const (
	// StickMaxHoldDeflection is the minimum magnitude treated as a full tilt.
	StickMaxHoldDeflection = 0.9
	// StickRampDelay is the seconds at full tilt before acceleration starts.
	StickRampDelay = 0.4
	// StickMaxAcceleration caps the hold multiplier.
	StickMaxAcceleration = 8.0
)

// StickSpeed applies the shared deflection and max-hold curve used by analog
// controls. Its result is a signed multiplier for the caller's base speed.
func StickSpeed(deflection, holdDuration float64) float64 {
	deflection = max(-1, min(1, deflection))
	acceleration := 1.0
	if math.Abs(deflection) >= StickMaxHoldDeflection {
		acceleration = StickAccelerationMultiplier(holdDuration)
	}
	return deflection * acceleration
}

// StickAccelerationMultiplier doubles speed once per second after the ramp
// delay, up to StickMaxAcceleration.
func StickAccelerationMultiplier(holdDuration float64) float64 {
	if holdDuration <= StickRampDelay {
		return 1
	}
	elapsed := math.Floor(holdDuration - StickRampDelay + 1e-9)
	return min(math.Pow(2, elapsed), StickMaxAcceleration)
}

// StickDrive tracks how long one direction has remained at maximum deflection
// and returns the shared signed speed multiplier for each frame.
type StickDrive struct {
	atMax        bool
	maxDirection int
	maxHoldAt    time.Time
}

// Update returns the current signed speed multiplier and advances hold state.
func (d *StickDrive) Update(deflection float64, now time.Time) float64 {
	return StickSpeed(deflection, d.UpdateHold(deflection, now))
}

// UpdateHold records deflection and returns the duration at maximum deflection.
func (d *StickDrive) UpdateHold(deflection float64, now time.Time) float64 {
	magnitude := math.Abs(deflection)
	direction := 1
	if deflection < 0 {
		direction = -1
	}
	if magnitude >= StickMaxHoldDeflection {
		if !d.atMax || d.maxDirection != direction {
			d.atMax = true
			d.maxDirection = direction
			d.maxHoldAt = now
		}
	} else {
		d.atMax = false
		d.maxDirection = 0
	}
	return d.HoldDuration(now)
}

// HoldDuration returns how long the stick has stayed at maximum deflection.
func (d *StickDrive) HoldDuration(now time.Time) float64 {
	if !d.atMax {
		return 0
	}
	return now.Sub(d.maxHoldAt).Seconds()
}

// Reset clears the current maximum-deflection hold.
func (d *StickDrive) Reset() {
	d.atMax = false
	d.maxDirection = 0
	d.maxHoldAt = time.Time{}
}
