package ui

// This file owns rendering of the context action-hints footer (the one compact
// `[Key] Verb` row at the very bottom of the overlay). The hint model lives in
// action_hints.go; this file only lays out and draws the ready-made line.

// renderActionHints draws the bottom context-actions footer. It is always
// shown while the UI is visible (all pages), occupying a single row. The line
// is rebuilt only when its content changes, so frame cost is negligible.
func (o *Overlay) renderActionHints(winW, winH, viewW, viewH int) {
	if o.face == nil {
		return
	}
	headerMargin := o.headerMarginX()
	maxW := winW - headerMargin*2
	if maxW < 1 {
		return
	}
	text := o.buildHintsText(o.ActionHints(), maxW)

	if text != o.hintTextCache {
		o.hintTextCache = text
		o.deleteTex(&o.hintTex)
		if text != "" {
			o.hintTex, o.hintTexW, o.hintTexH = o.renderTextToTex(text, o.textColor())
		}
	}
	if o.hintTex == 0 {
		return
	}

	lh := o.face.Metrics().Height.Ceil()
	hintY := winH - lh
	// Subtle backdrop behind the whole footer row.
	r, g, b := o.panelBgRGB()
	glDrawFilledRect(o.programRect, 0, float32(hintY), float32(winW), float32(lh), r, g, b, o.bgAlpha(), winW, winH, viewW, viewH)
	glDrawOverlayText(o.programText, o.hintTex, 1,
		float32(headerMargin), float32(hintY-textPadding(o.fontSize)), float32(o.hintTexW), float32(o.hintTexH), winW, winH, viewW, viewH)
}

// hintRowHeight returns the height (in pixels) the footer row occupies. It is
// constant (one text row) and must be reserved in the vertical layout budget.
func (o *Overlay) hintRowHeight() int {
	if o.face == nil {
		return 0
	}
	return o.face.Metrics().Height.Ceil()
}
