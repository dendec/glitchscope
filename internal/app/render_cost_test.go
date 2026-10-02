package app

import (
	"math"
	"testing"
	"time"
)

func TestFrameCostIncludesPresentationButNotSchedulerWait(t *testing.T) {
	var meter frameCostMeter
	now := time.Unix(100, 0)
	for i := range renderCostWindow + 1 {
		// 15 ms render + 10 ms presentation + 25 ms scheduled idle = 20 FPS.
		meter.AddFrame(15*time.Millisecond, 10*time.Millisecond, now.Add(time.Duration(i)*50*time.Millisecond))
		if i < renderCostWindow && meter.Full() {
			t.Fatal("cadence window filled before enough intervals were measured")
		}
	}
	if !meter.Full() || meter.Average() != 25*time.Millisecond || meter.Cadence(20) != 1 || meter.Utilization(20) != 0.5 {
		t.Fatalf("frame meter mixed work and waiting: %+v", meter)
	}
	meter.Reset()
	meter.AddFrame(15*time.Millisecond, 10*time.Millisecond, now.Add(time.Hour))
	if meter.Full() || meter.Cadence(20) != 0 {
		t.Fatal("reset retained interval across pause/transition/quality change")
	}
}

func TestCalibrationWindowAdaptsToMeasuredFrameTime(t *testing.T) {
	now := time.Unix(100, 0)
	var slow frameCostMeter
	for i := range calibrationSlowRenderSamples {
		slow.AddFrame(time.Second, 0, now.Add(time.Duration(i)*time.Second))
	}
	if !slow.CalibrationFull(60) {
		t.Fatal("three 1 FPS frames did not complete the shortened calibration window")
	}

	var fast frameCostMeter
	for i := range calibrationSlowRenderSamples {
		fast.AddFrame(10*time.Millisecond, 0, now.Add(time.Duration(i)*time.Second/60))
	}
	if fast.CalibrationFull(60) {
		t.Fatal("fast frames completed calibration before the normal six-sample window")
	}
	for i := calibrationSlowRenderSamples; i < calibrationRenderSamples; i++ {
		fast.AddFrame(10*time.Millisecond, 0, now.Add(time.Duration(i)*time.Second/60))
	}
	if !fast.CalibrationFull(60) {
		t.Fatal("normal six-sample calibration window did not complete")
	}
}

func TestCalibrationWindowShortensForSlowCadence(t *testing.T) {
	var meter frameCostMeter
	now := time.Unix(100, 0)
	for i := range calibrationSlowRenderSamples {
		meter.AddFrame(10*time.Millisecond, 0, now.Add(time.Duration(i)*time.Second))
	}
	if !meter.CalibrationFull(60) {
		t.Fatal("slow cadence did not complete the shortened calibration window")
	}
}

func TestCalibrationSevereFailureUsesThreeFrameMedian(t *testing.T) {
	now := time.Unix(100, 0)
	var meter frameCostMeter
	for i := range calibrationSlowRenderSamples {
		meter.AddFrame(time.Second, 0, now.Add(time.Duration(i)*time.Second))
	}
	if !meter.CalibrationSeverelyOverBudget(60) {
		t.Fatal("sustained 1 FPS render was not classified as a severe failure")
	}

	var stalled frameCostMeter
	for i, cost := range []time.Duration{time.Second, 16 * time.Millisecond, 16 * time.Millisecond} {
		stalled.AddFrame(cost, 0, now.Add(time.Duration(i)*time.Second))
	}
	if stalled.CalibrationSeverelyOverBudget(60) {
		t.Fatal("a single stalled frame bypassed the independent confirmation window")
	}
}

func TestFrameCostDetectsDeferredPresentationWork(t *testing.T) {
	var meter frameCostMeter
	now := time.Unix(100, 0)
	for i := range renderCostWindow + 1 {
		// Render-only timing reports 30 ms; a 60 ms presentation is the bottleneck.
		meter.AddFrame(30*time.Millisecond, 60*time.Millisecond, now.Add(time.Duration(i)*90*time.Millisecond))
	}
	if meter.Average() != 90*time.Millisecond || math.Abs(meter.Cadence(20)-1.8) > 0.001 {
		t.Fatal("slow presentation was hidden from adaptive policy")
	}
	meter.Reset()
	for i := range renderCostWindow + 1 {
		meter.AddFrame(30*time.Millisecond, 10*time.Millisecond, now.Add(time.Second+time.Duration(i)*50*time.Millisecond))
	}
	if meter.Average() != 40*time.Millisecond || meter.Cadence(20) != 1 {
		t.Fatal("resolution benefit in presentation was not measured")
	}
}

func TestServiceDelayDoesNotBecomeVisualizerOverload(t *testing.T) {
	var meter frameCostMeter
	now := time.Unix(100, 0)
	for i := range renderCostWindow + 1 {
		meter.AddFrame(20*time.Millisecond, 5*time.Millisecond, now.Add(time.Duration(i)*50*time.Millisecond))
	}
	if !meter.ExcludeServiceDelay(12700*time.Millisecond) || meter.Full() {
		t.Fatal("metadata/UI stall retained cadence history")
	}
	meter.AddFrame(20*time.Millisecond, 5*time.Millisecond, now.Add(13*time.Second))
	if meter.Cadence(20) != 0 {
		t.Fatal("12-second service stall counted as a render interval")
	}
	if meter.ExcludeServiceDelay(time.Millisecond) {
		t.Fatal("normal service tick reset measurements")
	}
	// Slow render/swap durations remain visible and are not filtered by size.
	meter.AddFrame(100*time.Millisecond, 100*time.Millisecond, now.Add(13200*time.Millisecond))
	if meter.Average() <= 100*time.Millisecond {
		t.Fatal("genuine render stall was discarded")
	}
}
