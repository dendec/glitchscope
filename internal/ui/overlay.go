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

static void drawText(unsigned int program, unsigned int text, float opacity,
	float x, float y, float textWidth, float textHeight, int w, int h) {
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
	// Go layout coordinates use a top-left origin; OpenGL uses bottom-left.
	float bottom = ((float)h - y - textHeight) / (float)h * 2.0f - 1.0f;
	float top = ((float)h - y) / (float)h * 2.0f - 1.0f;
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
	glUseProgram(0);
	glDisable(GL_BLEND);
}

static void drawOverlayText(unsigned int program, unsigned int text, float opacity,
	float x, float y, float textWidth, float textHeight, int w, int h) {
	glBindFramebuffer(GL_FRAMEBUFFER, 0);
	drawText(program, text, opacity, x, y, textWidth, textHeight, w, h);
}

static void drawFeedbackText(unsigned int program, unsigned int text, float opacity,
	float x, float y, float textWidth, float textHeight, int w, int h) {
	drawText(program, text, opacity, x, y, textWidth, textHeight, w, h);
}

static void drawFilledRect(unsigned int program,
	float x, float y, float w, float h, float r, float g, float b, float a,
	int winW, int winH) {
	glViewport(0, 0, winW, winH);
	glDisable(GL_DEPTH_TEST);
	glEnable(GL_BLEND);
	glBlendFunc(GL_SRC_ALPHA, GL_ONE_MINUS_SRC_ALPHA);
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
	glUseProgram(0);
	glDisable(GL_BLEND);
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
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"strings"
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
	refFontSize    = 26.0
	refHeight      = 720
)

// Overlay manages UI and notification rendering.
type Overlay struct {
	programText uint32
	programRect uint32
	face        font.Face

	// Notification state (existing).
	notifTex      uint32
	notifTexW     int
	notifTexH     int
	notifText     string
	notifAlpha    float64
	notifStarted  time.Time
	notifVisible  bool
	notifInjected bool
	notifHidden   bool

	// UI mode.
	uiVisible   bool
	panelEntered bool // true = inside panel navigating items, false = choosing panels

	// Screen dimensions and computed font size.
	screenW, screenH int
	fontSize         float64

	// UI data (updated each frame from main loop).
	albums       []string
	albumCursor  int
	trackInfos   []player.TrackInfo
	trackCursor  int
	position     float64
	duration     float64
	sampleRate   float32
	channels     int
	bpm          float64
	paused       bool
	fps          float64
	playingAlbum string
	playingTrack string
	focusPanel   int // 0=albums, 1=tracks

	// Cached UI textures.
	albumsTex              uint32
	albumsTexW, albumsTexH int
	tracksTex              uint32
	tracksTexW, tracksTexH int
	bottomTex              uint32
	bottomTexW, bottomTexH int
	fpsTex                 uint32
	fpsTexW, fpsTexH       int

	// Dirty flags.
	albumsDirty bool
	tracksDirty bool
	bottomDirty bool
	fpsDirty    bool
}

// New creates an Overlay.
func New() *Overlay {
	return &Overlay{
		programText: uint32(C.createTextProgram()),
		programRect: uint32(C.createRectProgram()),
	}
}

// Draw renders either the UI overlay or the notification.
func (o *Overlay) Draw(width, height int) {
	if o.uiVisible {
		o.renderUI(width, height)
		if o.notifVisible && o.notifTex != 0 && !o.notifHidden {
			o.renderNotification(width, height)
		}
	} else if o.notifVisible && !o.notifHidden && !o.notifInjected {
		o.renderNotification(width, height)
	}
}

// Update advances animations. Call every frame.
func (o *Overlay) Update() {
	if !o.notifVisible {
		return
	}
	if o.notifInjected && !o.uiVisible {
		o.hideNotification()
		return
	}
	elapsed := time.Since(o.notifStarted)
	if elapsed >= fadeInDuration+holdDuration {
		if o.uiVisible {
			o.hideNotification()
		}
		return
	}
	switch {
	case elapsed < fadeInDuration:
		o.notifAlpha = float64(elapsed) / float64(fadeInDuration)
	default:
		o.notifAlpha = 1.0
	}
}

// --- Notification (existing behavior) ---

