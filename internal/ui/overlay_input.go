package ui

import (
	"log/slog"
	"math"
	"strings"
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
	// Refresh the per-frame album snapshot before any navigation/render work
	// of this frame reads it (runs even while the UI is hidden).
	o.refreshAlbumsCache()
	o.notif.Update(o.uiVisible)
	if o.uiVisible && o.uiPage == PagePresets {
		o.requestScheduledPreview(time.Now())
		fps := int(o.PresetPreviewFPS() + 0.5)
		if fps != o.presetPreviewFPSNow {
			o.presetPreviewFPSNow = fps
			o.presetsDetailDirty = true
		}
	}

	if !o.uiVisible || !o.panelEntered {
		return
	}

	state := sdl.GetKeyboardState()
	now := time.Now()

	o.updateScrollHold(&o.scrollUp, state[sdl.SCANCODE_UP] != 0 || gamepadUp, now, func() { o.moveCursor(-1) })
	o.updateScrollHold(&o.scrollDown, state[sdl.SCANCODE_DOWN] != 0 || gamepadDown, now, func() { o.moveCursor(1) })
	if o.infoPanelFocused() {
		o.updateScrollHold(&o.scrollLeft, state[sdl.SCANCODE_LEFT] != 0, now, func() { o.scrollInfoHorizontal(-1) })
		o.updateScrollHold(&o.scrollRight, state[sdl.SCANCODE_RIGHT] != 0, now, func() { o.scrollInfoHorizontal(1) })
	} else {
		o.scrollLeft.active = false
		o.scrollRight.active = false
	}
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
	// A delayed frame must not catch up with several cursor moves before the
	// next swap; those intermediate selections could never be displayed.
	step()
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
		o.pageIndicatorDirty = true
		if !o.textureCacheReady {
			o.markAllDirty()
			o.textureCacheReady = true
		}
	}
	slog.Debug("ui visibility", "visible", o.uiVisible)
}

// NextScreen cycles Library → Presets → Settings → Help → Library.
func (o *Overlay) NextScreen() {
	o.focusPanel = 0
	o.marqueeL.invalidate(o)
	o.marqueeR.invalidate(o)
	o.pageIndicatorDirty = true
	switch o.uiPage {
	case PageLibrary:
		o.uiPage = PagePresets
		o.syncPresetTree()
	case PagePresets:
		o.uiPage = PageSettings
	case PageSettings:
		o.uiPage = PageHelp
	case PageHelp:
		o.uiPage = PageLibrary
	}
	o.panelEntered = true
	o.settingsEditing = false
	o.markAllDirty()
}

