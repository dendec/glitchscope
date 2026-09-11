package ui

import (
	"image"
	"image/color"
	"image/draw"
	"sort"
)

// prepareRadioFavicon turns a downloaded station image into a compact overlay
// asset. Transparent sources keep their alpha; opaque sources with a uniform
// matte use global color keying so enclosed background holes are removed too.
func prepareRadioFavicon(src image.Image) *image.RGBA {
	rgba := imageToRGBA(src)
	if !imageHasMeaningfulAlpha(src) {
		removeOpaqueRadioFaviconBackground(rgba)
	}
	rgba = cropRadioFavicon(rgba)
	defringeRadioFavicon(rgba)
	return rgba
}

func imageToRGBA(src image.Image) *image.RGBA {
	bounds := src.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(rgba, rgba.Bounds(), src, bounds.Min, draw.Src)
	return rgba
}

func removeOpaqueRadioFaviconBackground(rgba *image.RGBA) {
	w, h := rgba.Bounds().Dx(), rgba.Bounds().Dy()
	if w == 0 || h == 0 {
		return
	}

	edge := make([]color.RGBA, 0, 2*w+2*h)
	transparentEdge := false
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x != 0 && x != w-1 && y != 0 && y != h-1 {
				continue
			}
			pixel := rgba.RGBAAt(x, y)
			if pixel.A == 0 {
				transparentEdge = true
				continue
			}
			edge = append(edge, pixel)
		}
	}
	if len(edge) == 0 {
		return
	}

	background := medianColor(edge)
	edgeDistances := make([]int, len(edge))
	for i, pixel := range edge {
		edgeDistances[i] = radioFaviconColorDistance(pixel, background)
	}
	sort.Ints(edgeDistances)
	variation := edgeDistances[(len(edgeDistances)*3)/4]
	hardThreshold := min(42, max(12, variation*2+8))
	softThreshold := hardThreshold + 24

	matchingEdge := 0
	for _, distance := range edgeDistances {
		if distance <= hardThreshold {
			matchingEdge++
		}
	}
	if transparentEdge {
		return
	}
	// A photographic or full-bleed background must not be made transparent
	// just because a few edge pixels happen to be similar.
	if matchingEdge*100 < len(edge)*60 {
		return
	}
	if matchingEdge*100 >= len(edge)*85 {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				pixel := rgba.RGBAAt(x, y)
				if pixel.A == 0 {
					continue
				}
				distance := radioFaviconColorDistance(pixel, background)
				if distance <= hardThreshold {
					pixel.A = 0
				} else if distance < softThreshold {
					pixel.A = uint8(int(pixel.A) * (distance - hardThreshold) / (softThreshold - hardThreshold))
				}
				rgba.SetRGBA(x, y, pixel)
			}
		}
		return
	}

	visited := make([]bool, w*h)
	queue := make([]int, 0, w+h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x != 0 && x != w-1 && y != 0 && y != h-1 {
				continue
			}
			if radioFaviconBackgroundLike(rgba.RGBAAt(x, y), background, softThreshold) {
				queue = append(queue, y*w+x)
			}
		}
	}

	for len(queue) > 0 {
		index := queue[0]
		queue = queue[1:]
		if visited[index] {
			continue
		}
		visited[index] = true
		x, y := index%w, index/w
		pixel := rgba.RGBAAt(x, y)
		if pixel.A != 0 {
			distance := radioFaviconColorDistance(pixel, background)
			if distance <= hardThreshold {
				pixel.A = 0
			} else if distance < softThreshold {
				pixel.A = uint8(int(pixel.A) * (distance - hardThreshold) / (softThreshold - hardThreshold))
			}
			rgba.SetRGBA(x, y, pixel)
		}

		for _, neighbor := range [][2]int{{x - 1, y}, {x + 1, y}, {x, y - 1}, {x, y + 1}} {
			nx, ny := neighbor[0], neighbor[1]
			if nx < 0 || nx >= w || ny < 0 || ny >= h {
				continue
			}
			n := ny*w + nx
			if !visited[n] && radioFaviconBackgroundLike(rgba.RGBAAt(nx, ny), background, softThreshold) {
				queue = append(queue, n)
			}
		}
	}
}

func medianColor(pixels []color.RGBA) color.RGBA {
	red := make([]int, len(pixels))
	green := make([]int, len(pixels))
	blue := make([]int, len(pixels))
	for i, pixel := range pixels {
		red[i], green[i], blue[i] = int(pixel.R), int(pixel.G), int(pixel.B)
	}
	sort.Ints(red)
	sort.Ints(green)
	sort.Ints(blue)
	middle := len(pixels) / 2
	return color.RGBA{R: uint8(red[middle]), G: uint8(green[middle]), B: uint8(blue[middle]), A: 255}
}

func radioFaviconBackgroundLike(pixel, background color.RGBA, threshold int) bool {
	return pixel.A == 0 || radioFaviconColorDistance(pixel, background) <= threshold
}

func radioFaviconColorDistance(a, b color.RGBA) int {
	return max(absInt(int(a.R)-int(b.R)), max(absInt(int(a.G)-int(b.G)), absInt(int(a.B)-int(b.B))))
}

func cropRadioFavicon(rgba *image.RGBA) *image.RGBA {
	bounds := rgba.Bounds()
	minX, minY := bounds.Max.X, bounds.Max.Y
	maxX, maxY := bounds.Min.X, bounds.Min.Y
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if rgba.RGBAAt(x, y).A <= 8 {
				continue
			}
			minX = min(minX, x)
			minY = min(minY, y)
			maxX = max(maxX, x+1)
			maxY = max(maxY, y+1)
		}
	}
	if minX >= maxX || minY >= maxY {
		return nil
	}

	cropped := image.NewRGBA(image.Rect(0, 0, maxX-minX, maxY-minY))
	draw.Draw(cropped, cropped.Bounds(), rgba, image.Pt(minX, minY), draw.Src)
	return cropped
}

// defringeRadioFavicon copies nearby opaque RGB values into transparent
// pixels. Linear texture filtering otherwise interpolates a white JPEG matte
// into the edge of the logo even when its alpha has been removed.
func defringeRadioFavicon(rgba *image.RGBA) {
	if rgba == nil {
		return
	}
	w, h := rgba.Bounds().Dx(), rgba.Bounds().Dy()
	visited := make([]bool, w*h)
	queue := make([]int, 0, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if rgba.RGBAAt(x, y).A > 8 {
				index := y*w + x
				visited[index] = true
				queue = append(queue, index)
			}
		}
	}
	for len(queue) > 0 {
		index := queue[0]
		queue = queue[1:]
		x, y := index%w, index/w
		colorSource := rgba.RGBAAt(x, y)
		for _, neighbor := range [][2]int{{x - 1, y}, {x + 1, y}, {x, y - 1}, {x, y + 1}} {
			nx, ny := neighbor[0], neighbor[1]
			if nx < 0 || nx >= w || ny < 0 || ny >= h {
				continue
			}
			n := ny*w + nx
			if visited[n] {
				continue
			}
			visited[n] = true
			pixel := rgba.RGBAAt(nx, ny)
			if pixel.A <= 8 {
				pixel.R, pixel.G, pixel.B = colorSource.R, colorSource.G, colorSource.B
				rgba.SetRGBA(nx, ny, pixel)
			}
			queue = append(queue, n)
		}
	}
}
