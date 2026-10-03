package ui

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log/slog"
	"strings"

	"github.com/dendec/glitchscope/internal/player"
)

// This file owns embedded bitmap icons and their context-thread texture cache.

//go:embed assets/icons/*/*.png
var iconAssets embed.FS

const (
	iconLocalMusic = "local-music"
	iconFavorite   = "favorite"
	iconMicrophone = "microphone"
	iconRadio      = "radio"
	iconCatalogs   = "catalogs"
	iconLibrary    = "library"
	iconPresets    = "presets"
	iconSettings   = "settings"
	iconHelp       = "help"
	iconPlay       = "play"
	iconDelete     = "delete"
	iconTest       = "test"
	iconStar       = "star"
	iconPagePrev   = "page-prev"
	iconPageNext   = "page-next"
	iconDownloads  = "downloads"
	iconHome       = "home"
	iconClose      = "close"
)

var iconNames = []string{
	iconLocalMusic,
	iconFavorite,
	iconMicrophone,
	iconRadio,
	iconCatalogs,
	iconLibrary,
	iconPresets,
	iconSettings,
	iconHelp,
	iconPlay,
	iconDelete,
	iconTest,
	iconStar,
	iconPagePrev,
	iconPageNext,
	iconDownloads,
	iconHome,
	iconClose,
}

var iconRasterSizes = []int{16, 24, 36}

func sourceIconName(source sourceKind) string {
	switch source {
	case sourceMusic:
		return iconLocalMusic
	case sourceFavorites:
		return iconFavorite
	case sourceDownloads:
		return iconDownloads
	case sourceMicrophone:
		return iconMicrophone
	case sourceRadio:
		return iconRadio
	case sourceModland, sourceModArchive:
		return iconCatalogs
	default:
		return ""
	}
}

