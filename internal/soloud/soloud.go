// Package soloud provides a Go wrapper around the SoLoud audio engine C API.
package soloud

/*
#cgo CFLAGS: -I ../../lib/soloud/include
#cgo CXXFLAGS: -std=c++11 -DWITH_SDL2_STATIC -I ../../lib/soloud/include
#include <stdlib.h>
#include "soloud_c.h"

// Forward declaration for loadRawWave bridge.
int Wav_loadRawF32(void * aWav, float * aMem, unsigned int aLength, float aSamplerate, unsigned int aChannels);
*/
import "C"
import (
	"fmt"
	"unsafe"
)

// Soloud wraps the SoLoud audio engine.
type Soloud struct {
	p *C.Soloud
}

// Wav wraps a SoLoud Wav audio source (MP3/WAV/FLAC/Ogg).
type Wav struct {
	p *C.Wav
}

// New creates a SoLoud engine instance.
func New() *Soloud {
	return &Soloud{p: C.Soloud_create()}
}

// Init initializes the SoLoud engine with SDL2 backend and visualization enabled.
func (s *Soloud) Init() error {
	flags := C.SOLOUD_CLIP_ROUNDOFF | C.SOLOUD_ENABLE_VISUALIZATION
	r := int(C.Soloud_initEx(s.p, C.uint(flags), C.SOLOUD_AUTO, C.SOLOUD_AUTO, C.SOLOUD_AUTO, 2))
	if r != 0 {
		return fmt.Errorf("soloud init: %d", r)
	}
	return nil
}

// Deinit shuts down the audio backend.
func (s *Soloud) Deinit() {
	C.Soloud_deinit(s.p)
}

// Destroy frees the engine instance.
func (s *Soloud) Destroy() {
	C.Soloud_destroy(s.p)
}

// LoadWav loads an audio file (MP3, WAV, FLAC, Ogg) into a Wav source.
func LoadWav(path string) (*Wav, error) {
	p := C.Wav_create()
	cpath := C.CString(path)
	r := int(C.Wav_load(p, cpath))
	C.free(unsafe.Pointer(cpath))
	if r != 0 {
		C.Wav_destroy(p)
		return nil, fmt.Errorf("wav load: %d", r)
	}
	return &Wav{p: p}, nil
}

// NewWavFromF32 creates a Wav from raw interleaved float32 PCM data.
func NewWavFromF32(data []float32, sampleRate float32, channels uint) (*Wav, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("wav from f32: empty data")
	}
	if channels == 0 || len(data)%int(channels) != 0 {
		return nil, fmt.Errorf("wav from f32: invalid channel layout")
	}
	data = interleavedToPlanar(data, int(channels))
	p := C.Wav_create()
	r := int(C.Wav_loadRawF32(unsafe.Pointer(p),
		(*C.float)(unsafe.Pointer(&data[0])),
		C.uint(len(data)),
		C.float(sampleRate),
		C.uint(channels),
	))
	if r != 0 {
		C.Wav_destroy(p)
		return nil, fmt.Errorf("wav load raw: %d", r)
	}
	return &Wav{p: p}, nil
}

// interleavedToPlanar converts frames such as LRLR into SoLoud's LLRR layout.
func interleavedToPlanar(data []float32, channels int) []float32 {
	frames := len(data) / channels
	planar := make([]float32, len(data))
	for frame := 0; frame < frames; frame++ {
		for channel := 0; channel < channels; channel++ {
			planar[channel*frames+frame] = data[frame*channels+channel]
		}
	}
	return planar
}

// Destroy frees the Wav resource.
func (w *Wav) Destroy() {
	if w.p != nil {
		C.Wav_destroy(w.p)
		w.p = nil
	}
}

// Play starts playing a Wav source. Returns the voice handle.
func (s *Soloud) Play(w *Wav) uint {
	return uint(C.Soloud_play(s.p, (*C.AudioSource)(unsafe.Pointer(w.p))))
}

// GetWave returns the current waveform data (256 float32 samples).
// Visualization must be enabled with SOLOUD_ENABLE_VISUALIZATION.
// The returned slice references SoLoud's internal buffer — copy if persisting.
func (s *Soloud) GetWave() []float32 {
	p := C.Soloud_getWave(s.p)
	if p == nil {
		return nil
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(p)), 256)
}

// CalcFFT returns the current FFT data (256 float32 bins).
// Visualization must be enabled with SOLOUD_ENABLE_VISUALIZATION.
// The returned slice references SoLoud's internal buffer — copy if persisting.
func (s *Soloud) CalcFFT() []float32 {
	p := C.Soloud_calcFFT(s.p)
	if p == nil {
		return nil
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(p)), 256)
}

// IsValidVoiceHandle returns true if the voice handle is still active.
func (s *Soloud) IsValidVoiceHandle(voice uint) bool {
	return C.Soloud_isValidVoiceHandle(s.p, C.uint(voice)) != 0
}

// SetPause pauses/resumes a voice.
func (s *Soloud) SetPause(voice uint, pause bool) {
	v := 0
	if pause {
		v = 1
	}
	C.Soloud_setPause(s.p, C.uint(voice), C.int(v))
}

// GetPause returns whether a voice is paused.
func (s *Soloud) GetPause(voice uint) bool {
	return C.Soloud_getPause(s.p, C.uint(voice)) != 0
}

// GetStreamTime returns the current playback position in seconds for a voice.
func (s *Soloud) GetStreamTime(voice uint) float64 {
	return float64(C.Soloud_getStreamTime(s.p, C.uint(voice)))
}

// GetLength returns the total length in seconds of a Wav.
func (w *Wav) GetLength() float64 {
	return float64(C.Wav_getLength(w.p))
}

// GetSamplerate returns the sample rate of a voice.
func (s *Soloud) GetSamplerate(voice uint) float32 {
	return float32(C.Soloud_getSamplerate(s.p, C.uint(voice)))
}

// GetInfo returns an info value for a voice (e.g. channels = 2).
func (s *Soloud) GetInfo(voice uint, infoKey uint) float32 {
	return float32(C.Soloud_getInfo(s.p, C.uint(voice), C.uint(infoKey)))
}

// StopAll stops all playing voices.
func (s *Soloud) StopAll() {
	C.Soloud_stopAll(s.p)
}


