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
	float x, float y, float textWidth, float textHeight, int w, int h) {
	beginDraw(w, h);
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
	endDraw();
}

static void drawOverlayText(unsigned int program, unsigned int text, float opacity,
	float x, float y, float textWidth, float textHeight, int w, int h) {
	glBindFramebuffer(GL_FRAMEBUFFER, 0);
	drawText(program, text, opacity, x, y, textWidth, textHeight, w, h);
}

static void drawFilledRect(unsigned int program,
	float x, float y, float w, float h, float r, float g, float b, float a,
	int winW, int winH) {
	beginDraw(winW, winH);
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
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"github.com/dendec/mdpp/internal/config"
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

// Accent colour — light blue, used for the playing-track indicator,
// active setting row highlight, and selected value highlight.
const (
	accentR = 0.3
	accentG = 0.8
	accentB = 1.0
	accentA = 0.3

	panelBgAlpha = 0.4
	borderAlpha  = 0.5
	dimAlpha     = 0.3
)

// UIPage selects which screen the overlay shows.
type UIPage int

const (
	PageLibrary UIPage = iota
	PageSettings
	PagePresets
)

// SettingRow describes one line in the settings page.
type SettingRow struct {
	Label  string
	Values []string
	Index  int
}

// PresetCat holds a category name and its preset keys.
type PresetCat struct {
	Name    string
	Presets []string
}

// Overlay manages UI and notification rendering.
type Overlay struct {
	programText uint32
	programRect uint32
	face        font.Face

	// Notification.
	notif Notifier

	// UI mode.
	uiVisible    bool
	panelEntered bool // true = inside panel navigating items, false = choosing panels
	uiPage       UIPage

	// Screen dimensions and computed font size.
	screenW, screenH int
	fontSize         float64

	// UI data (updated each frame from main loop).
	albums       []string
	albumCursor  int
	trackInfos   []player.TrackInfo
	trackCursor  int
	statsLine    string
	position     float64
	duration     float64
	sampleRate   float32
	channels     int
	bpm          float64
	paused       bool
	presetName   string
	playingAlbum string
	playingTrack string
	focusPanel   int // 0=albums, 1=tracks

	// Settings page state.
	settingsRows        []SettingRow
	settingsCursor      int
	settingsValueCursor int
	settingsEditing     bool
	settingsDirty       bool
	settingsColL        listTex // left column: setting names
	settingsColR        listTex // right column: values of the focused setting

	// Presets page state. Uses its own texture columns (not shared with
	// Settings) so the two pages never alias each other's GL textures.
	presetCategories     []PresetCat
	presetCategoryCursor int
	presetCursor         int
	presetsColL          listTex // left column: category names
	presetsColR          listTex // right column: presets in the focused category
	presetsScrollL       int     // first visible row in the left column
	presetsScrollR       int     // first visible row in the right column

	// Cached UI textures.
	albumsTex                      uint32
	albumsTexW, albumsTexH         int
	tracksTex                      uint32
	tracksTexW, tracksTexH         int
	bottomTex                      uint32
	bottomTexW, bottomTexH         int
	statsTex                       uint32
	statsTexW, statsTexH           int
	presetNameTex                  uint32
	presetNameTexW, presetNameTexH int

	// Page indicator textures (one per page).
	pageIndicatorTex   [3]uint32
	pageIndicatorTexW  [3]int
	pageIndicatorTexH  [3]int
	pageIndicatorDirty bool

	// Dirty flags.
	albumsDirty     bool
	tracksDirty     bool
	statsDirty      bool
	bottomDirty     bool
	presetNameDirty bool
	presetsDirty    bool
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
		if o.notif.Visible() && o.notif.Tex() != 0 && !o.notif.Hidden() {
			o.notif.Render(o.programText, width, height)
		}
	} else if o.notif.Visible() && !o.notif.Hidden() && !o.notif.Injected() {
		o.notif.Render(o.programText, width, height)
	}
}

// Update advances animations. Call every frame.
func (o *Overlay) Update() {
	o.notif.Update(o.uiVisible)
}

// --- Notification (delegates to Notifier) ---

// ShowTrack begins the fade-in animation for the given track path.
func (o *Overlay) ShowTrack(path string) {
	o.notif.ShowTrack(path, o.fontSize)
}

// Inject stamps the notification text once into projectM's feedback framebuffer.
func (o *Overlay) Inject(width, height int) {
	if o.uiVisible {
		return
	}
	o.notif.Inject(o.programText, width, height)
}