// ShowTrack begins the fade-in animation for the given track path.
func (o *Overlay) ShowTrack(path string) {
	if o.notifHidden {
		return
	}
	notifSize := o.fontSize * 3.5
	if notifSize < 20 {
		notifSize = 20
	}
	f, err := opentype.Parse(unifontData)
	if err != nil {
		slog.Error("font parse", "error", err)
		return
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    notifSize,
		DPI:     72,
		Hinting: font.HintingNone,
	})
	if err != nil {
		slog.Error("font face", "error", err)
		return
	}
	defer func() { _ = face.Close() }()

	title := player.TrackTitle(path)
	if title == "" {
		return
	}

	bounds, _ := font.BoundString(face, title)
	const padding = 4
	o.notifTexW = (bounds.Max.X - bounds.Min.X).Ceil() + padding*2
	o.notifTexH = (bounds.Max.Y - bounds.Min.Y).Ceil() + padding*2
	if o.notifTexW == 0 || o.notifTexH == 0 {
		return
	}

	rgba := image.NewRGBA(image.Rect(0, 0, o.notifTexW, o.notifTexH))
	d := &font.Drawer{
		Dst:  rgba,
		Src:  image.NewUniform(color.RGBA{255, 255, 255, 255}),
		Face: face,
		Dot:  fixed.Point26_6{X: fixed.I(padding) - bounds.Min.X, Y: fixed.I(padding) - bounds.Min.Y},
	}
	d.DrawString(title)

	if o.notifTex != 0 {
		C.texDelete(C.uint(o.notifTex))
	}
	o.notifTex = uploadTexture(rgba)
	if o.notifTex == 0 {
		return
	}

	o.notifText = title
	o.notifStarted = time.Now()
	o.notifVisible = true
	o.notifInjected = false
	o.notifAlpha = 0
}

func (o *Overlay) renderNotification(width, height int) {
	if o.notifTex == 0 || o.programText == 0 || width <= 0 || height <= 0 {
		return
	}
	x, y, dw, dh := o.notifLayout(width, height)
	C.drawOverlayText(C.uint(o.programText), C.uint(o.notifTex), C.float(o.notifAlpha),
		C.float(x), C.float(y), C.float(dw), C.float(dh),
		C.int(width), C.int(height))
}

func (o *Overlay) notifLayout(width, height int) (x, y, drawWidth, drawHeight float32) {
	drawHeight = float32(height) * 0.12
	scale := drawHeight / float32(o.notifTexH)
	drawWidth = float32(o.notifTexW) * scale
	maxWidth := float32(width) * 0.85
	if drawWidth > maxWidth {
		scale = maxWidth / float32(o.notifTexW)
		drawWidth = maxWidth
		drawHeight = float32(o.notifTexH) * scale
	}
	x = (float32(width) - drawWidth) / 2
	y = (float32(height) - drawHeight) / 2
	return
}

// Inject stamps the notification text once into projectM's feedback framebuffer.
func (o *Overlay) Inject(width, height int) {
	if o.uiVisible || o.notifHidden || o.notifInjected || o.notifTex == 0 || o.programText == 0 || width <= 0 || height <= 0 {
		return
	}
	if time.Since(o.notifStarted) < fadeInDuration+holdDuration {
		return
	}
	x, y, dw, dh := o.notifLayout(width, height)
	C.drawFeedbackText(C.uint(o.programText), C.uint(o.notifTex), 1,
		C.float(x), C.float(y), C.float(dw), C.float(dh),
		C.int(width), C.int(height))
	o.notifInjected = true
}

// ToggleVisibility shows or hides the notification (B button legacy).
func (o *Overlay) ToggleVisibility() {
	o.notifHidden = !o.notifHidden
	if !o.notifHidden && o.notifText != "" {
		o.notifVisible = true
		o.notifStarted = time.Now()
		o.notifInjected = false
		o.notifAlpha = 0
	}
}

func (o *Overlay) hideNotification() {
	if o.notifTex != 0 {
		C.texDelete(C.uint(o.notifTex))
		o.notifTex = 0
	}
	o.notifVisible = false
	o.notifText = ""
	o.notifAlpha = 0
}

// --- UI mode ---

// ToggleUI shows or hides the full UI.
func (o *Overlay) ToggleUI() {
	o.uiVisible = !o.uiVisible
	if o.uiVisible {
		o.panelEntered = false
		o.focusPanel = 0
		o.albumsDirty = true
		o.tracksDirty = true
		o.bottomDirty = true
		o.fpsDirty = true
	}
	slog.Debug("ui visibility", "visible", o.uiVisible)
}

// UIVisible returns true if the overlay UI is currently shown.
func (o *Overlay) UIVisible() bool {
	return o.uiVisible
}

