package app

import (
	"testing"
	"time"

	"github.com/dendec/glitchscope/internal/config"
)

func TestFrameTiming(t *testing.T) {
	for _, hz := range []int32{0, -1, 50, 60, 120, 144, 240} {
		want := hz
		if want <= 0 {
			want = 60
		}
		period, fps := frameTiming(config.PerfModeUltra, hz)
		if fps != want || period != time.Second/time.Duration(want) {
			t.Fatalf("refresh %d: period=%v fps=%d", hz, period, fps)
		}
		for _, mode := range []config.PerformanceMode{config.PerfModePerformance, config.PerfModeBalanced, config.PerfModeEco} {
			period, fps = frameTiming(mode, hz)
			if period != mainFramePeriod || fps != mode.Params().VisualizerFPS {
				t.Fatalf("%v changed timing at %d Hz", mode, hz)
			}
		}
	}
}

func TestUltraPresentsEveryFrame(t *testing.T) {
	now := time.Now()
	if !presentationDue(config.PerfModeUltra, now, now.Add(time.Second), false, false, false) {
		t.Fatal("Ultra skipped presentation")
	}
}
