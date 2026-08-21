package app

import (
	"math"
	"time"
)

// Continuous-seek drivetrain. Mirrors the held-key scroll acceleration used
// for navigation (Overlay.updateScrollHold): speed is proportional to stick
// deflection, and holding the stick at max deflection for long enough
// accelerates it further (doubling each second, capped). See
// docs/SEEK-DESIGN.md.

const (
	// seekBaseSpeed is the seek velocity (seconds/second) at full deflection.
	seekBaseSpeed = 10.0
	// seekMaxAccel caps the additional multiplier from hold acceleration.
	seekMaxAccel = 8.0
	// seekMaxHoldAt is the deflection (absolute normalized axis) treated as
	// "held to the max" for acceleration purposes.
	seekMaxHoldAt = 0.9
	// seekRampDelay is how long the stick must sit at max deflection before
	// acceleration begins (seconds).
	seekRampDelay = 0.4
	// seekApplyThresh is the minimum accumulated seek delta (seconds) before a
	// Seek is actually issued, to avoid re-seeking the decoder every frame.
	seekApplyThresh = 0.1
)

// seekAccelMultiplier returns the speed multiplier for holding the stick at
// max deflection for holdDur seconds: 1 until the ramp delay elapses, then
// doubling each second, capped at seekMaxAccel. A small epsilon keeps the
// floor stable against binary float representation (e.g. 1.4-0.4 ≈ 0.9999…).
func seekAccelMultiplier(holdDur float64) float64 {
	if holdDur <= seekRampDelay {
		return 1
	}
	elapsed := math.Floor(holdDur - seekRampDelay + 1e-9)
	mult := math.Pow(2, elapsed)
	if mult > seekMaxAccel {
		mult = seekMaxAccel
	}
	return mult
}

// seekSpeed returns the effective seek velocity in seconds/second for a stick
// deflection in [-1,1] and the duration (seconds) the stick has been held at
// max deflection. Sign carries the direction.
func seekSpeed(deflection, holdDur float64) float64 {
	dir := 1.0
	if deflection < 0 {
		dir = -1
		deflection = -deflection
	}
	speed := deflection * seekBaseSpeed
	if deflection >= seekMaxHoldAt {
		speed *= seekAccelMultiplier(holdDur)
	}
	return dir * speed
}

// seekControl drives continuous seek across frames: it tracks whether the
// stick is pinned at max deflection (to measure the acceleration hold) and
// batches actual Seek calls so the decoder is not re-seeked every frame.
type seekControl struct {
	atMax       bool
	maxHoldAt   time.Time
	lastApplied float64 // last seek target actually applied (-1 = none)
	lastDir     int     // last applied direction (+1/-1)
}

// HoldDuration returns how long the stick has been held at max deflection.
func (c *seekControl) HoldDuration(now time.Time) float64 {
	if !c.atMax {
		return 0
	}
	return now.Sub(c.maxHoldAt).Seconds()
}

// UpdateHold records the stick deflection this frame and returns the updated
// hold duration (seconds at max deflection).
func (c *seekControl) UpdateHold(deflection float64, now time.Time) float64 {
	if deflection < 0 {
		deflection = -deflection
	}
	if deflection >= seekMaxHoldAt {
		if !c.atMax {
			c.atMax = true
			c.maxHoldAt = now
		}
	} else {
		c.atMax = false
	}
	return c.HoldDuration(now)
}

// Target computes the seek target for this frame. pos is the current
// playback position, velocity the signed speed (s/s, from seekSpeed) and dt
// the frame duration. Returns -1 when no seek should be issued this frame
// (not enough motion yet, or no input).
func (c *seekControl) Target(pos, velocity, dt float64) float64 {
	if velocity == 0 || dt <= 0 {
		return -1
	}
	dir := 1
	if velocity < 0 {
		dir = -1
	}
	delta := velocity * dt

	// Direction change: apply immediately regardless of magnitude.
	if c.lastDir != 0 && dir != c.lastDir {
		c.lastDir = dir
		c.lastApplied = clampSeekTarget(pos + delta)
		return c.lastApplied
	}

	// Otherwise batch until we've accumulated a meaningful delta.
	if c.lastApplied >= 0 {
		absDelta := delta
		if absDelta < 0 {
			absDelta = -absDelta
		}
		if absDelta < seekApplyThresh {
			return -1
		}
	}
	c.lastDir = dir
	c.lastApplied = clampSeekTarget(pos + delta)
	return c.lastApplied
}

// clampSeekTarget keeps a raw target non-negative; the upper bound is clamped
// by Player.Seek using Duration().
func clampSeekTarget(target float64) float64 {
	if target < 0 {
		return 0
	}
	return target
}