// SetScreenSize updates screen dimensions and recomputes font size.
// Call when window is created or resized.
func (o *Overlay) SetScreenSize(w, h int) {
	if o.screenW == w && o.screenH == h {
		return
	}
	o.screenW = w
	o.screenH = h
	newSize := float64(h) * refFontSize / float64(refHeight)
	if newSize < 10 {
		newSize = 10
	}
	if o.fontSize != newSize {
		o.fontSize = newSize
		o.rebuildFace()
		o.albumsDirty = true
		o.tracksDirty = true
		o.bottomDirty = true
		o.fpsDirty = true
	}
}

func (o *Overlay) rebuildFace() {
	f, err := opentype.Parse(unifontData)
	if err != nil {
		slog.Error("font parse", "error", err)
		return
	}
	if o.face != nil {
		_ = o.face.Close()
		o.face = nil
	}
	o.face, err = opentype.NewFace(f, &opentype.FaceOptions{
		Size:    o.fontSize,
		DPI:     72,
		Hinting: font.HintingNone,
	})
	if err != nil {
		slog.Error("font face", "error", err)
	}
}

// CursorUp moves the cursor up in the active panel. Only works when inside a panel.
func (o *Overlay) CursorUp() {
	if !o.panelEntered {
		return
	}
	switch o.focusPanel {
	case 0:
		if o.albumCursor > 0 {
			o.albumCursor--
			o.trackCursor = 0
			o.tracksDirty = true
			o.albumsDirty = true
		}
	case 1:
		if o.trackCursor > 0 {
			o.trackCursor--
			o.tracksDirty = true
		}
	}
}

// CursorDown moves the cursor down in the active panel. Only works when inside a panel.
func (o *Overlay) CursorDown() {
	if !o.panelEntered {
		return
	}
	switch o.focusPanel {
	case 0:
		if o.albumCursor < len(o.albums)-1 {
			o.albumCursor++
			o.trackCursor = 0
			o.tracksDirty = true
			o.albumsDirty = true
		}
	case 1:
		if o.trackCursor < len(o.trackInfos)-1 {
			o.trackCursor++
			o.tracksDirty = true
		}
	}
}

// FocusLeft switches focus to the previous panel. Only works in panel mode.
func (o *Overlay) FocusLeft() {
	if o.panelEntered {
		return
	}
	o.focusPanel--
	if o.focusPanel < 0 {
		o.focusPanel = 2
	}
	o.albumsDirty = true
	o.tracksDirty = true
}

// FocusRight switches focus to the next panel. Only works in panel mode.
func (o *Overlay) FocusRight() {
	if o.panelEntered {
		return
	}
	o.focusPanel = (o.focusPanel + 1) % 3
	o.albumsDirty = true
	o.tracksDirty = true
}

// Select enters the focused panel or confirms item selection.
// Returns true when an item was selected (caller should play).
func (o *Overlay) Select() bool {
	if !o.panelEntered {
		o.panelEntered = true
		o.albumsDirty = true
		o.tracksDirty = true
		return false
	}
	return o.focusPanel <= 1 // albums or tracks — items exist
}

// Back exits item mode or closes UI.
func (o *Overlay) Back() string {
	if o.panelEntered {
		o.panelEntered = false
		o.albumsDirty = true
		o.tracksDirty = true
		return ""
	}
	// Close UI.
	if o.uiVisible {
		o.uiVisible = false
		o.panelEntered = false
		o.focusPanel = 0
	}
	return ""
}

// --- Data setters ---

// SetAlbums updates the album list and marks dirty.
func (o *Overlay) SetAlbums(names []string, cursor int) {
	listChanged := len(o.albums) != len(names)
	if !listChanged {
		for i := range names {
			if o.albums[i] != names[i] {
				listChanged = true
				break
			}
		}
	}
	o.albums = names
	// While the UI is open, the overlay cursor is independent of the
	// library's currently playing album. When hidden, keep it synchronized.
	if listChanged || !o.uiVisible {
		o.albumCursor = cursor
	}
	o.albumsDirty = true
}

// SetTrackInfos updates the track list and marks dirty.
func (o *Overlay) SetTrackInfos(infos []player.TrackInfo, cursor int) {
	o.trackInfos = infos
	o.trackCursor = cursor
	o.tracksDirty = true
}

