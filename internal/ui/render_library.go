package ui

import "github.com/dendec/mdpp/internal/player"

// This file owns rendering for the Library page: the two-panel
// albums/tracks layout and the textures backing it (including the
// non-leaf preview panel used while browsing the modland format/album
// drill-down). Shared panel/list drawing primitives live in
// overlay_render.go; Settings/Presets rendering live in their own files.

// renderLibraryPanels draws the albums (left) and tracks (right) panels.
func (o *Overlay) renderLibraryPanels(winW, winH, viewW, viewH int, panelW, panelY, panelH, lh int) {
	// Text clip width must match the width truncateEnd/rebuildMarqueeLine
	// used to decide whether a row needs a marquee; otherwise a row can be
	// flagged as "too wide" yet have maxOffset <= 0 in drawMarqueeCol and
	// render blank instead of scrolling.
	textW := availableRowTextWidth(panelW)
	sbW := float32(o.scrollbarWidthPx())

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
		drawScrollbar(o, px+float32(panelW)-sbW, py, float32(panelH), len(o.albums), am, o.albumsScroll, winW, winH, viewW, viewH)
		if o.panelEntered && o.focusPanel == 0 && len(o.albums) > 0 {
			rowY := py + float32((o.albumCursor-o.albumsScroll)*lh)
			o.drawMarqueeCol(&o.marqueeL, px, py, float32(textW), float32(panelH), lh, rowY, winW, winH, viewW, viewH)
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
		drawScrollbar(o, tx+float32(panelW)-sbW, ty, float32(panelH), tracksCount, tm, o.tracksScroll, winW, winH, viewW, viewH)
		if o.panelEntered && o.focusPanel == 1 && len(o.trackInfos) > 0 {
			rowY := ty + float32((o.trackCursor-o.tracksScroll)*lh)
			o.drawMarqueeCol(&o.marqueeR, tx, ty, float32(textW), float32(panelH), lh, rowY, winW, winH, viewW, viewH)
		}
	}
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
		prefix := "  "
		if name == o.playingAlbum {
			prefix = "▸ "
		}
		line := prefix + name
		isCursor := i == o.albumCursor && o.focusPanel == 0
		rows = append(rows, listRow{
			text:   line,
			active: isCursor,
			bold:   o.panelEntered && isCursor,
		})
		if isCursor {
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
		prefix := "  "
		if info.Path == o.playingTrack {
			prefix = "▸ "
		}
		line := prefix + title + suffix
		isCursor := i == o.trackCursor && o.focusPanel == 1
		rows = append(rows, listRow{
			text:   line,
			active: isCursor,
			bold:   o.panelEntered && isCursor,
		})
		if isCursor {
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
