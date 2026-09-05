// Package app owns the preview renderer lifecycle.
// All GL operations happen on the main thread via ProcessNext().
package app

/*
#cgo LDFLAGS: -lGLESv2
#include <GLES2/gl2.h>

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

// ClearFB clears the current framebuffer to black at the window viewport.
func ClearFB(w, h int) {
	C.glBindFramebuffer(C.GL_FRAMEBUFFER, 0)
	C.glViewport(0, 0, C.GLsizei(w), C.GLsizei(h))
	C.glDisable(C.GL_SCISSOR_TEST)
	C.glColorMask(C.GL_TRUE, C.GL_TRUE, C.GL_TRUE, C.GL_TRUE)
	C.glClearColor(0, 0, 0, 1)
	C.glClear(C.GL_COLOR_BUFFER_BIT)
}

// previewJob is a pending thumbnail render request.
type previewJob struct {
	key  string // preset key (for cache lookup)
	data string // .milk content
}

// previewRenderer manages a dedicated projectM instance for preset preview.
// On the presets page the preview renders into the full window (as background)
// and simultaneously captures a corner into a thumbnail texture for the
// right-panel preview. All methods must be called on the main GL thread.
type previewRenderer struct {
	pm        *projectm.Handle
	tex       C.GLuint // thumbnail capture texture
	w, h      int      // current thumbnail render dimensions
	queue     []previewJob
	active    *previewJob
	nextFrame time.Time

	// FPS measurement.
	meter fpsMeter

	// Result of the last completed render.
	resultKey atomic.Value // string
	resultTex uint32       // GL texture ID

	ready bool // false if GL init failed
}

// newPreviewRenderer creates a second projectM instance for thumbnails.
// Call Resize() before the first ProcessNext() to set initial dimensions.
func newPreviewRenderer() *previewRenderer {
	r := &previewRenderer{}

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

// RenderFPS returns the measured render FPS of the preview instance.
func (r *previewRenderer) RenderFPS() float64 {
	return r.meter.Average()
}

// Resize recreates the capture texture at the given dimensions.
// No-op if dimensions haven't changed.
func (r *previewRenderer) Resize(w, h int) {
	if !r.isReady() || w <= 0 || h <= 0 {
		return
	}
	if w == r.w && h == r.h {
		return
	}
	// Destroy old resources.
	if r.tex != 0 {
		C.glDeleteTextures(1, &r.tex)
		r.tex = 0
	}
	// Create new.
	r.pm.SetWindowSize(w, h)
	C.prCreateThumbTexture(&r.tex, C.GLsizei(w), C.GLsizei(h))
	r.w, r.h = w, h
	r.Flush() // discard stale results
	slog.Debug("preview resized", "w", w, "h", h)
}

// Enqueue selects the latest preview job. Rapid navigation must not leave
// intermediate presets queued for rendering after the cursor has moved on.
func (r *previewRenderer) Enqueue(key, data string) {
	if !r.isReady() {
		return
	}
	if r.active != nil && r.active.key == key {
		return
	}
	r.active = nil
	r.queue = append(r.queue[:0], previewJob{
		key:  key,
		data: data,
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

func (r *previewRenderer) setResult(key string, tex uint32) {
	r.resultKey.Store(key)
	r.resultTex = tex
	slog.Debug("preview result stored", "key", key, "tex", tex)
}

// ProcessNext runs one preview step. Returns true if work was done.
//
// When fullScreen is true the preview renders at the current window size
// (the result stays on screen as background). When false it renders into
// the thumbnail-sized capture texture for the right-panel preview.
//
// The job stays active during animation so the thumbnail keeps playing.
// A new Enqueue replaces the current job.
func (r *previewRenderer) ProcessNext(fullScreen bool) bool {
	if !r.isReady() {
		return false
	}

	// Start next job if none active.
	if r.active == nil {
		if len(r.queue) == 0 {
			return false
		}
		r.active = &r.queue[0]
		r.queue = r.queue[1:]
		r.meter.Reset()
		r.nextFrame = time.Time{}
		slog.Debug("preview job started", "key", r.active.key)
		r.pm.LoadPresetData(r.active.data, false)
	}

	now := time.Now()
	if !previewFrameDue(now, r.nextFrame) {
		return false
	}
	r.nextFrame = now.Add(previewFramePeriod)

	started := time.Now()
	if fullScreen {
		// Render at full window size — result stays on screen as background.
		r.pm.RenderFrame()
	} else {
		// Render at thumbnail size and capture into the thumb texture.
		C.glViewport(0, 0, C.GLsizei(r.w), C.GLsizei(r.h))
		r.pm.RenderFrame()
		C.prCaptureThumb(r.tex, C.GLsizei(r.w), C.GLsizei(r.h))
		r.setResult(r.active.key, uint32(r.tex))
	}
	r.meter.AddDuration(time.Since(started))

	return true
}

func previewFrameDue(now, nextFrame time.Time) bool {
	return nextFrame.IsZero() || !now.Before(nextFrame)
}

// CaptureThumb copies the top-left corner of framebuffer 0 into the
// thumbnail texture at the given dimensions. Called after a full-screen
// ProcessNext(true) so the panel can display a mini version of the
// background.
func (r *previewRenderer) CaptureThumb(tW, tH int) {
	if !r.isReady() || tW <= 0 || tH <= 0 {
		return
	}
	// Recreate thumb texture if dimensions changed.
	if tW != r.w || tH != r.h {
		if r.tex != 0 {
			C.glDeleteTextures(1, &r.tex)
			r.tex = 0
		}
		C.prCreateThumbTexture(&r.tex, C.GLsizei(tW), C.GLsizei(tH))
		r.w, r.h = tW, tH
	}
	C.prCaptureThumb(r.tex, C.GLsizei(r.w), C.GLsizei(r.h))
	if r.active != nil {
		r.setResult(r.active.key, uint32(r.tex))
	}
}

// Flush cancels all pending jobs and clears the result.
func (r *previewRenderer) Flush() {
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
		C.glDeleteTextures(1, &r.tex)
		r.tex = 0
	}
	if r.pm != nil {
		r.pm.Destroy()
		r.pm = nil
	}
	r.ready = false
}
