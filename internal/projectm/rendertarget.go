package projectm

/*
#cgo windows CFLAGS: -DGLITCHSCOPE_GLEW_STATIC
#cgo linux LDFLAGS: -lGLESv2
#cgo windows LDFLAGS: -lglew32s -lopengl32
#include "../gl/compat.h"

#if defined(_WIN32)
#define GS_GLSL_VERTEX_HEADER "#version 120\n"
#define GS_GLSL_FRAGMENT_HEADER "#version 120\n"
#else
#define GS_GLSL_VERTEX_HEADER "#version 100\n"
#define GS_GLSL_FRAGMENT_HEADER "#version 100\nprecision mediump float;\n"
#endif

static GLuint rtCompileShader(GLenum type, const char *src) {
	GLuint s = glCreateShader(type);
	glShaderSource(s, 1, &src, 0);
	glCompileShader(s);
	GLint ok = 0;
	glGetShaderiv(s, GL_COMPILE_STATUS, &ok);
	if (!ok) { glDeleteShader(s); return 0; }
	return s;
}

static GLuint rtLinkProgram(const char *vsSrc, const char *fsSrc) {
	GLuint vs = rtCompileShader(GL_VERTEX_SHADER, vsSrc);
	GLuint fs = rtCompileShader(GL_FRAGMENT_SHADER, fsSrc);
	if (!vs || !fs) {
		if (vs) glDeleteShader(vs);
		if (fs) glDeleteShader(fs);
		return 0;
	}
	GLuint p = glCreateProgram();
	glAttachShader(p, vs);
	glAttachShader(p, fs);
	glLinkProgram(p);
	glDeleteShader(vs);
	glDeleteShader(fs);
	GLint ok = 0;
	glGetProgramiv(p, GL_LINK_STATUS, &ok);
	if (!ok) { glDeleteProgram(p); return 0; }
	return p;
}

static GLuint rtCreateBlitProgram() {
	const char *vs =
		GS_GLSL_VERTEX_HEADER
		"attribute vec2 pos;\n"
		"varying vec2 uv;\n"
		"void main() {\n"
		"  uv = pos * 0.5 + 0.5;\n"
		"  gl_Position = vec4(pos, 0.0, 1.0);\n"
		"}";
	const char *fs =
		GS_GLSL_FRAGMENT_HEADER
		"varying vec2 uv;\n"
		"uniform sampler2D tex;\n"
		"void main() {\n"
		"  gl_FragColor = texture2D(tex, uv);\n"
		"}";
	return rtLinkProgram(vs, fs);
}

static void rtDeleteProgram(GLuint program) {
	if (program) glDeleteProgram(program);
}

static void rtCreateTexture(GLuint *tex) {
	glGenTextures(1, tex);
	glBindTexture(GL_TEXTURE_2D, *tex);
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
	glBindTexture(GL_TEXTURE_2D, 0);
}

static void rtSetNearest(GLuint tex, GLboolean nearest) {
	glBindTexture(GL_TEXTURE_2D, tex);
	GLint filter = nearest ? GL_NEAREST : GL_LINEAR;
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, filter);
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, filter);
	glBindTexture(GL_TEXTURE_2D, 0);
}

static void rtDestroyTexture(GLuint tex) {
	if (tex) glDeleteTextures(1, &tex);
}

// rtResizeTexture reallocates the capture texture's storage. The next
// Capture() call fully overwrites it, so no clear is needed here.
static void rtResizeTexture(GLuint tex, GLsizei w, GLsizei h) {
	GLint oldTexture;
	glGetIntegerv(GL_TEXTURE_BINDING_2D, &oldTexture);
	glBindTexture(GL_TEXTURE_2D, tex);
	glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA, w, h, 0, GL_RGBA, GL_UNSIGNED_BYTE, NULL);
	glBindTexture(GL_TEXTURE_2D, oldTexture);
}

// rtCapture copies a w x h rectangle from the bottom-left corner of the
// currently bound read framebuffer (where projectM always draws its final
// composite, at whatever size was passed to projectm_set_window_size) into
// our texture.
static void rtCapture(GLuint tex, GLsizei w, GLsizei h) {
	glBindTexture(GL_TEXTURE_2D, tex);
	glCopyTexSubImage2D(GL_TEXTURE_2D, 0, 0, 0, 0, 0, w, h);
	glBindTexture(GL_TEXTURE_2D, 0);
}

static void rtBlit(GLuint program, GLuint tex, GLsizei dstW, GLsizei dstH) {
	static const float verts[] = { -1,-1, 1,-1, -1,1, 1,1 };
	glBindFramebuffer(GL_FRAMEBUFFER, 0);
	glViewport(0, 0, dstW, dstH);
	glDisable(GL_DEPTH_TEST);
	glDisable(GL_BLEND);
	glUseProgram(program);
	glActiveTexture(GL_TEXTURE0);
	glBindTexture(GL_TEXTURE_2D, tex);
	glUniform1i(glGetUniformLocation(program, "tex"), 0);
	GLint pos = glGetAttribLocation(program, "pos");
	glVertexAttribPointer(pos, 2, GL_FLOAT, GL_FALSE, 0, verts);
	glEnableVertexAttribArray(pos);
	glDrawArrays(GL_TRIANGLE_STRIP, 0, 4);
	glDisableVertexAttribArray(pos);
	glUseProgram(0);
}

static void rtSeedFeedback(GLuint program, GLuint tex, GLsizei dstW, GLsizei dstH) {
	static const float verts[] = { -1,-1, 1,-1, -1,1, 1,1 };
	glViewport(0, 0, dstW, dstH);
	glDisable(GL_DEPTH_TEST);
	glDisable(GL_BLEND);
	glUseProgram(program);
	glActiveTexture(GL_TEXTURE0);
	glBindTexture(GL_TEXTURE_2D, tex);
	glUniform1i(glGetUniformLocation(program, "tex"), 0);
	GLint pos = glGetAttribLocation(program, "pos");
	glVertexAttribPointer(pos, 2, GL_FLOAT, GL_FALSE, 0, verts);
	glEnableVertexAttribArray(pos);
	glDrawArrays(GL_TRIANGLE_STRIP, 0, 4);
	glDisableVertexAttribArray(pos);
	glUseProgram(0);
}
*/
import "C"

