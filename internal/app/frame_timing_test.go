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
		period, fps := frameTiming(config.FrameRateMax, hz)
		if fps != want || period != time.Second/time.Duration(want) {
			t.Fatalf("refresh %d: period=%v fps=%d", hz, period, fps)
		}
		for _, rate := range []config.FrameRate{config.FrameRate30, config.FrameRate25, config.FrameRate20} {
			period, fps = frameTiming(rate, hz)
			if period != mainFramePeriod || fps != rate.Target(hz) {
				t.Fatalf("%v changed timing at %d Hz", rate, hz)
			}
		}
	}
	if period, fps := frameTiming(config.FrameRate60, 50); period != time.Second/50 || fps != 50 {
		t.Fatalf("fixed cap above refresh was not bounded: period=%v fps=%d", period, fps)
	}
}

func TestIndependentFrameRateTiming(t *testing.T) {
	for _, rate := range config.AllFrameRates() {
		period, fps := frameTiming(rate, 120)
		wantFPS := rate.Target(120)
		wantPeriod := mainFramePeriod
		if rate.IsMax() {
			wantPeriod = time.Second / 120
		}
		if period != wantPeriod || fps != wantFPS {
			t.Fatalf("rate %v: period=%v fps=%d, want %v/%d", rate, period, fps, wantPeriod, wantFPS)
		}
	}
}

func TestMaxPresentsEveryFrame(t *testing.T) {
	now := time.Now()
	if !presentationDue(config.FrameRateMax, now, now.Add(time.Second), false, false, false) {
		t.Fatal("Max skipped presentation")
	}
}