// SetPlayback updates playback state.
func (o *Overlay) SetPlayback(pos, dur float64, sr float32, ch int, bpm float64, paused bool) {
	if o.position != pos || o.duration != dur || o.paused != paused {
		o.bottomDirty = true
	}
	o.position = pos
	o.duration = dur
	o.sampleRate = sr
	o.channels = ch
	o.bpm = bpm
	o.paused = paused
}

// SetPlaying identifies the currently playing album and track.
func (o *Overlay) SetPlaying(album, track string) {
	if o.playingAlbum != album || o.playingTrack != track {
		o.playingAlbum = album
		o.playingTrack = track
		o.albumsDirty = true
		o.tracksDirty = true
	}
}

// SetFPS sets the FPS counter value and marks dirty.
func (o *Overlay) SetFPS(fps float64) {
	o.fps = fps
	o.fpsDirty = true
}

// AlbumCursor returns the current album cursor index.
func (o *Overlay) AlbumCursor() int { return o.albumCursor }

// TrackCursor returns the current track cursor index.
func (o *Overlay) TrackCursor() int { return o.trackCursor }

// FocusPanel returns the focused panel (0=albums, 1=tracks).
func (o *Overlay) FocusPanel() int { return o.focusPanel }

// --- UI rendering ---

func (o *Overlay) renderUI(w, h int) {
	if o.programRect == 0 {
		return
	}

	// Full screen dim.
	C.drawFilledRect(C.uint(o.programRect), 0, 0, C.float(w), C.float(h), 0, 0, 0, 0.3, C.int(w), C.int(h))

	// FPS counter.
	if o.fpsDirty {
		o.rebuildFPSTex()
	}
	if o.fpsTex != 0 {
		C.drawOverlayText(C.uint(o.programText), C.uint(o.fpsTex), 1,
			5, 0, C.float(o.fpsTexW), C.float(o.fpsTexH), C.int(w), C.int(h))
	}

	// Layout constants — proportional to font size.
	bottomH := int(o.fontSize * 3.2)
	panelY := int(o.fontSize * 2)
	panelH := h - panelY - bottomH - int(o.fontSize*0.7)
	if panelH < 0 {
		panelH = 0
	}
	thirdW := w / 3

	lh := o.face.Metrics().Height.Ceil()

	// --- Albums panel (left, 1/3 width) ---
	if o.albumsDirty {
		o.rebuildAlbumsTex(thirdW, panelH)
	}
	if o.albumsTex != 0 {
		px := C.float(0)
		py := C.float(panelY)
		// Background — always semi-transparent black.
		C.drawFilledRect(C.uint(o.programRect), px, py, C.float(thirdW), C.float(panelH),
			0, 0, 0, 0.4, C.int(w), C.int(h))
		// Border if focused — 4 thin edges, no inner fill.
		if o.focusPanel == 0 {
			C.drawFilledRect(C.uint(o.programRect), px-1, py-1, C.float(thirdW+2), 1,
				1, 1, 1, 0.5, C.int(w), C.int(h))
			C.drawFilledRect(C.uint(o.programRect), px-1, py+C.float(panelH), C.float(thirdW+2), 1,
				1, 1, 1, 0.5, C.int(w), C.int(h))
			C.drawFilledRect(C.uint(o.programRect), px-1, py-1, 1, C.float(panelH+2),
				1, 1, 1, 0.5, C.int(w), C.int(h))
			C.drawFilledRect(C.uint(o.programRect), px+C.float(thirdW), py-1, 1, C.float(panelH+2),
				1, 1, 1, 0.5, C.int(w), C.int(h))
		}
		// Text
		C.drawOverlayText(C.uint(o.programText), C.uint(o.albumsTex), 1,
			px, py, C.float(o.albumsTexW), C.float(o.albumsTexH), C.int(w), C.int(h))
		// Item highlight
		if o.panelEntered && o.focusPanel == 0 && len(o.albums) > 0 {
			hiY := py + C.float(o.albumCursor*lh+2)
			C.drawFilledRect(C.uint(o.programRect), px+2, hiY, C.float(thirdW-4), C.float(lh),
				0.3, 0.8, 1, 0.3, C.int(w), C.int(h))
		}
	}

	// --- Tracks panel (right, 1/3 width) ---
	if o.tracksDirty {
		o.rebuildTracksTex(thirdW, panelH)
	}
	if o.tracksTex != 0 {
		tx := C.float(w * 2 / 3)
		ty := C.float(panelY)
		C.drawFilledRect(C.uint(o.programRect), tx, ty, C.float(thirdW), C.float(panelH),
			0, 0, 0, 0.4, C.int(w), C.int(h))
		if o.focusPanel == 1 {
			C.drawFilledRect(C.uint(o.programRect), tx-1, ty-1, C.float(thirdW+2), 1,
				1, 1, 1, 0.5, C.int(w), C.int(h))
			C.drawFilledRect(C.uint(o.programRect), tx-1, ty+C.float(panelH), C.float(thirdW+2), 1,
				1, 1, 1, 0.5, C.int(w), C.int(h))
			C.drawFilledRect(C.uint(o.programRect), tx-1, ty-1, 1, C.float(panelH+2),
				1, 1, 1, 0.5, C.int(w), C.int(h))
			C.drawFilledRect(C.uint(o.programRect), tx+C.float(thirdW), ty-1, 1, C.float(panelH+2),
				1, 1, 1, 0.5, C.int(w), C.int(h))
		}
		C.drawOverlayText(C.uint(o.programText), C.uint(o.tracksTex), 1,
			tx, ty, C.float(o.tracksTexW), C.float(o.tracksTexH), C.int(w), C.int(h))
		if o.panelEntered && o.focusPanel == 1 && len(o.trackInfos) > 0 {
			hiY := ty + C.float(o.trackCursor*lh+2)
			C.drawFilledRect(C.uint(o.programRect), tx+2, hiY, C.float(thirdW-4), C.float(lh),
				0.3, 0.8, 1, 0.3, C.int(w), C.int(h))
		}
	}

	// --- Bottom bar (full width) ---
	if o.bottomDirty {
		o.rebuildBottomTex(w, bottomH)
	}
	if o.bottomTex != 0 {
		by := C.float(h - bottomH)
		C.drawFilledRect(C.uint(o.programRect), 0, by, C.float(w), C.float(bottomH),
			0, 0, 0, 0.5, C.int(w), C.int(h))
		if o.focusPanel == 2 {
			C.drawFilledRect(C.uint(o.programRect), -1, by-1, C.float(w+2), 1,
				1, 1, 1, 0.5, C.int(w), C.int(h))
			C.drawFilledRect(C.uint(o.programRect), -1, by+C.float(bottomH), C.float(w+2), 1,
				1, 1, 1, 0.5, C.int(w), C.int(h))
			C.drawFilledRect(C.uint(o.programRect), -1, by-1, 1, C.float(bottomH+2),
				1, 1, 1, 0.5, C.int(w), C.int(h))
			C.drawFilledRect(C.uint(o.programRect), C.float(w), by-1, 1, C.float(bottomH+2),
				1, 1, 1, 0.5, C.int(w), C.int(h))
		}
		C.drawOverlayText(C.uint(o.programText), C.uint(o.bottomTex), 1,
			0, by, C.float(o.bottomTexW), C.float(o.bottomTexH), C.int(w), C.int(h))
	}
}

