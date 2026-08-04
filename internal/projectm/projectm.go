// Package projectm provides a Go wrapper around libprojectM C API.
package projectm

/*
#cgo CFLAGS: -I ../../lib/projectm/src/api/include -I ../../lib/projectm/build/src/api/include
#cgo LDFLAGS: -lGLESv2
#include <stdlib.h>
#include <GLES2/gl2.h>
#include "projectM-4/projectM.h"

// pmClearFeedbackBuffer zeroes projectM's internal feedback framebuffer.
//
// projectm_set_window_size() documents that it "resets the OpenGL renderer",
// which reallocates projectM's internal render targets (including the
// feedback buffer we stamp notification text into via
// projectm_opengl_bind_feedback_framebuffer). Freshly (re)allocated GL
// storage has undefined content until first written, so without this clear
// stale/garbage GPU memory — sometimes literally the previous frame's text
// glyphs recycled from freed memory — can bleed into the new buffer.
static void pmClearFeedbackBuffer(projectm_handle instance) {
	GLint oldFbo, oldViewport[4], oldScissorBox[4];
	GLboolean oldScissor, oldColorMask[4];
	GLfloat oldClearColor[4];

	glGetIntegerv(GL_FRAMEBUFFER_BINDING, &oldFbo);
	glGetIntegerv(GL_VIEWPORT, oldViewport);
	glGetIntegerv(GL_SCISSOR_BOX, oldScissorBox);
	glGetBooleanv(GL_SCISSOR_TEST, &oldScissor);
	glGetBooleanv(GL_COLOR_WRITEMASK, oldColorMask);
	glGetFloatv(GL_COLOR_CLEAR_VALUE, oldClearColor);

	projectm_opengl_bind_feedback_framebuffer(instance);
	glDisable(GL_SCISSOR_TEST);
	glColorMask(GL_TRUE, GL_TRUE, GL_TRUE, GL_TRUE);
	glClearColor(0, 0, 0, 0);
	glClear(GL_COLOR_BUFFER_BIT);

	glColorMask(oldColorMask[0], oldColorMask[1], oldColorMask[2], oldColorMask[3]);
	glClearColor(oldClearColor[0], oldClearColor[1], oldClearColor[2], oldClearColor[3]);
	glScissor(oldScissorBox[0], oldScissorBox[1], oldScissorBox[2], oldScissorBox[3]);
	glViewport(oldViewport[0], oldViewport[1], oldViewport[2], oldViewport[3]);
	glBindFramebuffer(GL_FRAMEBUFFER, oldFbo);
	if (oldScissor) {
		glEnable(GL_SCISSOR_TEST);
	} else {
		glDisable(GL_SCISSOR_TEST);
	}
}
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
//
// This resets projectM's internal OpenGL renderer, so the feedback
// framebuffer is cleared afterward to avoid stale/garbage GPU memory
// (e.g. leftover injected text) bleeding into the next frame.
func (h *Handle) SetWindowSize(width, height int) {
	C.projectm_set_window_size(h.p, C.size_t(width), C.size_t(height))
	C.pmClearFeedbackBuffer(h.p)
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
