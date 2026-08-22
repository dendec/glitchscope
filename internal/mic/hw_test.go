package mic

import (
	"os"
	"testing"
	"time"

	"github.com/veandco/go-sdl2/sdl"
)

// TestHwOpen probes a real capture device end to end. Gated behind
// GLITCHSCOPE_MIC_HW_TEST=1 — CI containers have no audio hardware.
func TestHwOpen(t *testing.T) {
	if os.Getenv("GLITCHSCOPE_MIC_HW_TEST") == "" {
		t.Skip("set GLITCHSCOPE_MIC_HW_TEST=1 to test real capture hardware")
	}
	if err := sdl.Init(sdl.INIT_AUDIO); err != nil {
		t.Fatalf("sdl init: %v", err)
	}
	defer sdl.Quit()
	devices := InputDevices()
	if len(devices) == 0 {
		t.Fatal("no SDL capture devices")
	}
	device := devices[0]
	c, err := OpenDevice(device)
	if err != nil {
		t.Fatalf("OpenDevice(%q): %v", device, err)
	}
	defer c.Close()
	deadline := time.Now().Add(2 * time.Second)
	samples := 0
	for time.Now().Before(deadline) {
		if w := c.Read(); w != nil {
			samples += len(w)
			if samples > 1000 {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Logf("device %q backend %s rate=%d ch=%d samples=%d", device, c.Backend(), c.Rate(), c.Channels(), samples)
	if samples == 0 {
		t.Fatal("no samples captured")
	}
}
