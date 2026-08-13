package ui

import (
	"testing"
	"time"
)

func TestMarqueeOffset(t *testing.T) {
	tests := []struct {
		name      string
		offset    float32
		contentW  int
		viewportW int
		want      float32
	}{
		{name: "content fits", offset: 100, contentW: 100, viewportW: 100, want: 0},
		{name: "scrolling", offset: 20, contentW: 200, viewportW: 100, want: 20},
		{name: "pause at end", offset: 120, contentW: 200, viewportW: 100, want: 100},
		{name: "cycle wraps", offset: 170, contentW: 200, viewportW: 100, want: 10},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := marqueeOffset(test.offset, test.contentW, test.viewportW); got != test.want {
				t.Fatalf("marqueeOffset(%v, %d, %d) = %v, want %v", test.offset, test.contentW, test.viewportW, got, test.want)
			}
		})
	}
}

func TestMarqueeWaitsBeforeScrolling(t *testing.T) {
	start := time.Unix(0, 0)
	m := marqueeState{tex: 1, texW: 200}
	o := &Overlay{}

	o.updateMarqueeCol(&m, start)
	o.updateMarqueeCol(&m, start.Add(marqueeDelay-time.Millisecond))
	if m.offset != 0 {
		t.Fatalf("offset before delay = %v, want 0", m.offset)
	}

	o.updateMarqueeCol(&m, start.Add(marqueeDelay+time.Second))
	if m.offset != marqueeSpeed {
		t.Fatalf("offset after one second = %v, want %v", m.offset, marqueeSpeed)
	}
}
