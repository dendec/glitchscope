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
	// Our themes (backward-compatible)
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
	// gamepad-osk themes
	config.ThemeAyuDark: {
		background: rgb(10, 14, 20), text: rgb(203, 204, 198),
		border: rgb(30, 37, 50), cursor: rgb(255, 180, 84), scrollbar: rgb(255, 200, 120),
	},
	config.ThemeCandy: {
		background: rgb(40, 18, 55), text: rgb(240, 210, 250),
		border: rgb(85, 45, 110), cursor: rgb(210, 80, 140), scrollbar: rgb(240, 110, 170),
	},
	config.ThemeCatppuccin: {
		background: rgb(30, 30, 46), text: rgb(205, 214, 244),
		border: rgb(58, 59, 78), cursor: rgb(137, 180, 250), scrollbar: rgb(180, 190, 254),
	},
	config.ThemeCatppuccinFrappe: {
		background: rgb(48, 52, 70), text: rgb(198, 208, 245),
		border: rgb(73, 77, 100), cursor: rgb(140, 170, 238), scrollbar: rgb(170, 194, 250),
	},
	config.ThemeCGA: {
		background: rgb(0, 0, 0), text: rgb(255, 255, 255),
		border: rgb(85, 255, 255), cursor: rgb(255, 85, 255), scrollbar: rgb(85, 255, 255),
	},
	config.ThemeChalk: {
		background: rgb(100, 95, 90), text: rgb(230, 225, 215),
		border: rgb(130, 124, 116), cursor: rgb(175, 140, 120), scrollbar: rgb(195, 160, 140),
	},
	config.ThemeCobalt: {
		background: rgb(0, 25, 60), text: rgb(230, 240, 255),
		border: rgb(15, 50, 100), cursor: rgb(60, 140, 255), scrollbar: rgb(100, 170, 255),
	},
	config.ThemeCopper: {
		background: rgb(30, 20, 12), text: rgb(230, 200, 165),
		border: rgb(68, 48, 28), cursor: rgb(180, 110, 50), scrollbar: rgb(210, 140, 70),
	},
	config.ThemeCoral: {
		background: rgb(25, 18, 18), text: rgb(255, 180, 170),
		border: rgb(65, 38, 36), cursor: rgb(255, 111, 97), scrollbar: rgb(255, 140, 128),
	},
	config.ThemeCyberpunk: {
		background: rgb(10, 8, 15), text: rgb(252, 226, 5),
		border: rgb(30, 25, 45), cursor: rgb(200, 180, 0), scrollbar: rgb(252, 226, 5),
	},
	config.ThemeDracula: {
		background: rgb(40, 42, 54), text: rgb(248, 248, 242),
		border: rgb(80, 83, 105), cursor: rgb(189, 147, 249), scrollbar: rgb(255, 121, 198),
	},
	config.ThemeEmber: {
		background: rgb(15, 5, 0), text: rgb(255, 200, 150),
		border: rgb(50, 18, 8), cursor: rgb(200, 60, 10), scrollbar: rgb(240, 100, 30),
	},
	config.ThemeEverforest: {
		background: rgb(47, 53, 47), text: rgb(211, 198, 170),
		border: rgb(70, 80, 66), cursor: rgb(143, 191, 115), scrollbar: rgb(167, 210, 140),
	},
	config.ThemeFjord: {
		background: rgb(60, 70, 100), text: rgb(190, 210, 240),
		border: rgb(88, 98, 132), cursor: rgb(220, 150, 60), scrollbar: rgb(240, 175, 80),
	},
	config.ThemeGameboy: {
		background: rgb(155, 188, 15), text: rgb(15, 56, 15),
		border: rgb(120, 155, 15), cursor: rgb(48, 98, 48), scrollbar: rgb(15, 56, 15),
	},
	config.ThemeGold: {
		background: rgb(18, 14, 5), text: rgb(212, 175, 55),
		border: rgb(80, 70, 25), cursor: rgb(160, 130, 30), scrollbar: rgb(212, 175, 55),
	},
	config.ThemeGotham: {
		background: rgb(10, 15, 20), text: rgb(152, 209, 206),
		border: rgb(20, 35, 42), cursor: rgb(38, 139, 210), scrollbar: rgb(50, 160, 230),
	},
	config.ThemeGruvbox: {
		background: rgb(40, 40, 40), text: rgb(235, 219, 178),
		border: rgb(80, 73, 69), cursor: rgb(215, 153, 33), scrollbar: rgb(250, 189, 47),
	},
	config.ThemeHorizon: {
		background: rgb(28, 30, 38), text: rgb(205, 200, 192),
		border: rgb(53, 56, 68), cursor: rgb(233, 86, 120), scrollbar: rgb(250, 110, 145),
	},
	config.ThemeIce: {
		background: rgb(15, 20, 30), text: rgb(200, 220, 245),
		border: rgb(40, 52, 70), cursor: rgb(100, 180, 255), scrollbar: rgb(150, 210, 255),
	},
	config.ThemeKanagawa: {
		background: rgb(31, 31, 40), text: rgb(220, 215, 186),
		border: rgb(54, 54, 70), cursor: rgb(127, 180, 202), scrollbar: rgb(160, 200, 220),
	},
	config.ThemeLavender: {
		background: rgb(38, 30, 52), text: rgb(220, 210, 240),
		border: rgb(68, 56, 90), cursor: rgb(150, 120, 200), scrollbar: rgb(180, 150, 230),
	},
	config.ThemeMaterial: {
		background: rgb(38, 50, 56), text: rgb(236, 239, 241),
		border: rgb(66, 82, 90), cursor: rgb(0, 150, 136), scrollbar: rgb(0, 188, 170),
	},
	config.ThemeMatrix: {
		background: rgb(0, 2, 0), text: rgb(0, 255, 65),
		border: rgb(0, 60, 0), cursor: rgb(0, 100, 20), scrollbar: rgb(0, 180, 40),
	},
	config.ThemeMellow: {
		background: rgb(42, 36, 28), text: rgb(220, 205, 175),
		border: rgb(72, 62, 48), cursor: rgb(180, 140, 60), scrollbar: rgb(210, 170, 85),
	},
	config.ThemeMidnight: {
		background: rgb(12, 5, 20), text: rgb(210, 190, 240),
		border: rgb(35, 16, 55), cursor: rgb(120, 60, 200), scrollbar: rgb(160, 90, 240),
	},
	config.ThemeMonokai: {
		background: rgb(39, 40, 34), text: rgb(248, 248, 242),
		border: rgb(65, 66, 56), cursor: rgb(166, 226, 46), scrollbar: rgb(190, 240, 80),
	},
	config.ThemeMoss: {
		background: rgb(90, 100, 60), text: rgb(30, 50, 20),
		border: rgb(118, 128, 88), cursor: rgb(170, 195, 70), scrollbar: rgb(150, 175, 50),
	},
	config.ThemeNavy: {
		background: rgb(10, 15, 35), text: rgb(170, 185, 220),
		border: rgb(25, 38, 72), cursor: rgb(40, 70, 140), scrollbar: rgb(60, 95, 175),
	},
	config.ThemeNeon: {
		background: rgb(5, 5, 5), text: rgb(255, 255, 255),
		border: rgb(0, 255, 128), cursor: rgb(255, 0, 255), scrollbar: rgb(0, 255, 255),
	},
	config.ThemeNightfox: {
		background: rgb(18, 21, 31), text: rgb(205, 214, 244),
		border: rgb(37, 44, 62), cursor: rgb(113, 155, 215), scrollbar: rgb(140, 180, 240),
	},
	config.ThemeNord: {
		background: rgb(46, 52, 64), text: rgb(216, 222, 233),
		border: rgb(67, 76, 94), cursor: rgb(94, 129, 172), scrollbar: rgb(129, 161, 193),
	},
	config.ThemeOcean: {
		background: rgb(15, 25, 45), text: rgb(190, 215, 240),
		border: rgb(35, 55, 90), cursor: rgb(30, 120, 190), scrollbar: rgb(50, 150, 220),
	},
	config.ThemeOlive: {
		background: rgb(20, 22, 15), text: rgb(195, 200, 175),
		border: rgb(48, 52, 35), cursor: rgb(85, 100, 45), scrollbar: rgb(110, 128, 60),
	},
	config.ThemeOneDark: {
		background: rgb(40, 44, 52), text: rgb(171, 178, 191),
		border: rgb(63, 68, 82), cursor: rgb(97, 175, 239), scrollbar: rgb(130, 195, 250),
	},
	config.ThemeOxocarbon: {
		background: rgb(22, 22, 22), text: rgb(220, 220, 220),
		border: rgb(48, 48, 48), cursor: rgb(51, 183, 255), scrollbar: rgb(85, 200, 255),
	},
	config.ThemePalenight: {
		background: rgb(41, 45, 62), text: rgb(166, 172, 205),
		border: rgb(65, 70, 95), cursor: rgb(130, 170, 255), scrollbar: rgb(160, 195, 255),
	},
	config.ThemePaper: {
		background: rgb(245, 240, 230), text: rgb(50, 45, 40),
		border: rgb(200, 193, 180), cursor: rgb(100, 140, 190), scrollbar: rgb(70, 110, 165),
	},
	config.ThemePlum: {
		background: rgb(80, 50, 80), text: rgb(220, 195, 230),
		border: rgb(110, 76, 110), cursor: rgb(210, 110, 160), scrollbar: rgb(230, 135, 180),
	},
	config.ThemeRetro: {
		background: rgb(0, 0, 0), text: rgb(255, 176, 0),
		border: rgb(50, 35, 0), cursor: rgb(80, 56, 0), scrollbar: rgb(120, 84, 0),
	},
	config.ThemeRosePine: {
		background: rgb(25, 23, 36), text: rgb(224, 222, 244),
		border: rgb(46, 42, 66), cursor: rgb(196, 167, 231), scrollbar: rgb(235, 188, 186),
	},
	config.ThemeSakura: {
		background: rgb(255, 228, 235), text: rgb(100, 50, 70),
		border: rgb(230, 185, 200), cursor: rgb(230, 100, 140), scrollbar: rgb(210, 70, 110),
	},
	config.ThemeSand: {
		background: rgb(180, 165, 130), text: rgb(55, 40, 25),
		border: rgb(165, 150, 118), cursor: rgb(185, 95, 60), scrollbar: rgb(165, 75, 40),
	},
	config.ThemeSlate: {
		background: rgb(30, 34, 38), text: rgb(200, 205, 210),
		border: rgb(58, 63, 69), cursor: rgb(90, 100, 115), scrollbar: rgb(120, 130, 145),
	},
	config.ThemeSolarizedLight: {
		background: rgb(253, 246, 227), text: rgb(101, 123, 131),
		border: rgb(210, 203, 183), cursor: rgb(38, 139, 210), scrollbar: rgb(42, 161, 152),
	},
	config.ThemeSteamGreen: {
		background: rgb(22, 32, 22), text: rgb(200, 220, 200),
		border: rgb(45, 70, 45), cursor: rgb(56, 150, 60), scrollbar: rgb(76, 185, 80),
	},
	config.ThemeSunset: {
		background: rgb(20, 10, 8), text: rgb(255, 200, 120),
		border: rgb(60, 30, 15), cursor: rgb(255, 140, 0), scrollbar: rgb(255, 180, 50),
	},
	config.ThemeTeal: {
		background: rgb(10, 22, 24), text: rgb(180, 230, 235),
		border: rgb(22, 52, 56), cursor: rgb(0, 173, 181), scrollbar: rgb(0, 210, 220),
	},
	config.ThemeTerminal: {
		background: rgb(0, 0, 0), text: rgb(0, 204, 0),
		border: rgb(0, 50, 0), cursor: rgb(0, 80, 0), scrollbar: rgb(0, 120, 0),
	},
	config.ThemeTokyoNight: {
		background: rgb(26, 27, 38), text: rgb(169, 177, 214),
		border: rgb(50, 56, 80), cursor: rgb(122, 162, 247), scrollbar: rgb(158, 186, 255),
	},
	config.ThemeTokyoStorm: {
		background: rgb(36, 40, 59), text: rgb(169, 177, 214),
		border: rgb(60, 67, 94), cursor: rgb(122, 162, 247), scrollbar: rgb(158, 186, 255),
	},
	config.ThemeVapor: {
		background: rgb(20, 10, 30), text: rgb(255, 180, 220),
		border: rgb(50, 28, 68), cursor: rgb(255, 110, 180), scrollbar: rgb(100, 255, 218),
	},
	config.ThemeVirtualBoy: {
		background: rgb(0, 0, 0), text: rgb(255, 0, 0),
		border: rgb(50, 0, 0), cursor: rgb(120, 0, 0), scrollbar: rgb(180, 0, 0),
	},
	config.ThemeWine: {
		background: rgb(25, 8, 12), text: rgb(230, 190, 195),
		border: rgb(65, 20, 30), cursor: rgb(140, 30, 50), scrollbar: rgb(180, 45, 65),
	},
	config.ThemeZXSpectrum: {
		background: rgb(0, 0, 0), text: rgb(255, 255, 255),
		border: rgb(0, 0, 205), cursor: rgb(205, 0, 0), scrollbar: rgb(205, 205, 0),
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
