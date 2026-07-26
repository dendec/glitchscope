package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/dendec/mdpp/internal/player"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// This file owns rendering primitives shared across pages: the top-level
// renderUI dispatch, panel background/border/scrollbar drawing, the
// two-column list-texture cache (listTex/listRow), generic text-to-texture
// rasterization, the page indicator, and the marquee draw (vs. update,
// which lives in overlay_presets.go). Per-page layout lives in
// render_library.go, render_settings.go, render_presets.go. GL/cgo calls
// are isolated behind the wrappers in gl.go.

const (
	panelWidthPct = 45 // each panel occupies this % of screen width

	// Header/footer layout, expressed as multiples of the current font size
	// so the whole overlay scales with it.
	headerTextRowsFactor = 2.0 // vertical space reserved for the stats + preset-name lines
	pageIndicatorHFactor = 1.0 // page indicator row height (was 1.5 — left a visible empty band)
	bottomBarHFactor     = 1.0 // playback status bar height (was 1.1 — bar now hugs the bottom edge tighter)
	panelBottomGapFactor = 0.5 // gap left between the panels and the bottom bar (was 0.7)

	headerMarginX = 5 // left margin for the stats/preset-name text
	headerLineGap = 4 // vertical gap between the stacked stats/preset-name lines
)

func (o *Overlay) renderUI(winW, winH, viewW, viewH int) {
	if o.programRect == 0 {
		return
	}

	bottomH := int(o.fontSize * bottomBarHFactor)
	indicatorH := int(o.fontSize * pageIndicatorHFactor)
	headerH := int(o.fontSize*headerTextRowsFactor) + indicatorH
	panelY := headerH
	panelH := winH - panelY - bottomH - int(o.fontSize*panelBottomGapFactor)
	if panelH < 0 {
		panelH = 0
	}
	panelW := winW * panelWidthPct / 100

	// Header backdrop — only behind the stats/preset-name/page-indicator
	// strip, not the whole screen, so the visualization stays visible
	// through the center and between panels.
	hR, hG, hB := o.panelBgRGB()
	glDrawFilledRect(o.programRect, 0, 0, float32(winW), float32(headerH), hR, hG, hB, o.bgAlpha(), winW, winH, viewW, viewH)

	// Stats line (FPS, MEM, CPU, GPU).
	if o.statsDirty {
		o.rebuildStatsTex()
	}
	if o.statsTex != 0 {
		glDrawOverlayText(o.programText, o.statsTex, 1,
			headerMarginX, 0, float32(o.statsTexW), float32(o.statsTexH), winW, winH, viewW, viewH)
	}
	// Preset name — below stats.
	if o.presetNameDirty {
		o.rebuildPresetNameTex()
	}
	if o.presetNameTex != 0 {
		glDrawOverlayText(o.programText, o.presetNameTex, 1,
			headerMarginX, float32(o.statsTexH+headerLineGap), float32(o.presetNameTexW), float32(o.presetNameTexH), winW, winH, viewW, viewH)
	}

	// Page indicator.
	o.renderPageIndicator(winW, winH, viewW, viewH)

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
	showBar := o.playingTrack != "" || o.loading
	if showBar && o.bottomDirty {
		o.rebuildBottomTex(winW, bottomH)
	}
	if showBar && o.bottomTex != 0 {
		by := float32(winH - bottomH)
		bR, bG, bB := o.panelBgRGB()
		glDrawFilledRect(o.programRect, 0, by, float32(winW), float32(bottomH), bR, bG, bB, o.bgAlpha(), winW, winH, viewW, viewH)
		textY := by + (float32(bottomH)-float32(o.bottomTexH))/2
		glDrawOverlayText(o.programText, o.bottomTex, 1,
			0, textY, float32(o.bottomTexW), float32(o.bottomTexH), winW, winH, viewW, viewH)
	}
}

// --- Panel drawing helpers ---

func drawPanelBg(o *Overlay, x, y, w, h float32, winW, winH, viewW, viewH int) {
	pR, pG, pB := o.panelBgRGB()
	glDrawFilledRect(o.programRect, x, y, w, h, pR, pG, pB, o.bgAlpha(), winW, winH, viewW, viewH)
}

