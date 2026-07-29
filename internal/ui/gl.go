// Package ui injects text into the visualizer's frame feedback.
package ui

/*
#cgo LDFLAGS: -lGLESv2
#include <GLES2/gl2.h>
#include <GLES2/gl2ext.h>

static unsigned int createTextProgram();
static unsigned int createRectProgram();

static unsigned int compileShader(unsigned int type, const char *source) {
	GLuint shader = glCreateShader(type);
	glShaderSource(shader, 1, &source, 0);
	glCompileShader(shader);
	GLint ok = 0;
	glGetShaderiv(shader, GL_COMPILE_STATUS, &ok);
	if (!ok) { glDeleteShader(shader); return 0; }
	return shader;
}

static unsigned int createProgram(const char *vs, const char *fs) {
	GLuint vertex = compileShader(GL_VERTEX_SHADER, vs);
	GLuint fragment = compileShader(GL_FRAGMENT_SHADER, fs);
	if (!vertex || !fragment) {
		if (vertex) glDeleteShader(vertex);
		if (fragment) glDeleteShader(fragment);
		return 0;
	}
	GLuint program = glCreateProgram();
	glAttachShader(program, vertex);
	glAttachShader(program, fragment);
	glLinkProgram(program);
	glDeleteShader(vertex);
	glDeleteShader(fragment);
	GLint ok = 0;
	glGetProgramiv(program, GL_LINK_STATUS, &ok);
	if (!ok) { glDeleteProgram(program); return 0; }
	return program;
}

static void beginDraw(int w, int h) {
	glViewport(0, 0, w, h);
	glDisable(GL_DEPTH_TEST);
	glEnable(GL_BLEND);
	glBlendFunc(GL_SRC_ALPHA, GL_ONE_MINUS_SRC_ALPHA);
}

static void endDraw() {
	glUseProgram(0);
	glDisable(GL_BLEND);
}

static void drawText(unsigned int program, unsigned int text, float opacity,
	float x, float y, float textWidth, float textHeight, int winW, int winH, int viewW, int viewH) {
	beginDraw(viewW, viewH);
	glUseProgram(program);
	glActiveTexture(GL_TEXTURE0);
	glBindTexture(GL_TEXTURE_2D, text);
	glUniform1i(glGetUniformLocation(program, "text"), 0);
	glUniform1f(glGetUniformLocation(program, "opacity"), opacity);
	float left = x / (float)winW * 2.0f - 1.0f;
	float right = (x + textWidth) / (float)winW * 2.0f - 1.0f;
	// Go layout coordinates use a top-left origin; OpenGL uses bottom-left.
	float bottom = ((float)winH - y - textHeight) / (float)winH * 2.0f - 1.0f;
	float top = ((float)winH - y) / (float)winH * 2.0f - 1.0f;
	float verts[] = { left,bottom, right,bottom, left,top, right,top };
	float uvs[]   = { 0,1, 1,1, 0,0, 1,0 };
	GLuint pos = (GLuint)glGetAttribLocation(program, "pos");
	glVertexAttribPointer(pos, 2, GL_FLOAT, GL_FALSE, 0, verts);
	glEnableVertexAttribArray(pos);
	GLuint tc = (GLuint)glGetAttribLocation(program, "tc");
	if (tc != (GLuint)-1) {
		glVertexAttribPointer(tc, 2, GL_FLOAT, GL_FALSE, 0, uvs);
		glEnableVertexAttribArray(tc);
	}
	glDrawArrays(GL_TRIANGLE_STRIP, 0, 4);
	glDisableVertexAttribArray(pos);
	if (tc != (GLuint)-1) glDisableVertexAttribArray(tc);
	endDraw();
}

static void drawOverlayText(unsigned int program, unsigned int text, float opacity,
	float x, float y, float textWidth, float textHeight, int winW, int winH, int viewW, int viewH) {
	drawText(program, text, opacity, x, y, textWidth, textHeight, winW, winH, viewW, viewH);
}

static void drawOverlayTextClipped(unsigned int program, unsigned int text, float opacity,
	float x, float y, float textWidth, float textHeight,
	float clipX, float clipY, float clipW, float clipH,
	int winW, int winH, int viewW, int viewH) {
	glEnable(GL_SCISSOR_TEST);
	// scissor uses bottom-left origin; clipX/clipY are top-left screen coords
	glScissor((GLint)clipX, (GLint)((float)winH - clipY - clipH), (GLsizei)clipW, (GLsizei)clipH);
	drawText(program, text, opacity, x, y, textWidth, textHeight, winW, winH, viewW, viewH);
	glDisable(GL_SCISSOR_TEST);
}

static void drawFilledRect(unsigned int program,
	float x, float y, float w, float h, float r, float g, float b, float a,
	int winW, int winH, int viewW, int viewH) {
	beginDraw(viewW, viewH);
	glUseProgram(program);
	float left = x / (float)winW * 2.0f - 1.0f;
	float right = (x + w) / (float)winW * 2.0f - 1.0f;
	float bottom = ((float)winH - y - h) / (float)winH * 2.0f - 1.0f;
	float top = ((float)winH - y) / (float)winH * 2.0f - 1.0f;
	float verts[] = { left,bottom, right,bottom, left,top, right,top };
	glUniform4f(glGetUniformLocation(program, "uColor"), r, g, b, a);
	GLuint pos = (GLuint)glGetAttribLocation(program, "pos");
	glVertexAttribPointer(pos, 2, GL_FLOAT, GL_FALSE, 0, verts);
	glEnableVertexAttribArray(pos);
	glDrawArrays(GL_TRIANGLE_STRIP, 0, 4);
	glDisableVertexAttribArray(pos);
	endDraw();
}

static unsigned int createTextProgram() {
	const char *vs =
		"#version 100\n"
		"attribute vec2 pos;\n"
		"attribute vec2 tc;\n"
		"varying vec2 uv;\n"
		"void main() { uv = tc; gl_Position = vec4(pos, 0.0, 1.0); }";
	const char *fs =
		"#version 100\n"
		"precision mediump float;\n"
		"uniform sampler2D text;\n"
		"uniform float opacity;\n"
		"varying vec2 uv;\n"
		"void main() {\n"
		"  vec4 glyph = texture2D(text, uv);\n"
		"  gl_FragColor = vec4(glyph.rgb, glyph.a * opacity);\n"
		"}";
	return createProgram(vs, fs);
}

static unsigned int createRectProgram() {
	const char *vs =
		"#version 100\n"
		"attribute vec2 pos;\n"
		"void main() { gl_Position = vec4(pos, 0.0, 1.0); }";
	const char *fs =
		"#version 100\n"
		"precision mediump float;\n"
		"uniform vec4 uColor;\n"
		"void main() { gl_FragColor = uColor; }";
	return createProgram(vs, fs);
}

static unsigned int texUpload(unsigned char *pixels, int w, int h) {
	GLuint tex;
	glGenTextures(1, &tex);
	glBindTexture(GL_TEXTURE_2D, tex);
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
	glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA, w, h, 0, GL_RGBA, GL_UNSIGNED_BYTE, pixels);
	return tex;
}

static void texDelete(unsigned int tex) {
	GLuint t = tex;
	glDeleteTextures(1, &t);
}
*/
import "C"
import (
	"image"
	"unsafe"
)

