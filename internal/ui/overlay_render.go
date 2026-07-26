package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"path/filepath"
	"strings"

	"github.com/dendec/mdpp/internal/player"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// This file owns all overlay UI rendering: panel layout, texture caching and
// text-to-texture rasterization. State mutation and input handling live in
// overlay.go; GL/cgo calls are isolated behind the wrappers in gl.go.

const panelWidthPct = 45 // each panel occupies this % of screen width

func (o *Overlay) renderUI(winW, winH, viewW, viewH int) {
	if o.programRect == 0 {
		return
	}

	// Full screen dim.
	glDrawFilledRect(o.programRect, 0, 0, float32(winW), float32(winH), 0, 0, 0, dimAlpha, winW, winH, viewW, viewH)

	// Stats line (FPS, MEM, CPU, GPU).
	if o.statsDirty {
		o.rebuildStatsTex()
	}
	if o.statsTex != 0 {
		glDrawOverlayText(o.programText, o.statsTex, 1,
			5, 0, float32(o.statsTexW), float32(o.statsTexH), winW, winH, viewW, viewH)
	}
	// Preset name — below stats.
	if o.presetNameDirty {
		o.rebuildPresetNameTex()
	}
	if o.presetNameTex != 0 {
		glDrawOverlayText(o.programText, o.presetNameTex, 1,
			5, float32(o.statsTexH+4), float32(o.presetNameTexW), float32(o.presetNameTexH), winW, winH, viewW, viewH)
	}

	// Page indicator.
	o.renderPageIndicator(winW, winH, viewW, viewH)

	// Layout constants — proportional to font size.
	bottomH := int(o.fontSize * 1.35)
	indicatorH := int(o.fontSize * 1.5)
	panelY := int(o.fontSize*2) + indicatorH
	panelH := winH - panelY - bottomH - int(o.fontSize*0.7)
	if panelH < 0 {
		panelH = 0
	}
	panelW := winW * panelWidthPct / 100

	lh := o.face.Metrics().Height.Ceil()

	switch o.uiPage {
	case PageSettings:
		o.renderSettingsPanels(winW, winH, viewW, viewH, panelW, panelY, panelH, lh)
	case PagePresets:
		o.renderPresetsPanels(winW, winH, viewW, viewH, panelW, panelY, panelH, lh)
	default:
		o.renderLibraryPanels(winW, winH, viewW, viewH, panelW, panelY, panelH, lh)
	}

	// --- Compact playback status (full width) ---
	if o.playingTrack != "" && o.bottomDirty {
		o.rebuildBottomTex(winW, bottomH)
	}
	if o.playingTrack != "" && o.bottomTex != 0 {
		by := float32(winH - bottomH)
		glDrawFilledRect(o.programRect, 0, by, float32(winW), float32(bottomH), 0, 0, 0, 0.5, winW, winH, viewW, viewH)
		glDrawOverlayText(o.programText, o.bottomTex, 1,
			0, by, float32(o.bottomTexW), float32(o.bottomTexH), winW, winH, viewW, viewH)
	}
}

