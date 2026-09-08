package ui

// This file owns rendering for the Presets page: two columns.
// Shared primitives in overlay_render.go; model in overlay_presets.go.

// PresetPreviewSize returns the preview render size for a window.
// Rendering at one quarter of the window dimensions balances detail and cost.
func PresetPreviewSize(winW, winH int) (w, h int) {
	if winW <= 0 || winH <= 0 {
		return 0, 0
	}
	w = max(1, winW/4)
	h = max(1, winH/4)
	return w, h
}

func (o *Overlay) renderPresetsPanels(winW, winH, viewW, viewH int, panelW, panelY, panelH, lh int) {
	texturesValid := glIsTexture(o.presetsColL.tex) && glIsTexture(o.presetsColR.tex)
	if !o.presetsDirty && !o.presetCursorDirty && !o.presetsDetailDirty && texturesValid {
		o.drawPresetsTextures(winW, winH, viewW, viewH, panelW, panelY, panelH, lh, o.presetsRightRows)
		return
	}
	rebuildList := o.presetsDirty || o.presetCursorDirty || !glIsTexture(o.presetsColL.tex)
	rebuildDetail := o.presetsDetailDirty || !glIsTexture(o.presetsColR.tex)
	o.presetsDirty = false
	o.presetCursorDirty = false
	o.presetsDetailDirty = false

	maxTextPx := o.availableRowTextWidth(panelW)

	cur := o.presetNav.current()
	if cur == nil {
		o.presetsRightRows = 0
		o.drawPresetsTextures(winW, winH, viewW, viewH, panelW, panelY, panelH, lh, 0)
		return
	}
	hasParent := len(o.presetNav.stack) > 1

	// Left panel — current tree level with ".." entry when not at root.
	totalNodes := len(cur.nodes)
	if hasParent {
		totalNodes++
	}

	maxRows := panelH / lh
	if maxRows < 1 {
		maxRows = 1
	}

	previousScroll := cur.scroll
	cur.scroll = scrollOffset(cur.scroll, cur.cursor, totalNodes, maxRows)
	rebuildList = rebuildList || cur.scroll != previousScroll
	leftEnd := cur.scroll + maxRows
	if leftEnd > totalNodes {
		leftEnd = totalNodes
	}
	if rebuildList {
		var leftRows []listRow
		for i := cur.scroll; i < leftEnd; i++ {
			isCursor := i == cur.cursor && o.panelEntered
			var line string
			if hasParent && i == 0 {
				line = ".."
			} else {
				nodeIdx := i
				if hasParent {
					nodeIdx--
				}
				line = nodeDisplayLine(&cur.nodes[nodeIdx], o.presetName)
			}
			leftRows = append(leftRows, listRow{text: line, active: isCursor})
		}
		o.rebuildListRows(&o.presetsColL, leftRows, maxTextPx, panelW)
	}

	// Marquee for focused node name.
	if o.panelEntered && cur.cursor >= cur.scroll && cur.cursor < leftEnd {
		var name string
		if hasParent && cur.cursor == 0 {
			name = ".."
		} else {
			nodeIdx := cur.cursor
			if hasParent {
				nodeIdx--
			}
			name = nodeDisplayLine(&cur.nodes[nodeIdx], o.presetName)
		}
		o.rebuildMarqueeLine(&o.marqueeL, name, maxTextPx, false)
	} else {
		o.marqueeL.invalidate(o)
	}

	if rebuildDetail {
		// Right panel — metadata text for the last settled selection.
		rightRows := o.buildPresetDetailRows()
		o.rebuildListRows(&o.presetsColR, rightRows, maxTextPx, panelW)
		o.marqueeR.invalidate(o)
		o.presetsRightRows = len(rightRows)
	}

	o.drawPresetsTextures(winW, winH, viewW, viewH, panelW, panelY, panelH, lh, o.presetsRightRows)
}

func (o *Overlay) drawPresetsTextures(winW, winH, viewW, viewH int, panelW, panelY, panelH, lh int, nRightRows int) {
	lx, ly := float32(0), float32(panelY)
	rx, ry := float32(winW-panelW), float32(panelY)
	colW, colH := float32(panelW), float32(panelH)
	sbW := float32(o.scrollbarWidthPx())

	maxRows := panelH / lh
	if maxRows < 1 {
		maxRows = 1
	}

	textW := float32(o.availableRowTextWidth(panelW))

	// Left panel.
	cur := o.presetNav.current()
	hasParent := cur != nil && len(o.presetNav.stack) > 1
	leftTotal := 0
	leftCursor := 0
	leftScroll := 0
	if cur != nil {
		leftTotal = len(cur.nodes)
		if hasParent {
			leftTotal++
		}
		leftCursor = cur.cursor
		leftScroll = cur.scroll
	}
	drawListColumn(o, lx, ly, colW, colH, o.presetsColL, o.panelEntered,
		leftCursor, leftScroll, lh, winW, winH, viewW, viewH)
	drawScrollbar(o, lx+colW-sbW, ly, colH, leftTotal, maxRows, leftScroll, winW, winH, viewW, viewH)
	if o.panelEntered && leftTotal > 0 {
		rowY := ly + float32((leftCursor-leftScroll)*lh)
		o.drawMarqueeCol(&o.marqueeL, lx, ly, textW, colH, lh, rowY, winW, winH, viewW, viewH)
	}

	// Right panel — metadata text.
	drawListColumn(o, rx, ry, colW, colH, o.presetsColR, false,
		0, 0, lh, winW, winH, viewW, viewH)

	// Thumbnail below text, centered in remaining space.
	if o.presetPreviewTex == nil || nRightRows == 0 {
		return
	}
	if o.presetDetailKey == "" {
		return
	}
	tex, rw, rh, ok := o.presetPreviewTex(o.presetDetailKey)
	if !ok || tex == 0 {
		return
	}
	textBottom := ry + float32(nRightRows*lh)
	remaining := colH - float32(nRightRows*lh)
	thumbW, thumbH := fitPresetPreview(float32(rw), float32(rh), colW, remaining)
	if thumbW == 0 || thumbH == 0 {
		return
	}
	tx := rx + (colW-thumbW)/2
	ty := textBottom + (remaining-thumbH)/2
	glDrawOverlayImage(o.programImage, tex, 1, tx, ty, thumbW, thumbH, winW, winH, viewW, viewH)
}

func fitPresetPreview(srcW, srcH, maxW, maxH float32) (w, h float32) {
	if srcW <= 0 || srcH <= 0 || maxW <= 0 || maxH <= 0 {
		return 0, 0
	}
	scale := min(maxW/srcW, maxH/srcH)
	return srcW * scale, srcH * scale
}
