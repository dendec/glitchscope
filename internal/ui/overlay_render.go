package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/dendec/glitchscope/internal/i18n"
	"github.com/dendec/glitchscope/internal/player"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// This file owns rendering primitives: renderUI dispatch, panel drawing,
// list-texture cache, text rasterization, page indicator, and marquee draw.
// Per-page layout lives in render_library.go, render_settings.go,
// render_presets.go. GL/cgo calls are in gl.go.

const (
	panelWidthPct = 50 // each panel occupies this % of screen width

	headerMarginAt480      = 5
	headerExtraHeightAt480 = 2

	// Design-time alpha values for decorative UI elements. Scaled by
	// uiAlpha() at draw time so they respect the Transparency setting.
	alphaAccentHighlight = float32(0.33) // cursor / selection highlight
	alphaTrackBg         = float32(0.15) // progress bar background, scrollbar track
	alphaTrackFill       = float32(0.70) // progress bar fill, scrollbar thumb
)

func (o *Overlay) renderUI(winW, winH, viewW, viewH int) {
	if o.programRect == 0 {
		return
	}

	// Draw preview background full-screen before any overlay elements.
	// This is the thumbnail texture stretched to fill the window on the
	// presets page.
	if o.previewBgTex != 0 {
		glDrawOverlayImage(o.programImage, o.previewBgTex, 1,
			0, 0, float32(winW), float32(winH), winW, winH, viewW, viewH)
	}

	// Rebuild header/footer text textures up front so they're ready before layout/draw below.
	if o.presetNameDirty {
		o.rebuildPresetNameTex()
	}
	if o.pageIndicatorDirty {
		o.rebuildPageIndicatorTextures()
	}
	if o.showsBreadcrumb() {
		o.rebuildBreadcrumbTex(winW)
	}
	if (o.playingPath != "" || o.loading) && o.bottomDirty {
		o.rebuildBottomTex(winW, o.face.Metrics().Height.Ceil())
	}

	// The menu header contains centered page navigation and, where meaningful,
	// a second row for the current navigation path. The same geometry is used
	// by pointer hit-testing.
	l := o.overlayLayout(winW, winH)
	lh := l.lineH
	navigationHeaderH := l.navigationHeaderHeight
	headerH := l.headerHeight
	statusRowH := l.statusRowH
	presetLineH := l.presetLineH
	hintRowH := l.hintRowH
	bottomH := l.bottomH
	panelY := l.panelY
	panelH := l.panelH
	panelW := l.panelW

	// Header backdrop.
	hR, hG, hB := o.panelBgRGB()
	glDrawFilledRect(o.programRect, 0, 0, float32(winW), float32(headerH), hR, hG, hB, o.bgAlpha(), winW, winH, viewW, viewH)

	o.renderPageIndicator(winW, winH, viewW, viewH)
	if o.pointerCloseVisible() {
		closeSize := max(lh, o.scalePx(24))
		closeX := winW - o.headerMarginX() - closeSize
		closeY := (headerH - closeSize) / 2
		if closeY < 0 {
			closeY = 0
		}
		o.drawIcon(iconClose, float32(closeX), float32(closeY), float32(closeSize), winW, winH, viewW, viewH)
	}
	if o.showsBreadcrumb() {
		headerMargin := o.headerMarginX()
		breadcrumbY := float32(navigationHeaderH - textPadding(o.fontSize))
		placements := o.breadcrumbPlacements(winW)
		textX := float32(headerMargin)
		if len(placements) > 0 && placements[0].icon != "" {
			o.drawIcon(iconHome, placements[0].x, float32(navigationHeaderH), float32(lh), winW, winH, viewW, viewH)
			// Text textures include transparent outline padding. Start the
			// texture inside the icon slot so the visible glyph begins at the
			// slot's end, keeping the visual gap equal to other icon labels.
			textX += placements[0].w - float32(textPadding(o.fontSize))
		}
		if o.breadcrumbTex != 0 {
			glDrawOverlayText(o.programText, o.breadcrumbTex, 1,
				textX, breadcrumbY, float32(o.breadcrumbTexW), float32(o.breadcrumbTexH), winW, winH, viewW, viewH)
		}
	}

	switch o.uiPage {
	case PageSettings:
		o.renderSettingsPanels(winW, winH, viewW, viewH, panelW, panelY, panelH, lh)
	case PagePresets:
		o.renderPresetsPanels(winW, winH, viewW, viewH, panelW, panelY, panelH, lh)
	case PageHelp:
		o.renderHelpPanels(winW, winH, viewW, viewH, panelW, panelY, panelH, lh)
	default:
		o.renderLibraryPanels(winW, winH, viewW, viewH, panelW, panelY, panelH, lh)
	}

	showBar := o.playingPath != "" || o.loading || o.presetNameTex != 0
	if showBar {
		by := float32(winH - bottomH)
		barBackdropH := float32(presetLineH + statusRowH)
		bR, bG, bB := o.panelBgRGB()
		glDrawFilledRect(o.programRect, 0, by, float32(winW), barBackdropH, bR, bG, bB, o.bgAlpha(), winW, winH, viewW, viewH)

		if o.presetNameTex != 0 {
			headerMargin := o.headerMarginX()
			if !o.drawMarquee(&o.presetNameMarquee, float32(headerMargin), by, float32(winW-headerMargin*2), float32(o.presetNameTexH), winW, winH, viewW, viewH) {
				glDrawOverlayText(o.programText, o.presetNameTex, 1,
					float32(headerMargin), by, float32(o.presetNameTexW), float32(o.presetNameTexH), winW, winH, viewW, viewH)
			}
		}
		if o.bottomTex != 0 || o.bottomPrefixTex != 0 {
			statusY := by + float32(presetLineH)
			if o.bottomPrefixTex != 0 {
				glDrawOverlayText(o.programText, o.bottomPrefixTex, 1,
					0, statusY, float32(o.bottomPrefixTexW), float32(o.bottomPrefixTexH), winW, winH, viewW, viewH)
				if o.bottomMarquee.tex != 0 && float32(o.bottomMarquee.texW) > float32(o.bottomTitleW) {
					o.drawMarquee(&o.bottomMarquee, float32(o.bottomTitleX), statusY, float32(o.bottomTitleW), float32(o.bottomTexH), winW, winH, viewW, viewH)
				} else if o.bottomTex != 0 {
					glDrawOverlayText(o.programText, o.bottomTex, 1,
						float32(o.bottomTitleX), statusY, float32(o.bottomTexW), float32(o.bottomTexH), winW, winH, viewW, viewH)
				}
				if o.bottomSuffixTex != 0 {
					glDrawOverlayText(o.programText, o.bottomSuffixTex, 1,
						float32(o.bottomTitleX+o.bottomTitleW), statusY, float32(o.bottomSuffixTexW), float32(o.bottomSuffixTexH), winW, winH, viewW, viewH)
				}
			} else {
				glDrawOverlayText(o.programText, o.bottomTex, 1,
					0, statusY, float32(o.bottomTexW), float32(o.bottomTexH), winW, winH, viewW, viewH)
			}
		}
	}

	// Progress bar under the bottom line.
	if !o.loading && o.duration > 0 {
		barH := o.scalePx(2)
		tc := o.textColor()
		r, g, b := float32(tc.R)/255, float32(tc.G)/255, float32(tc.B)/255
		barY := float32(winH - hintRowH - barH)
		glDrawFilledRect(o.programRect, 0, barY, float32(winW), float32(barH), r, g, b, o.uiAlpha(alphaTrackBg), winW, winH, viewW, viewH)
		progress := o.position / o.duration
		if progress > 1 {
			progress = 1
		}
		if progress < 0 {
			progress = 0
		}
		glDrawFilledRect(o.programRect, 0, barY, float32(float64(winW)*progress), float32(barH), r, g, b, o.uiAlpha(alphaTrackFill), winW, winH, viewW, viewH)
	}

	// Context action-hints footer: always shown across pages, at the very bottom.
	o.renderActionHints(winW, winH, viewW, viewH)
}

