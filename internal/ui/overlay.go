// Package ui injects text into the visualizer's frame feedback.
package ui

import (
	"log/slog"
	"math"
	"time"

	"github.com/dendec/mdpp/internal/player"
	"github.com/veandco/go-sdl2/sdl"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// This file owns Overlay's state, lifecycle and input handling (cursor
// movement, panel focus, page switching). Rendering lives in
// overlay_render.go; GL/cgo calls are isolated in gl.go.

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

// scrollHold tracks key-hold timing for scroll acceleration.
type scrollHold struct {
	holdStart  time.Time
	lastStep   time.Time
	active     bool
	uiPage     UIPage
	focusPanel int
}

// marqueeState tracks the scrolling animation for a single line that doesn't
// fit its panel. One instance per column (left/right); the active page reuses them.
type marqueeState struct {
	tex    uint32
	texW   int
	texH   int
	offset float32
	start  time.Time
}

func (m *marqueeState) reset() {
	m.offset = 0
	m.start = time.Time{}
}

func (m *marqueeState) invalidate(o *Overlay) {
	o.deleteTex(&m.tex)
	m.tex = 0
	m.offset = 0
	m.start = time.Time{}
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
	bitrate      float64
	bpm          float64
	channels     int
	paused       bool
	isTracker    bool
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

	// Scroll acceleration state.
	scrollUp   scrollHold
	scrollDown scrollHold

	// Scroll offsets for library panels.
	albumsScroll int
	tracksScroll int

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

// Update advances animations and handles scroll acceleration. Call every frame.
// gamepadUp/gamepadDown report whether the controller D-Pad up/down button is
// currently held (SDL controller button events don't auto-repeat, unlike
// keyboard events, so this must be polled explicitly).
func (o *Overlay) Update(gamepadUp, gamepadDown bool) {
	o.notif.Update(o.uiVisible)

	if !o.uiVisible || !o.panelEntered {
		return
	}

	state := sdl.GetKeyboardState()
	now := time.Now()

	o.updateScrollHold(&o.scrollUp, state[sdl.SCANCODE_UP] != 0 || gamepadUp, now, o.cursorUp1)
	o.updateScrollHold(&o.scrollDown, state[sdl.SCANCODE_DOWN] != 0 || gamepadDown, now, o.cursorDown1)
	o.updateMarquee(now)
}

// updateScrollHold drives held-key scroll acceleration for a single direction:
// after held is true for 0.2s, step repeatedly at a rate that doubles every
// second (starting at 5 steps/sec), invoking step for each advance.
func (o *Overlay) updateScrollHold(h *scrollHold, held bool, now time.Time, step func()) {
	if !held {
		h.active = false
		return
	}
	if !h.active || h.uiPage != o.uiPage || h.focusPanel != o.focusPanel {
		// New hold — start tracking (initial move already done by CursorUp/Down).
		*h = scrollHold{holdStart: now, lastStep: now, active: true, uiPage: o.uiPage, focusPanel: o.focusPanel}
		return
	}
	elapsed := now.Sub(h.holdStart).Seconds()
	if elapsed < 0.2 {
		return
	}
	rate := 5.0 * math.Pow(2, math.Floor(elapsed))
	stepInterval := 1.0 / rate
	sinceLast := now.Sub(h.lastStep).Seconds()
	if sinceLast < stepInterval {
		return
	}
	steps := int(sinceLast / stepInterval)
	if steps < 1 {
		steps = 1
	}
	for i := 0; i < steps; i++ {
		step()
	}
	h.lastStep = now
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

// ToggleUI shows or hides the full UI.
func (o *Overlay) ToggleUI() {
	if o.uiVisible {
		o.closeInjectPending = true
	}
	o.uiVisible = !o.uiVisible
	if o.uiVisible {
		o.panelEntered = true
		o.focusPanel = 1
		o.uiPage = PageLibrary
		o.markAllDirty()
	}
	slog.Debug("ui visibility", "visible", o.uiVisible)
}

// FocusPlayingTrack points the library page cursor at the given album/track
// and focuses the tracks (right) panel. Called when the playlist screen is
// opened so the cursor starts on the currently playing file.
func (o *Overlay) FocusPlayingTrack(albumIdx, trackIdx int) {
	o.albumCursor = albumIdx
	o.trackCursor = trackIdx
	o.focusPanel = 1
	o.panelEntered = true
	o.albumsDirty = true
	o.tracksDirty = true
}

// NextScreen cycles Library → Settings → Presets → Library.
func (o *Overlay) NextScreen() {
	o.focusPanel = 0
	o.marqueeL.invalidate(o)
	o.marqueeR.invalidate(o)
	switch o.uiPage {
	case PageLibrary:
		o.uiPage = PageSettings
	case PageSettings:
		o.uiPage = PagePresets
		o.syncPresetCursors()
	case PagePresets:
		o.uiPage = PageLibrary
	}
	o.panelEntered = true
	o.settingsEditing = false
	o.markAllDirty()
}

// PrevScreen cycles Library → Presets → Settings → Library.
func (o *Overlay) PrevScreen() {
	o.focusPanel = 0
	o.marqueeL.invalidate(o)
	o.marqueeR.invalidate(o)
	switch o.uiPage {
	case PageLibrary:
		o.uiPage = PagePresets
		o.syncPresetCursors()
	case PagePresets:
		o.uiPage = PageSettings
	case PageSettings:
		o.uiPage = PageLibrary
	}
	o.panelEntered = true
	o.settingsEditing = false
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
	newSize := float64(h) * refFontSize / float64(refHeight)
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

// --- Cursor & panel navigation ---

// CursorUp moves the cursor up by one item. Acceleration is driven by Update().
func (o *Overlay) CursorUp() {
	if !o.panelEntered {
		return
	}
	o.cursorUp1()
}

// cursorUp1 moves the cursor up by one item.
func (o *Overlay) cursorUp1() {
	o.invalidateActiveMarquee()
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

// CursorDown moves the cursor down by one item. Acceleration is driven by Update().
func (o *Overlay) CursorDown() {
	if !o.panelEntered {
		return
	}
	o.cursorDown1()
}

// cursorDown1 moves the cursor down by one item.
func (o *Overlay) cursorDown1() {
	o.invalidateActiveMarquee()
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

// focusPanelBy shifts focusPanel by delta (-1 or +1), clamped to the valid
// [0,1] range, and marks dirty if it actually changed. Shared by every page
// with a two-panel (left/right) layout.
func (o *Overlay) focusPanelBy(delta int, markDirty func()) {
	next := o.focusPanel + delta
	if next < 0 || next > 1 {
		return
	}
	o.focusPanel = next
	o.invalidateActiveMarquee()
	markDirty()
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
		o.focusPanelBy(-1, func() { o.presetsDirty = true })
	default: // Library
		o.focusPanelBy(-1, func() { o.albumsDirty = true; o.tracksDirty = true })
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
		o.focusPanelBy(1, func() { o.presetsDirty = true })
	default: // Library
		o.focusPanelBy(1, func() { o.albumsDirty = true; o.tracksDirty = true })
	}
}

// backToLeftPanel moves focus from the right panel back to the left one.
// Returns true if it handled the back action (focus was on the right panel),
// false if focus was already on the left panel (caller should exit/close).
func (o *Overlay) backToLeftPanel(markDirty func()) bool {
	if o.focusPanel != 1 {
		return false
	}
	o.focusPanel = 0
	markDirty()
	return true
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

	// Album panel selected — switch to tracks panel, let user pick a track.
	if o.focusPanel == 0 {
		o.focusPanel = 1
		o.tracksDirty = true
		return false
	}
	return true // track panel — play the track
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
			if o.backToLeftPanel(func() { o.presetsDirty = true }) {
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

	if o.backToLeftPanel(func() { o.albumsDirty = true; o.tracksDirty = true }) {
		return
	}
	// Close UI.
	if o.uiVisible {
		o.uiVisible = false
		o.panelEntered = false
		o.focusPanel = 0
		o.closeInjectPending = true
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

// syncPresetCursors positions category/preset cursors on the currently
// playing preset and focuses the presets (right) panel, so the Presets page
// opens with the cursor on the active preset — mirroring FocusPlayingTrack
// on the Library page.
func (o *Overlay) syncPresetCursors() {
	key := o.presetName
	if key == "" {
		return
	}
	for ci, cat := range o.presetCategories {
		for pi, p := range cat.Presets {
			if p == key {
				o.presetCategoryCursor = ci
				o.presetCursor = pi
				o.focusPanel = 1
				o.presetsDirty = true
				return
			}
		}
	}
}

// currentCategory returns the currently focused preset category, or nil.
func (o *Overlay) currentCategory() *PresetCat {
	if o.presetCategoryCursor >= len(o.presetCategories) {
		return nil
	}
	return &o.presetCategories[o.presetCategoryCursor]
}

const (
	marqueeDelay   = 1 * time.Second // pause before scrolling starts
	marqueeSpeed   = 60.0            // pixels per second
	marqueePauseAt = 1 * time.Second // pause at end before resetting
)

// updateMarquee advances the marquee scroll offset for both columns.
func (o *Overlay) updateMarquee(now time.Time) {
	o.updateMarqueeCol(&o.marqueeL, now)
	o.updateMarqueeCol(&o.marqueeR, now)
}

func (o *Overlay) updateMarqueeCol(m *marqueeState, now time.Time) {
	if m.tex == 0 || m.texW <= 0 {
		return
	}
	if m.start.IsZero() {
		m.start = now
		return
	}
	elapsed := now.Sub(m.start)
	if elapsed < marqueeDelay {
		return
	}
	scrollTime := elapsed - marqueeDelay
	// drawMarqueeCol clamps this phase to the active panel width.
	m.offset = float32(scrollTime.Seconds()) * marqueeSpeed
}

// invalidateActiveMarquee resets the marquee for the currently focused column.
func (o *Overlay) invalidateActiveMarquee() {
	if o.focusPanel == 0 {
		o.marqueeL.reset()
	} else {
		o.marqueeR.reset()
	}
}
