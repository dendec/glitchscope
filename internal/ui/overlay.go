// Package ui injects text into the visualizer's frame feedback.
package ui

/*
#cgo LDFLAGS: -lGLESv2
#include <GLES2/gl2.h>
#include <GLES2/gl2ext.h>

static const char *vertexShaderSource =
	"#version 100\n"
	"attribute vec2 pos;\n"
	"attribute vec2 tc;\n"
	"varying vec2 uv;\n"
	"void main() { uv = tc; gl_Position = vec4(pos, 0.0, 1.0); }";

static const char *fragmentShaderSource =
	"#version 100\n"
	"precision mediump float;\n"
	"uniform sampler2D text;\n"
	"uniform float opacity;\n"
	"varying vec2 uv;\n"
	"void main() {\n"
	"  vec4 glyph = texture2D(text, uv);\n"
	"  gl_FragColor = vec4(glyph.rgb, glyph.a * opacity);\n"
	"}";

static unsigned int compileShader(unsigned int type, const char *source) {
	GLuint shader = glCreateShader(type);
	glShaderSource(shader, 1, &source, 0);
	glCompileShader(shader);
	GLint ok = 0;
	glGetShaderiv(shader, GL_COMPILE_STATUS, &ok);
	if (!ok) {
		glDeleteShader(shader);
		return 0;
	}
	return shader;
}

static unsigned int createProgram() {
	GLuint vertex = compileShader(GL_VERTEX_SHADER, vertexShaderSource);
	GLuint fragment = compileShader(GL_FRAGMENT_SHADER, fragmentShaderSource);
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
	if (!ok) {
		glDeleteProgram(program);
		return 0;
	}
	return program;
}

static void drawText(unsigned int program, unsigned int text, float opacity,
	float x, float y, float textWidth, float textHeight, int w, int h, int flipY) {
	glViewport(0, 0, w, h);
	glDisable(GL_DEPTH_TEST);
	glEnable(GL_BLEND);
	glBlendFunc(GL_SRC_ALPHA, GL_ONE_MINUS_SRC_ALPHA);
	glUseProgram(program);
	glActiveTexture(GL_TEXTURE0);
	glBindTexture(GL_TEXTURE_2D, text);
	glUniform1i(glGetUniformLocation(program, "text"), 0);
	glUniform1f(glGetUniformLocation(program, "opacity"), opacity);
	float left = x / (float)w * 2.0f - 1.0f;
	float right = (x + textWidth) / (float)w * 2.0f - 1.0f;
	float bottom = y / (float)h * 2.0f - 1.0f;
	float top = (y + textHeight) / (float)h * 2.0f - 1.0f;
	float bv = flipY ? 1.0f : 0.0f;
	float tv = flipY ? 0.0f : 1.0f;
	float verts[] = { left,bottom, right,bottom, left,top, right,top };
	float uvs[]   = { 0,bv, 1,bv, 0,tv, 1,tv };
	GLuint pos = (GLuint)glGetAttribLocation(program, "pos");
	glVertexAttribPointer(pos, 2, GL_FLOAT, GL_FALSE, 0, verts);
	glEnableVertexAttribArray(pos);
	GLuint tc = (GLuint)glGetAttribLocation(program, "tc");
	glVertexAttribPointer(tc, 2, GL_FLOAT, GL_FALSE, 0, uvs);
	glEnableVertexAttribArray(tc);
	glDrawArrays(GL_TRIANGLE_STRIP, 0, 4);
	glDisableVertexAttribArray(pos);
	glDisableVertexAttribArray(tc);
	glUseProgram(0);
	glDisable(GL_BLEND);
}

static void drawFeedbackText(unsigned int program, unsigned int text, float opacity,
	float x, float y, float textWidth, float textHeight, int w, int h) {
	drawText(program, text, opacity, x, y, textWidth, textHeight, w, h, 1);
}

static void drawOverlayText(unsigned int program, unsigned int text, float opacity,
	float x, float y, float textWidth, float textHeight, int w, int h) {
	glBindFramebuffer(GL_FRAMEBUFFER, 0);
	drawText(program, text, opacity, x, y, textWidth, textHeight, w, h, 1);
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
	"image/color"
	"log/slog"
	"time"
	"unsafe"

	"github.com/dendec/mdpp/internal/player"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	fadeInDuration = 400 * time.Millisecond
	holdDuration   = 3 * time.Second
	fontSize       = 96.0
)

// Overlay manages text that is stamped into projectM's frame history.
type Overlay struct {
	tex      uint32
	texW     int
	texH     int
	text     string
	alpha    float64
	started  time.Time
	visible  bool
	injected bool
	program  uint32
	hidden   bool // B button toggles — overrides visibility
}

// New creates an Overlay. Font is loaded once; no GL state touched yet.
func New() *Overlay {
	return &Overlay{program: uint32(C.createProgram())}
}

// Draw renders readable text over the completed visualization frame.
func (o *Overlay) Draw(width, height int) {
	if o.hidden || !o.visible || o.tex == 0 || o.program == 0 || width <= 0 || height <= 0 {
		return
	}
	if o.injected {
		return
	}
	x, y, drawWidth, drawHeight := o.layout(width, height)
	C.drawOverlayText(C.uint(o.program), C.uint(o.tex), C.float(o.alpha),
		C.float(x), C.float(y), C.float(drawWidth), C.float(drawHeight),
		C.int(width), C.int(height))
}

// Inject stamps the text once into projectM's bound feedback framebuffer.
func (o *Overlay) Inject(width, height int) {
	if o.hidden || !o.visible || o.injected || o.tex == 0 || o.program == 0 || width <= 0 || height <= 0 {
		return
	}
	if time.Since(o.started) < fadeInDuration+holdDuration {
		return
	}
	x, y, drawWidth, drawHeight := o.layout(width, height)
	C.drawFeedbackText(C.uint(o.program), C.uint(o.tex), 1,
		C.float(x), C.float(y), C.float(drawWidth), C.float(drawHeight),
		C.int(width), C.int(height))
	o.injected = true
}

func (o *Overlay) layout(width, height int) (x, y, drawWidth, drawHeight float32) {
	drawHeight = float32(height) * 0.12
	scale := drawHeight / float32(o.texH)
	drawWidth = float32(o.texW) * scale
	maxWidth := float32(width) * 0.85
	if drawWidth > maxWidth {
		scale = maxWidth / float32(o.texW)
		drawWidth = maxWidth
		drawHeight = float32(o.texH) * scale
	}
	x = (float32(width) - drawWidth) / 2
	y = (float32(height) - drawHeight) / 2
	return x, y, drawWidth, drawHeight
}

// ShowTrack begins the fade-in animation for the given track path.
func (o *Overlay) ShowTrack(path string) {
	if o.hidden {
		return // user toggled overlay off
	}
	f, err := opentype.Parse(unifontData)
	if err != nil {
		slog.Error("font parse", "error", err)
		return
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    fontSize,
		DPI:     72,
		Hinting: font.HintingNone,
	})
	if err != nil {
		slog.Error("font face", "error", err)
		return
	}
	defer func() {
		if err := face.Close(); err != nil {
			slog.Debug("font face close", "error", err)
		}
	}()

	title := player.TrackTitle(path)
	if title == "" {
		return
	}

	// Measure the whole string so descenders such as g, p and y are retained.
	bounds, _ := font.BoundString(face, title)
	const padding = 4
	o.texW = (bounds.Max.X - bounds.Min.X).Ceil() + padding*2
	o.texH = (bounds.Max.Y - bounds.Min.Y).Ceil() + padding*2
	if o.texW == 0 || o.texH == 0 {
		slog.Warn("overlay zero-size text", "text", title)
		return
	}

	// Render to RGBA.
	rgba := image.NewRGBA(image.Rect(0, 0, o.texW, o.texH))
	d := &font.Drawer{
		Dst:  rgba,
		Src:  image.NewUniform(color.RGBA{255, 255, 255, 255}),
		Face: face,
		Dot: fixed.Point26_6{
			X: fixed.I(padding) - bounds.Min.X,
			Y: fixed.I(padding) - bounds.Min.Y,
		},
	}
	d.DrawString(title)

	// Upload to GL (replaces previous texture).
	if o.tex != 0 {
		C.texDelete(C.uint(o.tex))
	}
	o.tex = uploadTexture(rgba)
	if o.tex == 0 {
		slog.Error("overlay texture upload failed")
		return
	}

	o.text = title
	o.started = time.Now()
	o.visible = true
	o.injected = false
	o.alpha = 0
	slog.Info("overlay show", "text", title, "texW", o.texW, "texH", o.texH)
}

// ToggleVisibility shows or hides the overlay (B button).
func (o *Overlay) ToggleVisibility() {
	o.hidden = !o.hidden
	if !o.hidden && o.text != "" {
		// Re-show current text with fresh animation.
		o.visible = true
		o.started = time.Now()
		o.injected = false
		o.alpha = 0
	}
	slog.Debug("overlay visibility", "hidden", o.hidden)
}

// Update advances the fade animation. Call every frame.
func (o *Overlay) Update() {
	if !o.visible {
		return
	}
	if o.injected {
		o.hide()
		return
	}
	elapsed := time.Since(o.started)

	switch {
	case elapsed < fadeInDuration:
		o.alpha = float64(elapsed) / float64(fadeInDuration)
	case elapsed < fadeInDuration+holdDuration:
		o.alpha = 1.0
	default:
		o.alpha = 1.0
	}
}

// Close releases the GL texture if any.
func (o *Overlay) Close() {
	if o.tex != 0 {
		C.texDelete(C.uint(o.tex))
		o.tex = 0
	}
	if o.program != 0 {
		C.glDeleteProgram(C.uint(o.program))
		o.program = 0
	}
}

// --- internal ---

func (o *Overlay) hide() {
	if o.tex != 0 {
		C.texDelete(C.uint(o.tex))
		o.tex = 0
	}
	o.visible = false
	o.text = ""
	o.alpha = 0
}

func uploadTexture(rgba *image.RGBA) uint32 {
	pix := (*C.uchar)(unsafe.Pointer(&rgba.Pix[0]))
	tex := C.texUpload(pix, C.int(rgba.Rect.Dx()), C.int(rgba.Rect.Dy()))
	return uint32(tex)
}
