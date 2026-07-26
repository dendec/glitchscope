// Package ui injects text into the visualizer's frame feedback.
package ui

import (
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/dendec/mdpp/internal/player"
	"github.com/veandco/go-sdl2/sdl"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// modlandNamePrefix is prepended to modland album names by app.addModlandAlbums
// ("Modland: Format/Author"). Stripped when splitting into format/author for
// hierarchical browsing.
const modlandNamePrefix = "Modland: "

// navEntryKind classifies a row in the library navigation panel.
type navEntryKind int

const (
	entryLocalAlbum   navEntryKind = iota // real album/folder — leaf, has tracks
	entryModlandRoot                      // "Modland" pseudo-folder at the library root
	entryFormat                           // modland format bucket (e.g. "Protracker")
	entryModlandAlbum                     // modland author/album within a format — leaf, has tracks
)

// navEntry is one row shown in the library's left (navigation) panel.
type navEntry struct {
	label    string
	kind     navEntryKind
	albumIdx int    // index into Overlay.allAlbums when kind is a leaf album, else -1
	format   string // set when kind == entryFormat
}

// navLevel is a pushed navigation level (everything below the library root).
type navLevel struct {
	entries []navEntry
	cursor  int
	scroll  int
}

// splitModlandName splits a modland album's display name ("Modland:
// Format/Author") into its format and author parts.
func splitModlandName(name string) (format, author string) {
	n := strings.TrimPrefix(name, modlandNamePrefix)
	if idx := strings.IndexByte(n, '/'); idx >= 0 {
		return n[:idx], n[idx+1:]
	}
	return n, ""
}

func labelsOf(entries []navEntry) []string {
	labels := make([]string, len(entries))
	for i, e := range entries {
		labels[i] = e.label
	}
	return labels
}

// This file owns Overlay's state, lifecycle and input handling (cursor
// movement, panel focus, page switching). Rendering lives in
// overlay_render.go; GL/cgo calls are isolated in gl.go.

const (
	fadeInDuration = 400 * time.Millisecond
	holdDuration   = 3 * time.Second
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
	loading        bool   // true while async track load is in progress
	loadPercent    int64  // download progress percent [0..100], -1 = unknown
	focusPanel     int // 0=albums, 1=tracks

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
// opened so the cursor starts on the currently playing file — including
// deep-linking into the modland format/album drill-down when the playing
// track lives there, so reopening the UI always lands back on it.
func (o *Overlay) FocusPlayingTrack(albumIdx, trackIdx int) {
	o.navStack = nil
	o.refreshAlbumLabels()
	o.trackCursor = trackIdx

	if albumIdx >= 0 && albumIdx < len(o.allAlbums) && strings.HasPrefix(o.allAlbums[albumIdx].Path, player.ModlandPrefix) {
		o.focusModlandAlbum(albumIdx)
	} else {
		o.albumCursor = o.rootIndexOf(albumIdx)
		o.refreshPreview()
	}

	if e := o.currentEntry(); e != nil && (e.kind == entryLocalAlbum || e.kind == entryModlandAlbum) {
		o.focusPanel = 1
	} else {
		o.focusPanel = 0
	}
	o.panelEntered = true
	o.albumsDirty = true
	o.tracksDirty = true
}

// focusModlandAlbum drills the navigation stack down to the format and
// author/album containing allAlbums[albumIdx] (format level, then album
// level), positioning the cursor on the playing album at the bottom. The
// root level is left parked on the "Modland" entry, so popping back out
// lands on it rather than at row 0.
func (o *Overlay) focusModlandAlbum(albumIdx int) {
	o.rootCursor = o.rootModlandIndex()
	o.rootScroll = 0

	format, _ := splitModlandName(o.allAlbums[albumIdx].Name)

	formats := o.buildFormatEntries()
	o.navStack = append(o.navStack, navLevel{entries: formats, cursor: indexOfEntry(formats, func(e navEntry) bool { return e.format == format })})

	albums := o.buildAlbumsInFormatEntries(format)
	albumCursor := indexOfEntry(albums, func(e navEntry) bool { return e.albumIdx == albumIdx })
	o.navStack = append(o.navStack, navLevel{entries: albums, cursor: albumCursor})

	o.albumCursor = albumCursor
	o.albumsScroll = 0
	o.syncPanels()
}

// rootModlandIndex returns the root-level row index of the "Modland" entry,
// or 0 if there isn't one.
func (o *Overlay) rootModlandIndex() int {
	return indexOfEntry(o.rootEntries, func(e navEntry) bool { return e.kind == entryModlandRoot })
}

// indexOfEntry returns the index of the first entry matching pred, or 0 if
// none matches (row 0 is a safe fallback cursor position in every level).
func indexOfEntry(entries []navEntry, pred func(navEntry) bool) int {
	for i, e := range entries {
		if pred(e) {
			return i
		}
	}
	return 0
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
			o.refreshPreview()
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
			o.refreshPreview()
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

	// Album panel selected: drill into a non-leaf entry (Modland root or a
	// format bucket), or switch to the tracks panel for a leaf album.
	if o.focusPanel == 0 {
		if e := o.currentEntry(); e != nil {
			switch e.kind {
			case entryModlandRoot:
				o.pushLevel(o.buildFormatEntries())
				return false
			case entryFormat:
				o.pushLevel(o.buildAlbumsInFormatEntries(e.format))
				return false
			}
		}
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
	// Already on the left panel: go up one navigation level (e.g. out of a
	// modland format/album drill-down) before closing the UI.
	if o.popLevel() {
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

// buildRootEntries builds the library-root navigation rows from allAlbums:
// local albums verbatim, plus a single "Modland" entry when any modland
// albums are present.
func (o *Overlay) buildRootEntries() []navEntry {
	var entries []navEntry
	hasModland := false
	for i, a := range o.allAlbums {
		if strings.HasPrefix(a.Path, player.ModlandPrefix) {
			hasModland = true
			continue
		}
		entries = append(entries, navEntry{label: a.Name, kind: entryLocalAlbum, albumIdx: i})
	}
	if hasModland {
		entries = append(entries, navEntry{label: "Modland", kind: entryModlandRoot, albumIdx: -1})
	}
	return entries
}

// buildFormatEntries lists the distinct modland formats (e.g. "Protracker").
func (o *Overlay) buildFormatEntries() []navEntry {
	seen := map[string]bool{}
	var formats []string
	for _, a := range o.allAlbums {
		if !strings.HasPrefix(a.Path, player.ModlandPrefix) {
			continue
		}
		format, _ := splitModlandName(a.Name)
		if !seen[format] {
			seen[format] = true
			formats = append(formats, format)
		}
	}
	sort.Strings(formats)
	entries := make([]navEntry, len(formats))
	for i, f := range formats {
		entries[i] = navEntry{label: f, kind: entryFormat, format: f, albumIdx: -1}
	}
	return entries
}

// buildAlbumsInFormatEntries lists the modland albums (authors) within a
// single format. Each is a leaf with its own tracks.
func (o *Overlay) buildAlbumsInFormatEntries(format string) []navEntry {
	var entries []navEntry
	for i, a := range o.allAlbums {
		if !strings.HasPrefix(a.Path, player.ModlandPrefix) {
			continue
		}
		f, author := splitModlandName(a.Name)
		if f != format {
			continue
		}
		label := author
		if label == "" {
			label = f
		}
		entries = append(entries, navEntry{label: label, kind: entryModlandAlbum, albumIdx: i})
	}
	return entries
}

// rootIndexOf finds the root-level row for a real allAlbums index (only
// local albums are addressable this way — modland leaves live behind the
// "Modland" entry and fall back to root position 0).
func (o *Overlay) rootIndexOf(albumIdx int) int {
	for i, e := range o.rootEntries {
		if e.kind == entryLocalAlbum && e.albumIdx == albumIdx {
			return i
		}
	}
	return 0
}

// currentLevelEntries returns the rows for whichever level is currently
// displayed in the left panel: the root, or the top of navStack.
func (o *Overlay) currentLevelEntries() []navEntry {
	if len(o.navStack) == 0 {
		return o.rootEntries
	}
	return o.navStack[len(o.navStack)-1].entries
}

// currentEntry returns the row under the left-panel cursor, or nil.
func (o *Overlay) currentEntry() *navEntry {
	if o.albumCursor < 0 || o.albumCursor >= len(o.albumEntries) {
		return nil
	}
	return &o.albumEntries[o.albumCursor]
}

// refreshAlbumLabels recomputes albumEntries/albums from the current level.
func (o *Overlay) refreshAlbumLabels() {
	o.albumEntries = o.currentLevelEntries()
	o.albums = labelsOf(o.albumEntries)
}

// syncPanels refreshes both left-panel labels and the right-panel preview
// for the current cursor position, then marks both panels dirty. Common
// tail shared by pushLevel, popLevel and focusModlandAlbum.
func (o *Overlay) syncPanels() {
	o.refreshAlbumLabels()
	o.refreshPreview()
	o.albumsDirty = true
	o.tracksDirty = true
}

// refreshPreview recomputes the right-panel preview for a non-leaf entry
// under the cursor (formats list, or albums-in-format list). For a leaf
// entry, previewActive is false and the right panel falls back to the
// externally supplied trackInfos, as before.
func (o *Overlay) refreshPreview() {
	e := o.currentEntry()
	if e == nil {
		o.previewEntries = nil
		o.previewActive = false
		return
	}
	switch e.kind {
	case entryModlandRoot:
		o.previewEntries = o.buildFormatEntries()
		o.previewActive = true
	case entryFormat:
		o.previewEntries = o.buildAlbumsInFormatEntries(e.format)
		o.previewActive = true
	default:
		o.previewEntries = nil
		o.previewActive = false
	}
	o.tracksDirty = true
}

// pushLevel drills into a non-leaf entry, saving the current level's
// cursor/scroll so popLevel can restore it.
func (o *Overlay) pushLevel(entries []navEntry) {
	if len(o.navStack) == 0 {
		o.rootCursor, o.rootScroll = o.albumCursor, o.albumsScroll
	} else {
		top := &o.navStack[len(o.navStack)-1]
		top.cursor, top.scroll = o.albumCursor, o.albumsScroll
	}
	o.navStack = append(o.navStack, navLevel{entries: entries})
	o.albumCursor = 0
	o.albumsScroll = 0
	o.trackCursor = 0
	o.marqueeL.invalidate(o)
	o.syncPanels()
}

// popLevel goes up one navigation level, restoring the parent's cursor and
// scroll position. Returns false if already at the root (caller should
// treat this as "back"/exit instead).
func (o *Overlay) popLevel() bool {
	if len(o.navStack) == 0 {
		return false
	}
	o.navStack = o.navStack[:len(o.navStack)-1]
	if len(o.navStack) == 0 {
		o.albumCursor, o.albumsScroll = o.rootCursor, o.rootScroll
	} else {
		top := o.navStack[len(o.navStack)-1]
		o.albumCursor, o.albumsScroll = top.cursor, top.scroll
	}
	o.trackCursor = 0
	o.marqueeL.invalidate(o)
	o.syncPanels()
	return true
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

// AlbumCursor returns the real allAlbums index of the leaf album currently
// previewed/selected, or -1 when the cursor is on a non-leaf row (the
// "Modland" entry or a format bucket) — callers must not fetch tracks or
// play in that case, since there is no single album yet.
func (o *Overlay) AlbumCursor() int {
	e := o.currentEntry()
	if e == nil {
		return -1
	}
	switch e.kind {
	case entryLocalAlbum, entryModlandAlbum:
		return e.albumIdx
	default:
		return -1
	}
}

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
