// Package xmp provides tracker music metadata via libxmp.
// Audio playback uses SoLoud's built-in Xmp streaming.
package xmp

/*
#include <xmp.h>
#include <stdlib.h>
*/
import "C"

import (
	"context"
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

// Render decodes up to maxFrames frames of a tracker module into interleaved
// float32 stereo and returns the number of frames actually rendered. The SoLoud
// Xmp binding cannot seek backward (and forward only by coarse sample-discard),
// so pre-rendering a bounded buffer lets the player seek exactly in both
// directions. Rendering stops at end-of-song or when maxFrames is reached,
// whichever comes first. Returns (interleaved, channels, frames, err).
func Render(data []byte, samplerate, maxFrames int) ([]float32, int, int, error) {
	return RenderContext(context.Background(), data, samplerate, maxFrames)
}

// RenderContext checks cancellation between small decoder blocks.
func RenderContext(renderCtx context.Context, data []byte, samplerate, maxFrames int) ([]float32, int, int, error) {
	cancelErr := renderCtx.Err()
	if cancelErr != nil {
		return nil, 0, 0, cancelErr
	}
	if len(data) == 0 {
		return nil, 0, 0, fmt.Errorf("xmp: empty data")
	}
	if maxFrames <= 0 {
		return nil, 0, 0, nil
	}
	ctx := C.xmp_create_context()
	if ctx == nil {
		return nil, 0, 0, fmt.Errorf("xmp: create context failed")
	}
	defer C.xmp_free_context(ctx)

	if C.xmp_load_module_from_memory(ctx, unsafe.Pointer(&data[0]), C.long(len(data))) != 0 {
		return nil, 0, 0, fmt.Errorf("xmp: load module failed")
	}
	defer C.xmp_release_module(ctx)

	if C.xmp_start_player(ctx, C.int(samplerate), 0) != 0 {
		return nil, 0, 0, fmt.Errorf("xmp: start player failed")
	}
	defer C.xmp_end_player(ctx)

	out := make([]float32, 0, maxFrames*2)
	frames := 0
	for frames < maxFrames {
		if err := renderCtx.Err(); err != nil {
			return nil, 0, 0, err
		}
		if C.xmp_play_frame(ctx) < 0 {
			break // genuine end of song
		}
		var fi C.struct_xmp_frame_info
		C.xmp_get_frame_info(ctx, &fi)
		// fi.buffer holds the interleaved 16-bit stereo samples produced by the
		// last frame command; fi.buffer_size is its size in bytes (4 per frame).
		bytesAvail := int(fi.buffer_size)
		if bytesAvail < 4 {
			break
		}
		n := bytesAvail / 4
		if frames+n > maxFrames {
			n = maxFrames - frames
		}
		src := (*[1 << 28]int16)(unsafe.Pointer(fi.buffer))[: n*2 : n*2]
		for i := 0; i < n; i++ {
			out = append(out, float32(src[i*2])/32768.0, float32(src[i*2+1])/32768.0)
		}
		frames += n
		// Stop once we've played the whole nominal song length.
		if fi.total_time > 0 && fi.time >= fi.total_time {
			break
		}
	}
	return out, 2, frames, nil
}
