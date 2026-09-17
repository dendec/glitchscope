package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"

	"github.com/veandco/go-sdl2/sdl"
)

const windowIconAsset = "assets/icons/36/app.png"

// NewWindowIcon creates the SDL surface used for the native window icon.
// The caller owns the returned surface and must free it after the window is
// destroyed.
func NewWindowIcon() (*sdl.Surface, error) {
	data, err := iconAssets.ReadFile(windowIconAsset)
	if err != nil {
		return nil, fmt.Errorf("read window icon: %w", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode window icon: %w", err)
	}

	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("window icon has empty dimensions")
	}
	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(rgba, rgba.Bounds(), img, bounds.Min, draw.Src)

	surface, err := sdl.CreateRGBSurfaceWithFormat(
		0,
		int32(width),
		int32(height),
		32,
		uint32(sdl.PIXELFORMAT_RGBA32),
	)
	if err != nil {
		return nil, fmt.Errorf("create window icon surface: %w", err)
	}

	locked := false
	if surface.MustLock() {
		if err := surface.Lock(); err != nil {
			surface.Free()
			return nil, fmt.Errorf("lock window icon surface: %w", err)
		}
		locked = true
	}
	for y := 0; y < height; y++ {
		dstStart := y * int(surface.Pitch)
		srcStart := y * rgba.Stride
		copy(surface.Pixels()[dstStart:dstStart+width*4], rgba.Pix[srcStart:srcStart+width*4])
	}
	if locked {
		surface.Unlock()
	}
	return surface, nil
}
