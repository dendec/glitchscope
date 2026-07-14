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

const (
	Mono   = C.PROJECTM_MONO
	Stereo = C.PROJECTM_STEREO
)

// Handle wraps the opaque projectM instance handle.
type Handle struct {
	p C.projectm_handle
}

// Create creates a new projectM instance with default settings.
// OpenGL context must be current before calling this.
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

// LoadPresetFile loads a .milk preset file.
func (h *Handle) LoadPresetFile(path string, smooth bool) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	C.projectm_load_preset_file(h.p, cpath, C.bool(smooth))
}

// PCMAddFloat feeds PCM audio samples to projectM (float32, mono or stereo).
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

// RenderFrame renders a single visualizer frame.
// OpenGL context must be current.
func (h *Handle) RenderFrame() {
	C.projectm_opengl_render_frame(h.p)
}

// BindFeedbackFramebuffer binds the internal image consumed by the next frame.
// OpenGL drawing performed after this call becomes part of projectM's feedback.
func (h *Handle) BindFeedbackFramebuffer() {
	C.projectm_opengl_bind_feedback_framebuffer(h.p)
}

// SetPresetSwitchFailedCallback is a no-op until cgo callback forwarding is implemented.
func (h *Handle) SetPresetSwitchFailedCallback(cb func(filename, message string)) {
	_ = cb
}

// SetPresetSwitchRequestedCallback is a no-op until cgo callback forwarding is implemented.
func (h *Handle) SetPresetSwitchRequestedCallback(cb func(hardCut bool)) {
	_ = cb
}

// GetVersion returns the library version and VCS revision strings.
func GetVersion() (string, string) {
	major, minor, patch := C.int(0), C.int(0), C.int(0)
	C.projectm_get_version_components(&major, &minor, &patch)
	ver := fmt.Sprintf("%d.%d.%d", major, minor, patch)

	vcs := C.projectm_get_vcs_version_string()
	if vcs != nil {
		defer C.projectm_free_string(vcs)
	}
	return ver, C.GoString(vcs)
}

// PCMGetMaxSamples returns the maximum number of PCM samples per channel.
func PCMGetMaxSamples() int {
	return int(C.projectm_pcm_get_max_samples())
}
