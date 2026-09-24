// Package app owns the preview renderer lifecycle.
// All GL operations happen on the main thread via ProcessNext().
package app

/*
#cgo windows CFLAGS: -DGLITCHSCOPE_GLEW_STATIC
#cgo linux LDFLAGS: -lGLESv2
#cgo windows LDFLAGS: -lglew32s -lopengl32
#include "../gl/compat.h"

static void prCreateThumbTexture(GLuint *tex, GLsizei w, GLsizei h) {
	glGenTextures(1, tex);
	glBindTexture(GL_TEXTURE_2D, *tex);
	glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA, w, h, 0, GL_RGBA, GL_UNSIGNED_BYTE, NULL);
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
	glBindTexture(GL_TEXTURE_2D, 0);
}

static void prCaptureThumb(GLuint tex, GLsizei w, GLsizei h) {
	glBindTexture(GL_TEXTURE_2D, tex);
	glCopyTexSubImage2D(GL_TEXTURE_2D, 0, 0, 0, 0, 0, w, h);
	glBindTexture(GL_TEXTURE_2D, 0);
}

static void prDeleteTexture(GLuint tex) {
	if (tex) glDeleteTextures(1, &tex);
}

static void prSetViewport(GLsizei w, GLsizei h) {
	glViewport(0, 0, w, h);
}
*/
import "C"

import (
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/dendec/glitchscope/internal/projectm"
)

const (
	previewTargetFPS   = 25
	previewFramePeriod = time.Second / previewTargetFPS
)

// previewJob is a pending thumbnail render request.
type previewJob struct {
	key  string // preset key (for cache lookup)
	data string // .milk content
}

// previewRenderer manages a dedicated projectM instance for preset preview.
// On the presets page ProcessNext() renders at thumbnail size into a GL
// texture. The overlay then stretches that texture full-screen as the page
// background while displaying a properly scaled copy in the right panel.
// All methods must be called on the main GL thread.
type previewRenderer struct {
	pm   *projectm.Handle
	tex  C.GLuint // thumbnail capture texture
	w, h int      // current thumbnail render dimensions

	queue   []previewJob
	active  *previewJob
	loading *previewJob

	// Frame throttling: ProcessNext renders at most once per previewFramePeriod.
	nextFrame   time.Time
	framePeriod time.Duration

	// FPS measurement (render time only, not display cadence).
	meter fpsMeter

	// resultKey stores the key of the last successfully rendered preset.
	// resultTex is the GL texture ID containing that render.
	resultKey atomic.Value // string
	resultTex uint32

	ready bool // false if GL init failed
}

// newPreviewRenderer creates a second projectM instance for thumbnails.
// Call Resize() before the first ProcessNext() to set initial dimensions.
func newPreviewRenderer() *previewRenderer {
	r := &previewRenderer{framePeriod: previewFramePeriod}

	pm, err := projectm.Create()
	if err != nil {
		slog.Warn("preview pm create failed", "error", err)
		return r
	}
	r.pm = pm
	r.pm.SetFPS(previewTargetFPS)
	r.pm.SetHardCutEnabled(false)
	r.ready = true
	return r
}

// isReady reports whether the renderer can process jobs.
func (r *previewRenderer) isReady() bool {
	return r.ready && r.pm != nil
}

// RenderFPS returns the measured render time FPS of the preview instance.
// This is throughput (how fast a single frame renders), not display cadence.
func (r *previewRenderer) RenderFPS() float64 {
	return r.meter.Average()
}

// Resize recreates the capture texture and updates the projectM window size.
// No-op if dimensions haven't changed.
func (r *previewRenderer) Resize(w, h int) {
	if !r.isReady() || w <= 0 || h <= 0 {
		return
	}
	if w == r.w && h == r.h {
		return
	}
	if r.tex != 0 {
		C.prDeleteTexture(r.tex)
		r.tex = 0
	}
	r.pm.SetWindowSize(w, h)
	C.prCreateThumbTexture(&r.tex, C.GLsizei(w), C.GLsizei(h))
	r.w, r.h = w, h
	r.nextFrame = time.Time{}
	r.meter.Reset()
	r.resultKey.Store("")
	r.resultTex = 0
	slog.Debug("preview resized", "w", w, "h", h)
}

// Enqueue selects the latest preview job. Rapid navigation must not leave
// intermediate presets queued for rendering after the cursor has moved on.
func (r *previewRenderer) Enqueue(key, data string) {
	if !r.isReady() {
		return
	}
	if r.selectJob(key, data) {
		r.pm.CancelPresetLoad()
		slog.Debug("preview enqueued", "key", key, "queueLen", len(r.queue))
	}
}

// selectJob changes only the preview selection model. The active image keeps
// animating until the selected job commits; true asks the caller to cancel the
// old native preparation. Selecting the active preset cancels a pending change.
func (r *previewRenderer) selectJob(key, data string) bool {
	if r.loading != nil && r.loading.key == key {
		return false
	}
	if r.active != nil && r.active.key == key && r.loading == nil && len(r.queue) == 0 {
		return false
	}
	r.loading = nil
	r.queue = r.queue[:0]
	if r.active == nil || r.active.key != key {
		r.queue = append(r.queue, previewJob{key: key, data: data})
	}
	return true
}