// ToggleVisibility shows or hides the notification (B button legacy).
func (o *Overlay) ToggleVisibility() {
	o.notif.Toggle()
}

// --- UI mode ---

// ToggleUI shows or hides the full UI.
func (o *Overlay) ToggleUI() {
	o.uiVisible = !o.uiVisible
	if o.uiVisible {
		o.panelEntered = false
		o.focusPanel = 0
		o.uiPage = PageLibrary
		o.markAllDirty()
	}
	slog.Debug("ui visibility", "visible", o.uiVisible)
}

// NextScreen cycles Library → Settings → Presets → Library.
func (o *Overlay) NextScreen() {
	switch o.uiPage {
	case PageLibrary:
		o.uiPage = PageSettings
	case PageSettings:
		o.uiPage = PagePresets
	case PagePresets:
		o.uiPage = PageLibrary
	}
	o.panelEntered = true
	o.settingsEditing = false
	o.focusPanel = 0
	o.markAllDirty()
}

// PrevScreen cycles Library → Presets → Settings → Library.
func (o *Overlay) PrevScreen() {
	switch o.uiPage {
	case PageLibrary:
		o.uiPage = PagePresets
	case PagePresets:
		o.uiPage = PageSettings
	case PageSettings:
		o.uiPage = PageLibrary
	}
	o.panelEntered = true
	o.settingsEditing = false
	o.focusPanel = 0
	o.markAllDirty()
}

// SetSettingsRows updates the settings model and marks textures dirty.
func (o *Overlay) SetSettingsRows(rows []SettingRow, cursor int) {
	o.settingsRows = rows
	o.settingsCursor = cursor
	o.settingsDirty = true
}

// SetPresetCategories updates the presets model for the presets page.
// Only marks textures dirty if the content actually changed, since this may
// be called repeatedly (e.g. once at startup) with the same static data.
func (o *Overlay) SetPresetCategories(cats []PresetCat) {
	if presetCatsEqual(o.presetCategories, cats) {
		return
	}
	o.presetCategories = cats
	o.presetsDirty = true
}

func presetCatsEqual(a, b []PresetCat) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || len(a[i].Presets) != len(b[i].Presets) {
			return false
		}
		for j := range a[i].Presets {
			if a[i].Presets[j] != b[i].Presets[j] {
				return false
			}
		}
	}
	return true
}

// PresetCategoryCursor returns the current category cursor index.
func (o *Overlay) PresetCategoryCursor() int { return o.presetCategoryCursor }

// PresetCursor returns the current preset cursor index.
func (o *Overlay) PresetCursor() int { return o.presetCursor }

// SelectedPresetKey returns the full key of the preset the user selected on the presets page.
// Returns empty string if nothing valid is selected.
func (o *Overlay) SelectedPresetKey() string {
	if o.presetCategoryCursor >= len(o.presetCategories) {
		return ""
	}
	cat := o.presetCategories[o.presetCategoryCursor]
	if o.presetCursor >= len(cat.Presets) {
		return ""
	}
	return cat.Presets[o.presetCursor]
}

// UIVisible returns true if the overlay UI is currently shown.
func (o *Overlay) UIVisible() bool {
	return o.uiVisible
}

// SettingsCursor returns the current settings cursor index.
func (o *Overlay) SettingsCursor() int { return o.settingsCursor }

// IsSettingsPage reports whether the overlay is showing the settings page.
func (o *Overlay) IsSettingsPage() bool { return o.uiPage == PageSettings }

// IsPresetsPage reports whether the overlay is showing the presets page.
func (o *Overlay) IsPresetsPage() bool { return o.uiPage == PagePresets }

// IsSettingsEditing reports whether the user is editing a value (right panel active).
func (o *Overlay) IsSettingsEditing() bool { return o.settingsEditing }

