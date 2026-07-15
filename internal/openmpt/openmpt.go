// Package openmpt decodes tracker music via libopenmpt.
package openmpt

/*
#cgo LDFLAGS: -lopenmpt
#include <libopenmpt/libopenmpt.h>
#include <stdlib.h>

// Suppress deprecated warning for create_from_memory.
#pragma GCC diagnostic ignored "-Wdeprecated-declarations"
*/
import "C"
import (
	"fmt"
	"os"
	"unsafe"
)

// SupportedExts lists extensions handled by libopenmpt.
var SupportedExts = map[string]bool{
	".mod": true, ".xm": true, ".it": true, ".s3m": true,
	".mptm": true, ".stm": true, ".nst": true, ".wow": true,
	".ult": true, ".669": true, ".mtm": true, ".med": true,
	".far": true, ".mdl": true, ".ams": true, ".dsm": true,
	".amf": true, ".okt": true, ".dmf": true, ".ptm": true,
	".psm": true, ".mt2": true, ".dbm": true,
}

// DecodeToF32 decodes a tracker file to interleaved stereo float32 PCM at 44100 Hz.
// Returns raw PCM data, sample rate, estimated BPM, and tracker channel count.
// The PCM always contains two channels, independently of the tracker channel count.
// GetTrackerMeta returns BPM, channel count, and duration for a tracker file
// without decoding audio.
func GetTrackerMeta(path string) (bpm float64, channels int, duration float64, err error) {
	fileBuf, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, 0, err
	}
	mod := C.openmpt_module_create_from_memory(
		unsafe.Pointer(&fileBuf[0]),
		C.size_t(len(fileBuf)),
		nil, nil, nil,
	)
	if mod == nil {
		return 0, 0, 0, fmt.Errorf("openmpt: create module failed")
	}
	defer C.openmpt_module_destroy(mod)
	bpm = float64(C.openmpt_module_get_current_estimated_bpm(mod))
	channels = int(C.openmpt_module_get_num_channels(mod))
	duration = float64(C.openmpt_module_get_duration_seconds(mod))
	return bpm, channels, duration, nil
}

func DecodeToF32(path string) (data []float32, sampleRate int, bpm float64, channels int, err error) {
	fileBuf, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	if len(fileBuf) == 0 {
		return nil, 0, 0, 0, fmt.Errorf("openmpt: empty file")
	}

	mod := C.openmpt_module_create_from_memory(
		unsafe.Pointer(&fileBuf[0]),
		C.size_t(len(fileBuf)),
		nil, // logfunc
		nil, // loguser
		nil, // ctls
	)
	if mod == nil {
		return nil, 0, 0, 0, fmt.Errorf("openmpt: create module failed")
	}
	defer C.openmpt_module_destroy(mod)

	bpm = float64(C.openmpt_module_get_current_estimated_bpm(mod))
	channels = int(C.openmpt_module_get_num_channels(mod))

	const sr = 44100
	duration := float64(C.openmpt_module_get_duration_seconds(mod))
	if duration <= 0 || duration > 3600 {
		duration = 120 // fallback
	}

	totalFrames := int(duration * float64(sr))
	data = make([]float32, 0, totalFrames*2)

	const chunkFrames = 4096
	buf := make([]float32, chunkFrames*2)

	for framesDecoded := 0; framesDecoded < totalFrames; {
		framesToRead := chunkFrames
		if remaining := totalFrames - framesDecoded; remaining < framesToRead {
			framesToRead = remaining
		}
		frames := int(C.openmpt_module_read_interleaved_float_stereo(
			mod,
			C.int32_t(sr),
			C.size_t(framesToRead),
			(*C.float)(unsafe.Pointer(&buf[0])),
		))
		if frames <= 0 {
			break
		}
		data = append(data, buf[:frames*2]...)
		framesDecoded += frames
	}

	if len(data) == 0 {
		return nil, 0, 0, 0, fmt.Errorf("openmpt: no audio rendered")
	}

	return data, sr, bpm, channels, nil
}