// PrevScreen cycles Library → Help → Settings → Presets → Library.
func (o *Overlay) PrevScreen() {
	o.focusPanel = 0
	o.marqueeL.invalidate(o)
	o.marqueeR.invalidate(o)
	o.pageIndicatorDirty = true
	switch o.uiPage {
	case PageLibrary:
		o.uiPage = PageHelp
	case PageSettings:
		o.uiPage = PagePresets
		o.syncPresetTree()
	case PagePresets:
		o.uiPage = PageLibrary
	case PageHelp:
		o.uiPage = PageSettings
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
	o.moveCursor(-1)
}

func (o *Overlay) CursorDown() {
	if !o.panelEntered {
		return
	}
	o.moveCursor(1)
}

func (o *Overlay) scrollNCInfo(dir int) {
	next, changed := scrollPosition(o.ncInfoScroll, dir, o.ncInfoLines, o.ncInfoVisible)
	if changed {
		o.ncInfoScroll = next
		o.tracksDirty = true
	}
}

// moveCursor shifts the active cursor by dir (-1 up, +1 down), scoped to the
// current page and focused panel. Shared by CursorUp/CursorDown so the two
// directions can't drift out of sync.
func (o *Overlay) moveCursor(dir int) {
	o.invalidateActiveMarquee()
	if o.uiPage == PageHelp {
		if o.focusPanel == 0 {
			if o.helpView.InGrandChildren {
				o.helpMoveGrandChild(dir)
			} else if o.helpView.InChildren {
				o.helpMoveEntry(dir)
			} else {
				o.helpMoveTopic(dir)
			}
		} else {
			topic := helpTopic(HelpTopicID(o.helpView.TopicCursor))
			next := o.helpView.ContentTop + dir
			maxTop := o.helpMaxContentTop(topic)
			if next >= 0 && next <= maxTop {
				o.helpView.ContentTop = next
				o.helpDirty = true
			}
		}
		return
	}
	if o.uiPage == PageSettings {
		if o.settingsEditing {
			vals := o.settingsRows[o.settingsCursor].Values
			if next := o.settingsValueCursor + dir; next >= 0 && next < len(vals) {
				o.settingsValueCursor = next
				o.settingsDirty = true
			}
		} else if next := o.settingsCursor + dir; next >= 0 && next < len(o.settingsRows) {
			o.settingsCursor = next
			o.settingsDirty = true
		}
		return
	}
	if o.uiPage == PagePresets {
		cur := o.presetNav.current()
		if cur != nil {
			total := len(cur.nodes)
			if len(o.presetNav.stack) > 1 {
				total++ // ".." entry
			}
			if next := cur.cursor + dir; next >= 0 && next < total {
				cur.cursor = next
				o.presetCursorDirty = true
				o.schedulePreviewForSelected(time.Now())
			}
		}
		return
	}
	// --- Library page ---
	if o.isNC() {
		if o.focusPanel == 1 && o.ncRight == ncRightInfo {
			o.scrollNCInfo(dir)
			return
		}
		if o.focusPanel == 0 {
			if next := o.albumCursor + dir; next >= 0 && next < len(o.albums) {
				o.albumCursor = next
				o.tracksDirty = true
				o.albumsDirty = true
				o.refreshNCPreview()
			}
		} else {
			// Only two buttons — either direction toggles between them.
			o.ncRight = ncRightPlay + ncRightDelete - o.ncRight
			o.tracksDirty = true
			o.tracksContentDirty = true
		}
		return
	}
	// Catalog track info panel: the right side is a scrollable info view
	// (same scroll state as NC), not a track list.
	if o.focusPanel == 1 {
		if e := o.currentEntry(); e != nil && e.IsCatalogTrack() {
			o.scrollNCInfo(dir)
			return
		}
	}
	switch o.focusPanel {
	case 0:
		if next := o.albumCursor + dir; next >= 0 && next < len(o.albums) {
			o.albumCursor = next
			o.trackCursor = 0
			o.tracksDirty = true
			o.albumsDirty = true
		}
	case 1:
		if next := o.trackCursor + dir; next >= 0 && next < len(o.trackInfos) {
			o.trackCursor = next
			o.tracksDirty = true
		}
	}
}

func (o *Overlay) helpTextWidth() int {
	panelW := o.screenW * panelWidthPct / 100
	return availableRowTextWidth(panelW)
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

func (o *Overlay) scrollInfoHorizontal(dir int) bool {
	if o.infoMarquee.tex == 0 {
		return false
	}
	viewportW := o.infoMarquee.maxPx
	maxOffset := o.infoMarquee.texW - viewportW
	if maxOffset <= 0 {
		return false
	}
	const step = 40
	next, changed := scrollPosition(int(o.infoMarquee.offset), dir*step, o.infoMarquee.texW, viewportW)
	if changed {
		o.infoMarquee.offset = float32(next)
	}
	return changed
}

func scrollPosition(current, delta, content, viewport int) (int, bool) {
	maxPosition := max(0, content-viewport)
	next := max(0, min(current+delta, maxPosition))
	return next, next != current
}

func (o *Overlay) infoPanelFocused() bool {
	if o.focusPanel != 1 {
		return false
	}
	if o.isNC() {
		return o.ncRight == ncRightInfo
	}
	e := o.currentEntry()
	return e != nil && e.IsCatalogTrack()
}

func (o *Overlay) FocusLeft() {
	switch o.uiPage {
	case PageHelp:
		if o.focusPanel == 1 {
			o.focusPanel = 0
			o.helpDirty = true
		}
	case PageSettings:
		if o.panelEntered && o.settingsEditing {
			o.settingsEditing = false
			o.settingsDirty = true
		}
	case PagePresets:
		if o.presetNav.Collapse() {
			o.presetsDirty = true
			o.breadcrumbDirty = true
		}
	default: // Library
		if o.isNC() {
			if o.infoPanelFocused() && o.scrollInfoHorizontal(-1) {
				return
			}
			if o.focusPanel == 0 {
				return // already at leftmost
			}
			if o.ncRight == ncRightInfo {
				// Info → left panel
				o.focusPanel = 0
				o.ncRight = ncRightInfo
			} else if o.ncRight == ncRightPlay {
				// Play → Info
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
		if o.infoPanelFocused() && o.scrollInfoHorizontal(-1) {
			return
		}
		o.focusPanelBy(-1, func() { o.albumsDirty = true; o.tracksDirty = true })
	}
}

func (o *Overlay) FocusRight() {
	switch o.uiPage {
	case PageHelp:
		if o.focusPanel == 0 {
			o.focusPanel = 1
			o.helpDirty = true
		}
	case PageSettings:
		if o.panelEntered && !o.settingsEditing {
			o.settingsEditing = true
			if o.settingsCursor < len(o.settingsRows) {
				o.settingsValueCursor = o.settingsRows[o.settingsCursor].Index
			}
			o.settingsDirty = true
		}
	case PagePresets:
		node := o.presetNav.Selected()
		if node != nil && !node.isLeaf {
			o.presetNav.Expand(node)
			o.presetsDirty = true
			o.breadcrumbDirty = true
			o.settlePresetSelection()
		} else if node == nil && len(o.presetNav.stack) > 1 {
			// ".." entry — collapse.
			o.presetNav.Collapse()
			o.presetsDirty = true
			o.breadcrumbDirty = true
		}
	default: // Library
		if o.isNC() {
			if o.infoPanelFocused() && o.scrollInfoHorizontal(1) {
				return
			}
			if o.focusPanel == 0 {
				// left → Info
				o.focusPanel = 1
				o.ncRight = ncRightInfo
			} else if o.ncRight == ncRightInfo {
				// Info → Play
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
		if o.infoPanelFocused() && o.scrollInfoHorizontal(1) {
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
	if o.uiPage == PageHelp {
		if !o.panelEntered {
			o.panelEntered = true
			o.focusPanel = 0
		} else if o.focusPanel == 0 && !o.helpView.InChildren {
			o.enterHelpChildren()
		} else if o.focusPanel == 0 && o.helpView.InChildren && !o.helpView.InGrandChildren {
			// check if current entry has sub-entries (categories with children)
			topic := helpTopic(HelpTopicID(o.helpView.TopicCursor))
			if o.helpView.EntryCursor >= 0 && o.helpView.EntryCursor < len(topic.Children) {
				entry := topic.Children[o.helpView.EntryCursor]
				if len(entry.Children) > 0 {
					o.enterHelpGrandChildren()
				} else {
					o.focusPanel = 1
					o.helpView.ContentTop = 0
				}
			}
		} else if o.focusPanel == 0 {
			o.focusPanel = 1
			o.helpView.ContentTop = 0
		}
		return false
	}

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
		cur := o.presetNav.current()
		if cur == nil {
			return false
		}
		// ".." entry at position 0 when not at root.
		if len(o.presetNav.stack) > 1 && cur.cursor == 0 {
			o.presetNav.Collapse()
			o.presetsDirty = true
			o.breadcrumbDirty = true
			return false
		}
		node := o.presetNav.Selected()
		if node == nil {
			return false
		}
		if !node.isLeaf {
			o.presetNav.Expand(node)
			o.presetsDirty = true
			o.breadcrumbDirty = true
			return false
		}
		return node.key != ""
	}

	// Album panel selected: drill into a non-leaf entry or select a leaf.
	// One dispatch — entry kinds drive everything.
	if o.focusPanel == 0 {
		if e := o.currentEntry(); e != nil {
			slog.Debug("Select", "kind", e.kind, "label", e.label, "albumIdx", e.albumIdx, "trackIdx", e.trackIdx, "isCatalog", o.isCatalog())
			switch {
			case e.kind == entryParent:
				o.popLevel()
				return false
			case e.kind == entrySource:
				switch e.source {
				case sourceMusic:
					o.switchToNC(o.musicDir)
				case sourceMicrophone:
					o.micMenuRequested = true
				case sourceModland, sourceModArchive:
					o.switchToProvider(e.source)
				}
				return false
			case e.kind == entryMicrophoneDevice:
				o.micDeviceSelected = e.device
				return false
			case e.kind == entryMicrophoneStop:
				o.micStopRequested = true
				return false
			case e.kind == entryInfo:
				return false
			case e.IsNCDirectory():
				o.ncEnterDir(e.dirPath)
				return false
			case e.IsNCFile():
				return true // play the file
			case e.IsCatalogTrack():
				slog.Debug("Select: catalog track", "albumIdx", e.albumIdx, "trackIdx", e.trackIdx)
				return true // play the catalog track
			case o.isCatalog() && e.IsLeafAlbum():
				entries := o.buildCatalogTrackEntries(e.albumIdx)
				if len(entries) > 0 {
					o.pushLevel(navLevel{ctx: ctxCatalog, label: e.label, entries: entries})
				}
				return false
			case e.kind == entryFormat:
				o.pushLevel(navLevel{ctx: ctxCatalog, label: e.format, entries: o.buildAlbumsInFormatEntries(e.format)})
				return false
			case e.kind == entryModArchiveDir:
				entries := o.buildModArchiveEntries(e.url)
				if entries != nil {
					o.pushLevel(navLevel{ctx: ctxCatalog, label: strings.TrimSuffix(e.label, "/"), entries: entries})
					return false
				}
				return false
			}
		}
		// Local leaf album (or unknown entry) — move to the tracks panel.
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
	if o.uiPage == PageHelp {
		if o.panelEntered && o.focusPanel == 1 {
			o.focusPanel = 0
			o.helpDirty = true
			return
		}
		if o.helpView.InGrandChildren {
			o.helpView.InGrandChildren = false
			o.helpView.GrandChildCursor = 0
			o.helpView.GrandChildTop = 0
			o.helpView.ContentTop = 0
			o.helpDirty = true
			return
		}
		if o.helpView.InChildren {
			o.helpView.InChildren = false
			o.helpView.EntryCursor = 0
			o.helpView.EntryTop = 0
			o.helpView.ContentTop = 0
			o.helpDirty = true
			return
		}
		o.uiPage = PageLibrary
		o.focusPanel = 0
		o.panelEntered = false
		o.albumsDirty = true
		o.tracksDirty = true
		return
	}

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
			if o.presetNav.Collapse() {
				o.presetsDirty = true
				o.breadcrumbDirty = true
				return
			}
			o.panelEntered = false
			o.presetsDirty = true
			return
		}
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
	// Virtual source root — close the UI. Source roots are always the stack
	// bottom, so every source-level Back reaches this branch after popLevel.
	if o.topLevel().ctx == ctxSourceRoot && o.uiVisible {
		o.uiVisible = false
		o.panelEntered = false
		o.focusPanel = 0
		o.closeInjectPending = true
	}
}

func (o *Overlay) TrackCursor() int { return o.trackCursor }

func (o *Overlay) FocusPanel() int { return o.focusPanel }

func (o *Overlay) IsNCMode() bool { return o.isNC() }

func (o *Overlay) IsCatalogMode() bool { return o.isCatalog() }

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