func drawPanelBorder(o *Overlay, x, y, w, h float32, winW, winH, viewW, viewH int) {
	bR, bG, bB := o.borderRGB()
	bw := float32(o.borderWidthPx())
	glDrawFilledRect(o.programRect, x-bw, y-bw, w+2*bw, bw, bR, bG, bB, 1, winW, winH, viewW, viewH)
	glDrawFilledRect(o.programRect, x-bw, y+h, w+2*bw, bw, bR, bG, bB, 1, winW, winH, viewW, viewH)
	glDrawFilledRect(o.programRect, x-bw, y-bw, bw, h+2*bw, bR, bG, bB, 1, winW, winH, viewW, viewH)
	glDrawFilledRect(o.programRect, x+w, y-bw, bw, h+2*bw, bR, bG, bB, 1, winW, winH, viewW, viewH)
}

// drawScrollbar draws a track+thumb scrollbar in the current theme's text
// color, so it always reads consistently against the panel background.
func drawScrollbar(o *Overlay, sbX, panelY, panelH float32, totalItems, visibleItems, scrollPos int, winW, winH, viewW, viewH int) {
	if totalItems <= visibleItems {
		return
	}
	tR, tG, tB := o.textColor()
	r, g, b := float32(tR)/255, float32(tG)/255, float32(tB)/255
	thumbW := float32(o.scrollbarWidthPx())
	// Track.
	glDrawFilledRect(o.programRect, sbX, panelY, thumbW, panelH, r, g, b, 0.15, winW, winH, viewW, viewH)
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
	glDrawFilledRect(o.programRect, sbX, thumbY, thumbW, thumbH, r, g, b, 0.7, winW, winH, viewW, viewH)
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
			lines = append(lines, o.truncateEnd(row.text, maxTextW))
		}
		bold = append(bold, row.bold)
	}
	tR, tG, tB := o.textColor()
	return o.renderTextToTexBold(strings.Join(lines, "\n"), minW, bold, tR, tG, tB, 255)
}

// drawListColumn draws one column of a two-column list panel: background,
// optional focus border, and cached text. The selected row is already
// rendered bold into the cached texture (see listRow.bold); no separate
// highlight overlay is drawn.
// Shared by Settings and Presets so their layout logic stays in one place.
func drawListColumn(o *Overlay, x, y, w, h float32, t listTex, bordered bool, winW, winH, viewW, viewH int) {
	drawPanelBg(o, x, y, w, h, winW, winH, viewW, viewH)
	if bordered {
		drawPanelBorder(o, x, y, w, h, winW, winH, viewW, viewH)
	}
	if t.tex == 0 {
		return
	}
	glDrawOverlayText(o.programText, t.tex, 1, x, y, float32(t.w), float32(t.h), winW, winH, viewW, viewH)
}

func (o *Overlay) rebuildStatsTex() {
	o.statsDirty = false
	o.deleteTex(&o.statsTex)
	tR, tG, tB := o.textColor()
	o.statsTex, o.statsTexW, o.statsTexH = o.renderTextToTex(o.statsLine, tR, tG, tB, 255)
}

func (o *Overlay) rebuildPresetNameTex() {
	o.presetNameDirty = false
	o.deleteTex(&o.presetNameTex)
	tR, tG, tB := o.textColor()
	o.presetNameTex, o.presetNameTexW, o.presetNameTexH = o.renderTextToTex(o.presetName, tR, tG, tB, 255)
}

func (o *Overlay) rebuildBottomTex(w, botH int) {
	o.bottomDirty = false
	o.deleteTex(&o.bottomTex)

	if o.playingTrack == "" && !o.loading {
		return
	}

	title := ""
	if o.playingTrack != "" {
		title = player.TrackTitle(o.playingTrack)
		maxTitleW := w - int(o.fontSize*14)
		title = o.truncateEnd(title, maxTitleW)
	}

	// Loading indicator — shown while a background load is in flight.
	if o.loading {
		text := "⏳ " + title
		if title == "" {
			text = "⏳ loading…"
		}
		if o.loadPercent >= 0 {
			text += fmt.Sprintf("  %d%%", o.loadPercent)
		}
		tR, tG, tB := o.textColor()
		o.bottomTex, o.bottomTexW, o.bottomTexH = o.renderTextToTex(text, tR, tG, tB, 255)
		return
	}

	// Normal playback status.
	pos := formatDuration(o.position)
	dur := formatDuration(o.duration)
	status := "▶"
	if o.paused {
		status = "⏸"
	}

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
	tR, tG, tB := o.textColor()
	o.bottomTex, o.bottomTexW, o.bottomTexH = o.renderTextToTex(text, tR, tG, tB, 255)
}

