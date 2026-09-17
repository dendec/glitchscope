// Package projectm provides a Go wrapper around libprojectM C API.
package projectm

/*
#cgo CFLAGS: -I ../../lib/projectm/src/api/include -I ../../lib/projectm/build/src/api/include
#cgo windows CFLAGS: -DGLITCHSCOPE_GLEW_STATIC
#cgo linux LDFLAGS: -lGLESv2
#cgo windows LDFLAGS: -lglew32s -lopengl32
#include <stdlib.h>
#include "../gl/compat.h"
#include "projectM-4/projectM.h"

static int pmvInitOpenGL(void) {
#if defined(_WIN32)
	glewExperimental = GL_TRUE;
	GLenum err = glewInit();
	// glewInit may leave GL_INVALID_ENUM in the error queue on some drivers.
	(void)glGetError();
	return (int)err;
#else
	return 0;
#endif
}

static const char *pmvOpenGLError(int err) {
#if defined(_WIN32)
	return (const char *)glewGetErrorString((GLenum)err);
#else
	(void)err;
	return "";
#endif
}

#if defined(_WIN32)
extern __declspec(dllexport) void pmvProjectMPresetSwitchRequested(bool isHardCut);
#else
extern void pmvProjectMPresetSwitchRequested(bool isHardCut);
#endif
extern void projectm_opengl_bind_feedback_framebuffer(projectm_handle instance);
extern void projectm_set_preset_transition_filter(projectm_handle instance, bool nearest);

static void pmvPresetSwitchRequested(bool isHardCut, void* userData) {
	(void)userData;
	pmvProjectMPresetSwitchRequested(isHardCut);
}

static void pmvSetPresetSwitchRequestedCallback(projectm_handle instance) {
	projectm_set_preset_switch_requested_event_callback(instance, pmvPresetSwitchRequested, NULL);
}

static void pmvClearPresetSwitchRequestedCallback(projectm_handle instance) {
	projectm_set_preset_switch_requested_event_callback(instance, NULL, NULL);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

const Mono = C.PROJECTM_MONO

// InitOpenGL loads desktop OpenGL entry points on Windows. GLES2 platforms
// expose the same functions directly and need no loader step.
func InitOpenGL() error {
	if err := C.pmvInitOpenGL(); err != 0 {
		return fmt.Errorf("initialize OpenGL: %s", C.GoString(C.pmvOpenGLError(err)))
	}
	return nil
}

var presetSwitchRequestedHandler func(bool)

//export pmvProjectMPresetSwitchRequested
func pmvProjectMPresetSwitchRequested(isHardCut C.bool) {
	if presetSwitchRequestedHandler != nil {
		presetSwitchRequestedHandler(bool(isHardCut))
	}
}

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

// SetTransitionFilter controls interpolation while projectM blends presets.
func (h *Handle) SetTransitionFilter(nearest bool) {
	C.projectm_set_preset_transition_filter(h.p, C.bool(nearest))
}

// SetHardCutEnabled enables or disables beat-driven preset switch requests.
func (h *Handle) SetHardCutEnabled(enabled bool) {
	C.projectm_set_hard_cut_enabled(h.p, C.bool(enabled))
}

// SetPresetSwitchRequestedHandler registers the preset switch request handler.
func (h *Handle) SetPresetSwitchRequestedHandler(handler func(bool)) {
	presetSwitchRequestedHandler = handler
	if handler == nil {
		C.pmvClearPresetSwitchRequestedCallback(h.p)
		return
	}
	C.pmvSetPresetSwitchRequestedCallback(h.p)
}

// SetBeatSensitivity adjusts the beat reaction multiplier in the range 0..2.
func (h *Handle) SetBeatSensitivity(sensitivity float64) {
	C.projectm_set_beat_sensitivity(h.p, C.float(sensitivity))
}
