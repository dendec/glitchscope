package ui

import "github.com/dendec/pmv/internal/player"

// This file owns rendering for the Library page: albums/tracks panels.
// Shared primitives in overlay_render.go.

// renderLibraryPanels draws the albums (left) and tracks (right) panels.
func (o *Overlay) renderLibraryPanels(winW, winH, viewW, viewH int, panelW, panelY, panelH, lh int) {
	// Text clip width must match truncateEnd/rebuildMarqueeLine.
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
		if o.panelEntered && o.focusPanel == 0 && len(o.albums) > 0 {
			rowY := py + float32((o.albumCursor-o.albumsScroll)*lh)
			o.drawCursorHighlight(px, rowY, float32(panelW), float32(lh), winW, winH, viewW, viewH)
		}
		glDrawOverlayText(o.programText, o.albumsTex, 1,
			px, py, float32(o.albumsTexW), float32(o.albumsTexH), winW, winH, viewW, viewH)
		am := panelH / lh
		if am < 1 {
			am = 1
		}
		drawScrollbar(o, px+float32(panelW)-sbW, py, float32(panelH), len(o.albums), am, o.albumsScroll, winW, winH, viewW, viewH)
		// Cursor highlight row — drawn as separate overlay, no texture rebuild needed.
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
		if o.panelEntered && o.focusPanel == 1 && len(o.trackInfos) > 0 {
			rowY := ty + float32((o.trackCursor-o.tracksScroll)*lh)
			o.drawCursorHighlight(tx, rowY, float32(panelW), float32(lh), winW, winH, viewW, viewH)
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

	if len(o.albums) == 0 {
		o.deleteTex(&o.albumsTex)
		o.marqueeL.invalidate(o)
		o.albumsContentDirty = false
		return
	}

	lh := o.face.Metrics().Height.Ceil()
	maxRows := maxH / lh
	if maxRows < 1 {
		maxRows = 1
	}
	prevScroll := o.albumsScroll
	o.albumsScroll = scrollOffset(o.albumsScroll, o.albumCursor, len(o.albums), maxRows)

	// Skip expensive re-render if scroll didn't change — cursor highlight
	// is drawn as a separate overlay in renderLibraryPanels.
	if o.albumsScroll == prevScroll && o.albumsTex != 0 && !o.albumsContentDirty {
		maxTextPx := availableRowTextWidth(maxW)
		o.refreshCursorMarquee(&o.marqueeL, o.albums, o.albumCursor, o.focusPanel == 0, maxTextPx)
		return
	}

	o.deleteTex(&o.albumsTex)
	o.marqueeL.invalidate(o)

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
		rows = append(rows, listRow{text: line})
		if i == o.albumCursor && o.focusPanel == 0 {
			o.rebuildMarqueeLine(&o.marqueeL, line, maxTextPx)
		}
	}
	o.albumsTex, o.albumsTexW, o.albumsTexH = o.renderListRows(rows, maxTextPx, maxW)
	o.albumsContentDirty = false
}

func (o *Overlay) rebuildTracksTex(maxW, maxH int) {
	o.tracksDirty = false

	if o.previewActive {
		o.rebuildPreviewTex(maxW, maxH)
		o.tracksContentDirty = false
		return
	}

	if len(o.trackInfos) == 0 {
		o.deleteTex(&o.tracksTex)
		o.marqueeR.invalidate(o)
		o.tracksContentDirty = false
		return
	}

	lh := o.face.Metrics().Height.Ceil()
	maxRows := maxH / lh
	if maxRows < 1 {
		maxRows = 1
	}
	prevScroll := o.tracksScroll
	o.tracksScroll = scrollOffset(o.tracksScroll, o.trackCursor, len(o.trackInfos), maxRows)

	if o.tracksScroll == prevScroll && o.tracksTex != 0 && !o.tracksContentDirty {
		maxTextPx := availableRowTextWidth(maxW)
		o.refreshCursorMarqueeTracks(maxTextPx)
		return
	}

	o.deleteTex(&o.tracksTex)
	o.marqueeR.invalidate(o)

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
		rows = append(rows, listRow{text: line})
		if i == o.trackCursor && o.focusPanel == 1 {
			o.rebuildMarqueeLine(&o.marqueeR, line, maxTextPx)
		}
	}
	o.tracksTex, o.tracksTexW, o.tracksTexH = o.renderListRows(rows, maxTextPx, maxW)
	o.tracksContentDirty = false
}

// rebuildPreviewTex renders the right-panel preview for a non-leaf entry.
func (o *Overlay) rebuildPreviewTex(maxW, maxH int) {
	if len(o.previewEntries) == 0 {
		o.deleteTex(&o.tracksTex)
		o.marqueeR.invalidate(o)
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