// markAllDirty sets every texture-dirty flag. Used on resize, page open, etc.
func (o *Overlay) markAllDirty() {
	o.albumsDirty = true
	o.tracksDirty = true
	o.bottomDirty = true
	o.statsDirty = true
	o.settingsDirty = true
	o.presetsDirty = true
	o.pageIndicatorDirty = true
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
		o.markAllDirty()
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
	if o.uiPage == PageSettings {
		if o.settingsEditing {
			if o.settingsValueCursor > 0 {
				o.settingsValueCursor--
				o.settingsDirty = true
			}
		} else {
			if o.settingsCursor > 0 {
				o.settingsCursor--
				o.settingsDirty = true
			}
		}
		return
	}
	if o.uiPage == PagePresets {
		switch o.focusPanel {
		case 0:
			if o.presetCategoryCursor > 0 {
				o.presetCategoryCursor--
				o.presetCursor = 0
				o.presetsDirty = true
			}
		case 1:
			if cat := o.currentCategory(); cat != nil && o.presetCursor > 0 {
				o.presetCursor--
				o.presetsDirty = true
			}
		}
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
	if o.uiPage == PageSettings {
		if o.settingsEditing {
			vals := o.settingsRows[o.settingsCursor].Values
			if o.settingsValueCursor < len(vals)-1 {
				o.settingsValueCursor++
				o.settingsDirty = true
			}
		} else {
			if o.settingsCursor < len(o.settingsRows)-1 {
				o.settingsCursor++
				o.settingsDirty = true
			}
		}
		return
	}
	if o.uiPage == PagePresets {
		switch o.focusPanel {
		case 0:
			if o.presetCategoryCursor < len(o.presetCategories)-1 {
				o.presetCategoryCursor++
				o.presetCursor = 0
				o.presetsDirty = true
			}
		case 1:
			if cat := o.currentCategory(); cat != nil && o.presetCursor < len(cat.Presets)-1 {
				o.presetCursor++
				o.presetsDirty = true
			}
		}
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

// FocusLeft switches focus to the previous panel.
func (o *Overlay) FocusLeft() {
	switch o.uiPage {
	case PageSettings:
		if o.panelEntered && o.settingsEditing {
			o.settingsEditing = false
			o.settingsDirty = true
		}
	case PagePresets:
		if o.focusPanel > 0 {
			o.focusPanel--
			o.presetsDirty = true
		}
	default: // Library
		if o.panelEntered {
			return
		}
		o.focusPanel--
		if o.focusPanel < 0 {
			o.focusPanel = 1
		}
		o.albumsDirty = true
		o.tracksDirty = true
	}
}

// FocusRight switches focus to the next panel.
func (o *Overlay) FocusRight() {
	switch o.uiPage {
	case PageSettings:
		if o.panelEntered && !o.settingsEditing {
			o.settingsEditing = true
			if o.settingsCursor < len(o.settingsRows) {
				o.settingsValueCursor = o.settingsRows[o.settingsCursor].Index
			}
			o.settingsDirty = true
		}
	case PagePresets:
		if o.focusPanel < 1 {
			o.focusPanel++
			o.presetsDirty = true
		}
	default: // Library
		if o.panelEntered {
			return
		}
		o.focusPanel = (o.focusPanel + 1) % 2
		o.albumsDirty = true
		o.tracksDirty = true
	}
}

// Select enters the focused panel or confirms item selection.
// Returns true when an item was selected (caller should play / apply).
func (o *Overlay) Select() bool {
	if o.uiPage == PageSettings {
		if !o.panelEntered {
			// Enter settings page: activate left panel.
			o.panelEntered = true
			o.settingsDirty = true
			return false
		}
		if !o.settingsEditing {
			// Enter value editing mode: copy current index to value cursor.
			o.settingsEditing = true
			o.settingsValueCursor = o.settingsRows[o.settingsCursor].Index
			o.settingsDirty = true
			return false
		}
		// Confirm value: apply it.
		o.settingsRows[o.settingsCursor].Index = o.settingsValueCursor
		o.settingsEditing = false
		o.settingsDirty = true
		return true
	}

	if o.uiPage == PagePresets {
		if !o.panelEntered {
			o.panelEntered = true
			o.presetsDirty = true
			return false
		}
		if o.focusPanel == 0 {
			// Category selected — stay entered, switch to presets panel.
			o.focusPanel = 1
			o.presetCursor = 0
			o.presetsDirty = true
			return false
		}
		// Preset selected.
		return o.currentCategory() != nil && len(o.currentCategory().Presets) > 0
	}

	if !o.panelEntered {
		o.panelEntered = true
		o.albumsDirty = true
		o.tracksDirty = true
		return false
	}
	return o.focusPanel <= 1 // albums or tracks — items exist
}

// Back exits item mode or closes UI.
func (o *Overlay) Back() {
	if o.uiPage == PageSettings {
		if o.settingsEditing {
			// Cancel editing: revert value cursor.
			o.settingsEditing = false
			o.settingsDirty = true
			return
		}
		if o.panelEntered {
			o.panelEntered = false
			o.settingsDirty = true
			return
		}
		// Exit settings page.
		o.uiPage = PageLibrary
		o.panelEntered = false
		o.focusPanel = 0
		o.albumsDirty = true
		o.tracksDirty = true
		return
	}

	if o.uiPage == PagePresets {
		if o.panelEntered {
			if o.focusPanel == 1 {
				// In presets panel → go back to categories.
				o.focusPanel = 0
				o.presetsDirty = true
				return
			}
			// In categories panel → exit panel mode.
			o.panelEntered = false
			o.presetsDirty = true
			return
		}
		// Exit presets page.
		o.uiPage = PageLibrary
		o.panelEntered = false
		o.focusPanel = 0
		o.albumsDirty = true
		o.tracksDirty = true
		return
	}

	if o.panelEntered {
		o.panelEntered = false
		o.albumsDirty = true
		o.tracksDirty = true
		return
	}
	// Close UI.
	if o.uiVisible {
		o.uiVisible = false
		o.panelEntered = false
		o.focusPanel = 0
	}
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

// SetStats sets the stats line and marks dirty.
func (o *Overlay) SetStats(line string) {
	if o.statsLine == line {
		return
	}
	o.statsLine = line
	o.statsDirty = true
}

// SetPresetName sets the current preset name and marks dirty.
func (o *Overlay) SetPresetName(name string) {
	if o.presetName == name {
		return
	}
	o.presetName = name
	o.presetNameDirty = true
}

// AlbumCursor returns the current album cursor index.
func (o *Overlay) AlbumCursor() int { return o.albumCursor }

// TrackCursor returns the current track cursor index.
func (o *Overlay) TrackCursor() int { return o.trackCursor }

// FocusPanel returns the focused panel (0=albums, 1=tracks).
func (o *Overlay) FocusPanel() int { return o.focusPanel }

// SettingsRows returns the current settings rows.
func (o *Overlay) SettingsRows() []SettingRow { return o.settingsRows }

// currentCategory returns the currently focused preset category, or nil.
func (o *Overlay) currentCategory() *PresetCat {
	if o.presetCategoryCursor >= len(o.presetCategories) {
		return nil
	}
	return &o.presetCategories[o.presetCategoryCursor]
}

// --- UI rendering ---

func (o *Overlay) renderUI(w, h int) {
	if o.programRect == 0 {
		return
	}

	// Full screen dim.
	C.drawFilledRect(C.uint(o.programRect), 0, 0, C.float(w), C.float(h), 0, 0, 0, dimAlpha, C.int(w), C.int(h))

	// Stats line (FPS, MEM, CPU, GPU).
	if o.statsDirty {
		o.rebuildStatsTex()
	}
	if o.statsTex != 0 {
		C.drawOverlayText(C.uint(o.programText), C.uint(o.statsTex), 1,
			5, 0, C.float(o.statsTexW), C.float(o.statsTexH), C.int(w), C.int(h))
	}
	// Preset name — below stats.
	if o.presetNameDirty {
		o.rebuildPresetNameTex()
	}
	if o.presetNameTex != 0 {
		C.drawOverlayText(C.uint(o.programText), C.uint(o.presetNameTex), 1,
			C.float(5), C.float(o.statsTexH+4), C.float(o.presetNameTexW), C.float(o.presetNameTexH), C.int(w), C.int(h))
	}

	// Page indicator.
	o.renderPageIndicator(w, h)

	// Layout constants — proportional to font size.
	bottomH := int(o.fontSize * 3.2)
	indicatorH := int(o.fontSize * 1.5)
	panelY := int(o.fontSize*2) + indicatorH
	panelH := h - panelY - bottomH - int(o.fontSize*0.7)
	if panelH < 0 {
		panelH = 0
	}
	thirdW := w / 3

	lh := o.face.Metrics().Height.Ceil()

	switch o.uiPage {
	case PageSettings:
		o.renderSettingsPanels(w, h, thirdW, panelY, panelH, lh)
	case PagePresets:
		o.renderPresetsPanels(w, h, thirdW, panelY, panelH, lh)
	default:
		o.renderLibraryPanels(w, h, thirdW, panelY, panelH, lh)
	}

	// --- Bottom bar (full width) ---
	if o.bottomDirty {
		o.rebuildBottomTex(w, bottomH)
	}
	if o.bottomTex != 0 {
		by := C.float(h - bottomH)
		C.drawFilledRect(C.uint(o.programRect), 0, by, C.float(w), C.float(bottomH),
			0, 0, 0, 0.5, C.int(w), C.int(h))
		C.drawOverlayText(C.uint(o.programText), C.uint(o.bottomTex), 1,
			0, by, C.float(o.bottomTexW), C.float(o.bottomTexH), C.int(w), C.int(h))
	}
}

// renderLibraryPanels draws the albums (left) and tracks (right) panels.
func (o *Overlay) renderLibraryPanels(w, h int, thirdW, panelY, panelH, lh int) {
	// --- Albums panel ---
	if o.albumsDirty {
		o.rebuildAlbumsTex(thirdW, panelH)
	}
	if o.albumsTex != 0 {
		px := C.float(0)
		py := C.float(panelY)
		drawPanelBg(o, px, py, C.float(thirdW), C.float(panelH), C.int(w), C.int(h))
		if o.focusPanel == 0 {
			drawPanelBorder(o, px, py, C.float(thirdW), C.float(panelH), C.int(w), C.int(h))
		}
		C.drawOverlayText(C.uint(o.programText), C.uint(o.albumsTex), 1,
			px, py, C.float(o.albumsTexW), C.float(o.albumsTexH), C.int(w), C.int(h))
		if o.panelEntered && o.focusPanel == 0 && len(o.albums) > 0 {
			hiY := py + C.float(o.albumCursor*lh+2)
			drawAccentHighlight(o, px+2, hiY, C.float(thirdW-4), C.float(lh), C.int(w), C.int(h))
		}
	}

	// --- Tracks panel ---
	if o.tracksDirty {
		o.rebuildTracksTex(thirdW, panelH)
	}
	if o.tracksTex != 0 {
		tx := C.float(w * 2 / 3)
		ty := C.float(panelY)
		drawPanelBg(o, tx, ty, C.float(thirdW), C.float(panelH), C.int(w), C.int(h))
		if o.focusPanel == 1 {
			drawPanelBorder(o, tx, ty, C.float(thirdW), C.float(panelH), C.int(w), C.int(h))
		}
		C.drawOverlayText(C.uint(o.programText), C.uint(o.tracksTex), 1,
			tx, ty, C.float(o.tracksTexW), C.float(o.tracksTexH), C.int(w), C.int(h))
		if o.panelEntered && o.focusPanel == 1 && len(o.trackInfos) > 0 {
			hiY := ty + C.float(o.trackCursor*lh+2)
			drawAccentHighlight(o, tx+2, hiY, C.float(thirdW-4), C.float(lh), C.int(w), C.int(h))
		}
	}
}

// renderSettingsPanels draws the settings labels (left) and values (right).
func (o *Overlay) renderSettingsPanels(w, h int, thirdW, panelY, panelH, lh int) {
	if !o.settingsDirty {
		// Still draw textures from cache.
		o.drawSettingsTextures(w, h, thirdW, panelY, panelH, lh)
		return
	}
	o.settingsDirty = false

	// Rebuild left column texture (setting names).
	var leftLines []string
	for i, row := range o.settingsRows {
		prefix := "  "
		if i == o.settingsCursor && o.panelEntered {
			prefix = "▸ "
		}
		leftLines = append(leftLines, prefix+row.Label)
	}
	o.rebuildListTex(&o.settingsColL, leftLines)

	// Rebuild right column texture (values for the focused setting).
	var rightLines []string
	if o.settingsCursor < len(o.settingsRows) {
		row := o.settingsRows[o.settingsCursor]
		for i, v := range row.Values {
			mark := "  "
			selIdx := row.Index
			if o.settingsEditing {
				selIdx = o.settingsValueCursor
			}
			if i == selIdx {
				// ponytail: using a Unicode right-pointing triangle as the
				// selection marker; same approach as albums/tracks panels.
				mark = "▸ "
			}
			rightLines = append(rightLines, mark+v)
		}
	}
	o.rebuildListTex(&o.settingsColR, rightLines)

	o.drawSettingsTextures(w, h, thirdW, panelY, panelH, lh)
}

func (o *Overlay) drawSettingsTextures(w, h int, thirdW, panelY, panelH, lh int) {
	lx, ly := C.float(0), C.float(panelY)
	rx, ry := C.float(w*2/3), C.float(panelY)
	colW, colH := C.float(thirdW), C.float(panelH)
	winW, winH := C.int(w), C.int(h)

	leftHighlight := o.panelEntered && len(o.settingsRows) > 0 && !o.settingsEditing
	drawListColumn(o, lx, ly, colW, colH, o.settingsColL, o.panelEntered && !o.settingsEditing,
		o.settingsCursor, leftHighlight, lh, winW, winH)

	rightHighlight := false
	rightRow := 0
	if o.panelEntered && o.settingsEditing && len(o.settingsRows) > 0 {
		row := o.settingsRows[o.settingsCursor]
		if o.settingsValueCursor < len(row.Values) {
			rightHighlight = true
			rightRow = o.settingsValueCursor
		}
	}
	drawListColumn(o, rx, ry, colW, colH, o.settingsColR, o.panelEntered && o.settingsEditing,
		rightRow, rightHighlight, lh, winW, winH)
}

// --- Panel drawing helpers ---

func drawPanelBg(o *Overlay, x, y, w, h C.float, winW, winH C.int) {
	C.drawFilledRect(C.uint(o.programRect), x, y, w, h,
		0, 0, 0, panelBgAlpha, winW, winH)
}

func drawPanelBorder(o *Overlay, x, y, w, h C.float, winW, winH C.int) {
	C.drawFilledRect(C.uint(o.programRect), x-1, y-1, w+2, 1,
		1, 1, 1, borderAlpha, winW, winH)
	C.drawFilledRect(C.uint(o.programRect), x-1, y+h, w+2, 1,
		1, 1, 1, borderAlpha, winW, winH)
	C.drawFilledRect(C.uint(o.programRect), x-1, y-1, 1, h+2,
		1, 1, 1, borderAlpha, winW, winH)
	C.drawFilledRect(C.uint(o.programRect), x+w, y-1, 1, h+2,
		1, 1, 1, borderAlpha, winW, winH)
}

func drawAccentHighlight(o *Overlay, x, y, w, h C.float, winW, winH C.int) {
	C.drawFilledRect(C.uint(o.programRect), x, y, w, h,
		accentR, accentG, accentB, accentA, winW, winH)
}

// listTex caches a rendered text texture for one column of a two-column list
// panel (Settings, Presets, ...). Each page keeps its own instance so pages
// never alias each other's GL textures.
type listTex struct {
	tex  uint32
	w, h int
}

// rebuildListTex re-renders lines into t's cached texture, replacing any
// previous one.
func (o *Overlay) rebuildListTex(t *listTex, lines []string) {
	o.deleteTex(&t.tex)
	t.tex, t.w, t.h = o.renderTextToTex(strings.Join(lines, "\n"), 255, 255, 255, 255)
}

// drawListColumn draws one column of a two-column list panel: background,
// optional focus border, cached text, and an optional accent highlight row.
// Shared by Settings and Presets so their layout/highlight logic stays in
// one place.
func drawListColumn(o *Overlay, x, y, w, h C.float, t listTex, bordered bool,
	highlightRow int, showHighlight bool, lh int, winW, winH C.int) {
	drawPanelBg(o, x, y, w, h, winW, winH)
	if bordered {
		drawPanelBorder(o, x, y, w, h, winW, winH)
	}
	if t.tex == 0 {
		return
	}
	C.drawOverlayText(C.uint(o.programText), C.uint(t.tex), 1, x, y, C.float(t.w), C.float(t.h), winW, winH)
	if showHighlight {
		hiY := y + C.float(highlightRow*lh+2)
		drawAccentHighlight(o, x+2, hiY, w-4, C.float(lh), winW, winH)
	}
}

// --- GL wrapper functions (callable from other files in this package) ---

func glDrawOverlayText(program, tex uint32, opacity float32, x, y, w, h float32, winW, winH int) {
	C.drawOverlayText(C.uint(program), C.uint(tex), C.float(opacity),
		C.float(x), C.float(y), C.float(w), C.float(h), C.int(winW), C.int(winH))
}

func glDrawText(program, tex uint32, opacity float32, x, y, w, h float32, winW, winH int) {
	C.drawText(C.uint(program), C.uint(tex), C.float(opacity),
		C.float(x), C.float(y), C.float(w), C.float(h), C.int(winW), C.int(winH))
}

func glDeleteTex(tex uint32) {
	if tex != 0 {
		C.texDelete(C.uint(tex))
	}
}

func (o *Overlay) rebuildStatsTex() {
	o.statsDirty = false
	o.deleteTex(&o.statsTex)
	o.statsTex, o.statsTexW, o.statsTexH = o.renderTextToTex(o.statsLine, 255, 255, 255, 255)
}

func (o *Overlay) rebuildPresetNameTex() {
	o.presetNameDirty = false
	o.deleteTex(&o.presetNameTex)
	o.presetNameTex, o.presetNameTexW, o.presetNameTexH = o.renderTextToTex(o.presetName, 200, 200, 200, 255)
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

// truncateMiddle shortens s so it fits within maxPx pixels, replacing the
// middle with an ellipsis. Returns s unchanged if it already fits.
func (o *Overlay) truncateMiddle(s string, maxPx int) string {
	if o.face == nil || maxPx <= 0 {
		return s
	}
	if font.MeasureString(o.face, s).Ceil() <= maxPx {
		return s
	}
	const ellipsis = "…"
	runes := []rune(s)
	n := len(runes)
	for keep := (n - 1) / 2; keep > 0; keep-- {
		candidate := string(runes[:keep]) + ellipsis + string(runes[n-keep:])
		if font.MeasureString(o.face, candidate).Ceil() <= maxPx {
			return candidate
		}
	}
	return ellipsis
}

// BuildSettingsRows creates SettingRow entries from the current graphics
// config and window dimensions. Call on page open and on window resize.
func BuildSettingsRows(gs config.GraphicsSettings, winW, winH int) []SettingRow {
	resolutions := config.ComputeResolutions(winW, winH)
	resValues := make([]string, len(resolutions))
	resIndex := 0
	found := false
	for i, r := range resolutions {
		resValues[i] = r.String()
		if !found && r.Width == gs.RenderWidth && r.Height == gs.RenderHeight {
			resIndex = i
			found = true
		}
	}
	if !found && len(resolutions) > 0 {
		closest := config.ClosestResolution(resolutions, config.RenderResolution{Width: gs.RenderWidth, Height: gs.RenderHeight})
		for i, r := range resolutions {
			if r == closest {
				resIndex = i
				break
			}
		}
	}

	filters := config.AllFilters()
	filterValues := make([]string, len(filters))
	filterIndex := 0
	for i, f := range filters {
		filterValues[i] = f.String()
		if f == gs.UpscaleFilter {
			filterIndex = i
		}
	}

	return []SettingRow{
		{Label: "Render resolution", Values: resValues, Index: resIndex},
		{Label: "Upscale filter", Values: filterValues, Index: filterIndex},
	}
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
	o.notif.Hide()
	o.deleteTex(&o.albumsTex)
	o.deleteTex(&o.tracksTex)
	o.deleteTex(&o.bottomTex)
	o.deleteTex(&o.statsTex)
	o.deleteTex(&o.presetNameTex)
	o.deleteTex(&o.settingsColL.tex)
	o.deleteTex(&o.settingsColR.tex)
	o.deleteTex(&o.presetsColL.tex)
	o.deleteTex(&o.presetsColR.tex)
	for i := range o.pageIndicatorTex {
		o.deleteTex(&o.pageIndicatorTex[i])
	}
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

// --- Page indicator ---

func (o *Overlay) renderPageIndicator(w, h int) {
	if o.pageIndicatorDirty {
		o.rebuildPageIndicatorTextures()
	}
	pages := []string{"Library", "Settings", "Presets"}
	gap := int(o.fontSize * 2)
	lh := o.face.Metrics().Height.Ceil()

	// Compute total width.
	totalW := 0
	for i := range pages {
		totalW += o.pageIndicatorTexW[i]
		if i > 0 {
			totalW += gap
		}
	}

	startX := (w - totalW) / 2
	y := int(o.fontSize * 0.5)

	x := startX
	for i := range pages {
		if o.pageIndicatorTex[i] != 0 {
			C.drawOverlayText(C.uint(o.programText), C.uint(o.pageIndicatorTex[i]), 1,
				C.float(x), C.float(y), C.float(o.pageIndicatorTexW[i]), C.float(o.pageIndicatorTexH[i]),
				C.int(w), C.int(h))
		}
		// Accent background for active page.
		if UIPage(i) == o.uiPage {
			drawAccentHighlight(o, C.float(x-4), C.float(y-2), C.float(o.pageIndicatorTexW[i]+8), C.float(lh+4), C.int(w), C.int(h))
		}
		x += o.pageIndicatorTexW[i] + gap
	}
}

func (o *Overlay) rebuildPageIndicatorTextures() {
	o.pageIndicatorDirty = false
	pages := []string{"Library", "Settings", "Presets"}
	for i, name := range pages {
		o.deleteTex(&o.pageIndicatorTex[i])
		label := "  " + name + "  "
		if UIPage(i) == o.uiPage {
			label = "[ " + name + " ]"
		}
		o.pageIndicatorTex[i], o.pageIndicatorTexW[i], o.pageIndicatorTexH[i] =
			o.renderTextToTex(label, 255, 255, 255, 255)
	}
}

// --- Presets page ---

func (o *Overlay) renderPresetsPanels(w, h int, thirdW, panelY, panelH, lh int) {
	texturesValid := o.presetsColL.tex != 0 && o.presetsColR.tex != 0 &&
		C.glIsTexture(C.uint(o.presetsColL.tex)) != 0 &&
		C.glIsTexture(C.uint(o.presetsColR.tex)) != 0
	if !o.presetsDirty && texturesValid {
		o.drawPresetsTextures(w, h, thirdW, panelY, panelH, lh)
		return
	}
	o.presetsDirty = false

	// Reserve room for the prefix marker ("▸ ") and padding on each side.
	const rowPad = 16
	maxTextPx := thirdW - rowPad
	prefixPx := 0
	if o.face != nil {
		prefixPx = font.MeasureString(o.face, "▸ ").Ceil()
	}

	// Only render as many rows as fit in the panel: with hundreds/thousands
	// of presets, rendering every line into one texture can exceed the
	// GPU's max texture size, silently failing and leaving the panel a
	// solid black square. Scroll the window instead of rendering it all.
	maxRows := panelH / lh
	if maxRows < 1 {
		maxRows = 1
	}

	// Left panel — category names.
	o.presetsScrollL = scrollOffset(o.presetsScrollL, o.presetCategoryCursor, len(o.presetCategories), maxRows)
	var leftLines []string
	leftEnd := o.presetsScrollL + maxRows
	if leftEnd > len(o.presetCategories) {
		leftEnd = len(o.presetCategories)
	}
	for i := o.presetsScrollL; i < leftEnd; i++ {
		cat := o.presetCategories[i]
		prefix := "  "
		if i == o.presetCategoryCursor && o.panelEntered {
			prefix = "▸ "
		}
		leftLines = append(leftLines, prefix+o.truncateMiddle(cat.Name, maxTextPx-prefixPx))
	}
	o.rebuildListTex(&o.presetsColL, leftLines)

	// Right panel — presets in current category.
	var rightLines []string
	if cat := o.currentCategory(); cat != nil {
		o.presetsScrollR = scrollOffset(o.presetsScrollR, o.presetCursor, len(cat.Presets), maxRows)
		rightEnd := o.presetsScrollR + maxRows
		if rightEnd > len(cat.Presets) {
			rightEnd = len(cat.Presets)
		}
		for i := o.presetsScrollR; i < rightEnd; i++ {
			p := cat.Presets[i]
			mark := "  "
			if i == o.presetCursor && o.panelEntered && o.focusPanel == 1 {
				mark = "▸ "
			}
			name := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
			rightLines = append(rightLines, mark+o.truncateMiddle(name, maxTextPx-prefixPx))
		}
	} else {
		o.presetsScrollR = 0
	}
	o.rebuildListTex(&o.presetsColR, rightLines)

	o.drawPresetsTextures(w, h, thirdW, panelY, panelH, lh)
}

// scrollOffset returns the first visible row index for a list of totalRows
// items shown maxRows at a time, keeping cursor within the visible window
// while clamping to the list bounds.
func scrollOffset(current, cursor, totalRows, maxRows int) int {
	if totalRows <= maxRows {
		return 0
	}
	if cursor < current {
		current = cursor
	}
	if cursor >= current+maxRows {
		current = cursor - maxRows + 1
	}
	if current > totalRows-maxRows {
		current = totalRows - maxRows
	}
	if current < 0 {
		current = 0
	}
	return current
}

func (o *Overlay) drawPresetsTextures(w, h int, thirdW, panelY, panelH, lh int) {
	lx, ly := C.float(0), C.float(panelY)
	rx, ry := C.float(w*2/3), C.float(panelY)
	colW, colH := C.float(thirdW), C.float(panelH)
	winW, winH := C.int(w), C.int(h)

	leftHighlight := o.panelEntered && o.focusPanel == 0 && len(o.presetCategories) > 0
	drawListColumn(o, lx, ly, colW, colH, o.presetsColL, o.panelEntered && o.focusPanel == 0,
		o.presetCategoryCursor-o.presetsScrollL, leftHighlight, lh, winW, winH)

	rightHighlight := false
	if cat := o.currentCategory(); o.panelEntered && o.focusPanel == 1 && cat != nil && len(cat.Presets) > 0 {
		rightHighlight = true
	}
	drawListColumn(o, rx, ry, colW, colH, o.presetsColR, o.panelEntered && o.focusPanel == 1,
		o.presetCursor-o.presetsScrollR, rightHighlight, lh, winW, winH)
}