// --- Panel drawing helpers ---

func drawPanelBg(o *Overlay, x, y, w, h float32, winW, winH, viewW, viewH int) {
	pR, pG, pB := o.panelBgRGB()
	glDrawFilledRect(o.programRect, x, y, w, h, pR, pG, pB, o.bgAlpha(), winW, winH, viewW, viewH)
}

// drawPanelBorder draws the focus border inset along the panel's own edges
// (not straddling the rect), so it never bleeds into the header/bottom bars
// and the panels can sit flush against them.
func drawPanelBorder(o *Overlay, x, y, w, h float32, winW, winH, viewW, viewH int) {
	bR, bG, bB := o.borderRGB()
	bw := float32(o.borderWidthPx())
	glDrawFilledRect(o.programRect, x, y, w, bw, bR, bG, bB, 1, winW, winH, viewW, viewH)
	glDrawFilledRect(o.programRect, x, y+h-bw, w, bw, bR, bG, bB, 1, winW, winH, viewW, viewH)
	glDrawFilledRect(o.programRect, x, y, bw, h, bR, bG, bB, 1, winW, winH, viewW, viewH)
	glDrawFilledRect(o.programRect, x+w-bw, y, bw, h, bR, bG, bB, 1, winW, winH, viewW, viewH)
}

