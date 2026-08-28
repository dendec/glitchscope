package app

import "time"

type fpsMeter struct {
	values [fpsWindow]float64
	count  int
	next   int
	sum    float64
}

func (m *fpsMeter) AddDuration(duration time.Duration) {
	if duration <= 0 {
		return
	}
	m.add(1 / duration.Seconds())
}

func (m *fpsMeter) add(fps float64) {
	if m.count < len(m.values) {
		m.count++
	} else {
		m.sum -= m.values[m.next]
	}
	m.values[m.next] = fps
	m.sum += fps
	m.next = (m.next + 1) % len(m.values)
}

func (m *fpsMeter) Average() float64 {
	if m.count == 0 {
		return 0
	}
	return m.sum / float64(m.count)
}

func (m *fpsMeter) Full() bool {
	return m.count == len(m.values)
}

func (m *fpsMeter) Reset() {
	*m = fpsMeter{}
}