// truncateEnd shortens s so it fits within maxPx pixels, dropping characters
// from the end and appending an ellipsis. Returns s unchanged if it already
// fits.
func (o *Overlay) truncateEnd(s string, maxPx int) string {
	if o.face == nil || maxPx <= 0 {
		return s
	}
	if font.MeasureString(o.face, s).Ceil() <= maxPx {
		return s
	}
	const ellipsis = "…"
	runes := []rune(s)
	for keep := len(runes) - 1; keep > 0; keep-- {
		candidate := string(runes[:keep]) + ellipsis
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

const (
	pageIndicatorGapFactor = 2.0 // horizontal gap between page indicator labels, in font-size units
	pageIndicatorYFactor   = 0.5 // vertical offset of the page indicator row, in font-size units
)

func (o *Overlay) renderPageIndicator(winW, winH, viewW, viewH int) {
	if o.pageIndicatorDirty {
		o.rebuildPageIndicatorTextures()
	}
	pages := []string{"Library", "Settings", "Presets"}
	gap := int(o.fontSize * pageIndicatorGapFactor)

	// Compute total width.
	totalW := 0
	for i := range pages {
		totalW += o.pageIndicatorTexW[i]
		if i > 0 {
			totalW += gap
		}
	}

	startX := (winW - totalW) / 2
	y := int(o.fontSize * pageIndicatorYFactor)

	x := startX
	for i := range pages {
		// Active page is marked by square brackets in the label itself (see
		// rebuildPageIndicatorTextures) — no separate highlight box.
		if o.pageIndicatorTex[i] != 0 {
			glDrawOverlayText(o.programText, o.pageIndicatorTex[i], 1,
				float32(x), float32(y), float32(o.pageIndicatorTexW[i]), float32(o.pageIndicatorTexH[i]), winW, winH, viewW, viewH)
		}
		x += o.pageIndicatorTexW[i] + gap
	}
}

func (o *Overlay) rebuildPageIndicatorTextures() {
	o.pageIndicatorDirty = false
	pages := []string{"Library", "Settings", "Presets"}
	tR, tG, tB := o.textColor()
	for i, name := range pages {
		o.deleteTex(&o.pageIndicatorTex[i])
		label := "  " + name + "  "
		if UIPage(i) == o.uiPage {
			label = "[ " + name + " ]"
		}
		o.pageIndicatorTex[i], o.pageIndicatorTexW[i], o.pageIndicatorTexH[i] =
			o.renderTextToTex(label, tR, tG, tB, 255)
	}
}

// --- Marquee draw ---

// rebuildMarqueeLine builds a single-line texture for the focused item if its
// full text doesn't fit within maxPx. Rendered bold, matching the cursor row
// style used everywhere else. Returns true if the texture was built (i.e.
// the item is truncated and needs marquee scrolling).
func (o *Overlay) rebuildMarqueeLine(m *marqueeState, fullText string, maxPx int) bool {
	o.deleteTex(&m.tex)
	if o.face == nil || fullText == "" || maxPx <= 0 {
		return false
	}
	if font.MeasureString(o.face, fullText).Ceil() <= maxPx {
		return false
	}
	tR, tG, tB := o.textColor()
	m.tex, m.texW, m.texH = o.renderTextToTexBold(fullText, 0, []bool{true}, tR, tG, tB, 255)
	return true
}

// drawMarqueeCol draws the marquee overlay for one column if active.
// clipX/clipY/clipW/clipH define the panel bounds for scissor clipping.
// rowY is the vertical position of the focused row (texture is drawn there).
// Returns true if the marquee was drawn.
func (o *Overlay) drawMarqueeCol(m *marqueeState, clipX, clipY, clipW, clipH float32, lh int, rowY float32, winW, winH, viewW, viewH int) bool {
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
	pR, pG, pB := o.panelBgRGB()
	glDrawFilledRect(o.programRect, clipX, rowY, clipW, float32(lh), pR, pG, pB, o.bgAlpha(), winW, winH, viewW, viewH)
	// Marquee text on top.
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