// drawScrollbar draws a track+thumb scrollbar in the theme scrollbar color.
func drawScrollbar(o *Overlay, sbX, panelY, panelH float32, totalItems, visibleItems, scrollPos int, winW, winH, viewW, viewH int) {
	if totalItems <= visibleItems {
		return
	}
	tc := o.palette().scrollbar
	r, g, b := float32(tc.R)/255, float32(tc.G)/255, float32(tc.B)/255
	thumbW := float32(o.scrollbarWidthPx())
	// Track.
	glDrawFilledRect(o.programRect, sbX, panelY, thumbW, panelH, r, g, b, o.uiAlpha(alphaTrackBg), winW, winH, viewW, viewH)
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
	glDrawFilledRect(o.programRect, sbX, thumbY, thumbW, thumbH, r, g, b, o.uiAlpha(alphaTrackFill), winW, winH, viewW, viewH)
}

// scrollOffset returns the first visible row keeping cursor within the window.
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

// listTex caches a rendered text texture for one column of a two-column list.
type listTex struct {
	tex  uint32
	w, h int
}

type listRow struct {
	text   string
	active bool
	bold   bool
}

// rebuildListRows renders rows, reserving the active row for marquee.
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
	return o.renderTextToTexBold(strings.Join(lines, "\n"), minW, bold, o.textColor())
}

// drawListColumn draws one column of a two-column list: background, cursor,
// optional focus border, and cached text.
func drawListColumn(o *Overlay, x, y, w, h float32, t listTex, bordered bool, cursor, scroll, lh int, winW, winH, viewW, viewH int) {
	drawPanelBg(o, x, y, w, h, winW, winH, viewW, viewH)
	// Text textures include outline padding around the glyphs. Keep that
	// padding outside the first row so the visible glyph rows share the same
	// boundaries as pointer hit-testing and cursor navigation.
	padding := float32(textPadding(o.fontSize))
	if bordered && cursor >= scroll && cursor < scroll+int(h)/lh {
		rowY := y + float32((cursor-scroll)*lh) - padding
		o.drawCursorHighlight(x, rowY, w, float32(lh), winW, winH, viewW, viewH)
	}
	if bordered {
		drawPanelBorder(o, x, y, w, h, winW, winH, viewW, viewH)
	}
	if t.tex == 0 {
		return
	}
	glDrawOverlayTextClipped(o.programText, t.tex, 1,
		x, y-padding, float32(t.w), float32(t.h), x, y, w, h,
		winW, winH, viewW, viewH)
}

func (o *Overlay) rebuildStatsTex() {
	o.statsDirty = false
	o.deleteTex(&o.statsTex)
	o.statsMarquee.invalidate(o)
	o.statsTex, o.statsTexW, o.statsTexH = renderPlainTextToTex(o.statsLine, o.statsTextFace(), statsFontSize(o.fontSize), o.textColor())
}

// rebuildBreadcrumbTex re-renders the current navigation path only when its
// text or presentation constraints have changed.
func (o *Overlay) rebuildBreadcrumbTex(winW int) {
	if o.face == nil {
		return
	}
	text := o.breadcrumbDisplayText(winW)
	if !o.breadcrumbDirty && text == o.breadcrumbTextCache {
		return
	}
	o.breadcrumbDirty = false
	o.breadcrumbTextCache = text
	o.deleteTex(&o.breadcrumbTex)
	if text == "" {
		return
	}
	o.breadcrumbTex, o.breadcrumbTexW, o.breadcrumbTexH = o.renderTextToTex(text, o.textColor())
}