func nearestIconRasterSize(size int) int {
	if size <= 0 {
		return 0
	}
	best := iconRasterSizes[0]
	bestDistance := absInt(size - best)
	for _, candidate := range iconRasterSizes[1:] {
		if distance := absInt(size - candidate); distance < bestDistance {
			best = candidate
			bestDistance = distance
		}
	}
	return best
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// sourceIconTextOffset is the horizontal slot reserved for a source icon and
// the gap before its label. The text texture is moved as a whole so its
// leading glyph cannot overlap the independently rendered bitmap.
func (o *Overlay) sourceIconTextOffset(lineHeight int) int {
	if lineHeight <= 0 {
		return 0
	}
	return lineHeight + o.scalePx(4)
}

func (o *Overlay) ensureIconTextures(size int, iconColor color.RGBA) bool {
	rasterSize := nearestIconRasterSize(size)
	if rasterSize <= 0 {
		return false
	}
	if o.iconTextureSize == rasterSize && o.iconTextureColor == iconColor && len(o.iconTextures) == len(iconNames) {
		return true
	}
	o.deleteIconTextures()

	textures := make(map[string]uint32, len(iconNames))
	for _, name := range iconNames {
		path := fmt.Sprintf("assets/icons/%d/%s.png", rasterSize, name)
		data, err := iconAssets.ReadFile(path)
		if err != nil {
			slog.Error("icon asset missing", "path", path, "error", err)
			o.deleteTextureMap(textures)
			return false
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			slog.Error("icon asset decode failed", "path", path, "error", err)
			o.deleteTextureMap(textures)
			return false
		}
		rgba := outlinedIconRGBA(img, iconColor, shadowRadius(float64(rasterSize)))
		tex := glUploadNearestTexture(rgba)
		if tex == 0 {
			slog.Error("icon texture upload failed", "path", path)
			o.deleteTextureMap(textures)
			return false
		}
		textures[name] = tex
	}
	o.iconTextures = textures
	o.iconTextureSize = rasterSize
	o.iconTextureColor = iconColor
	return true
}

func (o *Overlay) deleteIconTextures() {
	o.deleteTextureMap(o.iconTextures)
	o.iconTextures = nil
	o.iconTextureSize = 0
	o.iconTextureColor = color.RGBA{}
}

func (o *Overlay) deleteTextureMap(textures map[string]uint32) {
	for _, tex := range textures {
		glDeleteTex(tex)
	}
}

func (o *Overlay) showsFavoriteFolderIcons() bool {
	return o.topLevel().ctx == ctxFavorites && o.topLevel().playlistID == ""
}

func (o *Overlay) favoriteIconTextInset(lh int) int {
	if o.favoritesView == nil || o.topLevel().ctx == ctxSourceRoot || o.showsFavoriteFolderIcons() {
		return 0
	}
	return o.sourceIconTextOffset(lh)
}

// All list icons use the same row-sized cell; trailing badges have their own
// reserved column, including while the selected filename scrolls.
func (o *Overlay) drawLibraryIcons(x, y, w, h float32, start, end, lh, winW, winH, viewW, viewH int) {
	if !o.ensureIconTextures(lh, o.textColor()) {
		return
	}
	for i := start; i < end; i++ {
		entry := o.albumEntries[i]
		iconX := x + float32(textPadding(o.fontSize))
		var name string
		switch {
		case o.topLevel().ctx == ctxSourceRoot:
			name = sourceIconName(entry.source)
		case o.showsFavoriteFolderIcons():
			name = favoriteFolderIconName(entry.format)
		case o.topLevel().ctx == ctxFavorites && o.topLevel().playlistID != "":
			if entry.filePath != "" {
				name = favoriteTrackSourceIconName(entry.filePath)
				iconX = x + w - float32(o.scrollbarWidthPx()+o.scalePx(5)+lh)
			}
		case o.favoritesView != nil:
			path := entry.filePath
			if entry.kind == entryRadioStation {
				path = entry.radioStation.Path()
			}
			if path != "" {
				name = favoriteFolderIconName(string(o.favoritesView.GetPlaylist(path)))
				iconX = x + w - float32(o.scrollbarWidthPx()+o.scalePx(5)+lh)
			}
		}
		if name != "" {
			o.drawIconClipped(name, iconX, y+float32((i-start)*lh), float32(lh), x, y, w, h, winW, winH, viewW, viewH)
		}
	}
}

func (o *Overlay) drawIcon(name string, x, y, size float32, winW, winH, viewW, viewH int) {
	tex, drawX, drawY, drawSize := o.iconDrawGeometry(name, x, y, size)
	if tex != 0 {
		glDrawOverlayText(o.programText, tex, 1, drawX, drawY, drawSize, drawSize, winW, winH, viewW, viewH)
	}
}

func (o *Overlay) drawIconClipped(name string, x, y, size, clipX, clipY, clipW, clipH float32, winW, winH, viewW, viewH int) {
	tex, drawX, drawY, drawSize := o.iconDrawGeometry(name, x, y, size)
	if tex != 0 {
		glDrawOverlayTextClipped(o.programText, tex, 1, drawX, drawY, drawSize, drawSize, clipX, clipY, clipW, clipH, winW, winH, viewW, viewH)
	}
}

func (o *Overlay) iconDrawGeometry(name string, x, y, size float32) (tex uint32, drawX, drawY, drawSize float32) {
	tex = o.iconTextures[name]
	if tex == 0 || o.iconTextureSize <= 0 {
		return 0, 0, 0, 0
	}
	rasterRadius := shadowRadius(float64(o.iconTextureSize))
	scale := size / float32(o.iconTextureSize)
	drawRadius := float32(rasterRadius) * scale
	return tex, x - drawRadius, y - drawRadius, float32(o.iconTextureSize+2*rasterRadius) * scale
}

func pageIconName(page UIPage) string {
	switch page {
	case PageLibrary:
		return iconLibrary
	case PagePresets:
		return iconPresets
	case PageSettings:
		return iconSettings
	case PageHelp:
		return iconHelp
	default:
		return ""
	}
}

func favoriteFolderIconName(playlistID string) string {
	switch player.PlaylistID(playlistID) {
	case player.PlaylistStar:
		return iconStar
	case player.PlaylistHeart:
		return iconFavorite
	case player.PlaylistNote:
		return iconLibrary
	default:
		return ""
	}
}

func favoriteTrackSourceIconName(path string) string {
	switch {
	case player.IsRadio(path):
		return iconRadio
	case player.IsModland(path), player.IsModArchive(path):
		return iconCatalogs
	case strings.HasPrefix(path, player.DownloadsPrefix):
		return iconDownloads
	default:
		return iconLocalMusic
	}
}

func outlinedIconRGBA(img image.Image, iconColor color.RGBA, radius int) *image.RGBA {
	bounds := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, bounds.Dx()+2*radius, bounds.Dy()+2*radius))
	mask := image.NewUniform(iconColor)
	draw.DrawMask(rgba, image.Rect(radius, radius, radius+bounds.Dx(), radius+bounds.Dy()), mask, image.Point{}, img, bounds.Min, draw.Src)
	applyOutlineShadow(rgba, iconColor, radius)
	return rgba
}
