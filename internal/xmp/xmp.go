// Package xmp provides tracker music metadata via libxmp.
// Audio playback uses SoLoud's built-in Xmp streaming.
package xmp

/*
#include <xmp.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"os"
	"unsafe"
)

// SupportedExts lists extensions handled by libxmp.
var SupportedExts = map[string]bool{
	// Core tracker formats
	".mod": true, ".xm": true, ".it": true, ".s3m": true,
	// Additional tracker formats
	".mptm": true, ".stm": true, ".nst": true, ".wow": true,
	".ult": true, ".669": true, ".mtm": true, ".med": true,
	".far": true, ".mdl": true, ".ams": true, ".dsm": true,
	".amf": true, ".okt": true, ".dmf": true, ".ptm": true,
	".psm": true, ".mt2": true, ".dbm": true,
	// Exotic formats supported by libxmp
	".abk": true, ".digi": true, ".dtt": true,
	".flx": true, ".gtk": true, ".imf": true, ".liq": true,
	".masi": true, ".mgt": true, ".mmd": true, ".mmdc": true,
	".mmcmp": true, ".muse": true, ".nt": true, ".pmd": true,
	".ppm": true, ".pru": true, ".pt36": true, ".rh": true,
	".rtm": true, ".sfx": true, ".sfx2": true, ".stim": true,
	".stx": true, ".tcb": true, ".tdd": true, ".tp": true,
	".uni": true, ".xd": true,
}

// GetTrackerMeta reads a tracker file and returns BPM, channels, and duration.
func GetTrackerMeta(path string) (bpm float64, channels int, duration float64, err error) {
	fileBuf, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, 0, err
	}
	return GetTrackerMetaFromBytes(fileBuf)
}

// GetTrackerMetaFromBytes returns BPM, channels, and duration from in-memory data.
func GetTrackerMetaFromBytes(data []byte) (bpm float64, channels int, duration float64, err error) {
	if len(data) == 0 {
		return 0, 0, 0, fmt.Errorf("xmp: empty data")
	}

	ctx := C.xmp_create_context()
	if ctx == nil {
		return 0, 0, 0, fmt.Errorf("xmp: create context failed")
	}
	defer C.xmp_free_context(ctx)

	ret := C.xmp_load_module_from_memory(ctx, unsafe.Pointer(&data[0]), C.long(len(data)))
	if ret != 0 {
		return 0, 0, 0, fmt.Errorf("xmp: load module failed: %d", ret)
	}
	defer C.xmp_release_module(ctx)

	var mi C.struct_xmp_module_info
	C.xmp_get_module_info(ctx, &mi)

	// Get frame info for duration.
	if C.xmp_start_player(ctx, 44100, 0) == 0 {
		var fi C.struct_xmp_frame_info
		C.xmp_get_frame_info(ctx, &fi)
		duration = float64(fi.total_time) / 1000.0 // ms to seconds
		C.xmp_end_player(ctx)
	}

	channels = int(mi.mod.chn)
	// BPM from module info.
	if mi.mod.bpm > 0 {
		bpm = float64(mi.mod.bpm)
	}

	return bpm, channels, duration, nil
}

// TryLoad attempts to load tracker data with libxmp.
func TryLoad(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("xmp: empty data")
	}
	ctx := C.xmp_create_context()
	if ctx == nil {
		return fmt.Errorf("xmp: create context failed")
	}
	defer C.xmp_free_context(ctx)
	ret := C.xmp_load_module_from_memory(ctx, unsafe.Pointer(&data[0]), C.long(len(data)))
	if ret != 0 {
		return fmt.Errorf("xmp load: %d", ret)
	}
	C.xmp_release_module(ctx)
	return nil
}