func (o *Overlay) statsTextFace() font.Face {
	if o.statsFace != nil {
		return o.statsFace
	}
	return o.face
}

func renderPlainTextToTex(text string, face font.Face, fontSize float64, textColor color.RGBA) (uint32, int, int) {
	if face == nil || text == "" {
		return 0, 0, 0
	}
	metrics := face.Metrics()
	lines := strings.Split(text, "\n")
	lineHeight := metrics.Height.Ceil()
	bounds := make([]fixed.Rectangle26_6, len(lines))
	contentW := 0
	for i, line := range lines {
		bounds[i], _ = font.BoundString(face, line)
		if width := (bounds[i].Max.X - bounds[i].Min.X).Ceil(); width > contentW {
			contentW = width
		}
	}
	contentH := lineHeight * len(lines)
	if contentW <= 0 || contentH <= 0 {
		return 0, 0, 0
	}
	rgba, texW, texH, _ := newShadowedTextRGBA(contentW, contentH, fontSize, textColor, func(rgba *image.RGBA, originX, originY int) {
		for i, line := range lines {
			d := &font.Drawer{
				Dst:  rgba,
				Src:  image.NewUniform(textColor),
				Face: face,
				Dot: fixed.Point26_6{
					X: fixed.I(originX) - bounds[i].Min.X,
					Y: fixed.I(originY+i*lineHeight) + metrics.Ascent,
				},
			}
			d.DrawString(line)
		}
	})
	return glUploadTexture(rgba), texW, texH
}

func (o *Overlay) rebuildPresetNameTex() {
	o.presetNameDirty = false
	o.deleteTex(&o.presetNameTex)
	maxW := o.screenW - o.headerMarginX()*2
	display := o.truncateEnd(o.presetName, maxW)
	o.presetNameTex, o.presetNameTexW, o.presetNameTexH = o.renderTextToTex(display, o.textColor())
	o.rebuildMarqueeLine(&o.presetNameMarquee, o.presetName, maxW, false)
}

func (o *Overlay) displayTrackPath(path string) string {
	if player.IsRadio(path) {
		station := o.playingAlbum
		if station == "" {
			station = strings.TrimPrefix(path, "radio:")
		}
		if title := strings.TrimSpace(o.radioNowPlayingTitle); o.radioNowPlayingPath == path && title != "" {
			return "radio/" + station + "/" + title
		}
		return "radio/" + station
	}
	if player.IsModArchive(path) {
		remote := player.RemotePath(path)
		entryName := ""
		if parsed, err := url.Parse(remote); err == nil {
			if parsed.Path != "" {
				remote = parsed.Path
			}
			entryName = parsed.Fragment
		}
		remote = strings.TrimPrefix(filepath.ToSlash(remote), "/")
		if strings.HasPrefix(remote, "modarchive_") {
			if slash := strings.IndexByte(remote, '/'); slash >= 0 {
				catalog := remote[:slash]
				if strings.HasSuffix(catalog, "_additions") {
					remote = strings.TrimSuffix(strings.TrimPrefix(catalog, "modarchive_"), "_additions") + remote[slash:]
				}
			}
		}
		remote = strings.TrimSuffix(remote, ".zip")
		if entryName != "" {
			entryName = strings.TrimPrefix(filepath.ToSlash(entryName), "/")
			if strings.HasSuffix(strings.ToLower(entryName), ".zip") {
				entryName = entryName[:len(entryName)-4]
			}
			if entryName != "" {
				remote = strings.TrimSuffix(remote, "/") + "/" + entryName
			}
		}
		return "modarchive/" + remote
	}
	if player.IsModland(path) {
		return "modland/" + strings.TrimPrefix(filepath.ToSlash(player.RemotePath(path)), "/")
	}
	if o.baseDir == "" || !filepath.IsAbs(path) {
		return filepath.ToSlash(path)
	}
	relative, err := filepath.Rel(o.baseDir, path)
	if err != nil || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}

