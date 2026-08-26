// Package app owns the preview renderer lifecycle.
// All GL operations happen on the main thread via ProcessNext().
package app

/*
#cgo LDFLAGS: -lGLESv2
#include <GLES2/gl2.h>

static GLuint prCreateFBO(GLuint tex) {
	GLuint fbo;
	glGenFramebuffers(1, &fbo);
	glBindFramebuffer(GL_FRAMEBUFFER, fbo);
	glFramebufferTexture2D(GL_FRAMEBUFFER, GL_COLOR_ATTACHMENT0, GL_TEXTURE_2D, tex, 0);
	GLint status = glCheckFramebufferStatus(GL_FRAMEBUFFER);
	glBindFramebuffer(GL_FRAMEBUFFER, 0);
	if (status != GL_FRAMEBUFFER_COMPLETE) {
		glDeleteFramebuffers(1, &fbo);
		return 0;
	}
	return fbo;
}

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
*/
import "C"

import (
	"log/slog"
	"sync/atomic"

	"github.com/dendec/glitchscope/internal/projectm"
)

const (
	thumbW        = 160
	thumbH        = 90
	defaultWarmup = 3
	maxQueue      = 10
)

// previewJob is a pending thumbnail render request.
type previewJob struct {
	key    string // preset key (for cache lookup)
	data   string // .milk content
	warmup int    // frames to render before capture
}

// previewRenderer manages a dedicated projectM instance for thumbnail rendering.
// All methods must be called on the main GL thread.
//
// Render ordering within the main frame:
//
//	pm.RenderFrame()        // main visualizer → fb0 at render resolution
//	rt.Capture()            // copy fb0 → rt.tex (saves main output)
//	preview.ProcessNext()   // pmPreview renders to fb0 at thumb size, captures corner
//	rt.BlitToScreen(w, h)  // restores fb0 from rt.tex (overwrites any preview artifacts)
//	overlay.Draw(w, h)     // UI draws; can reference preview.HasResult() for thumbnail
type previewRenderer struct {
	pm         *projectm.Handle
	fbo        C.GLuint
	tex        C.GLuint // thumbnail capture texture
	queue      []previewJob
	active     *previewJob
	warmupLeft int

	// Result of the last completed render.
	resultKey atomic.Value // string
	resultTex uint32       // GL texture ID

	ready bool // false if GL init failed
}

// newPreviewRenderer creates a second projectM instance and capture texture.
// Must be called on the main GL thread after the main pm is created.
func newPreviewRenderer() *previewRenderer {
	r := &previewRenderer{}

	pm, err := projectm.Create()
	if err != nil {
		slog.Warn("preview pm create failed", "error", err)
		return r
	}
	r.pm = pm
	r.pm.SetWindowSize(thumbW, thumbH)
	r.pm.SetHardCutEnabled(false)

	// Create capture texture and FBO (FBO validates completeness).
	C.prCreateThumbTexture(&r.tex, C.GLsizei(thumbW), C.GLsizei(thumbH))
	r.fbo = C.prCreateFBO(r.tex)
	if r.fbo == 0 {
		slog.Warn("preview FBO incomplete, thumbnail preview disabled")
		return r
	}
	r.ready = true
	slog.Info("preview renderer initialized", "thumbW", thumbW, "thumbH", thumbH)
	return r
}

// Enqueue adds a preview job. Drops the oldest if queue is full.
func (r *previewRenderer) Enqueue(key, data string) {
	if !r.ready || r.pm == nil {
		return
	}
	if r.active != nil && r.active.key == key {
		return
	}
	for _, j := range r.queue {
		if j.key == key {
			return
		}
	}
	if len(r.queue) >= maxQueue {
		r.queue = r.queue[1:]
	}
	r.queue = append(r.queue, previewJob{
		key:    key,
		data:   data,
		warmup: defaultWarmup,
	})
	slog.Debug("preview enqueued", "key", key, "queueLen", len(r.queue))
}

// HasResult returns the GL texture ID for the given key, if available.
func (r *previewRenderer) HasResult(key string) (uint32, bool) {
	if k, ok := r.resultKey.Load().(string); ok && k == key && r.resultTex != 0 {
		return r.resultTex, true
	}
	return 0, false
}

// SetResult stores the capture result. Called internally after capture.
func (r *previewRenderer) setResult(key string, tex uint32) {
	r.resultKey.Store(key)
	r.resultTex = tex
	slog.Debug("preview result stored", "key", key, "tex", tex)
}

// ProcessNext runs one preview step. Returns true if work was done.
//
// Call between rt.Capture() and rt.BlitToScreen() so that:
//  1. Main render is already saved in rt.tex
//  2. pmPreview renders to fb0 at thumb size (overwrites a corner)
//  3. We capture the corner into r.tex
//  4. rt.BlitToScreen() restores the full main render, erasing preview artifacts
func (r *previewRenderer) ProcessNext() bool {
	if !r.ready || r.pm == nil {
		return false
	}

	// Start next job if none active.
	if r.active == nil {
		if len(r.queue) == 0 {
			return false
		}
		r.active = &r.queue[0]
		r.queue = r.queue[1:]
		r.warmupLeft = r.active.warmup
		slog.Debug("preview job started", "key", r.active.key, "warmup", r.warmupLeft)
		r.pm.LoadPresetData(r.active.data, false)
		r.feedPCM()
	}

	// Render one frame.
	r.pm.RenderFrame()
	r.warmupLeft--

	// Capture when warmup is done.
	if r.warmupLeft <= 0 {
		r.capture()
		r.setResult(r.active.key, uint32(r.tex))
		r.active = nil
		return true
	}
	return true
}

// capture copies the bottom-left thumbW×thumbH region of fb0 into r.tex.
func (r *previewRenderer) capture() {
	C.glBindTexture(C.GL_TEXTURE_2D, r.tex)
	C.glCopyTexSubImage2D(C.GL_TEXTURE_2D, 0, 0, 0, 0, 0,
		C.GLsizei(thumbW), C.GLsizei(thumbH))
	C.glBindTexture(C.GL_TEXTURE_2D, 0)
	slog.Debug("preview captured", "key", r.active.key)
}

// feedPCM sends a minimal signal so audio-reactive presets produce a
// representative thumbnail. Not played through speakers.
func (r *previewRenderer) feedPCM() {
	samples := make([]float32, 512)
	for i := range samples {
		samples[i] = 0.3 * float32(i%200) / 200.0
	}
	r.pm.PCMAddFloat(samples, projectm.Mono)
}

// Flush cancels all pending jobs and clears the result.
func (r *previewRenderer) Flush() {
	r.queue = r.queue[:0]
	r.active = nil
	r.resultKey.Store("")
	r.resultTex = 0
}

// Destroy frees GL resources and the projectM instance.
func (r *previewRenderer) Destroy() {
	if r.fbo != 0 {
		C.glDeleteFramebuffers(1, &r.fbo)
		r.fbo = 0
	}
	if r.tex != 0 {
		C.glDeleteTextures(1, &r.tex)
		r.tex = 0
	}
	if r.pm != nil {
		r.pm.Destroy()
		r.pm = nil
	}
	r.ready = false
}
