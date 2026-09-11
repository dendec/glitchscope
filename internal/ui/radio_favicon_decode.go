package ui

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"

	_ "golang.org/x/image/bmp"
)

var icoSignature = [4]byte{0, 0, 1, 0}

func decodeRadioFavicon(data []byte) (image.Image, string, error) {
	img, format, err := decodeBoundedRadioImage(data)
	if err == nil {
		return img, format, nil
	}
	if len(data) < len(icoSignature) || !bytes.Equal(data[:len(icoSignature)], icoSignature[:]) {
		return nil, "", err
	}

	img, err = decodeICO(data)
	if err != nil {
		return nil, "", fmt.Errorf("decode ICO: %w", err)
	}
	return img, "ico", nil
}

// DecodeRadioFavicon prepares a bounded bitmap in a background worker. No GL
// operations occur here; the main thread only uploads the resulting image.
func DecodeRadioFavicon(data []byte) (*image.RGBA, error) {
	img, _, err := decodeRadioFavicon(data)
	if err != nil {
		return nil, err
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if max(w, h) > 256 {
		img = scaleRadioFavicon(img, max(1, w*256/max(w, h)), max(1, h*256/max(w, h)))
	}
	return prepareRadioFavicon(img), nil
}

func decodeBoundedRadioImage(data []byte) (image.Image, string, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 2048 || config.Height > 2048 || config.Width > (2<<20)/config.Height {
		return nil, "", fmt.Errorf("radio favicon exceeds pixel budget")
	}
	return image.Decode(bytes.NewReader(data))
}

type icoEntry struct {
	width, height int
	data          []byte
}

type icoCandidate struct {
	img   image.Image
	area  int
	alpha bool
}

func decodeICO(data []byte) (image.Image, error) {
	if len(data) < 6 || binary.LittleEndian.Uint16(data[0:2]) != 0 || binary.LittleEndian.Uint16(data[2:4]) != 1 {
		return nil, fmt.Errorf("invalid icon header")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count == 0 || count > 64 || len(data) < 6+count*16 {
		return nil, fmt.Errorf("invalid icon directory")
	}

	var best *icoCandidate
	for i := 0; i < count; i++ {
		offset := 6 + i*16
		width := int(data[offset])
		height := int(data[offset+1])
		if width == 0 {
			width = 256
		}
		if height == 0 {
			height = 256
		}
		size := int(binary.LittleEndian.Uint32(data[offset+8 : offset+12]))
		dataOffset := int(binary.LittleEndian.Uint32(data[offset+12 : offset+16]))
		if size <= 0 || dataOffset < 0 || size > len(data)-dataOffset {
			continue
		}

		entry := icoEntry{width: width, height: height, data: data[dataOffset : dataOffset+size]}
		img, err := decodeICOEntry(entry)
		if err != nil {
			continue
		}
		candidate := &icoCandidate{
			img:   img,
			area:  img.Bounds().Dx() * img.Bounds().Dy(),
			alpha: imageHasMeaningfulAlpha(img),
		}
		if best == nil || candidate.area > best.area || (candidate.area == best.area && candidate.alpha && !best.alpha) {
			best = candidate
		}
	}
	if best == nil {
		return nil, fmt.Errorf("ICO contains no supported image")
	}
	return best.img, nil
}

func decodeICOEntry(entry icoEntry) (image.Image, error) {
	if len(entry.data) >= 8 && bytes.Equal(entry.data[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
		img, _, err := decodeBoundedRadioImage(entry.data)
		return img, err
	}
	return decodeICOBitmap(entry.data, entry.width, entry.height)
}

func decodeICOBitmap(data []byte, entryWidth, entryHeight int) (image.Image, error) {
	if len(data) < 40 {
		return nil, fmt.Errorf("ICO bitmap header is truncated")
	}
	headerSize := int(binary.LittleEndian.Uint32(data[0:4]))
	if headerSize < 40 || headerSize > len(data) {
		return nil, fmt.Errorf("unsupported ICO bitmap header")
	}
	width := int(int32(binary.LittleEndian.Uint32(data[4:8])))
	headerHeight := int(int32(binary.LittleEndian.Uint32(data[8:12])))
	planes := binary.LittleEndian.Uint16(data[12:14])
	bitCount := int(binary.LittleEndian.Uint16(data[14:16]))
	compression := binary.LittleEndian.Uint32(data[16:20])
	colorsUsed := int(binary.LittleEndian.Uint32(data[32:36]))
	if width <= 0 {
		width = entryWidth
	}
	if width <= 0 || (planes != 0 && planes != 1) || compression != 0 {
		return nil, fmt.Errorf("unsupported ICO bitmap format")
	}

	height := headerHeight
	if height < 0 {
		height = -height
	}
	if height >= 2 {
		height /= 2
	}
	if height <= 0 {
		height = entryHeight
	}
	if height <= 0 || width > 256 || height > 256 || bitCount <= 0 || bitCount > 32 {
		return nil, fmt.Errorf("invalid ICO bitmap dimensions")
	}

	paletteCount := 0
	if bitCount <= 8 {
		paletteCount = colorsUsed
		if paletteCount == 0 {
			paletteCount = 1 << bitCount
		}
	}
	if paletteCount < 0 || paletteCount > 256 {
		return nil, fmt.Errorf("invalid ICO palette size")
	}
	pixelOffset := headerSize + paletteCount*4
	rowStride := ((bitCount*width + 31) / 32) * 4
	andStride := ((width + 31) / 32) * 4
	if pixelOffset < 0 || rowStride > 0 && height > (len(data)-pixelOffset)/rowStride {
		return nil, fmt.Errorf("ICO bitmap pixels are truncated")
	}
	andOffset := pixelOffset + rowStride*height
	if andOffset < 0 || andStride > 0 && height > (len(data)-andOffset)/andStride {
		return nil, fmt.Errorf("ICO bitmap mask is truncated")
	}

	palette := make([]color.RGBA, paletteCount)
	for i := range palette {
		offset := headerSize + i*4
		palette[i] = color.RGBA{R: data[offset+2], G: data[offset+1], B: data[offset], A: 255}
	}

	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	alphaBytesPresent := false
	for y := 0; y < height; y++ {
		sourceY := y
		if headerHeight > 0 {
			sourceY = height - 1 - y
		}
		row := data[pixelOffset+sourceY*rowStride:]
		for x := 0; x < width; x++ {
			pixel := color.RGBA{A: 255}
			switch bitCount {
			case 32:
				offset := x * 4
				pixel = color.RGBA{R: row[offset+2], G: row[offset+1], B: row[offset], A: row[offset+3]}
				if pixel.A != 0 {
					alphaBytesPresent = true
				}
			case 24:
				offset := x * 3
				pixel = color.RGBA{R: row[offset+2], G: row[offset+1], B: row[offset], A: 255}
			case 8, 4, 1:
				index := icoPaletteIndex(row, x, bitCount)
				if index >= len(palette) {
					return nil, fmt.Errorf("ICO palette index out of range")
				}
				pixel = palette[index]
			default:
				return nil, fmt.Errorf("unsupported ICO bit depth %d", bitCount)
			}
			rgba.SetRGBA(x, y, pixel)
		}
	}

	// Older 32-bit icons leave the alpha bytes at zero and rely entirely on
	// the AND mask. Treat such an image as opaque before applying that mask.
	if bitCount == 32 && !alphaBytesPresent {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				pixel := rgba.RGBAAt(x, y)
				pixel.A = 255
				rgba.SetRGBA(x, y, pixel)
			}
		}
	}
	for y := 0; y < height; y++ {
		sourceY := y
		if headerHeight > 0 {
			sourceY = height - 1 - y
		}
		row := data[andOffset+sourceY*andStride:]
		for x := 0; x < width; x++ {
			if row[x/8]&(1<<uint(7-x%8)) == 0 {
				continue
			}
			pixel := rgba.RGBAAt(x, y)
			pixel.A = 0
			rgba.SetRGBA(x, y, pixel)
		}
	}
	return rgba, nil
}

func icoPaletteIndex(row []byte, x, bitCount int) int {
	switch bitCount {
	case 8:
		return int(row[x])
	case 4:
		value := row[x/2]
		if x%2 == 0 {
			return int(value >> 4)
		}
		return int(value & 0x0f)
	case 1:
		return int((row[x/8] >> uint(7-x%8)) & 1)
	default:
		return 0
	}
}

func imageHasMeaningfulAlpha(img image.Image) bool {
	bounds := img.Bounds()
	area := bounds.Dx() * bounds.Dy()
	if area == 0 {
		return false
	}
	transparent := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := img.At(x, y).RGBA()
			if alpha < 0xff00 {
				transparent++
			}
		}
	}
	return transparent > max(1, area/500)
}