// renderLibraryPanels draws the albums (left) and tracks (right) panels.
func (o *Overlay) renderLibraryPanels(winW, winH, viewW, viewH int, panelW, panelY, panelH, lh int) {
	// Text clip width must match the width truncateMiddle/rebuildMarqueeLine
	// used to decide whether a row needs a marquee; otherwise a row can be
	// flagged as "too wide" yet have maxOffset <= 0 in drawMarqueeCol and
	// render blank instead of scrolling.
	textW := availableRowTextWidth(panelW)

	// --- Albums panel ---
	if o.albumsDirty {
		o.rebuildAlbumsTex(panelW, panelH)
	}
	if o.albumsTex != 0 {
		px, py := float32(0), float32(panelY)
		drawPanelBg(o, px, py, float32(panelW), float32(panelH), winW, winH, viewW, viewH)
		if o.focusPanel == 0 {
			drawPanelBorder(o, px, py, float32(panelW), float32(panelH), winW, winH, viewW, viewH)
		}
		glDrawOverlayText(o.programText, o.albumsTex, 1,
			px, py, float32(o.albumsTexW), float32(o.albumsTexH), winW, winH, viewW, viewH)
		am := panelH / lh
		if am < 1 {
			am = 1
		}
		drawScrollbar(o, px+float32(panelW-4), py, float32(panelH), len(o.albums), am, o.albumsScroll, winW, winH, viewW, viewH)
		if o.panelEntered && o.focusPanel == 0 && len(o.albums) > 0 {
			rowY := py + float32((o.albumCursor-o.albumsScroll)*lh)
			if !o.drawMarqueeCol(&o.marqueeL, px, py, float32(textW), float32(panelH), lh, rowY, px+2, float32(panelW-4), winW, winH, viewW, viewH) {
				drawAccentHighlight(o, px+2, rowY+2, float32(panelW-4), float32(lh), winW, winH, viewW, viewH)
			}
		}
	}

	// --- Tracks panel ---
	tracksX := winW - panelW
	if o.tracksDirty {
		o.rebuildTracksTex(panelW, panelH)
	}
	if o.tracksTex != 0 {
		tx, ty := float32(tracksX), float32(panelY)
		drawPanelBg(o, tx, ty, float32(panelW), float32(panelH), winW, winH, viewW, viewH)
		if o.focusPanel == 1 {
			drawPanelBorder(o, tx, ty, float32(panelW), float32(panelH), winW, winH, viewW, viewH)
		}
		glDrawOverlayText(o.programText, o.tracksTex, 1,
			tx, ty, float32(o.tracksTexW), float32(o.tracksTexH), winW, winH, viewW, viewH)
		tm := panelH / lh
		if tm < 1 {
			tm = 1
		}
		tracksCount := len(o.trackInfos)
		if o.previewActive {
			tracksCount = len(o.previewEntries)
		}
		drawScrollbar(o, tx+float32(panelW-4), ty, float32(panelH), tracksCount, tm, o.tracksScroll, winW, winH, viewW, viewH)
		if o.panelEntered && o.focusPanel == 1 && len(o.trackInfos) > 0 {
			rowY := ty + float32((o.trackCursor-o.tracksScroll)*lh)
			if !o.drawMarqueeCol(&o.marqueeR, tx, ty, float32(textW), float32(panelH), lh, rowY, tx+2, float32(panelW-4), winW, winH, viewW, viewH) {
				drawAccentHighlight(o, tx+2, rowY+2, float32(panelW-4), float32(lh), winW, winH, viewW, viewH)
			}
		}
	}
}

