package app

import "time"

const (
	renderCostWindow             = 10
	calibrationRenderSamples     = 6
	calibrationSlowRenderSamples = 3
)

// renderCostMeter is a fixed rolling window of positive durations.
// frameCostMeter uses separate windows for active work and presentation intervals.
type renderCostMeter struct {
	values [renderCostWindow]time.Duration
	count  int
	next   int
	sum    time.Duration
}

func (m *renderCostMeter) Add(cost time.Duration) {
	if cost <= 0 {
		return
	}
	if m.count < len(m.values) {
		m.count++
	} else {
		m.sum -= m.values[m.next]
	}
	m.values[m.next] = cost
	m.sum += cost
	m.next = (m.next + 1) % len(m.values)
}

func (m *renderCostMeter) Full() bool {
	return m.count == len(m.values)
}

func (m *renderCostMeter) Count() int {
	return m.count
}

func (m *renderCostMeter) LastThreeMedian() time.Duration {
	if m.count < calibrationSlowRenderSamples {
		return 0
	}
	last := (m.next + len(m.values) - 1) % len(m.values)
	previous := (last + len(m.values) - 1) % len(m.values)
	first := (previous + len(m.values) - 1) % len(m.values)
	return medianDuration3([calibrationSlowRenderSamples]time.Duration{
		m.values[first], m.values[previous], m.values[last],
	})
}

func (m *renderCostMeter) Average() time.Duration {
	if m.count == 0 {
		return 0
	}
	return m.sum / time.Duration(m.count)
}

func (m *renderCostMeter) Utilization(fps int32) float64 {
	return renderUtilization(m.Average(), fps)
}

func (m *renderCostMeter) Reset() {
	*m = renderCostMeter{}
}

func renderUtilization(cost time.Duration, fps int32) float64 {
	if fps <= 0 || cost <= 0 {
		return 0
	}
	return cost.Seconds() * float64(fps)
}

// frameCostMeter keeps active render/presentation work separate from cadence.
// Scheduler waits occur between samples and never inflate the active cost.
// Presentation includes swap waiting: it blocks the main thread and may expose
// deferred GPU work. These wall times are not GPU execution-time measurements.
type frameCostMeter struct {
	renderCostMeter
	intervals     renderCostMeter
	lastPresented time.Time
}

func (m *frameCostMeter) AddFrame(render, presentation time.Duration, presented time.Time) {
	m.Add(render + presentation)
	if !m.lastPresented.IsZero() {
		m.intervals.Add(presented.Sub(m.lastPresented))
	}
	m.lastPresented = presented
}

func (m *frameCostMeter) Full() bool {
	return m.renderCostMeter.Full() && m.intervals.Full()
}

func (m *frameCostMeter) FullFor(samples int) bool {
	samples = min(max(samples, 1), renderCostWindow)
	return m.renderCostMeter.Count() >= samples && m.intervals.Count() >= samples-1
}

func (m *frameCostMeter) CalibrationFull(fps int32) bool {
	samples := calibrationRenderSamples
	if fps > 0 && m.renderCostMeter.Count() >= calibrationSlowRenderSamples &&
		(m.Utilization(fps) > 1 || m.Cadence(fps) > cadenceTolerance) {
		samples = calibrationSlowRenderSamples
	}
	return m.FullFor(samples)
}

func (m *frameCostMeter) CalibrationSeverelyOverBudget(fps int32) bool {
	if fps <= 0 {
		return false
	}
	threshold := max(presetProbeFastFrame, 2*time.Second/time.Duration(fps))
	return m.renderCostMeter.LastThreeMedian() >= threshold
}

// Cadence is the mean presentation interval divided by the scheduled period.
// One means the requested FPS is sustained; larger values indicate missed cadence.
func (m *frameCostMeter) Cadence(fps int32) float64 {
	return m.intervals.Utilization(fps)
}

func (m *frameCostMeter) Reset() {
	*m = frameCostMeter{}
}

// ExcludeServiceDelay breaks cadence history after expensive non-render work.
// The caller measures only work before entering render/presentation.
func (m *frameCostMeter) ExcludeServiceDelay(elapsed time.Duration) bool {
	if elapsed <= mainFramePeriod {
		return false
	}
	m.Reset()
	return true
}