func (o *Overlay) rebuildFPSTex() {
	o.fpsDirty = false
	o.deleteTex(&o.fpsTex)
	o.fpsTex, o.fpsTexW, o.fpsTexH = o.renderTextToTex(fmt.Sprintf("FPS: %.0f", o.fps), 255, 255, 255, 255)
}

func (o *Overlay) rebuildAlbumsTex(maxW, maxH int) {
	o.albumsDirty = false
	o.deleteTex(&o.albumsTex)

	if len(o.albums) == 0 {
		return
	}

	var lines []string
	for i, name := range o.albums {
		prefix := "  "
		if i == o.albumCursor && o.focusPanel == 0 {
			prefix = "▸ "
		}
		mark := "  "
		if name == o.playingAlbum {
			mark = " ▶"
		}
		lines = append(lines, prefix+name+mark)
	}
	text := strings.Join(lines, "\n")
	o.albumsTex, o.albumsTexW, o.albumsTexH = o.renderTextToTex(text, 255, 255, 255, 255)
}

func (o *Overlay) rebuildTracksTex(maxW, maxH int) {
	o.tracksDirty = false
	o.deleteTex(&o.tracksTex)

	if len(o.trackInfos) == 0 {
		return
	}

	var lines []string
	for i, info := range o.trackInfos {
		prefix := "  "
		if i == o.trackCursor && o.focusPanel == 1 {
			prefix = "▸ "
		}
		dur := formatDuration(info.Duration)
		title := player.TrackTitle(info.Path)
		if info.Path == o.playingTrack {
			lines = append(lines, prefix+title+"  "+dur+" ◀")
		} else {
			lines = append(lines, prefix+title+"  "+dur)
		}
	}
	text := strings.Join(lines, "\n")
	o.tracksTex, o.tracksTexW, o.tracksTexH = o.renderTextToTex(text, 255, 255, 255, 255)
}

