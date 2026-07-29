package soloud

import (
	"os"
	"testing"
)

func TestNewGmeWithUpstreamNSF(t *testing.T) {
	data, err := os.ReadFile("../../lib/game-music-emu/test.nsf")
	if err != nil {
		t.Fatal(err)
	}

	gme, err := NewGme(data)
	if err != nil {
		t.Fatal(err)
	}
	defer gme.Destroy()

	if gme.GetTrackCount() <= 0 {
		t.Fatalf("track count = %d, want positive", gme.GetTrackCount())
	}
	if gme.GetLength() <= 0 {
		t.Fatalf("length = %f, want positive", gme.GetLength())
	}
	if gme.GetSampleRate() != 44100 {
		t.Fatalf("sample rate = %d, want 44100", gme.GetSampleRate())
	}
}
