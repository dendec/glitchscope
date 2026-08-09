package mic

import (
	"time"

	"github.com/veandco/go-sdl2/sdl"
)

// sdlCap wraps SDL2 audio capture in queue mode.
type sdlCap struct {
	dev      sdl.AudioDeviceID
	isF32    bool
	channels int
	rate     int
	buf      []byte
}

func inputDevices() []string {
	count := sdl.GetNumAudioDevices(true)
	if count <= 0 {
		return nil
	}
	devices := make([]string, 0, count)
	for i := 0; i < count; i++ {
		if name := sdl.GetAudioDeviceName(i, true); name != "" {
			devices = append(devices, name)
		}
	}
	return devices
}

func openSDL(name string) (*sdlCap, error) {
	desired := &sdl.AudioSpec{
		Freq:     48000,
		Format:   sdl.AUDIO_F32SYS,
		Channels: 1,
		Samples:  512,
	}
	obtained := &sdl.AudioSpec{}
	dev, err := sdl.OpenAudioDevice(name, true, desired, obtained,
		sdl.AUDIO_ALLOW_FREQUENCY_CHANGE|sdl.AUDIO_ALLOW_FORMAT_CHANGE|sdl.AUDIO_ALLOW_CHANNELS_CHANGE)
	if err != nil {
		return nil, err
	}
	sdl.PauseAudioDevice(dev, false)
	return &sdlCap{
		dev:      dev,
		isF32:    obtained.Format == sdl.AUDIO_F32SYS,
		channels: int(obtained.Channels),
		rate:     int(obtained.Freq),
		buf:      make([]byte, 8192),
	}, nil
}

// probe reads until d elapses or data arrives; returns bytes collected.
func (s *sdlCap) probe(d time.Duration) (int, bool, int) {
	return s.readLoop(d), s.isF32, s.channels
}

func (s *sdlCap) readLoop(d time.Duration) int {
	total := 0
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if n, err := sdl.DequeueAudio(s.dev, s.buf); err == nil && n > 0 {
			total += n
			if total > 0 {
				return total
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return total
}

func (s *sdlCap) read() []byte {
	n, err := sdl.DequeueAudio(s.dev, s.buf)
	if err != nil || n <= 0 {
		return nil
	}
	return s.buf[:n]
}

func (s *sdlCap) Close() {
	if s.dev != 0 {
		sdl.CloseAudioDevice(s.dev)
		s.dev = 0
	}
}
