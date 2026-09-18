package ui

// overlayLayout is the shared geometry contract for rendering and pointer
// hit-testing. All coordinates are in drawable/UI pixels.
type overlayLayout struct {
	windowW, windowH        int
	lineH                   int
	navigationHeaderHeight  int
	headerHeight            int
	panelY, panelH, panelW  int
	presetLineH, statusRowH int
	hintRowH, bottomH       int
}

func (o *Overlay) overlayLayout(winW, winH int) overlayLayout {
	lh := o.lineHeight()
	navigationHeaderH := o.headerHeight(lh)
	headerH := o.menuHeaderHeight(lh)
	statusRowH := lh
	if o.bottomTexH > statusRowH && o.face != nil {
		metrics := o.face.Metrics()
		lineGap := lh - metrics.Ascent.Ceil() - metrics.Descent.Ceil()
		if lineGap < 1 {
			lineGap = 1
		}
		statusRowH = lh + textPadding(o.fontSize) + lineGap
		if statusRowH > o.bottomTexH {
			statusRowH = o.bottomTexH
		}
	}
	presetLineH := 0
	if o.presetNameTex != 0 {
		presetLineH = lh
	}
	hintRowH := o.hintRowHeight()
	bottomH := presetLineH + statusRowH + hintRowH
	panelH := winH - headerH - bottomH
	if panelH < 0 {
		panelH = 0
	}
	return overlayLayout{
		windowW:                winW,
		windowH:                winH,
		lineH:                  lh,
		navigationHeaderHeight: navigationHeaderH,
		headerHeight:           headerH,
		panelY:                 headerH,
		panelH:                 panelH,
		panelW:                 winW * panelWidthPct / 100,
		presetLineH:            presetLineH,
		statusRowH:             statusRowH,
		hintRowH:               hintRowH,
		bottomH:                bottomH,
	}
}