// This file is the single point of contact with cgo/GLES. Every other file
// calls these Go wrappers instead of touching "C" directly.

func glCreateTextProgram() uint32 { return uint32(C.createTextProgram()) }
func glCreateRectProgram() uint32 { return uint32(C.createRectProgram()) }

func glDeleteProgram(program uint32) {
	if program != 0 {
		C.glDeleteProgram(C.uint(program))
	}
}

func glIsTexture(tex uint32) bool {
	return tex != 0 && C.glIsTexture(C.uint(tex)) != 0
}

func glDrawFilledRect(program uint32, x, y, w, h, r, g, b, a float32, winW, winH, viewW, viewH int) {
	C.drawFilledRect(C.uint(program), C.float(x), C.float(y), C.float(w), C.float(h),
		C.float(r), C.float(g), C.float(b), C.float(a), C.int(winW), C.int(winH), C.int(viewW), C.int(viewH))
}

func glDrawOverlayText(program, tex uint32, opacity float32, x, y, w, h float32, winW, winH, viewW, viewH int) {
	C.drawOverlayText(C.uint(program), C.uint(tex), C.float(opacity),
		C.float(x), C.float(y), C.float(w), C.float(h), C.int(winW), C.int(winH), C.int(viewW), C.int(viewH))
}

func glDrawOverlayTextClipped(program, tex uint32, opacity float32, x, y, w, h, clipX, clipY, clipW, clipH float32, winW, winH, viewW, viewH int) {
	C.drawOverlayTextClipped(C.uint(program), C.uint(tex), C.float(opacity),
		C.float(x), C.float(y), C.float(w), C.float(h),
		C.float(clipX), C.float(clipY), C.float(clipW), C.float(clipH),
		C.int(winW), C.int(winH), C.int(viewW), C.int(viewH))
}

func glDrawText(program, tex uint32, opacity float32, x, y, w, h float32, winW, winH, viewW, viewH int) {
	C.drawText(C.uint(program), C.uint(tex), C.float(opacity),
		C.float(x), C.float(y), C.float(w), C.float(h), C.int(winW), C.int(winH), C.int(viewW), C.int(viewH))
}

func glDeleteTex(tex uint32) {
	if tex != 0 {
		C.texDelete(C.uint(tex))
	}
}

// glUploadTexture uploads an RGBA image as a GL texture.
func glUploadTexture(rgba *image.RGBA) uint32 {
	if len(rgba.Pix) == 0 {
		return 0
	}
	pix := (*C.uchar)(unsafe.Pointer(&rgba.Pix[0]))
	return uint32(C.texUpload(pix, C.int(rgba.Rect.Dx()), C.int(rgba.Rect.Dy())))
}
