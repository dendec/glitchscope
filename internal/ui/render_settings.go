package ui

// This file owns rendering for the Settings page: two columns.
// Shared primitives in overlay_render.go.

// renderSettingsPanels draws the settings labels (left) and values (right).
func (o *Overlay) renderSettingsPanels(winW, winH, viewW, viewH int, panelW, panelY, panelH, lh int) {
	if !o.settingsDirty {
		// Still draw from cache.
		o.drawSettingsTextures(winW, winH, viewW, viewH, panelW, panelY, panelH, lh)
		return
	}
	o.settingsDirty = false

	maxRows := panelH / lh
	if maxRows < 1 {
		maxRows = 1
	}

	// Rebuild left column (setting names) with scroll window.
	leftTotal := len(o.settingsRows)
	o.albumsScroll = scrollOffset(o.albumsScroll, o.settingsCursor, leftTotal, maxRows)
	maxTextPx := o.availableRowTextWidth(panelW)
	var leftRows []listRow
	leftEnd := o.albumsScroll + maxRows
	if leftEnd > leftTotal {
		leftEnd = leftTotal
	}
	for i := o.albumsScroll; i < leftEnd; i++ {
		row := o.settingsRows[i]
		if row.Header {
			leftRows = append(leftRows, listRow{text: row.Label, bold: true})
		} else {
			isCursor := i == o.settingsCursor && o.panelEntered
			leftRows = append(leftRows, listRow{text: row.Label, active: isCursor})
		}
	}
	o.rebuildListRows(&o.settingsColL, leftRows, maxTextPx, panelW)

	// Rebuild marquee for focused setting name.
	o.marqueeL.invalidate(o)
	if o.panelEntered && !o.settingsEditing && o.settingsCursor >= o.albumsScroll && o.settingsCursor < leftEnd {
		row := o.settingsRows[o.settingsCursor]
		if !row.Header {
			o.rebuildMarqueeLine(&o.marqueeL, row.Label, maxTextPx, false)
		}
	}

	// Rebuild right column (values) with scroll window.
	var rightRows []listRow
	rightTotal := 0
	rightEnd := 0
	if o.settingsCursor < len(o.settingsRows) && !o.settingsRows[o.settingsCursor].Header {
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
			isCursor := i == selIdx && o.panelEntered && o.settingsEditing
			if i == selIdx {
				mark = "▸ "
			}
			line := mark + row.Values[i]
			rightRows = append(rightRows, listRow{text: line, active: isCursor})
		}
	}
	if o.settingsCursor >= 0 && o.settingsCursor < len(o.settingsRows) {
		value := o.settingsRows[o.settingsCursor].Index
		if o.settingsEditing {
			value = o.settingsValueCursor
		}
		description := o.selectedSettingDescription(o.settingsCursor, value)
		if description != "" && maxRows-len(rightRows) >= 2 {
			rightRows = append(rightRows, listRow{})
			lines := wrapHelpLines([]string{description}, o.face, maxTextPx)
			for _, line := range lines[:min(len(lines), maxRows-len(rightRows))] {
				rightRows = append(rightRows, listRow{text: line})
			}
		}
	}
	o.rebuildListRows(&o.settingsColR, rightRows, maxTextPx, panelW)

	// Rebuild marquee for focused setting value.
	o.marqueeR.invalidate(o)
	if o.panelEntered && o.settingsEditing && o.settingsCursor < len(o.settingsRows) {
		row := o.settingsRows[o.settingsCursor]
		if !row.Header {
			selIdx := o.settingsValueCursor
			if selIdx >= o.tracksScroll && selIdx < rightEnd {
				mark := "▸ "
				o.rebuildMarqueeLine(&o.marqueeR, mark+row.Values[selIdx], maxTextPx, false)
			}
		}
	}

	o.drawSettingsTextures(winW, winH, viewW, viewH, panelW, panelY, panelH, lh)
}

func (o *Overlay) drawSettingsTextures(winW, winH, viewW, viewH int, panelW, panelY, panelH, lh int) {
	lx, ly := float32(0), float32(panelY)
	rx, ry := float32(winW-panelW), float32(panelY)
	colW, colH := float32(panelW), float32(panelH)
	sbW := float32(o.scrollbarWidthPx())

	maxRows := panelH / lh
	if maxRows < 1 {
		maxRows = 1
	}

	textW := float32(o.availableRowTextWidth(panelW))

	drawListColumn(o, lx, ly, colW, colH, o.settingsColL, o.panelEntered && !o.settingsEditing,
		o.settingsCursor, o.albumsScroll, lh, winW, winH, viewW, viewH)
	drawScrollbar(o, lx+colW-sbW, ly, colH, len(o.settingsRows), maxRows, o.albumsScroll, winW, winH, viewW, viewH)
	if o.panelEntered && !o.settingsEditing && len(o.settingsRows) > 0 {
		rowY := ly + float32((o.settingsCursor-o.albumsScroll)*lh)
		o.drawMarqueeCol(&o.marqueeL, lx, ly, textW, colH, lh, rowY, winW, winH, viewW, viewH)
	}

	drawListColumn(o, rx, ry, colW, colH, o.settingsColR, o.panelEntered && o.settingsEditing,
		o.settingsValueCursor, o.tracksScroll, lh, winW, winH, viewW, viewH)
	rightTotal := 0
	if o.panelEntered && o.settingsEditing && o.settingsCursor < len(o.settingsRows) {
		rightTotal = len(o.settingsRows[o.settingsCursor].Values)
	}
	drawScrollbar(o, rx+colW-sbW, ry, colH, rightTotal, maxRows, o.tracksScroll, winW, winH, viewW, viewH)
	if o.panelEntered && o.settingsEditing && o.settingsValueCursor >= o.tracksScroll && o.settingsCursor < len(o.settingsRows) {
		rowY := ry + float32((o.settingsValueCursor-o.tracksScroll)*lh)
		o.drawMarqueeCol(&o.marqueeR, rx, ry, textW, colH, lh, rowY, winW, winH, viewW, viewH)
	}
}
