// Package openmpt provides tracker format support via libopenmpt (fallback).
// Primary playback uses libxmp; libopenmpt handles formats libxmp cannot load.
package openmpt

/*
#include <stddef.h>

void * openmpt_module_create_from_memory(const void * filedata, size_t filesize, void *logfunc, void * user, void * ctls);
void openmpt_module_destroy(void * mod);
size_t openmpt_module_read_float_stereo(void * mod, int samplerate, size_t count, float * left, float * right);
void openmpt_module_set_repeat_count(void * mod, int repeat_count);

double openmpt_module_get_duration_seconds(void * mod);
int openmpt_module_get_num_channels(void * mod);
const char * openmpt_module_get_format_name(void * mod);
const char * openmpt_module_get_metadata(void * mod, const char * key);
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// SupportedExts lists extensions handled by libopenmpt (fallback only).
var SupportedExts = map[string]bool{
	".dmf": true, // DefleMask (Digital Tracker DMF is handled by libxmp)
	".mo3": true, // OggMO3 compressed modules
	".ktm": true, // Karate
	".ims": true, // Velvet Studio
	".mdc": true, // Megadrive
	".txn": true, // MadTracker 2
}

// HasExt reports whether the extension is handled by libopenmpt.
func HasExt(ext string) bool {
	return SupportedExts[ext]
}

// TryLoad attempts to create an openmpt module from data.
func TryLoad(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("openmpt: empty data")
	}
	mod := C.openmpt_module_create_from_memory(
		unsafe.Pointer(&data[0]),
		C.size_t(len(data)),
		nil, nil, nil,
	)
	if mod == nil {
		return fmt.Errorf("openmpt: unsupported format")
	}
	C.openmpt_module_destroy(mod)
	return nil
}

// GetTrackerMeta reads tracker metadata via libopenmpt.
func GetTrackerMeta(data []byte) (bpm float64, channels int, duration float64, err error) {
	if len(data) == 0 {
		return 0, 0, 0, fmt.Errorf("openmpt: empty data")
	}

	mod := C.openmpt_module_create_from_memory(
		unsafe.Pointer(&data[0]),
		C.size_t(len(data)),
		nil, nil, nil,
	)
	if mod == nil {
		return 0, 0, 0, fmt.Errorf("openmpt: unsupported format")
	}
	defer C.openmpt_module_destroy(mod)

	duration = float64(C.openmpt_module_get_duration_seconds(mod))
	channels = int(C.openmpt_module_get_num_channels(mod))

	return bpm, channels, duration, nil
}

// Render decodes up to maxFrames frames of a tracker module into interleaved
// float32 stereo and returns the number of frames actually rendered. The
// libopenmpt SoLoud binding cannot seek backward (and forward only by coarse
// sample-discard), so pre-rendering a bounded buffer lets the player seek
// exactly in both directions. Rendering stops at end-of-song or when maxFrames
// is reached, whichever comes first. Returns (interleaved, channels, frames, err).
func Render(data []byte, samplerate, maxFrames int) ([]float32, int, int, error) {
	if len(data) == 0 {
		return nil, 0, 0, fmt.Errorf("openmpt: empty data")
	}
	if maxFrames <= 0 {
		return nil, 0, 0, nil
	}
	mod := C.openmpt_module_create_from_memory(
		unsafe.Pointer(&data[0]),
		C.size_t(len(data)),
		nil, nil, nil,
	)
	if mod == nil {
		return nil, 0, 0, fmt.Errorf("openmpt: unsupported format")
	}
	defer C.openmpt_module_destroy(mod)
	// Play through once and stop (instead of the infinite-loop streaming
	// source) so rendering is bounded for looping songs.
	C.openmpt_module_set_repeat_count(mod, 0)

	left := make([]float32, maxFrames)
	right := make([]float32, maxFrames)
	frames := 0
	for frames < maxFrames {
		n := maxFrames - frames
		read := int(C.openmpt_module_read_float_stereo(
			mod,
			C.int(samplerate),
			C.size_t(n),
			(*C.float)(unsafe.Pointer(&left[frames])),
			(*C.float)(unsafe.Pointer(&right[frames])),
		))
		if read <= 0 {
			break
		}
		frames += read
		if read < n {
			break
		}
	}
	interleaved := make([]float32, frames*2)
	for i := 0; i < frames; i++ {
		interleaved[i*2] = left[i]
		interleaved[i*2+1] = right[i]
	}
	return interleaved, 2, frames, nil
}
