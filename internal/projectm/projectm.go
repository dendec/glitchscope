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

// Keep the wrapper buildable against an unpatched source checkout. The Docker
// builder supplies the patched projectM header, while local test containers
// mount the repository's untouched submodule and link the patched library.
#ifndef GLITCHSCOPE_PROJECTM_PRESET_LOAD_API
extern const char* projectm_parallel_shader_compile(void);
extern void projectm_shader_worker_enable(bool enabled);
extern void projectm_shader_worker_run(void);
extern void projectm_shader_worker_stop(void);
extern const char* projectm_shader_compile_mode(void);
extern double projectm_preset_create_ms(projectm_handle instance);
extern double projectm_preset_initialize_ms(projectm_handle instance);
extern double projectm_preset_initialize_phase_ms(projectm_handle instance, int phase);
extern bool projectm_begin_preset_load(projectm_handle instance, const char* data,
	bool smooth_transition);
extern int projectm_poll_preset_load(projectm_handle instance);
extern bool projectm_commit_preset_load(projectm_handle instance);
extern void projectm_cancel_preset_load(projectm_handle instance);
extern const char* projectm_preset_load_error(projectm_handle instance);
#endif

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

static const char *pmvGLString(GLenum name) {
	const GLubyte *value = glGetString(name);
	return value != NULL ? (const char *)value : "";
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

// PresetLoadStatus is the native state of a staged preset.
type PresetLoadStatus int

const (
	PresetLoadIdle PresetLoadStatus = iota
	PresetLoadLoading
	PresetLoadReady
	PresetLoadFailed
)

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

// GLInfo describes the active driver and shading language selected by SDL.
type GLInfo struct {
	Vendor                 string
	Renderer               string
	Version                string
	ShadingLanguageVersion string
}

// QueryGLInfo reads the current OpenGL context metadata. The context must be
// current on the calling thread.
func QueryGLInfo() GLInfo {
	return GLInfo{
		Vendor:                 C.GoString(C.pmvGLString(C.GL_VENDOR)),
		Renderer:               C.GoString(C.pmvGLString(C.GL_RENDERER)),
		Version:                C.GoString(C.pmvGLString(C.GL_VERSION)),
		ShadingLanguageVersion: C.GoString(C.pmvGLString(C.GL_SHADING_LANGUAGE_VERSION)),
	}
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

// ParallelShaderCompile reports the current context's native compile mode.
// Call only on the GL thread with a current context.
func ParallelShaderCompile() string {
	mode := C.GoString(C.projectm_parallel_shader_compile())
	if mode == "" {
		return "synchronous"
	}
	return mode
}

// EnableShaderWorker enables submission after a shared context is ready.
func EnableShaderWorker() { C.projectm_shader_worker_enable(C.bool(true)) }

// RunShaderWorker blocks on its private queue with a shared GL context current.
func RunShaderWorker() { C.projectm_shader_worker_run() }

// StopShaderWorker wakes the worker and cancels queued jobs. An in-progress
// driver call finishes on the worker before RunShaderWorker returns.
func StopShaderWorker() { C.projectm_shader_worker_stop() }

// ShaderCompileMode reports the actual submission path for staged presets.
func ShaderCompileMode() string {
	mode := C.GoString(C.projectm_shader_compile_mode())
	if mode == "" {
		return "synchronous"
	}
	return mode
}

// PresetPrepareTimes separates construction and the four main initialization
// phases so Windows logs identify which part of a preset switch blocks.
func (h *Handle) PresetPrepareTimes() (createMS, initializeMS, expressionsMS, framebuffersMS, warpShaderMS, compositeShaderMS float64) {
	phase := func(index int) float64 {
		return float64(C.projectm_preset_initialize_phase_ms(h.p, C.int(index)))
	}
	return float64(C.projectm_preset_create_ms(h.p)),
		float64(C.projectm_preset_initialize_ms(h.p)),
		phase(0), phase(1), phase(2), phase(3)
}

// BeginPresetLoad prepares a preset without replacing the active preset.
// OpenGL work remains on the calling (main) thread; shader compilation is
// polled later; Windows compiles programs on a dedicated shared context.
func (h *Handle) BeginPresetLoad(data string, smooth bool) bool {
	cdata := C.CString(data)
	defer C.free(unsafe.Pointer(cdata))
	return bool(C.projectm_begin_preset_load(h.p, cdata, C.bool(smooth)))
}

// PollPresetLoad advances the pending native shader jobs.
func (h *Handle) PollPresetLoad() PresetLoadStatus {
	switch PresetLoadStatus(C.projectm_poll_preset_load(h.p)) {
	case PresetLoadLoading:
		return PresetLoadLoading
	case PresetLoadReady:
		return PresetLoadReady
	case PresetLoadFailed:
		return PresetLoadFailed
	default:
		return PresetLoadIdle
	}
}

// CommitPresetLoad starts the requested smooth transition after preparation.
func (h *Handle) CommitPresetLoad() bool {
	return bool(C.projectm_commit_preset_load(h.p))
}

// CancelPresetLoad drops a staged preset and keeps the active preset intact.
func (h *Handle) CancelPresetLoad() {
	C.projectm_cancel_preset_load(h.p)
}

// PresetLoadError returns the most recent native preparation error.
func (h *Handle) PresetLoadError() string {
	return C.GoString(C.projectm_preset_load_error(h.p))
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
