package app

import (
	"math"
	"testing"
	"time"
)

func TestSeekAccelMultiplier(t *testing.T) {
	tests := []struct {
		holdDur float64
		want    float64
	}{
		{0, 1},
		{0.3, 1},            // before ramp delay
		{seekRampDelay, 1},  // at the delay, still 1
		{1.4, 2},            // one doubling
		{2.4, 4},            // two doublings
		{3.4, 8},            // three doublings
		{100, seekMaxAccel}, // capped
	}
	for _, tt := range tests {
		if got := seekAccelMultiplier(tt.holdDur); got != tt.want {
			t.Errorf("seekAccelMultiplier(%v) = %v, want %v", tt.holdDur, got, tt.want)
		}
	}
}

func TestSeekSpeedUsesDeflection(t *testing.T) {
	// No hold acceleration: speed is linear in deflection and base at full.
	if got := seekSpeed(1.0, 0); got != seekBaseSpeed {
		t.Errorf("seekSpeed(1, 0) = %v, want %v", got, seekBaseSpeed)
	}
	if got := seekSpeed(0.5, 0); got != seekBaseSpeed*0.5 {
		t.Errorf("seekSpeed(0.5, 0) = %v, want %v", got, seekBaseSpeed*0.5)
	}
	if got := seekSpeed(0, 0); got != 0 {
		t.Errorf("seekSpeed(0, 0) = %v, want 0", got)
	}
	// Direction carried by sign.
	if got := seekSpeed(-1.0, 0); got != -seekBaseSpeed {
		t.Errorf("seekSpeed(-1, 0) = %v, want %v", got, -seekBaseSpeed)
	}
}

func TestSeekSpeedAccelerationOnlyAtMax(t *testing.T) {
	// Below the max-deflection threshold, long hold does not accelerate.
	if got := seekSpeed(0.5, 100); got != seekBaseSpeed*0.5 {
		t.Errorf("seekSpeed(0.5, 100) = %v, want %v", got, seekBaseSpeed*0.5)
	}
	// At max deflection, long hold accelerates.
	hold := 3.4
	want := seekBaseSpeed * seekAccelMultiplier(hold)
	if got := seekSpeed(1.0, hold); got != want {
		t.Errorf("seekSpeed(1, %v) = %v, want %v", hold, got, want)
	}
	// Capped.
	if got := seekSpeed(1.0, 100); got != seekBaseSpeed*seekMaxAccel {
		t.Errorf("seekSpeed(1, 100) = %v, want %v", got, seekBaseSpeed*seekMaxAccel)
	}
}

func TestSeekControlHoldTracking(t *testing.T) {
	var c seekControl
	start := time.Unix(0, 0)

	// Center stick: no hold.
	if d := c.UpdateHold(0, start); d != 0 {
		t.Fatalf("UpdateHold(0) = %v, want 0", d)
	}
	// Push to max: hold begins.
	if d := c.UpdateHold(1.0, start); d != 0 {
		t.Fatalf("UpdateHold(1.0) at t0 = %v, want 0", d)
	}
	// Still at max a second later: hold = 1s.
	if d := c.UpdateHold(1.0, start.Add(time.Second)); math.Abs(d-1.0) > 1e-9 {
		t.Fatalf("UpdateHold(1.0) at t1 = %v, want 1.0", d)
	}
	// Ease off: hold resets.
	if d := c.UpdateHold(0.3, start.Add(2*time.Second)); d != 0 {
		t.Fatalf("UpdateHold(0.3) = %v, want 0", d)
	}
	if d := c.HoldDuration(start.Add(3 * time.Second)); d != 0 {
		t.Fatalf("HoldDuration after easing off = %v, want 0", d)
	}
}

func TestSeekControlTargetBatching(t *testing.T) {
	var c seekControl
	dt := 1.0 / 60.0

	// No input -> no seek.
	if got := c.Target(10, 0, dt); got != -1 {
		t.Fatalf("Target(0 velocity) = %v, want -1", got)
	}
	// Full-deflection forward at 10 s/s: first frame delta ~0.17s > thresh.
	first := c.Target(10, seekBaseSpeed, dt)
	if first < 0 {
		t.Fatalf("first Target returned %v, want a target", first)
	}
	// Same direction at low speed below the apply threshold batches (returns -1)
	// until the accumulated delta is meaningful.
	lowSpeed := 0.1 // 0.1 s/s → delta per frame ~0.0017s
	got := c.Target(20, lowSpeed, dt)
	if got != -1 {
		t.Fatalf("low-speed Target = %v, want -1 (batched)", got)
	}
}

func TestSeekControlDirectionChangeAppliesImmediately(t *testing.T) {
	var c seekControl
	dt := 1.0 / 60.0

	// Start forward.
	if got := c.Target(10, seekBaseSpeed, dt); got < 0 {
		t.Fatalf("forward Target = %v, want target", got)
	}
	// Reverse direction: even a small delta must apply immediately.
	got := c.Target(12, -0.1, dt)
	if got < 0 {
		t.Fatalf("direction-change Target = %v, want target", got)
	}
	if got != 12-0.1*dt {
		t.Fatalf("direction-change Target = %v, want %v", got, 12-0.1*dt)
	}
}

func TestSeekSpeedAccelerationCurve(t *testing.T) {
	// Sanity: after 5s of max-hold, speed is capped at base*maxAccel.
	s := seekSpeed(1.0, 5.0)
	if s != seekBaseSpeed*seekMaxAccel {
		t.Fatalf("seekSpeed(1, 5) = %v, want %v", s, seekBaseSpeed*seekMaxAccel)
	}
}