// renderSettingsPanels draws the settings labels (left) and values (right).
func (o *Overlay) renderSettingsPanels(winW, winH, viewW, viewH int, panelW, panelY, panelH, lh int) {
	if !o.settingsDirty {
		// Still draw textures from cache.
		o.drawSettingsTextures(winW, winH, viewW, viewH, panelW, panelY, panelH, lh)
		return
	}
	o.settingsDirty = false

	maxRows := panelH / lh
	if maxRows < 1 {
		maxRows = 1
	}

	// Rebuild left column texture (setting names) with scroll window.
	leftTotal := len(o.settingsRows)
	o.albumsScroll = scrollOffset(o.albumsScroll, o.settingsCursor, leftTotal, maxRows)
	maxTextPx := availableRowTextWidth(panelW)
	var leftRows []listRow
	leftEnd := o.albumsScroll + maxRows
	if leftEnd > leftTotal {
		leftEnd = leftTotal
	}
	for i := o.albumsScroll; i < leftEnd; i++ {
		row := o.settingsRows[i]
		prefix := "  "
		if i == o.settingsCursor && o.panelEntered {
			prefix = "▸ "
		}
		line := prefix + row.Label
		leftRows = append(leftRows, listRow{text: line, active: i == o.settingsCursor && o.panelEntered})
	}
	o.rebuildListRows(&o.settingsColL, leftRows, maxTextPx, panelW)

	// Rebuild marquee for focused setting name.
	o.marqueeL.invalidate(o)
	if o.panelEntered && !o.settingsEditing && o.settingsCursor >= o.albumsScroll && o.settingsCursor < leftEnd {
		row := o.settingsRows[o.settingsCursor]
		prefix := "▸ "
		o.rebuildMarqueeLine(&o.marqueeL, prefix+row.Label, maxTextPx)
	}

	// Rebuild right column texture (values for the focused setting) with scroll window.
	var rightRows []listRow
	rightTotal := 0
	rightEnd := 0
	if o.settingsCursor < len(o.settingsRows) {
		row := o.settingsRows[o.settingsCursor]
		rightTotal = len(row.Values)
		selIdx := row.Index
		if o.settingsEditing {
			selIdx = o.settingsValueCursor
		}
		o.tracksScroll = scrollOffset(o.tracksScroll, selIdx, rightTotal, maxRows)
		rightEnd = o.tracksScroll + maxRows
		if rightEnd > rightTotal {
			rightEnd = rightTotal
		}
		for i := o.tracksScroll; i < rightEnd; i++ {
			mark := "  "
			if i == selIdx {
				mark = "▸ "
			}
			line := mark + row.Values[i]
			rightRows = append(rightRows, listRow{text: line, active: i == selIdx && o.panelEntered && o.settingsEditing})
		}
	}
	o.rebuildListRows(&o.settingsColR, rightRows, maxTextPx, panelW)

	// Rebuild marquee for focused setting value.
	o.marqueeR.invalidate(o)
	if o.panelEntered && o.settingsEditing && o.settingsCursor < len(o.settingsRows) {
		row := o.settingsRows[o.settingsCursor]
		selIdx := o.settingsValueCursor
		if selIdx >= o.tracksScroll && selIdx < rightEnd {
			mark := "▸ "
			o.rebuildMarqueeLine(&o.marqueeR, mark+row.Values[selIdx], maxTextPx)
		}
	}

	o.drawSettingsTextures(winW, winH, viewW, viewH, panelW, panelY, panelH, lh)
}

func (o *Overlay) drawSettingsTextures(winW, winH, viewW, viewH int, panelW, panelY, panelH, lh int) {
	lx, ly := float32(0), float32(panelY)
	rx, ry := float32(winW-panelW), float32(panelY)
	colW, colH := float32(panelW), float32(panelH)

	maxRows := panelH / lh
	if maxRows < 1 {
		maxRows = 1
	}

	textW := float32(availableRowTextWidth(panelW))

	leftHighlight := o.panelEntered && !o.settingsEditing && len(o.settingsRows) > 0
	drawListColumn(o, lx, ly, colW, colH, o.settingsColL, o.panelEntered && !o.settingsEditing,
		o.settingsCursor-o.albumsScroll, leftHighlight, lh, winW, winH, viewW, viewH)
	drawScrollbar(o, lx+colW-3, ly, colH, len(o.settingsRows), maxRows, o.albumsScroll, winW, winH, viewW, viewH)
	if o.panelEntered && !o.settingsEditing && len(o.settingsRows) > 0 {
		rowY := ly + float32((o.settingsCursor-o.albumsScroll)*lh)
		o.drawMarqueeCol(&o.marqueeL, lx, ly, textW, colH, lh, rowY, lx+2, colW-4, winW, winH, viewW, viewH)
	}

	rightHighlight := false
	rightRow := 0
	rightTotal := 0
	if o.panelEntered && o.settingsEditing && len(o.settingsRows) > 0 {
		row := o.settingsRows[o.settingsCursor]
		rightTotal = len(row.Values)
		if o.settingsValueCursor < len(row.Values) {
			rightHighlight = true
			rightRow = o.settingsValueCursor - o.tracksScroll
		}
	}
	drawListColumn(o, rx, ry, colW, colH, o.settingsColR, o.panelEntered && o.settingsEditing,
		rightRow, rightHighlight, lh, winW, winH, viewW, viewH)
	drawScrollbar(o, rx+colW-3, ry, colH, rightTotal, maxRows, o.tracksScroll, winW, winH, viewW, viewH)
	if o.panelEntered && o.settingsEditing && rightHighlight {
		rowY := ry + float32(rightRow*lh)
		o.drawMarqueeCol(&o.marqueeR, rx, ry, textW, colH, lh, rowY, rx+2, colW-4, winW, winH, viewW, viewH)
	}
}

