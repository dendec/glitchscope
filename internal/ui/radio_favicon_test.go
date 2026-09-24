package ui

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"testing"
	"time"

	"github.com/dendec/glitchscope/internal/radio"
)

func TestRadioFaviconRequestWaitsForNavigationToSettle(t *testing.T) {
	station := radio.Station{StationUUID: "station-id", Name: "Station", Favicon: "https://example.test/favicon.png"}
	var requests int
	o := &Overlay{
		uiVisible:    true,
		panelEntered: true,
		uiPage:       PageLibrary,
		albumEntries: []navEntry{{kind: entryRadioStation, radioStation: station}},
		radioFaviconReq: func(got radio.Station) bool {
			if got.Path() != station.Path() {
				t.Fatalf("requested station %q, want %q", got.Path(), station.Path())
			}
			requests++
			return true
		},
	}
	start := time.Now()
	o.updateRadioFaviconPreview(start, true)
	o.updateRadioFaviconPreview(start.Add(time.Second), true)
	deadline := start.Add(time.Second + radioFaviconIdleDelay)
	o.updateRadioFaviconPreview(deadline.Add(-time.Nanosecond), false)
	if requests != 0 {
		t.Fatalf("requested favicon before idle delay elapsed: %d requests", requests)
	}
	o.updateRadioFaviconPreview(deadline, false)
	if requests != 1 {
		t.Fatalf("requests after navigation settled = %d, want 1", requests)
	}
	o.updateRadioFaviconPreview(deadline.Add(time.Second), false)
	if requests != 1 {
		t.Fatalf("repeated request for unchanged selection: %d", requests)
	}
}

func TestRadioFaviconRequestRetriesWhenWorkerPoolIsFull(t *testing.T) {
	station := radio.Station{StationUUID: "station-id", Favicon: "https://example.test/favicon.png"}
	var requests int
	o := &Overlay{
		uiVisible:    true,
		panelEntered: true,
		uiPage:       PageLibrary,
		albumEntries: []navEntry{{kind: entryRadioStation, radioStation: station}},
		radioFaviconReq: func(radio.Station) bool {
			requests++
			return requests > 1
		},
	}
	start := time.Now()
	o.updateRadioFaviconPreview(start, false)
	o.updateRadioFaviconPreview(start.Add(radioFaviconIdleDelay), false)
	o.updateRadioFaviconPreview(start.Add(2*radioFaviconIdleDelay-time.Nanosecond), false)
	if requests != 1 {
		t.Fatalf("requests before retry deadline = %d, want 1", requests)
	}
	o.updateRadioFaviconPreview(start.Add(2*radioFaviconIdleDelay), false)
	if requests != 2 {
		t.Fatalf("requests after retry deadline = %d, want 2", requests)
	}
}

func TestPrepareRadioFaviconRemovesJPEGBackgroundIncludingInnerHoles(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 80, 40))
	draw.Draw(src, src.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(src, image.Rect(20, 10, 60, 30), image.NewUniform(color.RGBA{R: 30, G: 70, B: 190, A: 255}), image.Point{}, draw.Src)
	draw.Draw(src, image.Rect(35, 15, 45, 25), image.NewUniform(color.White), image.Point{}, draw.Src)

	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, src, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode JPEG: %v", err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatalf("decode JPEG: %v", err)
	}

	content := prepareRadioFavicon(decoded)
	if content == nil {
		t.Fatal("prepareRadioFavicon returned nil")
	}
	if content.Bounds().Dx() >= src.Bounds().Dx() || content.Bounds().Dy() >= src.Bounds().Dy() {
		t.Fatalf("favicon was not cropped: got %v from source %v", content.Bounds(), src.Bounds())
	}

	if content.RGBAAt(content.Bounds().Dx()/2, content.Bounds().Dy()/2).A != 0 {
		t.Fatal("JPEG background hole was not removed")
	}
	if content.RGBAAt(5, 5).A == 0 {
		t.Fatal("logo content became transparent")
	}
}

func TestPrepareRadioFaviconCropsTransparentPNG(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 50, 50))
	draw.Draw(src, image.Rect(15, 12, 35, 38), image.NewUniform(color.RGBA{R: 240, G: 30, B: 40, A: 255}), image.Point{}, draw.Src)

	got := prepareRadioFavicon(src)
	if got == nil {
		t.Fatal("prepareRadioFavicon returned nil")
	}
	if got.RGBAAt(0, 0).A == 0 {
		t.Fatal("cropped logo content became transparent")
	}
	if got.Bounds().Dx() >= src.Bounds().Dx() || got.Bounds().Dy() >= src.Bounds().Dy() {
		t.Fatalf("transparent padding was not cropped: got %v from source %v", got.Bounds(), src.Bounds())
	}
}

func TestPrepareRadioFaviconPreservesOpaqueWhitePNGDetails(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 50, 50))
	draw.Draw(src, image.Rect(10, 10, 40, 40), image.NewUniform(color.RGBA{R: 30, G: 70, B: 190, A: 255}), image.Point{}, draw.Src)
	draw.Draw(src, image.Rect(20, 20, 30, 30), image.NewUniform(color.White), image.Point{}, draw.Src)

	got := prepareRadioFavicon(src)
	if got == nil {
		t.Fatal("prepareRadioFavicon returned nil")
	}
	if got.RGBAAt(10, 10).A == 0 {
		t.Fatal("opaque white PNG detail was removed")
	}
}

