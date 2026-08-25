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

	// Prepend ".." entry if not at root.
	hasParent := len(o.presetNav.stack) > 1
	totalNodes := len(cur.nodes)
	if hasParent {
		totalNodes++
	}

	cur.scroll = scrollOffset(cur.scroll, cur.cursor, totalNodes, maxRows)
	var leftRows []listRow
	leftEnd := cur.scroll + maxRows
	if leftEnd > totalNodes {
		leftEnd = totalNodes
	}
	for i := cur.scroll; i < leftEnd; i++ {
		var line string
		isCursor := i == cur.cursor && o.panelEntered
		if hasParent && i == 0 {
			// ".." entry at position 0.
			line = ".."
		} else {
			nodeIdx := i
			if hasParent {
				nodeIdx--
			}
			node := cur.nodes[nodeIdx]
			prefix := "  "
			if node.isLeaf {
				// Mark currently-playing preset.
				if node.key == o.presetName {
					prefix = "▸ "
				}
				line = prefix + node.name
			} else {
				// Directory: trailing "/", no triangle.
				line = node.name + "/"
			}
		}
		leftRows = append(leftRows, listRow{text: line, active: isCursor})
	}
	o.rebuildListRows(&o.presetsColL, leftRows, maxTextPx, panelW)

	// Marquee for focused node name.
	o.marqueeL.invalidate(o)
	if o.panelEntered && cur.cursor >= cur.scroll && cur.cursor < leftEnd {
		var name string
		if hasParent && cur.cursor == 0 {
			name = ".."
		} else {
			nodeIdx := cur.cursor
			if hasParent {
				nodeIdx--
			}
			node := cur.nodes[nodeIdx]
			if node.isLeaf {
				prefix := "  "
				if node.key == o.presetName {
					prefix = "▸ "
				}
				name = prefix + node.name
			} else {
				name = node.name + "/"
			}
		}
		o.rebuildMarqueeLine(&o.marqueeL, name, maxTextPx, true)
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
	hasParent := cur != nil && len(o.presetNav.stack) > 1
	leftTotal := 0
	leftCursor := 0
	leftScroll := 0
	if cur != nil {
		leftTotal = len(cur.nodes)
		if hasParent {
			leftTotal++ // account for ".." entry
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

	// Right panel (detail — placeholder for Phase 3).
	drawListColumn(o, rx, ry, colW, colH, o.presetsColR, false,
		0, 0, lh, winW, winH, viewW, viewH)
}
