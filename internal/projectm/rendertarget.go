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

static GLuint rtCreateFBO() {
	GLuint fbo;
	glGenFramebuffers(1, &fbo);
	return fbo;
}

// rtResizeTexture allocates texture storage and clears it through a complete FBO.
// The caller owns the GL context; all modified GL state is restored before return.
static int rtResizeTextureStorage(GLuint tex, GLuint fbo, GLsizei w, GLsizei h) {
	GLint oldFbo, oldTexture, oldViewport[4], oldScissorBox[4];
	GLboolean oldScissor, oldColorMask[4];
	GLfloat oldClearColor[4];

	glGetIntegerv(GL_FRAMEBUFFER_BINDING, &oldFbo);
	glGetIntegerv(GL_TEXTURE_BINDING_2D, &oldTexture);
	glGetIntegerv(GL_VIEWPORT, oldViewport);
	glGetIntegerv(GL_SCISSOR_BOX, oldScissorBox);
	glGetBooleanv(GL_SCISSOR_TEST, &oldScissor);
	glGetBooleanv(GL_COLOR_WRITEMASK, oldColorMask);
	glGetFloatv(GL_COLOR_CLEAR_VALUE, oldClearColor);

	glBindTexture(GL_TEXTURE_2D, tex);
	glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA, w, h, 0, GL_RGBA, GL_UNSIGNED_BYTE, NULL);
	glBindFramebuffer(GL_FRAMEBUFFER, fbo);
	glFramebufferTexture2D(GL_FRAMEBUFFER, GL_COLOR_ATTACHMENT0, GL_TEXTURE_2D, tex, 0);
	GLboolean complete = glCheckFramebufferStatus(GL_FRAMEBUFFER) == GL_FRAMEBUFFER_COMPLETE;
	if (complete) {
		glDisable(GL_SCISSOR_TEST);
		glColorMask(GL_TRUE, GL_TRUE, GL_TRUE, GL_TRUE);
		glViewport(0, 0, w, h);
		glClearColor(0, 0, 0, 0);
		glClear(GL_COLOR_BUFFER_BIT);
	}

	glColorMask(oldColorMask[0], oldColorMask[1], oldColorMask[2], oldColorMask[3]);
	glClearColor(oldClearColor[0], oldClearColor[1], oldClearColor[2], oldClearColor[3]);
	glScissor(oldScissorBox[0], oldScissorBox[1], oldScissorBox[2], oldScissorBox[3]);
	glViewport(oldViewport[0], oldViewport[1], oldViewport[2], oldViewport[3]);
	glBindFramebuffer(GL_FRAMEBUFFER, oldFbo);
	glBindTexture(GL_TEXTURE_2D, oldTexture);
	if (oldScissor) {
		glEnable(GL_SCISSOR_TEST);
	} else {
		glDisable(GL_SCISSOR_TEST);
	}
	return complete ? 1 : 0;
}

static void rtDeleteFBO(GLuint fbo) {
	if (fbo) glDeleteFramebuffers(1, &fbo);
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
*/
import "C"

// RenderTarget upscales projectM's reduced-resolution output to fill the screen.
//
// libprojectM always draws into framebuffer 0 at SetWindowSize dimensions.
// We capture that corner with glCopyTexSubImage2D and blit/upscale to the full window.
type RenderTarget struct {
	tex, fbo, program C.GLuint
	w, h              int
}

// NewRenderTarget creates the capture texture and blit shader.
func NewRenderTarget(width, height int) *RenderTarget {
	rt := &RenderTarget{program: C.rtCreateBlitProgram(), fbo: C.rtCreateFBO()}
	C.rtCreateTexture(&rt.tex)
	if C.rtResizeTextureStorage(rt.tex, rt.fbo, C.GLsizei(width), C.GLsizei(height)) == 0 {
		C.rtDeleteFBO(rt.fbo)
		rt.fbo = 0
	} else {
		rt.w, rt.h = width, height
	}
	return rt
}

// SetNearest selects nearest-neighbour filtering for the upscale pass.
func (rt *RenderTarget) SetNearest(nearest bool) {
	value := C.GLboolean(0)
	if nearest {
		value = C.GLboolean(1)
	}
	C.rtSetNearest(rt.tex, value)
}

// Resize updates the dimensions for the next Capture call.
func (rt *RenderTarget) Resize(width, height int) {
	if width <= 0 || height <= 0 || rt.tex == 0 {
		return
	}
	if rt.fbo == 0 || C.rtResizeTextureStorage(rt.tex, rt.fbo, C.GLsizei(width), C.GLsizei(height)) == 0 {
		return
	}
	rt.w, rt.h = width, height
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

// BlitToScreen draws the captured texture scaled up to fill the viewport.
func (rt *RenderTarget) BlitToScreen(dstW, dstH int) {
	C.rtBlit(rt.program, rt.tex, C.GLsizei(dstW), C.GLsizei(dstH))
}

// Destroy frees the GL resources.
func (rt *RenderTarget) Destroy() {
	C.rtDestroyTexture(rt.tex)
	rt.tex = 0
	if rt.fbo != 0 {
		C.rtDeleteFBO(rt.fbo)
		rt.fbo = 0
	}
	if rt.program != 0 {
		C.glDeleteProgram(rt.program)
		rt.program = 0
	}
}