func (o *Overlay) rebuildBottomTex(w, botH int) {
	o.bottomDirty = false
	o.deleteTex(&o.bottomTex)

	pos := formatDuration(o.position)
	dur := formatDuration(o.duration)
	status := "▶"
	if o.paused {
		status = "⏸"
	}

	// Progress bar.
	barW := w * 60 / 100
	barStr := renderProgressBar(barW, o.duration, o.position)

	// Tech info.
	tech := fmt.Sprintf("%.0fHz", o.sampleRate)
	if o.channels > 0 {
		tech += fmt.Sprintf(", %dch", o.channels)
	}
	if o.bpm > 0 {
		tech += fmt.Sprintf(", %.0f BPM", o.bpm)
	}

	text := fmt.Sprintf("%s  %s / %s\n%s Play    ⏭ Next    ⏮ Prev\n%s",
		barStr, pos, dur, status, tech)
	o.bottomTex, o.bottomTexW, o.bottomTexH = o.renderTextToTex(text, 255, 255, 255, 255)
}

// --- Helpers ---

func (o *Overlay) deleteTex(tex *uint32) {
	if *tex != 0 {
		C.texDelete(C.uint(*tex))
		*tex = 0
	}
}

func (o *Overlay) renderTextToTex(text string, r, g, b, a byte) (uint32, int, int) {
	if o.face == nil {
		return 0, 0, 0
	}
	lines := strings.Split(text, "\n")
	const pad = 4
	lineHeight := o.face.Metrics().Height.Ceil()
	texW := 0
	for _, line := range lines {
		bounds, _ := font.BoundString(o.face, line)
		if width := (bounds.Max.X - bounds.Min.X).Ceil(); width > texW {
			texW = width
		}
	}
	texW += pad * 2
	texH := lineHeight*len(lines) + pad*2
	if texW <= 0 || texH <= 0 {
		return 0, 0, 0
	}

	rgba := image.NewRGBA(image.Rect(0, 0, texW, texH))
	for i, line := range lines {
		bounds, _ := font.BoundString(o.face, line)
		d := &font.Drawer{
			Dst:  rgba,
			Src:  image.NewUniform(color.RGBA{r, g, b, a}),
			Face: o.face,
			Dot: fixed.Point26_6{
				X: fixed.I(pad) - bounds.Min.X,
				Y: fixed.I(pad+i*lineHeight) - bounds.Min.Y,
			},
		}
		d.DrawString(line)
	}

	tex := uploadTexture(rgba)
	return tex, texW, texH
}

func uploadTexture(rgba *image.RGBA) uint32 {
	if len(rgba.Pix) == 0 {
		return 0
	}
	pix := (*C.uchar)(unsafe.Pointer(&rgba.Pix[0]))
	return uint32(C.texUpload(pix, C.int(rgba.Rect.Dx()), C.int(rgba.Rect.Dy())))
}

func formatDuration(sec float64) string {
	if sec <= 0 {
		return "0:00"
	}
	m := int(sec) / 60
	s := int(sec) % 60
	return fmt.Sprintf("%d:%02d", m, s)
}

func renderProgressBar(pixelWidth int, total, current float64) string {
	if total <= 0 {
		return ""
	}
	filled := int(float64(pixelWidth-2) * current / total)
	if filled < 0 {
		filled = 0
	}
	if filled > pixelWidth-2 {
		filled = pixelWidth - 2
	}
	empty := pixelWidth - 2 - filled
	return "█" + strings.Repeat("█", filled) + strings.Repeat("░", empty) + "█"
}

// Close releases GL resources.
func (o *Overlay) Close() {
	o.deleteTex(&o.notifTex)
	o.deleteTex(&o.albumsTex)
	o.deleteTex(&o.tracksTex)
	o.deleteTex(&o.bottomTex)
	o.deleteTex(&o.fpsTex)
	if o.programText != 0 {
		C.glDeleteProgram(C.uint(o.programText))
		o.programText = 0
	}
	if o.programRect != 0 {
		C.glDeleteProgram(C.uint(o.programRect))
		o.programRect = 0
	}
	if o.face != nil {
		_ = o.face.Close()
	}
}
