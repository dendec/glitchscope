package projectm

/*
#cgo LDFLAGS: -lGLESv2
#include <GLES2/gl2.h>

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
		"#version 100\n"
		"attribute vec2 pos;\n"
		"varying vec2 uv;\n"
		"void main() {\n"
		"  uv = pos * 0.5 + 0.5;\n"
		"  gl_Position = vec4(pos, 0.0, 1.0);\n"
		"}";
	const char *fs =
		"#version 100\n"
		"precision mediump float;\n"
		"varying vec2 uv;\n"
		"uniform sampler2D tex;\n"
		"void main() {\n"
		"  gl_FragColor = texture2D(tex, uv);\n"
		"}";
	return rtLinkProgram(vs, fs);
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

// rtCapture copies a w x h rectangle from the bottom-left corner of the
// currently bound read framebuffer (where projectM always draws its final
// composite, at whatever size was passed to projectm_set_window_size) into
// our texture.
static void rtCapture(GLuint tex, GLsizei w, GLsizei h) {
	glBindTexture(GL_TEXTURE_2D, tex);
	glCopyTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA, 0, 0, w, h, 0);
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
*/
import "C"

// RenderTarget upscales projectM's reduced-resolution output to fill the
// real screen.
//
// libprojectM's C API always draws its final composite directly into
// framebuffer 0 (the real window framebuffer), at a viewport sized to
// whatever was passed to SetWindowSize — there is no hook to redirect
// rendering into an arbitrary FBO. So instead of rendering off-screen, we
// let projectM render into the bottom-left corner of the screen at reduced
// size, capture that corner into a texture with glCopyTexImage2D right
// after RenderFrame, and then blit/upscale that texture over the whole
// window.
//
// Rendering at a lower resolution cuts per-pixel shader cost (blur, video
// echo, composite/warp color equations) roughly quadratically with the
// linear scale factor. This is the dominant cost for heavy presets — the
// per-pixel mesh (SetMeshSize) only controls the coarse warp vertex grid,
// not the full-screen fragment passes, so lowering it below the library's
// floor of 8x8 has no further effect.
type RenderTarget struct {
	tex, program C.GLuint
	w, h         int
}

// NewRenderTarget creates the capture texture and blit shader.
// Must be called with a current OpenGL context.
func NewRenderTarget(width, height int) *RenderTarget {
	rt := &RenderTarget{program: C.rtCreateBlitProgram()}
	C.rtCreateTexture(&rt.tex)
	rt.w, rt.h = width, height
	return rt
}

// SetNearest selects nearest-neighbour filtering for the upscale pass.
// Linear filtering remains the default when nearest is false.
func (rt *RenderTarget) SetNearest(nearest bool) {
	value := C.GLboolean(0)
	if nearest {
		value = C.GLboolean(1)
	}
	C.rtSetNearest(rt.tex, value)
}

// Resize updates the dimensions used for the next Capture call.
// glCopyTexImage2D reallocates texture storage as needed, so no GL work is
// required here.
func (rt *RenderTarget) Resize(width, height int) {
	rt.w, rt.h = width, height
}

// Size returns the current render target dimensions.
func (rt *RenderTarget) Size() (int, int) {
	return rt.w, rt.h
}

// Capture copies projectM's just-rendered output (bottom-left corner of the
// currently bound framebuffer, sized rt.w x rt.h) into the internal texture.
// Call right after pm.RenderFrame().
func (rt *RenderTarget) Capture() {
	C.rtCapture(rt.tex, C.GLsizei(rt.w), C.GLsizei(rt.h))
}

// BlitToScreen draws the captured texture scaled up to fill a
// dstW x dstH viewport on framebuffer 0.
func (rt *RenderTarget) BlitToScreen(dstW, dstH int) {
	C.rtBlit(rt.program, rt.tex, C.GLsizei(dstW), C.GLsizei(dstH))
}

// Destroy frees the GL resources.
func (rt *RenderTarget) Destroy() {
	C.rtDestroyTexture(rt.tex)
	rt.tex = 0
	if rt.program != 0 {
		C.glDeleteProgram(rt.program)
		rt.program = 0
	}
}
