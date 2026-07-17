// Package openmpt provides tracker music metadata via libopenmpt.
// Audio playback uses SoLoud's built-in Openmpt streaming instead.
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

// GetTrackerMeta reads a tracker file and returns BPM, channel count, and duration.
func GetTrackerMeta(path string) (bpm float64, channels int, duration float64, err error) {
	fileBuf, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, 0, err
	}
	return GetTrackerMetaFromBytes(fileBuf)
}

// GetTrackerMetaFromBytes returns BPM, channel count, and duration from in-memory
// tracker file data. Caller owns the buffer.
func GetTrackerMetaFromBytes(data []byte) (bpm float64, channels int, duration float64, err error) {
	if len(data) == 0 {
		return 0, 0, 0, fmt.Errorf("openmpt: empty data")
	}
	mod := C.openmpt_module_create_from_memory(
		unsafe.Pointer(&data[0]),
		C.size_t(len(data)),
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
