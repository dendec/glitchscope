// Package ui injects text into the visualizer's frame feedback.
package ui

import (
	"log/slog"
	"math"
	"time"

	"github.com/dendec/mdpp/internal/config"
	"github.com/dendec/mdpp/internal/player"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// This file owns the Overlay struct, its lifecycle (New/Close/Draw/Update)
// and the frame-data setters (Set*) that the app loop feeds each frame.
// Navigation-tree state lives in overlay_nav.go, cursor/input handling and
// page switching in overlay_input.go, theme/color derivation in
// overlay_theme.go, presets model + marquee animation in
// overlay_presets.go. Rendering lives in overlay_render.go; GL/cgo calls are
// isolated in gl.go.

const (
	fadeInDuration = 400 * time.Millisecond
	holdDuration   = 3 * time.Second
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
	allAlbums      []player.Album // raw flat album list as received from SetAlbums
	rootEntries    []navEntry     // library-root navigation rows, built from allAlbums
	rootCursor     int            // saved cursor for the root level while drilled in
	rootScroll     int            // saved scroll for the root level while drilled in
	navStack       []navLevel     // pushed navigation levels (modland format/album drill-down)
	albumEntries   []navEntry     // rows currently shown in the left panel (root or top of navStack)
	albums         []string       // display labels for albumEntries (kept for rendering)
	albumCursor    int
	trackInfos     []player.TrackInfo
	previewEntries []navEntry // right-panel preview rows when the left cursor is on a non-leaf entry
	previewActive  bool       // true when the right panel shows previewEntries instead of trackInfos
	trackCursor    int
	statsLine      string
	position       float64
	duration       float64
	sampleRate     float32
	bitrate        float64
	bpm            float64
	channels       int
	paused         bool
	isTracker      bool
	presetName     string
	playingAlbum   string
	playingTrack   string
	loading        bool  // true while async track load is in progress
	loadPercent    int64 // download progress percent [0..100], -1 = unknown
	focusPanel     int   // 0=albums, 1=tracks

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

	// Scroll acceleration state.
	scrollUp   scrollHold
	scrollDown scrollHold

	// Scroll offsets for library panels.
	albumsScroll int
	tracksScroll int

	// Theme and transparency.
	theme        config.Theme
	transparency float32 // 0.0–1.0

	// Marquee state for left and right columns (shared across pages).
	marqueeL marqueeState
	marqueeR marqueeState

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
	albumsDirty        bool
	tracksDirty        bool
	statsDirty         bool
	bottomDirty        bool
	presetNameDirty    bool
	presetsDirty       bool
	closeInjectPending bool
}

// New creates an Overlay.
func New() *Overlay {
	return &Overlay{
		programText: glCreateTextProgram(),
		programRect: glCreateRectProgram(),
	}
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
	o.marqueeL.invalidate(o)
	o.marqueeR.invalidate(o)
	for i := range o.pageIndicatorTex {
		o.deleteTex(&o.pageIndicatorTex[i])
	}
	glDeleteProgram(o.programText)
	o.programText = 0
	glDeleteProgram(o.programRect)
	o.programRect = 0
	if o.face != nil {
		_ = o.face.Close()
	}
}

// Draw renders either the UI overlay or the notification.
func (o *Overlay) Draw(width, height int) {
	if o.uiVisible {
		o.renderUI(width, height, width, height)
		if o.notif.Visible() && o.notif.Tex() != 0 && !o.notif.Hidden() {
			o.notif.Render(o.programText, width, height)
		}
	} else if o.notif.Visible() && !o.notif.Hidden() && !o.notif.Injected() {
		o.notif.Render(o.programText, width, height)
	}
}

// --- Notification (delegates to Notifier) ---

// ShowTrack begins the fade-in animation for the given track path or notification text.
func (o *Overlay) ShowTrack(path string) {
	o.notif.ShowTrack(path, o.fontSize)
}

// Inject stamps the notification text or closing UI state once into projectM's feedback framebuffer.
func (o *Overlay) Inject(width, height int) {
	if o.closeInjectPending {
		o.closeInjectPending = false
		o.renderUI(o.screenW, o.screenH, width, height)
	}
	if o.uiVisible {
		return
	}
	o.notif.Inject(o.programText, o.screenW, o.screenH, width, height)
}

// ToggleVisibility shows or hides the notification (B button legacy).
func (o *Overlay) ToggleVisibility() {
	o.notif.Toggle()
}

// --- UI mode ---

// SetSettingsRows updates the settings model and marks textures dirty.
func (o *Overlay) SetSettingsRows(rows []SettingRow, cursor int) {
	o.settingsRows = rows
	o.settingsCursor = cursor
	o.settingsDirty = true
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
	o.marqueeL.invalidate(o)
	o.marqueeR.invalidate(o)
}

// SetScreenSize updates screen dimensions and recomputes font size.
// Call when window is created or resized.
func (o *Overlay) SetScreenSize(w, h int) {
	if o.screenW == w && o.screenH == h {
		return
	}
	// The panel width and row clipping width depend on w even when the font
	// size (which depends only on h) is unchanged.
	o.screenW = w
	o.screenH = h
	newSize := math.Round(float64(h) / 30)
	if newSize < 10 {
		newSize = 10
	}
	if o.fontSize != newSize {
		o.fontSize = newSize
		o.rebuildFace()
	}
	o.markAllDirty()
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

// --- Data setters ---

// SetAlbums updates the flat album list (local + modland) and rebuilds the
// library-root navigation rows: local albums are shown directly, modland
// albums are collapsed behind a single "Modland" entry that drills into
// formats, then albums, then tracks. Marks dirty as needed.
func (o *Overlay) SetAlbums(albums []player.Album, cursor int) {
	listChanged := len(o.allAlbums) != len(albums)
	if !listChanged {
		for i := range albums {
			if o.allAlbums[i].Name != albums[i].Name || o.allAlbums[i].Path != albums[i].Path {
				listChanged = true
				break
			}
		}
	}
	if listChanged {
		o.allAlbums = albums
		o.rootEntries = o.buildRootEntries()
		// Library rescanned — any drill-down position is now stale.
		o.navStack = nil
	}
	// While the UI is open, the overlay cursor is independent of the
	// library's currently playing album. When hidden, keep it synchronized.
	// Only meaningful at the root level; a cursor deep in a modland
	// drill-down is left untouched.
	if (listChanged || !o.uiVisible) && len(o.navStack) == 0 {
		o.albumCursor = o.rootIndexOf(cursor)
	}
	if listChanged {
		o.refreshAlbumLabels()
	}
	o.refreshPreview()
	o.albumsDirty = true
}

// SetTrackInfos updates the track list and marks dirty.
func (o *Overlay) SetTrackInfos(infos []player.TrackInfo, cursor int) {
	o.trackInfos = infos
	o.trackCursor = cursor
	o.tracksDirty = true
}

// SetPlayback updates playback state.
func (o *Overlay) SetPlayback(pos, dur float64, sr float32, bitrate, bpm float64, ch int, paused, isTracker bool) {
	if o.position != pos || o.duration != dur || o.paused != paused ||
		o.sampleRate != sr || o.bitrate != bitrate || o.bpm != bpm || o.channels != ch || o.isTracker != isTracker {
		o.bottomDirty = true
	}
	o.position = pos
	o.duration = dur
	o.sampleRate = sr
	o.bitrate = bitrate
	o.bpm = bpm
	o.channels = ch
	o.paused = paused
	o.isTracker = isTracker
}

// SetPlaying identifies the currently playing album and track.
func (o *Overlay) SetPlaying(album, track string) {
	if o.playingAlbum != album || o.playingTrack != track {
		o.playingAlbum = album
		o.playingTrack = track
		o.albumsDirty = true
		o.tracksDirty = true
		o.bottomDirty = true
	}
}

// SetLoading updates the async-load indicator. active is true while a
// background load is in flight; percent is the download progress [0..100]
// or -1 when unknown (decode phase, or no download).
func (o *Overlay) SetLoading(active bool, percent int64) {
	if o.loading != active || o.loadPercent != percent {
		o.loading = active
		o.loadPercent = percent
		o.bottomDirty = true
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

// SettingsRows returns the current settings rows.
func (o *Overlay) SettingsRows() []SettingRow { return o.settingsRows }
