package ui

import (
	"log/slog"
	"math"
	"time"

	"github.com/dendec/pmv/internal/modarchive"
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
		o.focusPanel = 0
		o.uiPage = PageLibrary
		o.ncRight = ncRightInfo
		o.ncConfirm = false
		if !o.textureCacheReady {
			o.markAllDirty()
			o.textureCacheReady = true
		}
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
	// --- Library page ---
	if o.isNC() {
		if o.focusPanel == 0 {
			if o.albumCursor > 0 {
				o.albumCursor--
				o.tracksDirty = true
				o.albumsDirty = true
				o.refreshNCPreview()
			}
		} else if o.ncRight == ncRightPlay {
			o.ncRight = ncRightDelete
			o.tracksDirty = true
			o.tracksContentDirty = true
		} else if o.ncRight == ncRightDelete {
			o.ncRight = ncRightPlay
			o.tracksDirty = true
			o.tracksContentDirty = true
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
		} else if o.settingsCursor < len(o.settingsRows)-1 {
			o.settingsCursor++
			o.settingsDirty = true
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
	// --- Library page ---
	if o.isNC() {
		if o.focusPanel == 0 {
			if o.albumCursor < len(o.albums)-1 {
				o.albumCursor++
				o.tracksDirty = true
				o.albumsDirty = true
				o.refreshNCPreview()
			}
		} else if o.ncRight == ncRightPlay {
			o.ncRight = ncRightDelete
			o.tracksDirty = true
			o.tracksContentDirty = true
		} else if o.ncRight == ncRightDelete {
			o.ncRight = ncRightPlay
			o.tracksDirty = true
			o.tracksContentDirty = true
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
		if o.isNC() {
			if o.focusPanel == 0 {
				return // already at leftmost
			}
			if o.ncRight == ncRightPlay {
				// Play → left panel
				o.focusPanel = 0
				o.ncRight = ncRightInfo
			} else {
				// Delete → Play
				o.ncRight = ncRightPlay
			}
			o.albumsDirty = true
			o.tracksDirty = true
			o.tracksContentDirty = true
			return
		}
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
		if o.isNC() {
			if o.focusPanel == 0 {
				// left → Play
				o.focusPanel = 1
				o.ncRight = ncRightPlay
			} else if o.ncRight == ncRightPlay {
				// Play → Delete
				o.ncRight = ncRightDelete
			} else {
				return // already at Delete (rightmost)
			}
			o.albumsDirty = true
			o.tracksDirty = true
			o.tracksContentDirty = true
			return
		}
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
	if o.isNC() {
		o.ncRight = ncRightInfo
	}
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

	// Album panel selected: drill into a non-leaf entry (NC dir, catalog
	// root/format/directory, local folder) or switch to the tracks panel
	// for a leaf album. One dispatch — entry kinds drive everything.
	if o.focusPanel == 0 {
		if e := o.currentEntry(); e != nil {
			switch {
			case e.IsNCDirectory():
				o.ncEnterDir(e.dirPath)
				return false
			case e.IsNCFile():
				return true // play the file
			case e.kind == entryModlandRoot:
				o.pushLevel(navLevel{ctx: ctxCatalog, entries: o.buildFormatEntries()})
				return false
			case e.kind == entryModArchiveRoot:
				o.pushLevel(navLevel{ctx: ctxCatalog, entries: o.buildModArchiveEntries(modarchive.BaseURL)})
				return false
			case e.kind == entryFormat:
				o.pushLevel(navLevel{ctx: ctxCatalog, entries: o.buildAlbumsInFormatEntries(e.format)})
				return false
			case e.kind == entryLocalDir:
				o.pushLevel(navLevel{ctx: ctxLibrary, entries: o.buildLocalDirEntries(e.dirPath)})
				return false
			case e.kind == entryModArchiveDir:
				entries := o.buildModArchiveEntries(e.url)
				if len(entries) == 1 && entries[0].kind == entryModArchiveAlbum {
					// Keep the parent directory list visible and turn the selected
					// row into the resolved virtual album in place.
					level := o.currentLevelEntries()
					level[o.albumCursor] = entries[0]
					o.refreshAlbumLabels()
					o.focusPanel = 1
					o.trackCursor = 0
					o.refreshPreview()
					o.tracksDirty = true
					return false
				}
				if len(entries) > 0 {
					o.pushLevel(navLevel{ctx: ctxCatalog, entries: entries})
					return false
				}
				return false
			}
		}
		// Leaf album (or unknown entry) — move to the tracks panel.
		o.focusPanel = 1
		o.tracksDirty = true
		return false
	}
	// Right panel.
	if o.isNC() {
		// NC focus machine: Play / Delete buttons.
		switch o.ncRight {
		case ncRightPlay:
			return true // play file or directory (app handles both)
		case ncRightDelete:
			if o.ncConfirm {
				// Delete confirmed — app layer handles actual deletion.
				o.ncConfirm = false
				o.ncDeleteConfirmed = true
				o.tracksDirty = true
				o.tracksContentDirty = true
				return false
			}
			o.ncConfirm = true
			o.tracksDirty = true
			o.tracksContentDirty = true
			return false
		}
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

	// NC: cancel delete confirm dialog — focus stays on Delete button.
	if o.isNC() && o.ncConfirm {
		o.ncConfirm = false
		o.tracksDirty = true
		o.tracksContentDirty = true
		return
	}
	if o.backToLeftPanel(func() { o.albumsDirty = true; o.tracksDirty = true }) {
		return
	}
	// On the left panel: go up one navigation level.
	if o.popLevel() {
		return
	}
	// At a root level.
	if o.isNC() {
		// NC root — close the UI.
		if o.uiVisible {
			o.uiVisible = false
			o.panelEntered = false
			o.focusPanel = 0
			o.closeInjectPending = true
		}
		return
	}
	// Library root — switch to the NC browser.
	o.switchToNC(o.baseDir)
}

func (o *Overlay) TrackCursor() int { return o.trackCursor }

func (o *Overlay) FocusPanel() int { return o.focusPanel }

func (o *Overlay) IsNCMode() bool { return o.isNC() }

// NCSelectedFilePath returns the file path for the selected NC file entry,
// or empty string if not applicable.
func (o *Overlay) NCSelectedFilePath() string {
	if !o.isNC() {
		return ""
	}
	e := o.currentEntry()
	if e == nil {
		return ""
	}
	if e.IsNCFile() {
		return e.filePath
	}
	return ""
}

// NCPlaySelected returns the path to play for the current NC selection.
// For a file: returns the file path. For a directory: returns the dirPath.
// Returns empty string if nothing to play.
func (o *Overlay) NCPlaySelected() string {
	if !o.isNC() {
		return ""
	}
	e := o.currentEntry()
	if e == nil {
		return ""
	}
	if e.IsNCFile() {
		return e.filePath
	}
	if e.IsNCDirectory() {
		return e.dirPath
	}
	return ""
}

// NCConfirmDelete activates the delete confirmation dialog.
func (o *Overlay) NCConfirmDelete() {
	o.ncConfirm = true
	o.tracksDirty = true
	o.tracksContentDirty = true
}

// NCIsConfirmingDelete reports if the delete confirm dialog is active.
func (o *Overlay) NCIsConfirmingDelete() bool {
	return o.ncConfirm
}

// NCCancelDelete cancels the delete confirmation.
func (o *Overlay) NCCancelDelete() {
	o.ncConfirm = false
	o.tracksDirty = true
	o.tracksContentDirty = true
}

// NCConsumeDeleteConfirmed returns true if a delete was just confirmed,
// and resets the flag. One-shot — call once per frame after Select.
func (o *Overlay) NCConsumeDeleteConfirmed() bool {
	if !o.ncDeleteConfirmed {
		return false
	}
	o.ncDeleteConfirmed = false
	return true
}

// NCDeletePath returns the path to delete for the current NC selection.
// Empty string if nothing to delete.
func (o *Overlay) NCDeletePath() string {
	if !o.isNC() {
		return ""
	}
	e := o.currentEntry()
	if e == nil {
		return ""
	}
	if e.IsNCFile() {
		return e.filePath
	}
	if e.IsNCDirectory() && e.dirPath != o.baseDir {
		return e.dirPath
	}
	return ""
}