func (o *Overlay) rebuildBottomTex(w, botH int) {
	o.bottomDirty = false
	o.deleteTex(&o.bottomTex)
	o.deleteTex(&o.bottomPrefixTex)
	o.deleteTex(&o.bottomSuffixTex)
	o.bottomTitleX = 0
	o.bottomTitleW = 0

	if o.playingPath == "" && !o.loading {
		return
	}

	title := ""
	if o.playingPath != "" {
		title = o.displayTrackPath(o.playingPath)
	}

	// Loading indicator.
	if o.loading {
		text := "⏳ " + title
		if title == "" {
			text = "⏳ " + o.catalog.Text(i18n.ValueLoading)
		}
		if o.loadPercent >= 0 {
			text += fmt.Sprintf("  %d%%", o.loadPercent)
		}
		o.rebuildBottomText(text, w)
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

	suffix := fmt.Sprintf("  %s", pos)
	if o.duration > 0 {
		suffix = fmt.Sprintf("  %s/%s", pos, dur)
	}
	if info != "" {
		suffix += "  " + info
	}
	prefix := status + " "
	o.bottomTitleX = font.MeasureString(o.face, prefix).Ceil()
	o.bottomTitleW = w - o.bottomTitleX - font.MeasureString(o.face, suffix).Ceil()
	if o.bottomTitleW < 1 {
		o.bottomTitleW = 1
	}
	displayTitle := o.truncateEnd(title, o.bottomTitleW)
	o.bottomPrefixTex, o.bottomPrefixTexW, o.bottomPrefixTexH = o.renderTextToTex(prefix, o.textColor())
	o.bottomTex, o.bottomTexW, o.bottomTexH = o.renderTextToTex(displayTitle, o.textColor())
	o.bottomSuffixTex, o.bottomSuffixTexW, o.bottomSuffixTexH = o.renderTextToTex(suffix, o.textColor())
	o.rebuildMarqueeLine(&o.bottomMarquee, title, o.bottomTitleW, false)
}

func (o *Overlay) rebuildBottomText(text string, maxW int) {
	o.deleteTex(&o.bottomTex)
	display := o.truncateEnd(text, maxW)
	o.bottomTex, o.bottomTexW, o.bottomTexH = o.renderTextToTex(display, o.textColor())
	if o.loading {
		o.rebuildMarqueeLine(&o.bottomMarquee, text, maxW, false)
	}
}

// truncateEnd shortens s to fit maxPx, appending ellipsis.
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

func (o *Overlay) renderTextToTex(text string, textColor color.RGBA) (uint32, int, int) {
	return o.renderTextToTexBold(text, 0, nil, textColor)
}

// renderTextToTexBold renders text with faux-bold for lines marked in bold.
// Supports inline bold via **text** markers within lines.
func (o *Overlay) renderTextToTexBold(text string, minW int, bold []bool, textColor color.RGBA) (uint32, int, int) {
	if o.face == nil {
		return 0, 0, 0
	}
	lines := strings.Split(text, "\n")
	metrics := o.face.Metrics()
	lineHeight := metrics.Height.Ceil()
	bounds := make([]fixed.Rectangle26_6, len(lines))
	contentW := 0
	for i, line := range lines {
		clean := strings.ReplaceAll(line, "**", "")
		bounds[i], _ = font.BoundString(o.face, clean)
		if width := (bounds[i].Max.X - bounds[i].Min.X).Ceil(); width > contentW {
			contentW = width
		}
	}
	padding := textPadding(o.fontSize)
	if minContentW := minW - padding*2; contentW < minContentW {
		contentW = minContentW
	}
	contentH := lineHeight * len(lines)
	if contentW <= 0 || contentH <= 0 {
		return 0, 0, 0
	}

	rgba, texW, texH, _ := newShadowedTextRGBA(contentW, contentH, o.fontSize, textColor, func(rgba *image.RGBA, originX, originY int) {
		for i, line := range lines {
			startDot := fixed.Point26_6{
				X: fixed.I(originX) - bounds[i].Min.X,
				Y: fixed.I(originY+i*lineHeight) + metrics.Ascent,
			}
			isLineBold := i < len(bold) && bold[i]
			hasMarkers := strings.Contains(line, "**")
			if isLineBold || hasMarkers {
				segments := strings.Split(line, "**")
				d := &font.Drawer{
					Dst:  rgba,
					Src:  image.NewUniform(textColor),
					Face: o.face,
					Dot:  startDot,
				}
				for j, seg := range segments {
					if seg == "" {
						continue
					}
					segStart := d.Dot
					if j%2 == 1 || isLineBold {
						d.DrawString(seg)
						d.Dot = segStart
						d.Dot.X += fixed.I(1)
						d.DrawString(seg)
						d.Dot = segStart
						d.Dot.X += fixed.I(font.MeasureString(o.face, seg).Ceil())
					} else {
						d.DrawString(seg)
					}
				}
			} else {
				d := &font.Drawer{
					Dst:  rgba,
					Src:  image.NewUniform(textColor),
					Face: o.face,
					Dot:  startDot,
				}
				d.DrawString(line)
			}
		}
	})

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

func formatSize(bytes int64) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%d B", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	}
}

// --- Page indicator ---

const (
	pageIndicatorGapFactor = 0.75 // horizontal gap between page indicator labels, in font-size units
)

func statsFontSize(fontSize float64) float64 {
	return max(10, math.Round(fontSize*0.75))
}

func (o *Overlay) headerMarginX() int {
	return o.scalePx(headerMarginAt480)
}

func (o *Overlay) headerHeight(lineHeight int) int {
	return lineHeight + o.scalePx(headerExtraHeightAt480)
}

func (o *Overlay) showsBreadcrumb() bool {
	return o.uiPage == PageLibrary || o.uiPage == PagePresets
}

func (o *Overlay) menuHeaderHeight(lineHeight int) int {
	height := o.headerHeight(lineHeight)
	if o.showsBreadcrumb() {
		height += lineHeight
	}
	return height
}

func centeredTextY(containerHeight, lineHeight, padding int) int {
	return (containerHeight-lineHeight)/2 - padding
}

func (o *Overlay) pageIndicatorWidth() int {
	width := 0
	iconSlot := o.pageIndicatorIconSize() + o.scalePx(2)
	for i, textureWidth := range o.pageIndicatorTexW {
		width += iconSlot + textureWidth
		if i > 0 {
			width += int(o.fontSize * pageIndicatorGapFactor)
		}
	}
	return width + iconSlot*2
}

func (o *Overlay) pageIndicatorIconSize() int {
	if o.face != nil {
		return o.face.Metrics().Height.Ceil()
	}
	return int(math.Round(o.fontSize))
}

func (o *Overlay) pageIndicatorX(winW int) int {
	return (winW - o.pageIndicatorWidth()) / 2
}

// renderPageIndicator draws the page names (Library/Presets/Settings/Help)
// centered on the menu header row.
func (o *Overlay) renderPageIndicator(winW, winH, viewW, viewH int) {
	if o.pageIndicatorDirty {
		o.rebuildPageIndicatorTextures()
	}
	pages := []string{o.catalog.Text(i18n.PageLibrary), o.catalog.Text(i18n.PagePresets), o.catalog.Text(i18n.PageSettings), o.catalog.Text(i18n.PageHelp)}
	gap := int(o.fontSize * pageIndicatorGapFactor)

	x := o.pageIndicatorX(winW)
	lh := o.face.Metrics().Height.Ceil()
	headerH := o.headerHeight(lh)
	y := float32(centeredTextY(headerH, lh, textPadding(o.fontSize)))
	iconY := float32((headerH - lh) / 2)
	iconGap := o.scalePx(2)
	o.ensureIconTextures(lh, o.textColor())
	o.drawIcon(iconPagePrev, float32(x), iconY, float32(lh), winW, winH, viewW, viewH)
	x += lh + iconGap

	for i := range pages {
		o.drawIcon(pageIconName(UIPage(i)), float32(x), iconY, float32(lh), winW, winH, viewW, viewH)
		x += lh + iconGap
		if o.pageIndicatorTex[i] != 0 {
			glDrawOverlayText(o.programText, o.pageIndicatorTex[i], 1,
				float32(x), y, float32(o.pageIndicatorTexW[i]), float32(o.pageIndicatorTexH[i]), winW, winH, viewW, viewH)
		}
		x += o.pageIndicatorTexW[i] + gap
	}
	o.drawIcon(iconPageNext, float32(x-gap), iconY, float32(lh), winW, winH, viewW, viewH)
}

func (o *Overlay) rebuildPageIndicatorTextures() {
	o.pageIndicatorDirty = false
	pages := []string{o.catalog.Text(i18n.PageLibrary), o.catalog.Text(i18n.PagePresets), o.catalog.Text(i18n.PageSettings), o.catalog.Text(i18n.PageHelp)}
	textColor := o.textColor()
	for i, name := range pages {
		o.deleteTex(&o.pageIndicatorTex[i])
		label := " " + name + " "
		if UIPage(i) == o.uiPage {
			label = "[" + name + "]"
		}
		o.pageIndicatorTex[i], o.pageIndicatorTexW[i], o.pageIndicatorTexH[i] = o.renderTextToTex(label, textColor)
	}
}

// --- Marquee draw ---

// drawMarqueeCol draws the marquee overlay for one column if active.
func (o *Overlay) drawMarqueeCol(m *marqueeState, clipX, clipY, clipW, clipH float32, lh int, rowY float32, winW, winH, viewW, viewH int) bool {
	if m.tex == 0 {
		return false
	}

	textX := clipX
	borderW := float32(o.borderWidthPx())
	clipX += borderW
	clipY += borderW
	clipW -= borderW * 2
	clipH -= borderW * 2
	if clipW <= 0 || clipH <= 0 || float32(m.texW) <= clipW {
		return false
	}
	offset := marqueeOffset(m.offset, m.texW, int(clipW))
	glDrawOverlayTextClipped(o.programText, m.tex, 1,
		textX-offset, rowY, float32(m.texW), float32(m.texH),
		clipX, clipY, clipW, clipH,
		winW, winH, viewW, viewH)
	return true
}

func (o *Overlay) drawMarquee(m *marqueeState, x, y, w, h float32, winW, winH, viewW, viewH int) bool {
	if m.tex == 0 || float32(m.texW) <= w {
		return false
	}
	offset := marqueeOffset(m.offset, m.texW, int(w))
	glDrawOverlayTextClipped(o.programText, m.tex, 1,
		x-offset, y, float32(m.texW), float32(m.texH), x, y, w, h,
		winW, winH, viewW, viewH)
	return true
}

// availableRowTextWidth is the max glyph advance that fits in a row,
// accounting for texture padding, scrollbar, and row edges.
func (o *Overlay) availableRowTextWidth(panelW int) int {
	texturePad := o.scalePx(4)
	rowEdgePad := o.scalePx(4)
	width := panelW - 2*texturePad - o.scrollbarWidthPx() - rowEdgePad
	if width < 1 {
		return 1
	}
	return width
}

// drawCursorHighlight draws a subtle highlight behind the focused row.
func (o *Overlay) drawCursorHighlight(x, y, w, h float32, winW, winH, viewW, viewH int) {
	tc := o.palette().cursor
	r, g, b := float32(tc.R)/255, float32(tc.G)/255, float32(tc.B)/255
	// Textures start at the row origin and place glyphs after their scaled
	// outline padding. Keep the highlight aligned with the glyph row.
	metrics := o.face.Metrics()
	lineGap := metrics.Height.Ceil() - metrics.Ascent.Ceil() - metrics.Descent.Ceil()
	if lineGap < 1 {
		lineGap = 1
	}
	padding := textPadding(o.fontSize) + lineGap
	glDrawFilledRect(o.programRect, x, y+float32(padding), w, h, r, g, b, alphaAccentHighlight, winW, winH, viewW, viewH)
}

// renderStatsOnly draws a minimal stats bar (FPS/MEM/CPU) without the full UI.
func (o *Overlay) renderStatsOnly(winW, winH int) {
	if o.programRect == 0 || o.programText == 0 {
		return
	}
	if o.statsDirty {
		o.rebuildStatsTex()
	}
	if o.statsTex != 0 {
		statsSize := statsFontSize(o.fontSize)
		glDrawOverlayText(o.programText, o.statsTex, 1,
			float32(o.headerMarginX()), float32(-textPadding(statsSize)), float32(o.statsTexW), float32(o.statsTexH), winW, winH, winW, winH)
	}
}
