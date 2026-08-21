// Package soloud provides a Go wrapper around the SoLoud audio engine C API.
package soloud

/*
#cgo CFLAGS: -I ../../lib/soloud/include
#cgo CXXFLAGS: -std=c++11 -DWITH_SDL2_STATIC -I ../../lib/soloud/include -I ../../lib/game-music-emu/gme
#include "bridge_sid.h"
#include <stdlib.h>
#include "soloud_c.h"
void *Ffmpeg_create(void);
void Ffmpeg_destroy(void *source);
int Ffmpeg_loadMem(void *source, const unsigned char *data, unsigned int length);
int Ffmpeg_loadFile(void *source, const char *path);
unsigned int Ffmpeg_getLengthMs(void *source);
unsigned int Ffmpeg_getChannels(void *source);
unsigned int Ffmpeg_getSampleRate(void *source);
unsigned int Wav_getChannels(Wav * aClassPtr);
void *Gme_create(void);
void Gme_destroy(void *source);
int Gme_loadMem(void *source, const unsigned char *data, unsigned int length);
unsigned int Gme_getLengthMs(void *source);
unsigned int Gme_getTrackCount(void *source);
const char *Gme_getTitle(void *source);
const char *Gme_getAuthor(void *source);
unsigned int Gme_getSampleRate(void *source);
void *Hvl_create(void);
void Hvl_destroy(void *source);
int Hvl_loadMem(void *source, const unsigned char *data, unsigned int length);
unsigned int Hvl_getLengthMs(void *source);
unsigned int Hvl_getTrackCount(void *source);
unsigned int Hvl_getSampleRate(void *source);
void *SidSource_create(void);
void SidSource_destroy(void *source);
int SidSource_loadMem(void *source, const unsigned char *data, unsigned int length);
unsigned int SidSource_getLengthMs(void *source);
unsigned int SidSource_getTrackCount(void *source);
unsigned int SidSource_getSampleRate(void *source);
unsigned int Sid_render(const unsigned char *data, unsigned int length, float *out, unsigned int maxFrames);
void *Ayumi_create(void);
void Ayumi_destroy(void *source);
int Ayumi_loadMem(void *source, const unsigned char *data, unsigned int length);
unsigned int Ayumi_getLengthMs(void *source);
unsigned int Ayumi_getSampleRate(void *source);
void *Pt3_create(void);
void Pt3_destroy(void *source);
int Pt3_loadMem(void *source, const unsigned char *data, unsigned int length);
unsigned int Pt3_getLengthMs(void *source);
unsigned int Pt3_getSampleRate(void *source);
void *Ym_create(void);
void Ym_destroy(void *source);
int Ym_loadMem(void *source, const unsigned char *data, unsigned int length);
unsigned int Ym_getLengthMs(void *source);
unsigned int Ym_getSampleRate(void *source);
unsigned int Ym_render(const unsigned char *data, unsigned int length, float *outL, float *outR, unsigned int maxFrames);
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

// Gme wraps a Game Music Emu SoLoud source.
type Gme struct {
	p unsafe.Pointer
}

// Hvl wraps the AHX/HVL SoLoud source.
type Hvl struct {
	p unsafe.Pointer
}

// Sid wraps a cRSID SID source.
type Sid struct {
	p unsafe.Pointer
}

// Ayumi wraps the AY/YM VTX source adapter.
type Ayumi struct {
	p unsafe.Pointer
}

// Pt3 wraps the PT3 player source.
type Pt3 struct {
	p unsafe.Pointer
}

// Ym wraps the libstsound YM/LHARC source adapter.
type Ym struct {
	p unsafe.Pointer
}

// Ffmpeg wraps the FFmpeg audio source adapter.
type Ffmpeg struct {
	p unsafe.Pointer
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

// Destroy frees the engine instance.
func (s *Soloud) Destroy() {
	C.Soloud_destroy(s.p)
}

// AudioSource is the common interface for all SoLoud audio source types.
type AudioSource interface {
	raw() unsafe.Pointer
	Destroy()
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

// NewWavFromSamples builds a memory-backed Wav audio source from decoded
// planar float32 PCM (channel c spans samples[c*n .. (c+1)*n)). SoLoud copies
// the samples, so callers may free their buffer after this returns. This is
// how pre-rendered tracker/chip tracks are played back so that seeking is
// exact and bidirectional regardless of the native decoder's seek support.
func NewWavFromSamples(planar []float32, sampleRate float64, channels int) (*Wav, error) {
	if len(planar) == 0 || sampleRate <= 0 || channels < 1 {
		return nil, fmt.Errorf("wav from samples: bad params (len=%d sr=%g ch=%d)", len(planar), sampleRate, channels)
	}
	if channels*int(len(planar)/channels) != len(planar) {
		return nil, fmt.Errorf("wav from samples: %d samples not divisible by %d channels", len(planar), channels)
	}
	p := C.Wav_create()
	r := int(C.Wav_loadRawWaveEx(
		p,
		(*C.float)(unsafe.Pointer(&planar[0])),
		C.uint(len(planar)),
		C.float(sampleRate),
		C.uint(channels),
		1, // aCopy = true
		0, // aTakeOwnership = false
	))
	if r != 0 {
		C.Wav_destroy(p)
		return nil, fmt.Errorf("wav from samples: loadRawWaveEx: %d", r)
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

// NewGme creates a Game Music Emu source from game music data.
func NewGme(data []byte) (*Gme, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("gme: empty data")
	}
	p := C.Gme_create()
	if p == nil {
		return nil, fmt.Errorf("gme: create failed")
	}
	r := int(C.Gme_loadMem(p, (*C.uchar)(unsafe.Pointer(&data[0])), C.uint(len(data))))
	if r != 0 {
		C.Gme_destroy(p)
		return nil, fmt.Errorf("gme load: %d", r)
	}
	return &Gme{p: p}, nil
}

// NewHvl creates an AHX/HVL source from module data.
func NewHvl(data []byte) (*Hvl, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("hvl: empty data")
	}
	p := C.Hvl_create()
	if p == nil {
		return nil, fmt.Errorf("hvl: create failed")
	}
	if C.Hvl_loadMem(p, (*C.uchar)(unsafe.Pointer(&data[0])), C.uint(len(data))) != 0 {
		C.Hvl_destroy(p)
		return nil, fmt.Errorf("hvl: load failed")
	}
	return &Hvl{p: p}, nil
}

// NewSid creates a cRSID source from SID or RSID data.
func NewSid(data []byte) (*Sid, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("sid: empty data")
	}
	p := C.SidSource_create()
	if p == nil {
		return nil, fmt.Errorf("sid: create failed")
	}
	if C.SidSource_loadMem(p, (*C.uchar)(unsafe.Pointer(&data[0])), C.uint(len(data))) != 0 {
		C.SidSource_destroy(p)
		return nil, fmt.Errorf("sid: load failed")
	}
	return &Sid{p: p}, nil
}

// NewAyumi creates an AY/YM source from a raw-register VTX file.
func NewAyumi(data []byte) (*Ayumi, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("ayumi: empty data")
	}
	p := C.Ayumi_create()
	if p == nil {
		return nil, fmt.Errorf("ayumi: create failed")
	}
	r := int(C.Ayumi_loadMem(p, (*C.uchar)(unsafe.Pointer(&data[0])), C.uint(len(data))))
	if r != 0 {
		C.Ayumi_destroy(p)
		return nil, fmt.Errorf("ayumi: load failed")
	}
	return &Ayumi{p: p}, nil
}

// NewPt3 creates a PT3 source from module data.
func NewPt3(data []byte) (*Pt3, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("pt3: empty data")
	}
	p := C.Pt3_create()
	if p == nil {
		return nil, fmt.Errorf("pt3: create failed")
	}
	if C.Pt3_loadMem(p, (*C.uchar)(unsafe.Pointer(&data[0])), C.uint(len(data))) != 0 {
		C.Pt3_destroy(p)
		return nil, fmt.Errorf("pt3: load failed")
	}
	return &Pt3{p: p}, nil
}

// NewYm creates a YM source from YM or LHA/LHARC-packed data.
func NewYm(data []byte) (*Ym, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("ym: empty data")
	}
	p := C.Ym_create()
	if p == nil {
		return nil, fmt.Errorf("ym: create failed")
	}
	if C.Ym_loadMem(p, (*C.uchar)(unsafe.Pointer(&data[0])), C.uint(len(data))) != 0 {
		C.Ym_destroy(p)
		return nil, fmt.Errorf("ym: load failed")
	}
	return &Ym{p: p}, nil
}

// NewFfmpeg creates an FFmpeg source from in-memory audio data.
func NewFfmpeg(data []byte) (*Ffmpeg, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("ffmpeg: empty data")
	}
	p := C.Ffmpeg_create()
	if p == nil {
		return nil, fmt.Errorf("ffmpeg: create failed")
	}
	if C.Ffmpeg_loadMem(p, (*C.uchar)(unsafe.Pointer(&data[0])), C.uint(len(data))) != 0 {
		C.Ffmpeg_destroy(p)
		return nil, fmt.Errorf("ffmpeg: load failed")
	}
	return &Ffmpeg{p: p}, nil
}

// NewFfmpegFile creates an FFmpeg source that decodes an audio file by
// streaming it from disk, so the whole file is never loaded into memory. This
// matters on low-memory handhelds for large FLAC/MP3/WAV files.
func NewFfmpegFile(path string) (*Ffmpeg, error) {
	if path == "" {
		return nil, fmt.Errorf("ffmpeg: empty path")
	}
	p := C.Ffmpeg_create()
	if p == nil {
		return nil, fmt.Errorf("ffmpeg: create failed")
	}
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	if C.Ffmpeg_loadFile(p, cpath) != 0 {
		C.Ffmpeg_destroy(p)
		return nil, fmt.Errorf("ffmpeg: load file %q failed", path)
	}
	return &Ffmpeg{p: p}, nil
}

// raw returns the underlying C pointer for generic playback.
func (w *Wav) raw() unsafe.Pointer { return unsafe.Pointer(w.p) }

// Destroy frees the Wav resource.
func (w *Wav) Destroy() {
	if w.p != nil {
		C.Wav_destroy(w.p)
		w.p = nil
	}
}

// raw returns the underlying C pointer for generic playback.
func (x *Xmp) raw() unsafe.Pointer { return unsafe.Pointer(x.p) }

// Destroy frees the Xmp resource.
func (x *Xmp) Destroy() {
	if x.p != nil {
		C.Xmp_destroy(x.p)
		x.p = nil
	}
}

// raw returns the underlying C pointer for generic playback.
func (o *Openmpt) raw() unsafe.Pointer { return unsafe.Pointer(o.p) }

// Destroy frees the Openmpt resource.
func (o *Openmpt) Destroy() {
	if o.p != nil {
		C.Openmpt_destroy(o.p)
		o.p = nil
	}
}

// raw returns the underlying C pointer for generic playback.
func (g *Gme) raw() unsafe.Pointer { return g.p }

// Destroy frees the GME resource.
func (g *Gme) Destroy() {
	if g.p != nil {
		C.Gme_destroy(g.p)
		g.p = nil
	}
}

// raw returns the underlying C pointer for generic playback.
func (h *Hvl) raw() unsafe.Pointer { return h.p }

// Destroy frees the AHX/HVL resource.
func (h *Hvl) Destroy() {
	if h.p != nil {
		C.Hvl_destroy(h.p)
		h.p = nil
	}
}

// raw returns the underlying C pointer for generic playback.
func (s *Sid) raw() unsafe.Pointer { return s.p }

// Destroy frees the SID resource.
func (s *Sid) Destroy() {
	if s.p != nil {
		C.SidSource_destroy(s.p)
		s.p = nil
	}
}

// raw returns the underlying C pointer for generic playback.
func (a *Ayumi) raw() unsafe.Pointer { return a.p }

// Destroy frees the Ayumi resource.
func (a *Ayumi) Destroy() {
	if a.p != nil {
		C.Ayumi_destroy(a.p)
		a.p = nil
	}
}

// raw returns the underlying C pointer for generic playback.
func (p *Pt3) raw() unsafe.Pointer { return p.p }

// Destroy frees the PT3 resource.
func (p *Pt3) Destroy() {
	if p.p != nil {
		C.Pt3_destroy(p.p)
		p.p = nil
	}
}

// raw returns the underlying C pointer for generic playback.
func (y *Ym) raw() unsafe.Pointer { return y.p }

// Destroy frees the YM resource.
func (y *Ym) Destroy() {
	if y.p != nil {
		C.Ym_destroy(y.p)
		y.p = nil
	}
}

// raw returns the underlying C pointer for generic playback.
func (f *Ffmpeg) raw() unsafe.Pointer { return f.p }

// Destroy frees the FFmpeg resource.
func (f *Ffmpeg) Destroy() {
	if f.p != nil {
		C.Ffmpeg_destroy(f.p)
		f.p = nil
	}
}

// PlaySource starts playing any AudioSource. Returns the voice handle.
func (s *Soloud) PlaySource(src AudioSource) uint {
	return uint(C.Soloud_play(s.p, (*C.AudioSource)(src.raw())))
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

// PlayGme starts playing a Game Music Emu source.
func (s *Soloud) PlayGme(g *Gme) uint {
	return uint(C.Soloud_play(s.p, (*C.AudioSource)(g.p)))
}

// PlaySid starts playing a SID source.
func (s *Soloud) PlaySid(sid *Sid) uint {
	return uint(C.Soloud_play(s.p, (*C.AudioSource)(sid.p)))
}

// PlayAyumi starts playing an AY/YM source.
func (s *Soloud) PlayAyumi(a *Ayumi) uint {
	return uint(C.Soloud_play(s.p, (*C.AudioSource)(a.p)))
}

// PlayPt3 starts playing a PT3 source.
func (s *Soloud) PlayPt3(p *Pt3) uint {
	return uint(C.Soloud_play(s.p, (*C.AudioSource)(p.p)))
}

// PlayYm starts playing a YM source.
func (s *Soloud) PlayYm(y *Ym) uint {
	return uint(C.Soloud_play(s.p, (*C.AudioSource)(y.p)))
}

// PlayHvl starts playing an AHX/HVL source.
func (s *Soloud) PlayHvl(h *Hvl) uint {
	return uint(C.Soloud_play(s.p, (*C.AudioSource)(h.p)))
}

// PlayFfmpeg starts playing an FFmpeg source.
func (s *Soloud) PlayFfmpeg(f *Ffmpeg) uint {
	return uint(C.Soloud_play(s.p, (*C.AudioSource)(f.p)))
}

// RenderYm decodes up to maxFrames frames of a YM/LHARC tune into interleaved
// float32 stereo. Returns (interleaved, channels, frames, err). Used to build a
// seekable pre-rendered Wav because libstsound's native YM seek is unreliable.
func RenderYm(data []byte, maxFrames int) ([]float32, int, int, error) {
	if len(data) == 0 || maxFrames <= 0 {
		return nil, 0, 0, nil
	}
	left := make([]float32, maxFrames)
	right := make([]float32, maxFrames)
	frames := int(C.Ym_render(
		(*C.uchar)(unsafe.Pointer(&data[0])),
		C.uint(len(data)),
		(*C.float)(unsafe.Pointer(&left[0])),
		(*C.float)(unsafe.Pointer(&right[0])),
		C.uint(maxFrames),
	))
	if frames <= 0 {
		return nil, 0, 0, fmt.Errorf("ym render: no frames decoded")
	}
	left = left[:frames]
	right = right[:frames]
	interleaved := make([]float32, frames*2)
	for i := 0; i < frames; i++ {
		interleaved[i*2] = left[i]
		interleaved[i*2+1] = right[i]
	}
	return interleaved, 2, frames, nil
}

// RenderSid decodes up to maxFrames frames of a SID tune into mono float32
// samples. Returns (mono, channels, frames, err). SID has no native seek at
// all, so the pre-rendered buffer is what makes seeking possible.
func RenderSid(data []byte, maxFrames int) ([]float32, int, int, error) {
	if len(data) == 0 || maxFrames <= 0 {
		return nil, 0, 0, nil
	}
	mono := make([]float32, maxFrames)
	frames := int(C.Sid_render(
		(*C.uchar)(unsafe.Pointer(&data[0])),
		C.uint(len(data)),
		(*C.float)(unsafe.Pointer(&mono[0])),
		C.uint(maxFrames),
	))
	if frames <= 0 {
		return nil, 0, 0, fmt.Errorf("sid render: no frames decoded")
	}
	return mono[:frames], 1, frames, nil
}

func (g *Gme) GetLength() float64 {
	return float64(C.Gme_getLengthMs(g.p)) / 1000
}

func (h *Hvl) GetLength() float64 { return float64(C.Hvl_getLengthMs(h.p)) / 1000 }
func (h *Hvl) GetTrackCount() int { return int(C.Hvl_getTrackCount(h.p)) }
func (h *Hvl) GetSampleRate() int { return int(C.Hvl_getSampleRate(h.p)) }

func (g *Gme) GetTrackCount() int { return int(C.Gme_getTrackCount(g.p)) }
func (g *Gme) GetSampleRate() int { return int(C.Gme_getSampleRate(g.p)) }

func (s *Sid) GetLength() float64 { return float64(C.SidSource_getLengthMs(s.p)) / 1000 }
func (s *Sid) GetTrackCount() int { return int(C.SidSource_getTrackCount(s.p)) }
func (s *Sid) GetSampleRate() int { return int(C.SidSource_getSampleRate(s.p)) }

func (a *Ayumi) GetLength() float64  { return float64(C.Ayumi_getLengthMs(a.p)) / 1000 }
func (a *Ayumi) GetSampleRate() int  { return int(C.Ayumi_getSampleRate(a.p)) }
func (p *Pt3) GetLength() float64    { return float64(C.Pt3_getLengthMs(p.p)) / 1000 }
func (p *Pt3) GetSampleRate() int    { return int(C.Pt3_getSampleRate(p.p)) }
func (y *Ym) GetLength() float64     { return float64(C.Ym_getLengthMs(y.p)) / 1000 }
func (y *Ym) GetSampleRate() int     { return int(C.Ym_getSampleRate(y.p)) }
func (f *Ffmpeg) GetLength() float64 { return float64(C.Ffmpeg_getLengthMs(f.p)) / 1000 }
func (f *Ffmpeg) GetChannels() int   { return int(C.Ffmpeg_getChannels(f.p)) }
func (f *Ffmpeg) GetSampleRate() int { return int(C.Ffmpeg_getSampleRate(f.p)) }

// GetWave returns the current waveform data (256 float32 samples).
// Visualization must be enabled. The slice references SoLoud's internal buffer.
func (s *Soloud) GetWave() []float32 {
	p := C.Soloud_getWave(s.p)
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

// Seek moves a voice to the requested position in seconds.
func (s *Soloud) Seek(voice uint, seconds float64) error {
	if C.Soloud_seek(s.p, C.uint(voice), C.double(seconds)) != 0 {
		return fmt.Errorf("seek voice %d to %.3f seconds failed", voice, seconds)
	}
	return nil
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

// StopAll stops all playing voices.
func (s *Soloud) StopAll() {
	C.Soloud_stopAll(s.p)
}

// GetVersion returns the SoLoud version.
func (s *Soloud) GetVersion() uint {
	return uint(C.Soloud_getVersion(s.p))
}

// GetBackendId returns the active audio backend identifier.
func (s *Soloud) GetBackendId() uint {
	return uint(C.Soloud_getBackendId(s.p))
}

// GetBackendString returns a human-readable audio backend name.
func (s *Soloud) GetBackendString() string {
	return C.GoString(C.Soloud_getBackendString(s.p))
}

// GetBackendChannels returns the number of channels the audio backend uses.
func (s *Soloud) GetBackendChannels() uint {
	return uint(C.Soloud_getBackendChannels(s.p))
}

// GetBackendSamplerate returns the audio backend sample rate in Hz.
func (s *Soloud) GetBackendSamplerate() uint {
	return uint(C.Soloud_getBackendSamplerate(s.p))
}

// GetBackendBufferSize returns the audio backend buffer size in samples.
func (s *Soloud) GetBackendBufferSize() uint {
	return uint(C.Soloud_getBackendBufferSize(s.p))
}