// --- Panel drawing helpers ---

func drawPanelBg(o *Overlay, x, y, w, h float32, winW, winH, viewW, viewH int) {
	glDrawFilledRect(o.programRect, x, y, w, h, 0, 0, 0, panelBgAlpha, winW, winH, viewW, viewH)
}

func drawPanelBorder(o *Overlay, x, y, w, h float32, winW, winH, viewW, viewH int) {
	glDrawFilledRect(o.programRect, x-1, y-1, w+2, 1, 1, 1, 1, borderAlpha, winW, winH, viewW, viewH)
	glDrawFilledRect(o.programRect, x-1, y+h, w+2, 1, 1, 1, 1, borderAlpha, winW, winH, viewW, viewH)
	glDrawFilledRect(o.programRect, x-1, y-1, 1, h+2, 1, 1, 1, borderAlpha, winW, winH, viewW, viewH)
	glDrawFilledRect(o.programRect, x+w, y-1, 1, h+2, 1, 1, 1, borderAlpha, winW, winH, viewW, viewH)
}

func drawAccentHighlight(o *Overlay, x, y, w, h float32, winW, winH, viewW, viewH int) {
	glDrawFilledRect(o.programRect, x, y, w, h, accentR, accentG, accentB, accentA, winW, winH, viewW, viewH)
}

func drawScrollbar(o *Overlay, sbX, panelY, panelH float32, totalItems, visibleItems, scrollPos int, winW, winH, viewW, viewH int) {
	if totalItems <= visibleItems {
		return
	}
	thumbW := float32(3)
	// Track.
	glDrawFilledRect(o.programRect, sbX, panelY, thumbW, panelH, 1, 1, 1, 0.1, winW, winH, viewW, viewH)
	// Thumb.
	thumbH := panelH * float32(visibleItems) / float32(totalItems)
	if thumbH < 8 {
		thumbH = 8
	}
	maxScroll := totalItems - visibleItems
	if maxScroll < 1 {
		maxScroll = 1
	}
	thumbY := panelY + (panelH-thumbH)*float32(scrollPos)/float32(maxScroll)
	glDrawFilledRect(o.programRect, sbX, thumbY, thumbW, thumbH, 1, 1, 1, 0.35, winW, winH, viewW, viewH)
}

// listTex caches a rendered text texture for one column of a two-column list
// panel (Settings, Presets, ...). Each page keeps its own instance so pages
// never alias each other's GL textures.
type listTex struct {
	tex  uint32
	w, h int
}

type listRow struct {
	text   string
	active bool
	bold   bool
}

// rebuildListRows renders rows into t, reserving the active row for marquee
// text when its full value does not fit the available width.
func (o *Overlay) rebuildListRows(t *listTex, rows []listRow, maxTextW, minW int) {
	o.deleteTex(&t.tex)
	t.tex, t.w, t.h = o.renderListRows(rows, maxTextW, minW)
}

func (o *Overlay) renderListRows(rows []listRow, maxTextW, minW int) (uint32, int, int) {
	lines := make([]string, 0, len(rows))
	bold := make([]bool, 0, len(rows))
	for _, row := range rows {
		if row.active && font.MeasureString(o.face, row.text).Ceil() > maxTextW {
			lines = append(lines, "")
		} else {
			lines = append(lines, o.truncateMiddle(row.text, maxTextW))
		}
		bold = append(bold, row.bold)
	}
	return o.renderTextToTexBold(strings.Join(lines, "\n"), minW, bold, 255, 255, 255, 255)
}

