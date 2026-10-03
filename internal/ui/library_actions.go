package ui

import (
	"image"

	"github.com/dendec/glitchscope/internal/i18n"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// Actions reserve pixel cells for cursor, icon, and label. Text bounding-box
// normalization must never determine the position of an independently drawn icon.
type libraryAction struct {
	icon, label       string
	selected, confirm bool
}

type actionPlacement struct {
	libraryAction
	cursorX, iconX, labelX, endX int
}

func layoutLibraryActions(actions []libraryAction, iconSize, gap, cursorWidth int, measure func(string) int) []actionPlacement {
	placements := make([]actionPlacement, 0, len(actions))
	x := 0
	for _, action := range actions {
		label := "[" + action.label
		if action.confirm {
			label += "?"
		}
		action.label = label + "]"
		item := actionPlacement{libraryAction: action, cursorX: x, iconX: x + cursorWidth + gap}
		item.labelX = item.iconX + iconSize + gap
		item.endX = item.labelX + measure(action.label)
		placements = append(placements, item)
		x = item.endX + 2*gap
	}
	return placements
}

func (o *Overlay) rebuildNCActionsTex() {
	actions := []libraryAction{{icon: iconPlay, label: o.catalog.Text(i18n.ActionPlay), selected: o.ncRight == ncRightPlay}}
	if o.ncInfoFile != "" || (o.ncInfoIsDir && o.ncInfoDir != o.baseDir) {
		actions = append(actions, libraryAction{icon: iconDelete, label: o.catalog.Text(i18n.ActionDelete), selected: o.ncRight == ncRightDelete, confirm: o.ncConfirm})
	}
	o.rebuildActionBar(actions)
}

func (o *Overlay) rebuildCatalogActionsTex() {
	o.rebuildActionBar([]libraryAction{{icon: iconDelete, label: o.catalog.Text(i18n.ActionDeleteDownload), selected: o.focusPanel == 1, confirm: o.ncConfirm}})
}

func (o *Overlay) rebuildActionBar(actions []libraryAction) {
	o.deleteTex(&o.ncActionsTex)
	o.ncActionsTex, o.ncActionsTexW, o.ncActionsTexH, o.actionPlacements = o.buildActionBar(actions)
}

func (o *Overlay) buildActionBar(actions []libraryAction) (uint32, int, int, []actionPlacement) {
	if o.face == nil || len(actions) == 0 {
		return 0, 0, 0, nil
	}
	metrics := o.face.Metrics()
	lh := metrics.Height.Ceil()
	measure := func(s string) int { return font.MeasureString(o.face, s).Ceil() }
	placements := layoutLibraryActions(actions, lh, o.scalePx(4), measure(">"), measure)
	tc := o.textColor()
	rgba, w, h, _ := newShadowedTextRGBA(placements[len(placements)-1].endX, lh, o.fontSize, tc, func(rgba *image.RGBA, x, y int) {
		d := font.Drawer{Dst: rgba, Src: image.NewUniform(tc), Face: o.face}
		for _, item := range placements {
			if item.selected {
				d.Dot = fixed.Point26_6{X: fixed.I(x + item.cursorX), Y: fixed.I(y) + metrics.Ascent}
				d.DrawString(">")
			}
			d.Dot = fixed.Point26_6{X: fixed.I(x + item.labelX), Y: fixed.I(y) + metrics.Ascent}
			d.DrawString(item.label)
		}
	})
	return glUploadTexture(rgba), w, h, placements
}

// actionPanelHeights reserves the bottom action row inside the full panel.
func (o *Overlay) actionPanelHeights(panelH, lh int, hasActions bool) (metadataH, actionH int) {
	panelH = max(0, panelH)
	if hasActions {
		actionH = min(panelH, o.actionBarHeight(lh))
	}
	return panelH - actionH, actionH
}

// drawInfoPanelFrame always covers the complete panel, including its buttons.
func (o *Overlay) drawInfoPanelFrame(x, y, w, h float32, focused bool, winW, winH, viewW, viewH int) {
	drawPanelBg(o, x, y, w, h, winW, winH, viewW, viewH)
	if focused {
		drawPanelBorder(o, x, y, w, h, winW, winH, viewW, viewH)
	}
}

func (o *Overlay) drawPanelActions(tex uint32, texW, texH int, placements []actionPlacement, x, y, w, h float32, winW, winH, viewW, viewH int) {
	if tex == 0 || h <= 0 {
		return
	}
	glDrawOverlayTextClipped(o.programText, tex, 1,
		x, y, float32(texW), float32(texH), x, y, w, h,
		winW, winH, viewW, viewH)
	o.drawActionPlacementIcons(placements, x, y, w, h, winW, winH, viewW, viewH)
}

func (o *Overlay) drawActionPlacementIcons(placements []actionPlacement, x, y, w, h float32, winW, winH, viewW, viewH int) {
	lh := o.face.Metrics().Height.Ceil()
	if !o.ensureIconTextures(lh, o.textColor()) {
		return
	}
	for _, item := range placements {
		o.drawIconClipped(item.icon, x+float32(textPadding(o.fontSize)+item.iconX), y+float32(textPadding(o.fontSize)), float32(lh), x, y, w, h, winW, winH, viewW, viewH)
	}
}
