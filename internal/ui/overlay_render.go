package ui

import (
	"fmt"
	"image"
	"image/color"
	"path/filepath"
	"strings"

	"github.com/dendec/mdpp/internal/player"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// This file owns all overlay UI rendering: panel layout, texture caching and
// text-to-texture rasterization. State mutation and input handling live in
// overlay.go; GL/cgo calls are isolated behind the wrappers in gl.go.

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
	thirdW := winW / 3

	lh := o.face.Metrics().Height.Ceil()

	switch o.uiPage {
	case PageSettings:
		o.renderSettingsPanels(winW, winH, viewW, viewH, thirdW, panelY, panelH, lh)
	case PagePresets:
		o.renderPresetsPanels(winW, winH, viewW, viewH, thirdW, panelY, panelH, lh)
	default:
		o.renderLibraryPanels(winW, winH, viewW, viewH, thirdW, panelY, panelH, lh)
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
func (o *Overlay) renderLibraryPanels(winW, winH, viewW, viewH int, thirdW, panelY, panelH, lh int) {
	// --- Albums panel ---
	if o.albumsDirty {
		o.rebuildAlbumsTex(thirdW, panelH)
	}
	if o.albumsTex != 0 {
		px, py := float32(0), float32(panelY)
		drawPanelBg(o, px, py, float32(thirdW), float32(panelH), winW, winH, viewW, viewH)
		if o.focusPanel == 0 {
			drawPanelBorder(o, px, py, float32(thirdW), float32(panelH), winW, winH, viewW, viewH)
		}
		glDrawOverlayText(o.programText, o.albumsTex, 1,
			px, py, float32(o.albumsTexW), float32(o.albumsTexH), winW, winH, viewW, viewH)
		if o.panelEntered && o.focusPanel == 0 && len(o.albums) > 0 {
			hiY := py + float32((o.albumCursor-o.albumsScroll)*lh+2)
			drawAccentHighlight(o, px+2, hiY, float32(thirdW-4), float32(lh), winW, winH, viewW, viewH)
		}
		am := panelH / lh
		if am < 1 {
			am = 1
		}
		drawScrollbar(o, px+float32(thirdW-4), py, float32(panelH), len(o.albums), am, o.albumsScroll, winW, winH, viewW, viewH)
	}

	// --- Tracks panel ---
	if o.tracksDirty {
		o.rebuildTracksTex(thirdW, panelH)
	}
	if o.tracksTex != 0 {
		tx, ty := float32(winW*2/3), float32(panelY)
		drawPanelBg(o, tx, ty, float32(thirdW), float32(panelH), winW, winH, viewW, viewH)
		if o.focusPanel == 1 {
			drawPanelBorder(o, tx, ty, float32(thirdW), float32(panelH), winW, winH, viewW, viewH)
		}
		glDrawOverlayText(o.programText, o.tracksTex, 1,
			tx, ty, float32(o.tracksTexW), float32(o.tracksTexH), winW, winH, viewW, viewH)
		if o.panelEntered && o.focusPanel == 1 && len(o.trackInfos) > 0 {
			hiY := ty + float32((o.trackCursor-o.tracksScroll)*lh+2)
			drawAccentHighlight(o, tx+2, hiY, float32(thirdW-4), float32(lh), winW, winH, viewW, viewH)
		}
		tm := panelH / lh
		if tm < 1 {
			tm = 1
		}
		drawScrollbar(o, tx+float32(thirdW-4), ty, float32(panelH), len(o.trackInfos), tm, o.tracksScroll, winW, winH, viewW, viewH)
	}
}

// renderSettingsPanels draws the settings labels (left) and values (right).
func (o *Overlay) renderSettingsPanels(winW, winH, viewW, viewH int, thirdW, panelY, panelH, lh int) {
	if !o.settingsDirty {
		// Still draw textures from cache.
		o.drawSettingsTextures(winW, winH, viewW, viewH, thirdW, panelY, panelH, lh)
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
	var leftLines []string
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
		leftLines = append(leftLines, prefix+row.Label)
	}
	o.rebuildListTex(&o.settingsColL, leftLines)

	// Rebuild right column texture (values for the focused setting) with scroll window.
	var rightLines []string
	rightTotal := 0
	if o.settingsCursor < len(o.settingsRows) {
		row := o.settingsRows[o.settingsCursor]
		rightTotal = len(row.Values)
		selIdx := row.Index
		if o.settingsEditing {
			selIdx = o.settingsValueCursor
		}
		o.tracksScroll = scrollOffset(o.tracksScroll, selIdx, rightTotal, maxRows)
		rightEnd := o.tracksScroll + maxRows
		if rightEnd > rightTotal {
			rightEnd = rightTotal
		}
		for i := o.tracksScroll; i < rightEnd; i++ {
			mark := "  "
			if i == selIdx {
				mark = "▸ "
			}
			rightLines = append(rightLines, mark+row.Values[i])
		}
	}
	o.rebuildListTex(&o.settingsColR, rightLines)

	o.drawSettingsTextures(winW, winH, viewW, viewH, thirdW, panelY, panelH, lh)
}

func (o *Overlay) drawSettingsTextures(winW, winH, viewW, viewH int, thirdW, panelY, panelH, lh int) {
	lx, ly := float32(0), float32(panelY)
	rx, ry := float32(winW*2/3), float32(panelY)
	colW, colH := float32(thirdW), float32(panelH)

	maxRows := panelH / lh
	if maxRows < 1 {
		maxRows = 1
	}

	leftHighlight := o.panelEntered && len(o.settingsRows) > 0 && !o.settingsEditing
	drawListColumn(o, lx, ly, colW, colH, o.settingsColL, o.panelEntered && !o.settingsEditing,
		o.settingsCursor-o.albumsScroll, leftHighlight, lh, winW, winH, viewW, viewH)
	drawScrollbar(o, lx+colW-3, ly, colH, len(o.settingsRows), maxRows, o.albumsScroll, winW, winH, viewW, viewH)

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
		hiY := y + float32(highlightRow*lh+2)
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
	var lines []string
	for i := start; i < end; i++ {
		name := o.albums[i]
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
	var lines []string
	for i := start; i < end; i++ {
		info := o.trackInfos[i]
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

	text := fmt.Sprintf("%s %s  %s/%s", status, title, pos, dur)
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

func (o *Overlay) renderPresetsPanels(winW, winH, viewW, viewH int, thirdW, panelY, panelH, lh int) {
	texturesValid := glIsTexture(o.presetsColL.tex) && glIsTexture(o.presetsColR.tex)
	if !o.presetsDirty && texturesValid {
		o.drawPresetsTextures(winW, winH, viewW, viewH, thirdW, panelY, panelH, lh)
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

	o.drawPresetsTextures(winW, winH, viewW, viewH, thirdW, panelY, panelH, lh)
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

func (o *Overlay) drawPresetsTextures(winW, winH, viewW, viewH int, thirdW, panelY, panelH, lh int) {
	lx, ly := float32(0), float32(panelY)
	rx, ry := float32(winW*2/3), float32(panelY)
	colW, colH := float32(thirdW), float32(panelH)

	maxRows := panelH / lh
	if maxRows < 1 {
		maxRows = 1
	}

	leftHighlight := o.panelEntered && o.focusPanel == 0 && len(o.presetCategories) > 0
	drawListColumn(o, lx, ly, colW, colH, o.presetsColL, o.panelEntered && o.focusPanel == 0,
		o.presetCategoryCursor-o.presetsScrollL, leftHighlight, lh, winW, winH, viewW, viewH)
	drawScrollbar(o, lx+colW-3, ly, colH, len(o.presetCategories), maxRows, o.presetsScrollL, winW, winH, viewW, viewH)

	rightHighlight := false
	rightTotal := 0
	if cat := o.currentCategory(); o.panelEntered && o.focusPanel == 1 && cat != nil && len(cat.Presets) > 0 {
		rightHighlight = true
		rightTotal = len(cat.Presets)
	}
	drawListColumn(o, rx, ry, colW, colH, o.presetsColR, o.panelEntered && o.focusPanel == 1,
		o.presetCursor-o.presetsScrollR, rightHighlight, lh, winW, winH, viewW, viewH)
	drawScrollbar(o, rx+colW-3, ry, colH, rightTotal, maxRows, o.presetsScrollR, winW, winH, viewW, viewH)
}
