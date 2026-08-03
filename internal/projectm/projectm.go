// Package projectm provides a Go wrapper around libprojectM C API.
package projectm

/*
#cgo CFLAGS: -I ../../lib/projectm/src/api/include -I ../../lib/projectm/build/src/api/include
#include <stdlib.h>
#include "projectM-4/projectM.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

const Mono = C.PROJECTM_MONO

// Handle wraps the opaque projectM instance handle.
type Handle struct {
	p C.projectm_handle
}

// Create creates a new projectM instance. OpenGL context must be current.
func Create() (*Handle, error) {
	h := C.projectm_create()
	if h == nil {
		return nil, fmt.Errorf("projectm_create: failed (no OpenGL context?)")
	}
	return &Handle{p: h}, nil
}

// Destroy frees the projectM instance.
func (h *Handle) Destroy() {
	if h.p != nil {
		C.projectm_destroy(h.p)
		h.p = nil
	}
}

// LoadPresetData loads a preset from an in-memory .milk string.
func (h *Handle) LoadPresetData(data string, smooth bool) {
	cdata := C.CString(data)
	defer C.free(unsafe.Pointer(cdata))
	C.projectm_load_preset_data(h.p, cdata, C.bool(smooth))
}

// PCMAddFloat feeds PCM audio samples (float32, mono or stereo).
func (h *Handle) PCMAddFloat(samples []float32, channels int) {
	if len(samples) == 0 {
		return
	}
	C.projectm_pcm_add_float(h.p, (*C.float)(&samples[0]), C.uint(len(samples)), C.projectm_channels(channels))
}

// SetWindowSize notifies projectM of the viewport dimensions.
func (h *Handle) SetWindowSize(width, height int) {
	C.projectm_set_window_size(h.p, C.size_t(width), C.size_t(height))
}

// RenderFrame renders a single visualizer frame. OpenGL context must be current.
func (h *Handle) RenderFrame() {
	C.projectm_opengl_render_frame(h.p)
}

// SetMeshSize sets per-pixel equation mesh resolution. Clamped [8,400].
func (h *Handle) SetMeshSize(width, height int) {
	C.projectm_set_mesh_size(h.p, C.size_t(width), C.size_t(height))
}

// GetMeshSize returns current mesh resolution.
func (pm *Handle) GetMeshSize() (int, int) {
	var mw, mh C.size_t
	C.projectm_get_mesh_size(pm.p, &mw, &mh)
	return int(mw), int(mh)
}

// SetFPS reports actual frame rate to presets for time-dependent expressions.
func (h *Handle) SetFPS(fps int32) {
	C.projectm_set_fps(h.p, C.int32_t(fps))
}

// SetTextureSearchPaths tells projectM where to look for user textures.
func (h *Handle) SetTextureSearchPaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	cPaths := make([]*C.char, len(paths))
	for i, p := range paths {
		cPaths[i] = C.CString(p)
	}
	defer func() {
		for _, cp := range cPaths {
			C.free(unsafe.Pointer(cp))
		}
	}()
	C.projectm_set_texture_search_paths(h.p, &cPaths[0], C.size_t(len(paths)))
}

// BindFeedbackFramebuffer binds the internal image consumed by the next frame.
func (h *Handle) BindFeedbackFramebuffer() {
	C.projectm_opengl_bind_feedback_framebuffer(h.p)
}

// SetSoftCutDuration sets the transition duration for smooth preset cuts.
func (h *Handle) SetSoftCutDuration(seconds float64) {
	C.projectm_set_soft_cut_duration(h.p, C.double(seconds))
}
