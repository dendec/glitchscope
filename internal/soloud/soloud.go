// Package soloud provides a Go wrapper around the SoLoud audio engine C API.
package soloud

/*
#cgo CFLAGS: -I ../../lib/soloud/include
#cgo CXXFLAGS: -std=c++11 -DWITH_SDL2_STATIC -I ../../lib/soloud/include
#include <stdlib.h>
#include "soloud_c.h"
unsigned int Wav_getChannels(Wav * aClassPtr);
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

// Xmp wraps a SoLoud Xmp audio source for streaming tracker music.
type Xmp struct {
	p *C.Xmp
}

// Openmpt wraps a SoLoud Openmpt audio source (fallback for unsupported formats).
type Openmpt struct {
	p *C.Openmpt
}

// New creates a SoLoud engine instance.
func New() *Soloud {
	return &Soloud{p: C.Soloud_create()}
}

// Init initializes the SoLoud engine with SDL2 backend and visualization.
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

// LoadWav loads an audio file into a Wav source.
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

// NewXmp creates an Xmp source from tracker file data.
// aCopy=1 / aTakeOwnership=0: SoLoud copies data into its own buffer.
func NewXmp(data []byte) (*Xmp, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("xmp: empty data")
	}
	p := C.Xmp_create()
	r := int(C.Xmp_loadMemEx(
		p,
		(*C.uchar)(unsafe.Pointer(&data[0])),
		C.uint(len(data)),
		1, // aCopy = true
		0, // aTakeOwnership = false
	))
	if r != 0 {
		C.Xmp_destroy(p)
		return nil, fmt.Errorf("xmp load: %d", r)
	}
	return &Xmp{p: p}, nil
}

// NewOpenmpt creates an Openmpt source from tracker file data.
func NewOpenmpt(data []byte) (*Openmpt, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("openmpt: empty data")
	}
	p := C.Openmpt_create()
	r := int(C.Openmpt_loadMemEx(
		p,
		(*C.uchar)(unsafe.Pointer(&data[0])),
		C.uint(len(data)),
		1, // aCopy = true
		0, // aTakeOwnership = false
	))
	if r != 0 {
		C.Openmpt_destroy(p)
		return nil, fmt.Errorf("openmpt load: %d", r)
	}
	return &Openmpt{p: p}, nil
}

// Destroy frees the Wav resource.
func (w *Wav) Destroy() {
	if w.p != nil {
		C.Wav_destroy(w.p)
		w.p = nil
	}
}

// Destroy frees the Xmp resource.
func (x *Xmp) Destroy() {
	if x.p != nil {
		C.Xmp_destroy(x.p)
		x.p = nil
	}
}

// Destroy frees the Openmpt resource.
func (o *Openmpt) Destroy() {
	if o.p != nil {
		C.Openmpt_destroy(o.p)
		o.p = nil
	}
}

// Play starts playing a Wav source. Returns the voice handle.
func (s *Soloud) Play(w *Wav) uint {
	return uint(C.Soloud_play(s.p, (*C.AudioSource)(unsafe.Pointer(w.p))))
}

// PlayXmp starts playing an Xmp source.
func (s *Soloud) PlayXmp(x *Xmp) uint {
	return uint(C.Soloud_play(s.p, (*C.AudioSource)(unsafe.Pointer(x.p))))
}

// PlayOpenmpt starts playing an Openmpt source.
func (s *Soloud) PlayOpenmpt(o *Openmpt) uint {
	return uint(C.Soloud_play(s.p, (*C.AudioSource)(unsafe.Pointer(o.p))))
}

// GetWave returns the current waveform data (256 float32 samples).
// Visualization must be enabled. The slice references SoLoud's internal buffer.
func (s *Soloud) GetWave() []float32 {
	p := C.Soloud_getWave(s.p)
	if p == nil {
		return nil
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(p)), 256)
}

// CalcFFT returns the current FFT data (256 float32 bins).
// Visualization must be enabled. The slice references SoLoud's internal buffer.
func (s *Soloud) CalcFFT() []float32 {
	p := C.Soloud_calcFFT(s.p)
	if p == nil {
		return nil
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(p)), 256)
}

func (s *Soloud) IsValidVoiceHandle(voice uint) bool {
	return C.Soloud_isValidVoiceHandle(s.p, C.uint(voice)) != 0
}

func (s *Soloud) SetPause(voice uint, pause bool) {
	v := 0
	if pause {
		v = 1
	}
	C.Soloud_setPause(s.p, C.uint(voice), C.int(v))
}

func (s *Soloud) GetPause(voice uint) bool {
	return C.Soloud_getPause(s.p, C.uint(voice)) != 0
}

func (s *Soloud) GetStreamTime(voice uint) float64 {
	return float64(C.Soloud_getStreamTime(s.p, C.uint(voice)))
}

// GetLength returns the total length in seconds of a Wav.
func (w *Wav) GetLength() float64 {
	return float64(C.Wav_getLength(w.p))
}

// GetChannels returns the number of decoded audio channels.
func (w *Wav) GetChannels() int {
	return int(C.Wav_getChannels(w.p))
}

func (s *Soloud) GetSamplerate(voice uint) float32 {
	return float32(C.Soloud_getSamplerate(s.p, C.uint(voice)))
}

// GetInfo returns an info value for a voice.
func (s *Soloud) GetInfo(voice uint, infoKey uint) float32 {
	return float32(C.Soloud_getInfo(s.p, C.uint(voice), C.uint(infoKey)))
}

// StopAll stops all playing voices.
func (s *Soloud) StopAll() {
	C.Soloud_stopAll(s.p)
}
