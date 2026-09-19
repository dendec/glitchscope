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
	statusRowH := 0
	if o.showPlayerBar {
		statusRowH = o.playerStatusRowHeight(lh)
	}
	presetLineH := 0
	if o.showPlayerBar && o.presetNameTex != 0 {
		presetLineH = o.playerStatusRowHeight(lh)
	}
	hintRowH := o.hintRowHeight()
	bottomH := presetLineH + statusRowH + hintRowH
	if o.showPlayerBar {
		bottomH += o.scalePx(4)
	}
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

// Player rows reserve the glyph line and its outline, not the transparent
// padding stored around text textures.
func (o *Overlay) playerStatusRowHeight(lh int) int {
	return lh + 2*shadowRadius(o.fontSize)
}

func (o *Overlay) playerBarTextY(rowY float32) float32 {
	return rowY + float32(shadowRadius(o.fontSize)-textPadding(o.fontSize))
}
