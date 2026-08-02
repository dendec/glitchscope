package ui

import (
	"log/slog"
	"math"
	"time"

	"github.com/veandco/go-sdl2/sdl"
)

// This file owns cursor/panel input handling and page switching.
// Navigation-tree construction lives in overlay_nav.go; rendering in
// overlay_render.go; struct fields in overlay.go.

// scrollHold tracks key-hold timing for scroll acceleration.
type scrollHold struct {
	holdStart  time.Time
	lastStep   time.Time
	active     bool
	uiPage     UIPage
	focusPanel int
}

// Update advances animations and handles scroll acceleration. Call every frame.
// gamepadUp/gamepadDown report D-Pad state (SDL button events don't auto-repeat).
func (o *Overlay) Update(gamepadUp, gamepadDown bool) {
	o.notif.Update(o.uiVisible)

	if !o.uiVisible || !o.panelEntered {
		return
	}

	// Overlay-owned ModArchive albums can't be resolved via the library's
	// SetTrackInfos; re-assert their track list each frame.
	o.refreshVirtualTracks()

	state := sdl.GetKeyboardState()
	now := time.Now()

	o.updateScrollHold(&o.scrollUp, state[sdl.SCANCODE_UP] != 0 || gamepadUp, now, o.cursorUp1)
	o.updateScrollHold(&o.scrollDown, state[sdl.SCANCODE_DOWN] != 0 || gamepadDown, now, o.cursorDown1)
	o.updateMarquee(now)
}

// updateScrollHold drives held-key scroll acceleration: after held for 0.2s,
// steps at a rate that doubles every second (starting at 5 steps/sec).
func (o *Overlay) updateScrollHold(h *scrollHold, held bool, now time.Time, step func()) {
	if !held {
		h.active = false
		return
	}
	if !h.active || h.uiPage != o.uiPage || h.focusPanel != o.focusPanel {
		// New hold — start tracking.
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

// --- Cursor & panel navigation ---

func (o *Overlay) CursorUp() {
	if !o.panelEntered {
		return
	}
	o.cursorUp1()
}

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

func (o *Overlay) CursorDown() {
	if !o.panelEntered {
		return
	}
	o.cursorDown1()
}

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

// focusPanelBy shifts focusPanel by delta, clamped to [0,1].
func (o *Overlay) focusPanelBy(delta int, markDirty func()) {
	next := o.focusPanel + delta
	if next < 0 || next > 1 {
		return
	}
	o.focusPanel = next
	o.invalidateActiveMarquee()
	markDirty()
}

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

// backToLeftPanel moves focus from right to left panel.
// Returns true if it handled the action.
func (o *Overlay) backToLeftPanel(markDirty func()) bool {
	if o.focusPanel != 1 {
		return false
	}
	o.focusPanel = 0
	markDirty()
	return true
}

// Select enters the focused panel or confirms item selection.
// Returns true when an item was selected.
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

	// Album panel selected: drill into a non-leaf entry (Modland/ModArchive root or a
	// format/directory bucket), or switch to the tracks panel for a leaf album.
	if o.focusPanel == 0 {
		if e := o.currentEntry(); e != nil {
			switch e.kind {
			case entryModlandRoot:
				o.pushLevel(o.buildFormatEntries())
				return false
			case entryFormat:
				o.pushLevel(o.buildAlbumsInFormatEntries(e.format))
				return false
			case entryLocalDir:
				o.pushLevel(o.buildLocalDirEntries(e.dirPath))
				return false
			case entryModArchiveRoot:
				entries := o.buildModArchiveEntries("http://modarchive.textfiles.com/")
				if len(entries) > 0 {
					o.pushLevel(entries)
				}
				return false
			case entryModArchiveDir:
				entries := o.buildModArchiveEntries(e.url)
				if len(entries) == 1 && entries[0].kind == entryModArchiveAlbum {
					o.pushLevel(entries)
					o.focusPanel = 1
					o.tracksDirty = true
					return false
				}
				if len(entries) > 0 {
					o.pushLevel(entries)
					return false
				}
			}
		}
		o.focusPanel = 1
		o.tracksDirty = true
		return false
	}
	return true // track panel — play the track
}

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

func (o *Overlay) TrackCursor() int { return o.trackCursor }

func (o *Overlay) FocusPanel() int { return o.focusPanel }
