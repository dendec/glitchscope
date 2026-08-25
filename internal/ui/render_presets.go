package ui

// This file owns rendering for the Presets page: two columns.
// Shared primitives in overlay_render.go; model in overlay_presets.go.

func (o *Overlay) renderPresetsPanels(winW, winH, viewW, viewH int, panelW, panelY, panelH, lh int) {
	texturesValid := glIsTexture(o.presetsColL.tex) && glIsTexture(o.presetsColR.tex)
	if !o.presetsDirty && texturesValid {
		o.drawPresetsTextures(winW, winH, viewW, viewH, panelW, panelY, panelH, lh)
		return
	}
	o.presetsDirty = false

	maxTextPx := availableRowTextWidth(panelW)
	maxRows := panelH / lh
	if maxRows < 1 {
		maxRows = 1
	}

	// Left panel — current tree level.
	cur := o.presetNav.current()
	if cur == nil {
		o.drawPresetsTextures(winW, winH, viewW, viewH, panelW, panelY, panelH, lh)
		return
	}

	cur.scroll = scrollOffset(cur.scroll, cur.cursor, len(cur.nodes), maxRows)
	var leftRows []listRow
	leftEnd := cur.scroll + maxRows
	if leftEnd > len(cur.nodes) {
		leftEnd = len(cur.nodes)
	}
	for i := cur.scroll; i < leftEnd; i++ {
		node := cur.nodes[i]
		prefix := "  "
		if !node.isLeaf {
			prefix = "▸ "
		}
		// Mark currently-playing preset.
		if node.isLeaf && node.key == o.presetName {
			prefix = "▸ "
		}
		isCursor := i == cur.cursor && o.panelEntered
		leftRows = append(leftRows, listRow{text: prefix + node.name, active: isCursor})
	}
	o.rebuildListRows(&o.presetsColL, leftRows, maxTextPx, panelW)

	// Marquee for focused node name.
	o.marqueeL.invalidate(o)
	if o.panelEntered && cur.cursor >= cur.scroll && cur.cursor < leftEnd {
		node := cur.nodes[cur.cursor]
		prefix := "  "
		if !node.isLeaf {
			prefix = "▸ "
		}
		if node.isLeaf && node.key == o.presetName {
			prefix = "▸ "
		}
		o.rebuildMarqueeLine(&o.marqueeL, prefix+node.name, maxTextPx, true)
	}

	// Right panel — detail view (placeholder for Phase 3).
	// For now, show selected node name or empty.
	o.rebuildListRows(&o.presetsColR, nil, maxTextPx, panelW)
	o.marqueeR.invalidate(o)

	o.drawPresetsTextures(winW, winH, viewW, viewH, panelW, panelY, panelH, lh)
}

func (o *Overlay) drawPresetsTextures(winW, winH, viewW, viewH int, panelW, panelY, panelH, lh int) {
	lx, ly := float32(0), float32(panelY)
	rx, ry := float32(winW-panelW), float32(panelY)
	colW, colH := float32(panelW), float32(panelH)
	sbW := float32(o.scrollbarWidthPx())

	maxRows := panelH / lh
	if maxRows < 1 {
		maxRows = 1
	}

	textW := float32(availableRowTextWidth(panelW))

	// Left panel.
	cur := o.presetNav.current()
	leftTotal := 0
	leftCursor := 0
	leftScroll := 0
	if cur != nil {
		leftTotal = len(cur.nodes)
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

	// Right panel (detail — placeholder for Phase 3).
	drawListColumn(o, rx, ry, colW, colH, o.presetsColR, false,
		0, 0, lh, winW, winH, viewW, viewH)
}
