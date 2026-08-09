// Package mic captures the default microphone and exposes it as mono float32
// samples for the visualizer. Two backends, tried in order:
//
//  1. SDL2 audio capture (go-sdl2, already linked) — works on clean
//     ALSA/PulseAudio setups and PortMaster.
//  2. Raw ALSA "hw:" device via cgo — bypasses the PulseAudio compat layer
//     that PipeWire setups can leave broken (SDL/Pulse then deliver zero
//     bytes even though the hardware works).
//
// Open probes each candidate and keeps the first that actually delivers
// samples, so a silent-but-working mic still counts (silence is data).
package mic

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// Capture is an open, working capture device (SDL or ALSA backend).
type Capture struct {
	sdl      *sdlCap
	alsa     *alsaCap
	isF32    bool
	channels int
	rate     int
}

// InputDevices returns SDL capture devices available to the current audio
// driver. SDL audio must have been initialized before calling it.
func InputDevices() []string {
	return inputDevices()
}

// Open opens a working capture device. Blocks up to ~1s probing backends.
func Open() (*Capture, error) {
	if s, err := openSDL(""); err == nil {
		if bytes, _, _ := s.probe(600 * time.Millisecond); bytes > 0 {
			return &Capture{sdl: s, isF32: s.isF32, channels: s.channels, rate: s.rate}, nil
		}
		s.Close()
	}
	if a, err := openALSA(); err == nil {
		if bytes, f32, ch, rate := a.probe(600 * time.Millisecond); bytes > 0 {
			return &Capture{alsa: a, isF32: f32, channels: ch, rate: rate}, nil
		}
		a.Close()
	}
	return nil, fmt.Errorf("no working capture device")
}

// OpenDevice opens and probes a selected SDL capture device. When SDL's ALSA
// backend cannot configure the selected device, raw ALSA keeps capture working
// on platforms whose SDL device name cannot be reopened by SDL.
func OpenDevice(name string) (*Capture, error) {
	if name == "" {
		return nil, fmt.Errorf("empty capture device name")
	}
	s, err := openSDL(name)
	if err == nil {
		if bytes, _, _ := s.probe(600 * time.Millisecond); bytes > 0 {
			return &Capture{sdl: s, isF32: s.isF32, channels: s.channels, rate: s.rate}, nil
		}
		s.Close()
	}
	if a, alsaErr := openALSA(); alsaErr == nil {
		if bytes, f32, ch, rate := a.probe(600 * time.Millisecond); bytes > 0 {
			return &Capture{alsa: a, isF32: f32, channels: ch, rate: rate}, nil
		}
		a.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("open capture device %q: %w", name, err)
	}
	return nil, fmt.Errorf("capture device %q produced no audio data", name)
}

// Read dequeues pending samples as mono float32 in [-1, 1].
// Returns nil when no new audio is queued.
func (c *Capture) Read() []float32 {
	var raw []byte
	if c.sdl != nil {
		raw = c.sdl.read()
	} else {
		raw = c.alsa.read()
	}
	if len(raw) == 0 {
		return nil
	}
	return ToMono(raw, c.isF32, c.channels)
}

// Close stops and closes the capture device.
func (c *Capture) Close() {
	if c.sdl != nil {
		c.sdl.Close()
		c.sdl = nil
	}
	if c.alsa != nil {
		c.alsa.Close()
		c.alsa = nil
	}
}

// Rate returns the negotiated sample rate in Hz.
func (c *Capture) Rate() int { return c.rate }

// Channels returns the negotiated channel count.
func (c *Capture) Channels() int { return c.channels }

// Backend reports which capture backend is in use ("sdl" or "alsa").
func (c *Capture) Backend() string {
	if c.sdl != nil {
		return "sdl"
	}
	return "alsa"
}

// ToMono converts raw interleaved S16 or F32 samples to mono float32.
// A pure helper so the conversion is testable without a capture device.
func ToMono(data []byte, f32 bool, channels int) []float32 {
	width := 2
	if f32 {
		width = 4
	}
	if channels <= 0 || len(data) < width {
		return nil
	}
	frames := len(data) / (width * channels)
	out := make([]float32, frames)
	for i := 0; i < frames; i++ {
		var sum float32
		for ch := 0; ch < channels; ch++ {
			off := (i*channels + ch) * width
			if f32 {
				sum += math.Float32frombits(binary.NativeEndian.Uint32(data[off:]))
			} else {
				sum += float32(int16(binary.NativeEndian.Uint16(data[off:]))) / 32768
			}
		}
		out[i] = sum / float32(channels)
	}
	return out
}