// Result returns the GL texture ID and key of the last successfully
// rendered preset, or (0, "") if none is available.
func (r *previewRenderer) Result() (tex uint32, key string) {
	if k, ok := r.resultKey.Load().(string); ok && k != "" && r.resultTex != 0 {
		return r.resultTex, k
	}
	return 0, ""
}

func (r *previewRenderer) setResult(key string, tex uint32) {
	r.resultKey.Store(key)
	r.resultTex = tex
}

// HasResult reports whether the given key matches the last rendered preset
// and returns its GL texture ID.
func (r *previewRenderer) HasResult(key string) (uint32, bool) {
	tex, k := r.Result()
	if k == key && tex != 0 {
		return tex, true
	}
	return 0, false
}

// ProcessNext runs one preview step. Returns true if a new frame was rendered.
//
// The job stays active during animation so the thumbnail keeps playing.
// A new Enqueue replaces the current job.
func (r *previewRenderer) ProcessNext() bool {
	if !r.isReady() {
		return false
	}

	if len(r.queue) > 0 {
		job := r.queue[len(r.queue)-1]
		r.queue = r.queue[:0]
		r.loading = &job
		started := time.Now()
		if !r.pm.BeginPresetLoad(job.data, false) {
			slog.Warn("preview load failed", "key", job.key, "error", r.pm.PresetLoadError())
			r.loading = nil
		} else {
			createMS, initializeMS, expressionsMS, framebuffersMS, warpShaderMS, compositeShaderMS := r.pm.PresetPrepareTimes()
			slog.Info("preview prepare", "key", job.key, "shader_compile_mode", projectm.ShaderCompileMode(),
				"prepare_ms", time.Since(started).Milliseconds(), "native_create_ms", createMS, "native_initialize_ms", initializeMS,
				"native_expressions_ms", expressionsMS, "native_framebuffers_ms", framebuffersMS,
				"native_warp_shader_ms", warpShaderMS, "native_composite_shader_ms", compositeShaderMS)
		}
	}
	if r.loading != nil {
		switch r.pm.PollPresetLoad() {
		case projectm.PresetLoadReady:
			if r.pm.CommitPresetLoad() {
				r.active = r.loading
				r.meter.Reset()
				r.nextFrame = time.Time{}
			} else {
				slog.Warn("preview commit failed", "key", r.loading.key, "error", r.pm.PresetLoadError())
			}
			r.loading = nil
		case projectm.PresetLoadFailed:
			slog.Warn("preview load failed", "key", r.loading.key, "error", r.pm.PresetLoadError())
			r.loading = nil
		}
	}
	if r.active == nil {
		return false
	}

	now := time.Now()
	if !previewFrameDue(now, r.nextFrame) {
		return false
	}
	if r.nextFrame.IsZero() {
		r.nextFrame = now.Add(r.framePeriod)
	} else {
		r.nextFrame = r.nextFrame.Add(r.framePeriod)
		if now.After(r.nextFrame) {
			r.nextFrame = now
		}
	}

	C.prSetViewport(C.GLsizei(r.w), C.GLsizei(r.h))
	started := time.Now()
	r.pm.RenderFrame()
	r.meter.AddDuration(time.Since(started))
	C.prCaptureThumb(r.tex, C.GLsizei(r.w), C.GLsizei(r.h))
	r.setResult(r.active.key, uint32(r.tex))

	return true
}

// SkipThrottle forces the next ProcessNext call to render immediately by
// resetting the frame throttle timer. Call when entering the presets page
// to avoid a black flash on the first frame.
func (r *previewRenderer) SkipThrottle() {
	r.nextFrame = time.Time{}
}

// Flush cancels all pending jobs and clears the result.
func (r *previewRenderer) Flush() {
	if r.pm != nil {
		r.pm.CancelPresetLoad()
	}
	r.loading = nil
	r.queue = r.queue[:0]
	r.active = nil
	r.nextFrame = time.Time{}
	r.meter.Reset()
	r.resultKey.Store("")
	r.resultTex = 0
}

// Destroy frees GL resources and the projectM instance.
func (r *previewRenderer) Destroy() {
	if r.tex != 0 {
		C.prDeleteTexture(r.tex)
		r.tex = 0
	}
	if r.pm != nil {
		r.pm.Destroy()
		r.pm = nil
	}
	r.ready = false
}

func previewFrameDue(now, nextFrame time.Time) bool {
	return nextFrame.IsZero() || !now.Before(nextFrame)
}

// SetFPS keeps preview within the selected frame-rate budget.
func (r *previewRenderer) SetFPS(fps int32) {
	fps = min(previewTargetFPS, max(1, fps))
	r.framePeriod = time.Second / time.Duration(fps)
	if r.pm != nil {
		r.pm.SetFPS(fps)
	}
}
