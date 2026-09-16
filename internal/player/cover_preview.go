package player

import (
	"bytes"
	"image"
	"image/jpeg"
	"log/slog"

	xdraw "golang.org/x/image/draw"
)

// ReadCoverPreview extracts and scales embedded art off the UI thread. Nil is
// also a completed result: files without art must not be retried on each draw.
func ReadCoverPreview(path, baseDir string) *image.RGBA {
	resolver := Resolver{baseDir: baseDir}
	local := resolver.ResolveLocalPath(path)
	if local == "" || IsRadio(path) {
		return nil
	}
	data := ExtractCoverArt(local)
	if len(data) == 0 {
		return nil
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		slog.Debug("cover header", "path", path, "error", err)
		return nil
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 16*1024*1024 {
		return nil
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		slog.Debug("cover decode", "path", path, "error", err)
		return nil
	}
	w, h := config.Width, config.Height
	if max(w, h) > 512 {
		w = max(1, w*512/max(config.Width, config.Height))
		h = max(1, h*512/max(config.Width, config.Height))
	}
	scaled := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.NearestNeighbor.Scale(scaled, scaled.Bounds(), img, img.Bounds(), xdraw.Over, nil)
	return scaled
}
