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

	cur := o.presetNav.current()
	if cur == nil {
		o.drawPresetsTextures(winW, winH, viewW, viewH, panelW, panelY, panelH, lh)
		return
	}
	hasParent := len(o.presetNav.stack) > 1

	// Left panel — current tree level with ".." entry when not at root.
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
			name = nodeDisplayLine(&cur.nodes[nodeIdx], o.presetName)
		}
		o.rebuildMarqueeLine(&o.marqueeL, name, maxTextPx, true)
	}

	// Right panel — preset metadata for selected .milk node.
	rightRows := o.buildPresetDetailRows()
	o.rebuildListRows(&o.presetsColR, rightRows, maxTextPx, panelW)
	o.marqueeR.invalidate(o)

	o.drawPresetsTextures(winW, winH, viewW, viewH, panelW, panelY, panelH, lh)
}

func (o *Overlay) drawPreviewThumb(tex uint32, x, y, w, h, panelW float32, winW, winH, viewW, viewH int) {
	glDrawOverlayText(o.programText, tex, 1, x+4, y+4, w, h, winW, winH, viewW, viewH)
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

	// Right panel — thumbnail + metadata detail.
	var thumbTex uint32
	var thumbDisplayH float32
	if o.presetPreviewTex != nil {
		node := o.presetNav.Selected()
		if node != nil && node.isLeaf {
			if tex, rw, rh, ok := o.presetPreviewTex(node.key); ok && tex != 0 {
				thumbTex = tex
				// Scale render dimensions to fit panel width.
				aspect := float64(rw) / float64(rh)
				thumbDisplayH = float32(float64(colW-8) / aspect)
			}
		}
	}
	if thumbTex != 0 {
		thumbDisplayW := colW - 8
		o.drawPreviewThumb(thumbTex, rx, ry, thumbDisplayW, thumbDisplayH, colW, winW, winH, viewW, viewH)
		ry += thumbDisplayH + 4
		colH -= thumbDisplayH + 4
	}
	drawListColumn(o, rx, ry, colW, colH, o.presetsColR, false,
		0, 0, lh, winW, winH, viewW, viewH)
	// Draw thumbnail on top of panel background.
	if thumbTex != 0 {
		thumbDisplayW := colW - 8
		o.drawPreviewThumb(thumbTex, rx, ry-thumbDisplayH-4, thumbDisplayW, thumbDisplayH, colW, winW, winH, viewW, viewH)
	}
}
