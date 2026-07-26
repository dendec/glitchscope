package ui

import (
	"math"

	"github.com/dendec/mdpp/internal/config"
)

// This file owns theme/color derivation for the overlay: the current theme
// and transparency setting, and the colors/pixel widths every render helper
// in overlay_render.go reads from them. Struct fields live in overlay.go.

// SetTheme updates the UI theme and transparency, marking all textures dirty.
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
}

// textColor returns the main text color for the current theme. Every piece
// of overlay text (stats, preset name, panel rows, bottom bar) uses this
// single color so the UI reads as one consistent surface.
func (o *Overlay) textColor() (byte, byte, byte) {
	if o.theme == config.ThemeLight {
		return 0, 0, 0
	}
	return 255, 255, 255
}

// panelBgRGB returns the panel background color for the current theme.
func (o *Overlay) panelBgRGB() (float32, float32, float32) {
	if o.theme == config.ThemeLight {
		return 1, 1, 1
	}
	return 0, 0, 0
}

// borderRGB returns the panel border color for the current theme.
func (o *Overlay) borderRGB() (float32, float32, float32) {
	if o.theme == config.ThemeLight {
		return 0, 0, 0
	}
	return 1, 1, 1
}

// transparencyAlpha returns the alpha value driving panel/background
// opacity for the current transparency setting: 0 is fully opaque (alpha
// 1.0), 100 is fully transparent (alpha 0.0).
func (o *Overlay) transparencyAlpha() float32 {
	return 1.0 - o.transparency
}

// bgAlpha returns the panel background alpha for the current transparency setting.
func (o *Overlay) bgAlpha() float32 { return o.transparencyAlpha() }

// borderWidthPx returns the focus-border thickness in pixels, scaled with
// screen height so it stays visually consistent across resolutions: 1px at
// 480 lines, growing by 1px per additional 480 lines. Always a whole pixel
// count, never less than 1.
func (o *Overlay) borderWidthPx() int {
	w := int(math.Round(float64(o.screenH) / 480))
	if w < 1 {
		w = 1
	}
	return w
}

// scrollbarWidthPx returns the scrollbar thickness in pixels: always twice
// the focus-border thickness, so the two scale together.
func (o *Overlay) scrollbarWidthPx() int {
	return o.borderWidthPx() * 2
}