// drawListColumn draws one column of a two-column list panel: background,
// optional focus border, cached text, and an optional accent highlight row.
// Shared by Settings and Presets so their layout/highlight logic stays in
// one place.
func drawListColumn(o *Overlay, x, y, w, h float32, t listTex, bordered bool,
	highlightRow int, showHighlight bool, lh int, winW, winH, viewW, viewH int) {
	drawPanelBg(o, x, y, w, h, winW, winH, viewW, viewH)
	if bordered {
		drawPanelBorder(o, x, y, w, h, winW, winH, viewW, viewH)
	}
	if t.tex == 0 {
		return
	}
	glDrawOverlayText(o.programText, t.tex, 1, x, y, float32(t.w), float32(t.h), winW, winH, viewW, viewH)
	if showHighlight {
		hiY := y + float32(highlightRow*lh)
		drawAccentHighlight(o, x+2, hiY, w-4, float32(lh), winW, winH, viewW, viewH)
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
	o.marqueeL.invalidate(o)

	if len(o.albums) == 0 {
		return
	}

	lh := o.face.Metrics().Height.Ceil()
	maxRows := maxH / lh
	if maxRows < 1 {
		maxRows = 1
	}
	o.albumsScroll = scrollOffset(o.albumsScroll, o.albumCursor, len(o.albums), maxRows)

	start := o.albumsScroll
	end := start + maxRows
	if end > len(o.albums) {
		end = len(o.albums)
	}
	maxTextPx := availableRowTextWidth(maxW)
	var rows []listRow
	for i := start; i < end; i++ {
		name := o.albums[i]
		line := name
		rows = append(rows, listRow{
			text:   line,
			active: i == o.albumCursor && o.focusPanel == 0,
			bold:   name == o.playingAlbum,
		})
		if i == o.albumCursor && o.focusPanel == 0 {
			o.rebuildMarqueeLine(&o.marqueeL, line, maxTextPx)
		}
	}
	o.albumsTex, o.albumsTexW, o.albumsTexH = o.renderListRows(rows, maxTextPx, maxW)
}

func (o *Overlay) rebuildTracksTex(maxW, maxH int) {
	o.tracksDirty = false
	o.deleteTex(&o.tracksTex)
	o.marqueeR.invalidate(o)

	if o.previewActive {
		o.rebuildPreviewTex(maxW, maxH)
		return
	}

	if len(o.trackInfos) == 0 {
		return
	}

	lh := o.face.Metrics().Height.Ceil()
	maxRows := maxH / lh
	if maxRows < 1 {
		maxRows = 1
	}
	o.tracksScroll = scrollOffset(o.tracksScroll, o.trackCursor, len(o.trackInfos), maxRows)

	start := o.tracksScroll
	end := start + maxRows
	if end > len(o.trackInfos) {
		end = len(o.trackInfos)
	}
	maxTextPx := availableRowTextWidth(maxW)
	var rows []listRow
	for i := start; i < end; i++ {
		info := o.trackInfos[i]
		title := player.TrackTitle(info.Path)
		suffix := ""
		if info.Duration > 0 {
			suffix = "  " + formatDuration(info.Duration)
		}
		line := title + suffix
		rows = append(rows, listRow{
			text:   line,
			active: i == o.trackCursor && o.focusPanel == 1,
			bold:   info.Path == o.playingTrack,
		})
		if i == o.trackCursor && o.focusPanel == 1 {
			o.rebuildMarqueeLine(&o.marqueeR, line, maxTextPx)
		}
	}
	o.tracksTex, o.tracksTexW, o.tracksTexH = o.renderListRows(rows, maxTextPx, maxW)
}

// rebuildPreviewTex renders the right-panel preview of a non-leaf left-panel
// entry: the modland format list (when "Modland" is highlighted) or the
// album list within a format (when a format is highlighted). Read-only —
// there is no cursor/selection inside this panel; the user drills in by
// pressing Select on the left-panel entry instead.
func (o *Overlay) rebuildPreviewTex(maxW, maxH int) {
	if len(o.previewEntries) == 0 {
		return
	}

	lh := o.face.Metrics().Height.Ceil()
	maxRows := maxH / lh
	if maxRows < 1 {
		maxRows = 1
	}
	maxTextPx := availableRowTextWidth(maxW)

	end := len(o.previewEntries)
	if end > maxRows {
		end = maxRows
	}
	rows := make([]listRow, end)
	for i := 0; i < end; i++ {
		rows[i] = listRow{text: o.previewEntries[i].label}
	}
	o.tracksTex, o.tracksTexW, o.tracksTexH = o.renderListRows(rows, maxTextPx, maxW)
}

func (o *Overlay) rebuildBottomTex(w, botH int) {
	o.bottomDirty = false
	o.deleteTex(&o.bottomTex)

	if o.playingTrack == "" {
		return
	}

	pos := formatDuration(o.position)
	dur := formatDuration(o.duration)
	status := "▶"
	if o.paused {
		status = "⏸"
	}

	title := player.TrackTitle(o.playingTrack)
	maxTitleW := w - int(o.fontSize*14)
	title = o.truncateMiddle(title, maxTitleW)
	info := ""
	if !o.isTracker && o.sampleRate > 0 {
		info = fmt.Sprintf("%.0fkHz", o.sampleRate/1000)
	}
	if !o.isTracker && o.bitrate > 0 {
		info += fmt.Sprintf(" %.0fkbps", o.bitrate)
	}
	if o.isTracker {
		if o.bpm > 0 {
			info += fmt.Sprintf(" %.0f BPM", o.bpm)
		}
		if o.channels > 0 {
			info += fmt.Sprintf(" %dch", o.channels)
		}
	} else if o.channels > 0 {
		switch o.channels {
		case 1:
			info += " mono"
		case 2:
			info += " stereo"
		default:
			info += fmt.Sprintf(" %dch", o.channels)
		}
	}

	text := fmt.Sprintf("%s %s  %s", status, title, pos)
	if o.duration > 0 {
		text = fmt.Sprintf("%s %s  %s/%s", status, title, pos, dur)
	}
	if info != "" {
		text += "  " + info
	}
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

// --- Texture helpers ---

func (o *Overlay) deleteTex(tex *uint32) {
	if *tex != 0 {
		glDeleteTex(*tex)
		*tex = 0
	}
}

func (o *Overlay) renderTextToTex(text string, r, g, b, a byte) (uint32, int, int) {
	return o.renderTextToTexBold(text, 0, nil, r, g, b, a)
}

// renderTextToTexBold is like renderTextToTexWithMinWidth but lines whose
// index is present (and true) in bold are rendered with a faux-bold pass
// (drawn a second time offset by one pixel) — used instead of a glyph marker
// to indicate the currently playing row.
func (o *Overlay) renderTextToTexBold(text string, minW int, bold []bool, r, g, b, a byte) (uint32, int, int) {
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
	if texW < minW {
		texW = minW
	}
	texH := lineHeight*len(lines) + pad*2
	if texW <= 0 || texH <= 0 {
		return 0, 0, 0
	}

	rgba := image.NewRGBA(image.Rect(0, 0, texW, texH))
	for i, line := range lines {
		bounds, _ := font.BoundString(o.face, line)
		startDot := fixed.Point26_6{
			X: fixed.I(pad) - bounds.Min.X,
			Y: fixed.I(pad+i*lineHeight) - bounds.Min.Y,
		}
		d := &font.Drawer{
			Dst:  rgba,
			Src:  image.NewUniform(color.RGBA{r, g, b, a}),
			Face: o.face,
			Dot:  startDot,
		}
		d.DrawString(line)
		if i < len(bold) && bold[i] {
			// Faux bold: redraw the same line one pixel to the right,
			// starting from the same origin (DrawString mutates Dot).
			d.Dot = startDot
			d.Dot.X += fixed.I(1)
			d.DrawString(line)
		}
	}

	return glUploadTexture(rgba), texW, texH
}

func formatDuration(sec float64) string {
	if sec <= 0 {
		return "0:00"
	}
	m := int(sec) / 60
	s := int(sec) % 60
	return fmt.Sprintf("%d:%02d", m, s)
}

// --- Page indicator ---

func (o *Overlay) renderPageIndicator(winW, winH, viewW, viewH int) {
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

	startX := (winW - totalW) / 2
	y := int(o.fontSize * 0.5)

	x := startX
	for i := range pages {
		if o.pageIndicatorTex[i] != 0 {
			glDrawOverlayText(o.programText, o.pageIndicatorTex[i], 1,
				float32(x), float32(y), float32(o.pageIndicatorTexW[i]), float32(o.pageIndicatorTexH[i]), winW, winH, viewW, viewH)
		}
		// Accent background for active page.
		if UIPage(i) == o.uiPage {
			drawAccentHighlight(o, float32(x-4), float32(y-2), float32(o.pageIndicatorTexW[i]+8), float32(lh+4), winW, winH, viewW, viewH)
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

func (o *Overlay) renderPresetsPanels(winW, winH, viewW, viewH int, panelW, panelY, panelH, lh int) {
	texturesValid := glIsTexture(o.presetsColL.tex) && glIsTexture(o.presetsColR.tex)
	if !o.presetsDirty && texturesValid {
		o.drawPresetsTextures(winW, winH, viewW, viewH, panelW, panelY, panelH, lh)
		return
	}
	o.presetsDirty = false

	// Leave room for the texture's horizontal padding, the scrollbar and the
	// clipped row edges. Prefixes and status icons are part of the measured
	// line, so they consume this same budget.
	maxTextPx := availableRowTextWidth(panelW)

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
	var leftRows []listRow
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
		line := prefix + cat.Name
		leftRows = append(leftRows, listRow{text: line, active: i == o.presetCategoryCursor && o.panelEntered && o.focusPanel == 0})
	}
	o.rebuildListRows(&o.presetsColL, leftRows, maxTextPx, panelW)

	// Rebuild marquee for focused category name.
	o.marqueeL.invalidate(o)
	if o.panelEntered && o.focusPanel == 0 && o.presetCategoryCursor >= o.presetsScrollL && o.presetCategoryCursor < leftEnd {
		cat := o.presetCategories[o.presetCategoryCursor]
		prefix := "▸ "
		o.rebuildMarqueeLine(&o.marqueeL, prefix+cat.Name, maxTextPx)
	}

	// Right panel — presets in current category.
	var rightRows []listRow
	rightEnd := 0
	if cat := o.currentCategory(); cat != nil {
		o.presetsScrollR = scrollOffset(o.presetsScrollR, o.presetCursor, len(cat.Presets), maxRows)
		rightEnd = o.presetsScrollR + maxRows
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
			line := mark + name
			rightRows = append(rightRows, listRow{text: line, active: i == o.presetCursor && o.panelEntered && o.focusPanel == 1})
		}
	} else {
		o.presetsScrollR = 0
	}
	o.rebuildListRows(&o.presetsColR, rightRows, maxTextPx, panelW)

	// Rebuild marquee for focused preset name.
	o.marqueeR.invalidate(o)
	if o.panelEntered && o.focusPanel == 1 {
		if cat := o.currentCategory(); cat != nil && o.presetCursor >= o.presetsScrollR && o.presetCursor < rightEnd {
			p := cat.Presets[o.presetCursor]
			name := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
			mark := "▸ "
			o.rebuildMarqueeLine(&o.marqueeR, mark+name, maxTextPx)
		}
	}

	o.drawPresetsTextures(winW, winH, viewW, viewH, panelW, panelY, panelH, lh)
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

func (o *Overlay) drawPresetsTextures(winW, winH, viewW, viewH int, panelW, panelY, panelH, lh int) {
	lx, ly := float32(0), float32(panelY)
	rx, ry := float32(winW-panelW), float32(panelY)
	colW, colH := float32(panelW), float32(panelH)

	maxRows := panelH / lh
	if maxRows < 1 {
		maxRows = 1
	}

	textW := float32(availableRowTextWidth(panelW))

	leftHighlight := o.panelEntered && o.focusPanel == 0 && len(o.presetCategories) > 0
	drawListColumn(o, lx, ly, colW, colH, o.presetsColL, o.panelEntered && o.focusPanel == 0,
		o.presetCategoryCursor-o.presetsScrollL, leftHighlight, lh, winW, winH, viewW, viewH)
	drawScrollbar(o, lx+colW-3, ly, colH, len(o.presetCategories), maxRows, o.presetsScrollL, winW, winH, viewW, viewH)
	if o.panelEntered && o.focusPanel == 0 && len(o.presetCategories) > 0 {
		rowY := ly + float32((o.presetCategoryCursor-o.presetsScrollL)*lh)
		o.drawMarqueeCol(&o.marqueeL, lx, ly, textW, colH, lh, rowY, lx+2, colW-4, winW, winH, viewW, viewH)
	}

	rightHighlight := false
	rightTotal := 0
	if cat := o.currentCategory(); o.panelEntered && o.focusPanel == 1 && cat != nil && len(cat.Presets) > 0 {
		rightHighlight = true
		rightTotal = len(cat.Presets)
	}
	drawListColumn(o, rx, ry, colW, colH, o.presetsColR, o.panelEntered && o.focusPanel == 1,
		o.presetCursor-o.presetsScrollR, rightHighlight, lh, winW, winH, viewW, viewH)
	drawScrollbar(o, rx+colW-3, ry, colH, rightTotal, maxRows, o.presetsScrollR, winW, winH, viewW, viewH)
	if rightHighlight {
		rowY := ry + float32((o.presetCursor-o.presetsScrollR)*lh)
		o.drawMarqueeCol(&o.marqueeR, rx, ry, textW, colH, lh, rowY, rx+2, colW-4, winW, winH, viewW, viewH)
	}
}

// --- Marquee helpers ---

// rebuildMarqueeLine builds a single-line texture for the focused item if its
// full text doesn't fit within maxPx. Returns true if the texture was built
// (i.e. the item is truncated and needs marquee scrolling).
func (o *Overlay) rebuildMarqueeLine(m *marqueeState, fullText string, maxPx int) bool {
	o.deleteTex(&m.tex)
	if o.face == nil || fullText == "" || maxPx <= 0 {
		return false
	}
	if font.MeasureString(o.face, fullText).Ceil() <= maxPx {
		return false
	}
	m.tex, m.texW, m.texH = o.renderTextToTex(fullText, 255, 255, 255, 255)
	return true
}

// drawMarqueeCol draws the marquee overlay for one column if active.
// clipX/clipY/clipW/clipH define the panel bounds for scissor clipping.
// rowY is the vertical position of the focused row (texture is drawn there).
// accentX/accentW define the accent highlight area (passed to avoid double-drawing).
// Returns true if the marquee was drawn (caller should skip separate accent highlight).
func (o *Overlay) drawMarqueeCol(m *marqueeState, clipX, clipY, clipW, clipH float32, lh int, rowY float32, accentX, accentW float32, winW, winH, viewW, viewH int) bool {
	if m.tex == 0 {
		return false
	}

	maxOffset := float32(m.texW) - clipW
	if maxOffset <= 0 {
		return false
	}
	// Wrap offset: pause at end, then reset.
	offset := m.offset
	cycle := maxOffset + float32(marqueePauseAt.Seconds())*marqueeSpeed
	if cycle > 0 {
		offset = float32(math.Mod(float64(offset), float64(cycle)))
	}
	if offset > maxOffset {
		offset = maxOffset // pause at end
	}
	// Semi-transparent background matching panel style.
	glDrawFilledRect(o.programRect, clipX, rowY, clipW, float32(lh), 0, 0, 0, panelBgAlpha, winW, winH, viewW, viewH)
	// 2. Accent highlight on top of black background.
	if accentW > 0 {
		drawAccentHighlight(o, accentX, rowY, accentW, float32(lh), winW, winH, viewW, viewH)
	}
	// 3. Marquee text on top of everything.
	glDrawOverlayTextClipped(o.programText, m.tex, 1,
		clipX-offset, rowY, float32(m.texW), float32(m.texH),
		clipX, clipY, clipW, clipH,
		winW, winH, viewW, viewH)
	return true
}

// availableRowTextWidth is the maximum glyph advance that can be rendered in
// a row. renderTextToTex adds four pixels on either side; the scrollbar and
// the clipped row edges must not be covered by text.
func availableRowTextWidth(panelW int) int {
	const texturePad = 4
	const scrollbarW = 4
	const rowEdgePad = 4
	width := panelW - 2*texturePad - scrollbarW - rowEdgePad
	if width < 1 {
		return 1
	}
	return width
}
