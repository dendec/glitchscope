package ui

import (
	"path/filepath"
	"strings"
)

// This file owns rendering for the Presets page: two columns (categories /
// presets in the focused category). Shared panel/list drawing primitives
// live in overlay_render.go; presets model lives in overlay_presets.go.

func (o *Overlay) renderPresetsPanels(winW, winH, viewW, viewH int, panelW, panelY, panelH, lh int) {
	texturesValid := glIsTexture(o.presetsColL.tex) && glIsTexture(o.presetsColR.tex)
	if !o.presetsDirty && texturesValid {
		o.drawPresetsTextures(winW, winH, viewW, viewH, panelW, panelY, panelH, lh)
		return
	}
	o.presetsDirty = false

	// Leave room for the texture's horizontal padding, the scrollbar and the
	// clipped row edges. Prefixes and status icons are part of the measured
	// line, so they consume this same budget.
	maxTextPx := availableRowTextWidth(panelW)

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
	var leftRows []listRow
	leftEnd := o.presetsScrollL + maxRows
	if leftEnd > len(o.presetCategories) {
		leftEnd = len(o.presetCategories)
	}
	for i := o.presetsScrollL; i < leftEnd; i++ {
		cat := o.presetCategories[i]
		prefix := "  "
		if o.presetName != "" {
			for _, p := range cat.Presets {
				if p == o.presetName {
					prefix = "▸ "
					break
				}
			}
		}
		line := prefix + cat.Name
		isCursor := i == o.presetCategoryCursor && o.panelEntered && o.focusPanel == 0
		leftRows = append(leftRows, listRow{text: line, active: isCursor, bold: isCursor})
	}
	o.rebuildListRows(&o.presetsColL, leftRows, maxTextPx, panelW)

	// Rebuild marquee for focused category name.
	o.marqueeL.invalidate(o)
	if o.panelEntered && o.focusPanel == 0 && o.presetCategoryCursor >= o.presetsScrollL && o.presetCategoryCursor < leftEnd {
		cat := o.presetCategories[o.presetCategoryCursor]
		prefix := "  "
		for _, p := range cat.Presets {
			if p == o.presetName {
				prefix = "▸ "
				break
			}
		}
		o.rebuildMarqueeLine(&o.marqueeL, prefix+cat.Name, maxTextPx)
	}

	// Right panel — presets in current category.
	var rightRows []listRow
	rightEnd := 0
	if cat := o.currentCategory(); cat != nil {
		o.presetsScrollR = scrollOffset(o.presetsScrollR, o.presetCursor, len(cat.Presets), maxRows)
		rightEnd = o.presetsScrollR + maxRows
		if rightEnd > len(cat.Presets) {
			rightEnd = len(cat.Presets)
		}
		for i := o.presetsScrollR; i < rightEnd; i++ {
			p := cat.Presets[i]
			mark := "  "
			if p == o.presetName {
				mark = "▸ "
			}
			isCursor := i == o.presetCursor && o.panelEntered && o.focusPanel == 1
			name := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
			line := mark + name
			rightRows = append(rightRows, listRow{text: line, active: isCursor, bold: isCursor})
		}
	} else {
		o.presetsScrollR = 0
	}
	o.rebuildListRows(&o.presetsColR, rightRows, maxTextPx, panelW)

	// Rebuild marquee for focused preset name.
	o.marqueeR.invalidate(o)
	if o.panelEntered && o.focusPanel == 1 {
		if cat := o.currentCategory(); cat != nil && o.presetCursor >= o.presetsScrollR && o.presetCursor < rightEnd {
			p := cat.Presets[o.presetCursor]
			name := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
			mark := "  "
			if p == o.presetName {
				mark = "▸ "
			}
			o.rebuildMarqueeLine(&o.marqueeR, mark+name, maxTextPx)
		}
	}

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

	drawListColumn(o, lx, ly, colW, colH, o.presetsColL, o.panelEntered && o.focusPanel == 0, winW, winH, viewW, viewH)
	drawScrollbar(o, lx+colW-sbW, ly, colH, len(o.presetCategories), maxRows, o.presetsScrollL, winW, winH, viewW, viewH)
	if o.panelEntered && o.focusPanel == 0 && len(o.presetCategories) > 0 {
		rowY := ly + float32((o.presetCategoryCursor-o.presetsScrollL)*lh)
		o.drawMarqueeCol(&o.marqueeL, lx, ly, textW, colH, lh, rowY, winW, winH, viewW, viewH)
	}

	rightTotal := 0
	if cat := o.currentCategory(); cat != nil {
		rightTotal = len(cat.Presets)
	}
	drawListColumn(o, rx, ry, colW, colH, o.presetsColR, o.panelEntered && o.focusPanel == 1, winW, winH, viewW, viewH)
	drawScrollbar(o, rx+colW-sbW, ry, colH, rightTotal, maxRows, o.presetsScrollR, winW, winH, viewW, viewH)
	if o.panelEntered && o.focusPanel == 1 && rightTotal > 0 {
		rowY := ry + float32((o.presetCursor-o.presetsScrollR)*lh)
		o.drawMarqueeCol(&o.marqueeR, rx, ry, textW, colH, lh, rowY, winW, winH, viewW, viewH)
	}
}