func TestFitRadioFaviconSizeUpscalesTinyLogos(t *testing.T) {
	if gotW, gotH := fitRadioFaviconSize(120, 60, 640, 180); gotW != 128 || gotH != 64 {
		t.Fatalf("fitRadioFaviconSize() = %dx%d, want 128x64", gotW, gotH)
	}
	if gotW, gotH := fitRadioFaviconSize(1920, 1080, 640, 180); gotW != 320 || gotH != 180 {
		t.Fatalf("fitRadioFaviconSize() = %dx%d, want 320x180", gotW, gotH)
	}
	if gotW, gotH := fitRadioFaviconSize(20, 10, 64, 180); gotW != 64 || gotH != 32 {
		t.Fatalf("fitRadioFaviconSize() = %dx%d, want 64x32 when panel width caps upscale", gotW, gotH)
	}
}

func TestScaleRadioFaviconUsesNearestNeighborForUpscale(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	src.SetRGBA(1, 0, color.RGBA{B: 255, A: 255})

	got := scaleRadioFavicon(src, 4, 2)
	for x := 0; x < 2; x++ {
		for y := 0; y < 2; y++ {
			if pixel := got.RGBAAt(x, y); pixel != (color.RGBA{R: 255, A: 255}) {
				t.Fatalf("upscaled red block pixel (%d,%d) = %#v", x, y, pixel)
			}
		}
	}
	for x := 2; x < 4; x++ {
		for y := 0; y < 2; y++ {
			if pixel := got.RGBAAt(x, y); pixel != (color.RGBA{B: 255, A: 255}) {
				t.Fatalf("upscaled blue block pixel (%d,%d) = %#v", x, y, pixel)
			}
		}
	}
}

func TestDecodeRadioFaviconSupportsICOWithEmbeddedPNG(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 16, 16))
	draw.Draw(src, src.Bounds(), image.NewUniform(color.RGBA{R: 10, G: 20, B: 30, A: 255}), image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, src); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	data := make([]byte, 22+encoded.Len())
	binary.LittleEndian.PutUint16(data[2:4], 1)
	binary.LittleEndian.PutUint16(data[4:6], 1)
	data[6], data[7] = 16, 16
	binary.LittleEndian.PutUint16(data[10:12], 32)
	binary.LittleEndian.PutUint32(data[14:18], uint32(encoded.Len()))
	binary.LittleEndian.PutUint32(data[18:22], 22)
	copy(data[22:], encoded.Bytes())

	got, format, err := decodeRadioFavicon(data)
	if err != nil {
		t.Fatalf("decode ICO: %v", err)
	}
	if format != "ico" || got.Bounds().Dx() != 16 || got.Bounds().Dy() != 16 {
		t.Fatalf("decoded ICO = format %q, bounds %v", format, got.Bounds())
	}
}

func TestDecodeRadioFaviconSupportsBMP(t *testing.T) {
	data := make([]byte, 70)
	data[0], data[1] = 'B', 'M'
	binary.LittleEndian.PutUint32(data[2:6], uint32(len(data)))
	binary.LittleEndian.PutUint32(data[10:14], 54)
	binary.LittleEndian.PutUint32(data[14:18], 40)
	binary.LittleEndian.PutUint32(data[18:22], 2)
	binary.LittleEndian.PutUint32(data[22:26], 2)
	binary.LittleEndian.PutUint16(data[26:28], 1)
	binary.LittleEndian.PutUint16(data[28:30], 24)
	// Two bottom-up BGR rows, each padded to eight bytes.
	copy(data[54:62], []byte{255, 0, 0, 0, 255, 0, 0, 0})
	copy(data[62:70], []byte{0, 0, 255, 255, 255, 255, 0, 0})

	got, format, err := decodeRadioFavicon(data)
	if err != nil {
		t.Fatalf("decode BMP: %v", err)
	}
	if format != "bmp" || got.Bounds().Dx() != 2 || got.Bounds().Dy() != 2 {
		t.Fatalf("decoded BMP = format %q, bounds %v", format, got.Bounds())
	}
}

func TestRadioFaviconCacheIsBounded(t *testing.T) {
	o := &Overlay{}
	for i := 0; i < 100; i++ {
		o.SetRadioFavicon(fmt.Sprint(i), image.NewRGBA(image.Rect(0, 0, 1, 1)))
	}
	if len(o.radioFavicons) > 32 {
		t.Fatalf("unbounded favicon cache: %d", len(o.radioFavicons))
	}
}

func TestRadioFaviconRejectsOversizedPNG(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	binary.BigEndian.PutUint32(data[16:20], 100000)
	binary.BigEndian.PutUint32(data[20:24], 100000)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	if _, _, err := decodeRadioFavicon(data); err == nil {
		t.Fatal("accepted image exceeding decoded pixel budget")
	}
}
