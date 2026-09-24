package app

import (
	"time"

	"github.com/dendec/glitchscope/internal/input"
)

// Continuous-seek drivetrain. It uses the shared analog response curve from
// internal/input, also used by left-stick list scrolling. See docs/SEEK-DESIGN.md.

const (
	// seekBaseSpeed is the seek velocity (seconds/second) at full deflection.
	seekBaseSpeed = 10.0
	// Keep these names for the seek policy and its regression tests; the shared
	// stick implementation owns their values.
	seekMaxAccel  = input.StickMaxAcceleration
	seekMaxHoldAt = input.StickMaxHoldDeflection
	seekRampDelay = input.StickRampDelay
	// seekApplyThresh is the minimum accumulated seek delta (seconds) before a
	// Seek is actually issued, to avoid re-seeking the decoder every frame.
	seekApplyThresh = 0.1
)

// seekAccelMultiplier returns the speed multiplier for holding the stick at
// max deflection for holdDur seconds: 1 until the ramp delay elapses, then
// doubling each second, capped at seekMaxAccel. A small epsilon keeps the
// floor stable against binary float representation (e.g. 1.4-0.4 ≈ 0.9999…).
func seekAccelMultiplier(holdDur float64) float64 {
	return input.StickAccelerationMultiplier(holdDur)
}

// seekSpeed returns the effective seek velocity in seconds/second for a stick
// deflection in [-1,1] and the duration (seconds) the stick has been held at
// max deflection. Sign carries the direction.
func seekSpeed(deflection, holdDur float64) float64 {
	return input.StickSpeed(deflection, holdDur) * seekBaseSpeed
}

// seekControl drives continuous seek across frames: it tracks whether the
// stick is pinned at max deflection (to measure the acceleration hold) and
// batches actual Seek calls so the decoder is not re-seeked every frame.
type seekControl struct {
	drive       input.StickDrive
	lastApplied float64 // last seek target actually applied (-1 = none)
	lastDir     int     // last applied direction (+1/-1)
}

func (c *seekControl) Speed(deflection float64, now time.Time) float64 {
	return c.drive.Update(deflection, now) * seekBaseSpeed
}

// HoldDuration returns how long the stick has been held at max deflection.
func (c *seekControl) HoldDuration(now time.Time) float64 {
	return c.drive.HoldDuration(now)
}

// UpdateHold records the stick deflection this frame and returns the updated
// hold duration (seconds at max deflection).
func (c *seekControl) UpdateHold(deflection float64, now time.Time) float64 {
	return c.drive.UpdateHold(deflection, now)
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
