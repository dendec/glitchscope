package ui

import (
	"image/color"
	"math"

	"github.com/dendec/glitchscope/internal/config"
)

// This file owns theme/color derivation. Struct fields in overlay.go.

type themePalette struct {
	background color.RGBA
	text       color.RGBA
	border     color.RGBA
	cursor     color.RGBA
	scrollbar  color.RGBA
}

func rgb(r, g, b uint8) color.RGBA { return color.RGBA{r, g, b, 255} }

var themePalettes = map[config.Theme]themePalette{
	config.ThemeDark: {
		background: rgb(10, 12, 16), text: rgb(235, 238, 245),
		border: rgb(98, 110, 130), cursor: rgb(45, 212, 191), scrollbar: rgb(103, 232, 249),
	},
	config.ThemeLight: {
		background: rgb(246, 244, 238), text: rgb(28, 32, 39),
		border: rgb(78, 85, 97), cursor: rgb(255, 183, 3), scrollbar: rgb(0, 121, 107),
	},
	config.ThemeAmber: {
		background: rgb(24, 17, 5), text: rgb(255, 224, 150),
		border: rgb(226, 149, 48), cursor: rgb(255, 110, 64), scrollbar: rgb(255, 193, 7),
	},
	config.ThemeCyan: {
		background: rgb(4, 22, 28), text: rgb(188, 239, 245),
		border: rgb(38, 198, 218), cursor: rgb(255, 94, 91), scrollbar: rgb(0, 188, 212),
	},
	config.ThemeSolarized: {
		background: rgb(0, 43, 54), text: rgb(147, 161, 161),
		border: rgb(42, 161, 152), cursor: rgb(203, 75, 22), scrollbar: rgb(181, 137, 0),
	},
	config.ThemeHighContrast: {
		background: rgb(0, 0, 0), text: rgb(255, 255, 255),
		border: rgb(255, 255, 255), cursor: rgb(255, 235, 0), scrollbar: rgb(0, 229, 255),
	},
	config.ThemeForest: {
		background: rgb(7, 24, 17), text: rgb(210, 235, 219),
		border: rgb(90, 166, 120), cursor: rgb(238, 178, 17), scrollbar: rgb(100, 210, 150),
	},
	config.ThemeSynthwave: {
		background: rgb(24, 12, 38), text: rgb(246, 230, 255),
		border: rgb(57, 221, 215), cursor: rgb(255, 71, 159), scrollbar: rgb(82, 236, 255),
	},
	config.ThemeCherry: {
		background: rgb(34, 8, 17), text: rgb(255, 224, 232),
		border: rgb(239, 71, 111), cursor: rgb(67, 230, 176), scrollbar: rgb(255, 132, 164),
	},
}

func (o *Overlay) palette() themePalette {
	palette, ok := themePalettes[o.theme]
	if !ok {
		return themePalettes[config.ThemeDark]
	}
	return palette
}

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
	return o.palette().text
}

func (o *Overlay) panelBgRGB() (float32, float32, float32) {
	background := o.palette().background
	return float32(background.R) / 255, float32(background.G) / 255, float32(background.B) / 255
}

func (o *Overlay) borderRGB() (float32, float32, float32) {
	border := o.palette().border
	return float32(border.R) / 255, float32(border.G) / 255, float32(border.B) / 255
}

// transparencyAlpha returns the alpha for the current transparency setting.
func (o *Overlay) transparencyAlpha() float32 {
	return 1.0 - o.transparency
}

// bgAlpha returns the panel background alpha.
func (o *Overlay) bgAlpha() float32 { return o.transparencyAlpha() }

// scalePx converts a pixel size designed at 480p to the current screen height.
func (o *Overlay) scalePx(at480 int) int {
	scaled := int(math.Round(float64(at480*o.screenH) / 480))
	if at480 > 0 && scaled < 1 {
		return 1
	}
	return scaled
}

// borderWidthPx returns the focus-border thickness, scaled with screen height.
func (o *Overlay) borderWidthPx() int {
	return o.scalePx(1)
}

// scrollbarWidthPx returns the scrollbar thickness (twice borderWidth).
func (o *Overlay) scrollbarWidthPx() int {
	return o.borderWidthPx() * 2
}
