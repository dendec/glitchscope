package ui

import (
	"image"
	"image/color"
	"math"
)

// shadowColorFor returns the outline color for a given text color.
// Dark text gets a light halo, light text gets a dark halo — contrast
// against busy visualizer backgrounds, not mid-gray.
func shadowColorFor(textColor color.RGBA) color.RGBA {
	// Perceptual luma; textColor is always pure black or white in practice,
	// but this also degrades gracefully if that ever changes.
	luma := 0.299*float64(textColor.R) + 0.587*float64(textColor.G) + 0.114*float64(textColor.B)
	if luma > 127.5 {
		// Light (e.g. white) text: dark theme -> near-black halo.
		return color.RGBA{20, 20, 20, 255}
	}
	// Dark (e.g. black) text: light theme -> near-white halo.
	return color.RGBA{235, 235, 235, 255}
}

func shadowRadius(fontSizePx float64) int {
	r := int(math.Round(fontSizePx / 14))
	if r < 1 {
		r = 1
	}
	return r
}

func textPadding(fontSizePx float64) int {
	extra := int(math.Round(fontSizePx * 4 / 14))
	if extra < 1 {
		extra = 1
	}
	return shadowRadius(fontSizePx) + extra
}

// applyOutlineShadow paints a solid outline behind glyphs by dilating the
// coverage mask `radius` times. Caller must leave `radius` px of padding.
func applyOutlineShadow(rgba *image.RGBA, textColor color.RGBA, radius int) {
	w := rgba.Rect.Dx()
	h := rgba.Rect.Dy()
	if w == 0 || h == 0 || radius <= 0 {
		return
	}

	coverage := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			_, _, _, a := rgba.At(rgba.Rect.Min.X+x, rgba.Rect.Min.Y+y).RGBA()
			coverage[y*w+x] = uint8(a >> 8)
		}
	}

	buf0 := make([]uint8, w*h)
	buf1 := make([]uint8, w*h)
	copy(buf0, coverage)
	src, dst := buf0, buf1
	for pass := 0; pass < radius; pass++ {
		dilate3x3(dst, src, w, h)
		src, dst = dst, src
	}
	// src = final dilated mask (never aliases coverage)

	shadowColor := shadowColorFor(textColor)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			glyphA := coverage[i]
			if glyphA == 255 {
				continue // fully opaque glyph pixel; already the desired color
			}
			if src[i] == 0 {
				continue // outside the outline, leave transparent
			}
			// Blend glyph color over the fully opaque outline color.
			a := uint32(glyphA)
			inv := uint32(255 - glyphA)
			rgba.SetRGBA(rgba.Rect.Min.X+x, rgba.Rect.Min.Y+y, color.RGBA{
				R: uint8((uint32(textColor.R)*a + uint32(shadowColor.R)*inv) / 255),
				G: uint8((uint32(textColor.G)*a + uint32(shadowColor.G)*inv) / 255),
				B: uint8((uint32(textColor.B)*a + uint32(shadowColor.B)*inv) / 255),
				A: 255,
			})
		}
	}
}

// newShadowedTextRGBA renders text with its outline baked in. Callers supply
// content size and a draw callback; the outline is applied in-place.
func newShadowedTextRGBA(contentW, contentH int, fontSizePx float64, textColor color.RGBA, draw func(rgba *image.RGBA, originX, originY int)) (rgba *image.RGBA, texW, texH, padding int) {
	radius := shadowRadius(fontSizePx)
	padding = textPadding(fontSizePx)
	texW = contentW + padding*2
	texH = contentH + padding*2
	rgba = image.NewRGBA(image.Rect(0, 0, texW, texH))
	draw(rgba, padding, padding)
	applyOutlineShadow(rgba, textColor, radius)
	return rgba, texW, texH, padding
}

func dilate3x3(out, src []uint8, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m := src[y*w+x]
			for dy := -1; dy <= 1; dy++ {
				ny := y + dy
				if ny < 0 || ny >= h {
					continue
				}
				row := ny * w
				for dx := -1; dx <= 1; dx++ {
					nx := x + dx
					if nx < 0 || nx >= w {
						continue
					}
					if v := src[row+nx]; v > m {
						m = v
					}
				}
			}
			out[y*w+x] = m
		}
	}
}