// RenderTarget upscales projectM's reduced-resolution output to fill the screen.
//
// libprojectM always draws into framebuffer 0 at SetWindowSize dimensions.
// We capture that corner with glCopyTexSubImage2D and blit/upscale to the full window.
type RenderTarget struct {
	tex, previousTex, program C.GLuint
	w, h                      int
	nearest                   bool
	showPrevious              bool
}

// NewRenderTarget creates the capture texture and blit shader.
func NewRenderTarget(width, height int) *RenderTarget {
	rt := &RenderTarget{program: C.rtCreateBlitProgram()}
	C.rtCreateTexture(&rt.tex)
	C.rtResizeTexture(rt.tex, C.GLsizei(width), C.GLsizei(height))
	rt.w, rt.h = width, height
	return rt
}

// SetNearest selects nearest-neighbour filtering for the upscale pass.
func (rt *RenderTarget) SetNearest(nearest bool) {
	value := C.GLboolean(0)
	if nearest {
		value = C.GLboolean(1)
	}
	C.rtSetNearest(rt.tex, value)
	rt.nearest = nearest
}

// Resize updates the dimensions for the next Capture call while retaining the
// last image for the first presentation at the new resolution.
func (rt *RenderTarget) Resize(width, height int) {
	if width <= 0 || height <= 0 || rt.tex == 0 || (width == rt.w && height == rt.h) {
		return
	}

	var nextTex C.GLuint
	C.rtCreateTexture(&nextTex)
	C.rtResizeTexture(nextTex, C.GLsizei(width), C.GLsizei(height))
	if rt.nearest {
		C.rtSetNearest(nextTex, C.GLboolean(1))
	}
	if rt.previousTex != 0 {
		C.rtDestroyTexture(rt.previousTex)
	}
	rt.previousTex = rt.tex
	rt.tex = nextTex
	rt.w, rt.h = width, height
	rt.showPrevious = true
}

// Size returns the current render target dimensions.
func (rt *RenderTarget) Size() (int, int) {
	return rt.w, rt.h
}

// Capture copies projectM's just-rendered output into the internal texture.
// Call right after pm.RenderFrame().
func (rt *RenderTarget) Capture() {
	C.rtCapture(rt.tex, C.GLsizei(rt.w), C.GLsizei(rt.h))
}

// SeedFeedback scales the image retained by Resize into the currently bound
// projectM feedback framebuffer.
func (rt *RenderTarget) SeedFeedback() {
	if !rt.showPrevious || rt.previousTex == 0 {
		return
	}
	C.rtSeedFeedback(rt.program, rt.previousTex, C.GLsizei(rt.w), C.GLsizei(rt.h))
}

// BlitToScreen draws the captured texture scaled up to fill the viewport.
func (rt *RenderTarget) BlitToScreen(dstW, dstH int) {
	tex := rt.tex
	if rt.showPrevious && rt.previousTex != 0 {
		tex = rt.previousTex
	}
	C.rtBlit(rt.program, tex, C.GLsizei(dstW), C.GLsizei(dstH))
	if rt.showPrevious {
		C.rtDestroyTexture(rt.previousTex)
		rt.previousTex = 0
		rt.showPrevious = false
	}
}

// Destroy frees the GL resources.
func (rt *RenderTarget) Destroy() {
	C.rtDestroyTexture(rt.tex)
	rt.tex = 0
	C.rtDestroyTexture(rt.previousTex)
	rt.previousTex = 0
	if rt.program != 0 {
		C.rtDeleteProgram(rt.program)
		rt.program = 0
	}
}
