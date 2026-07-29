package ui

import (
	"image/color"
	"math"

	"github.com/dendec/pmv/internal/config"
)

// This file owns theme/color derivation. Struct fields in overlay.go.

// SetTheme updates the UI theme and transparency, marking textures dirty.
func (o *Overlay) SetTheme(t config.Theme, transparency int) {
	tr := float32(transparency) / 100.0
	if tr < 0 {
		tr = 0
	} else if tr > 1 {
		tr = 1
	}
	if o.theme == t && o.transparency == tr {
		return
	}
	o.theme = t
	o.transparency = tr
	o.markAllDirty()
	o.notif.Retheme(o.textColor())
}

// textColor returns the main text color for the current theme.
func (o *Overlay) textColor() color.RGBA {
	if o.theme == config.ThemeLight {
		return color.RGBA{0, 0, 0, 255}
	}
	return color.RGBA{255, 255, 255, 255}
}

func (o *Overlay) panelBgRGB() (float32, float32, float32) {
	if o.theme == config.ThemeLight {
		return 1, 1, 1
	}
	return 0, 0, 0
}

func (o *Overlay) borderRGB() (float32, float32, float32) {
	if o.theme == config.ThemeLight {
		return 0, 0, 0
	}
	return 1, 1, 1
}

// transparencyAlpha returns the alpha for the current transparency setting.
func (o *Overlay) transparencyAlpha() float32 {
	return 1.0 - o.transparency
}

// bgAlpha returns the panel background alpha.
func (o *Overlay) bgAlpha() float32 { return o.transparencyAlpha() }

// borderWidthPx returns the focus-border thickness, scaled with screen height.
func (o *Overlay) borderWidthPx() int {
	w := int(math.Round(float64(o.screenH) / 480))
	if w < 1 {
		w = 1
	}
	return w
}

// scrollbarWidthPx returns the scrollbar thickness (twice borderWidth).
func (o *Overlay) scrollbarWidthPx() int {
	return o.borderWidthPx() * 2
}
