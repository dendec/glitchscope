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
	o.actionPlacements = nil
	if o.face == nil || len(actions) == 0 {
		return
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
	o.ncActionsTex, o.ncActionsTexW, o.ncActionsTexH = glUploadTexture(rgba), w, h
	o.actionPlacements = placements
}

func (o *Overlay) drawActionIcons(x, y, w, h float32, winW, winH, viewW, viewH int) {
	lh := o.face.Metrics().Height.Ceil()
	if !o.ensureIconTextures(lh, o.textColor()) {
		return
	}
	for _, item := range o.actionPlacements {
		o.drawIconClipped(item.icon, x+float32(textPadding(o.fontSize)+item.iconX), y+float32(textPadding(o.fontSize)), float32(lh), x, y, w, h, winW, winH, viewW, viewH)
	}
}
