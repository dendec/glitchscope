// Package soloud provides a Go wrapper around the SoLoud audio engine C API.
package soloud

/*
#cgo CFLAGS: -I ../../lib/soloud/include
#cgo CXXFLAGS: -std=c++11 -DWITH_SDL2_STATIC -I ../../lib/soloud/include
#include <stdlib.h>
#include "soloud_c.h"
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

// StopAll stops all playing voices.
func (s *Soloud) StopAll() {
	C.Soloud_stopAll(s.p)
}
