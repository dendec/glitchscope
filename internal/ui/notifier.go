package ui

import (
	"image"
	"image/color"
	"log/slog"
	"time"

	"github.com/dendec/glitchscope/internal/i18n"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type Notifier struct {
	tex        uint32
	texW       int
	texH       int
	text       string
	textKey    i18n.Key
	textArgs   []any
	fontSize   float64
	alpha      float64
	started    time.Time
	visible    bool
	injected   bool
	hidden     bool
	cachedFont *opentype.Font // parsed once, reused
}

func (n *Notifier) Visible() bool { return n.visible }

func (n *Notifier) Hidden() bool { return n.hidden }

func (n *Notifier) Injected() bool { return n.injected }

func (n *Notifier) Tex() uint32 { return n.tex }

func (n *Notifier) Alpha() float64 { return n.alpha }

func (n *Notifier) TextKey() i18n.Key { return n.textKey }

// ShowTrack begins the fade-in animation for the given track path.
func (n *Notifier) ShowTrack(path string, fontSize float64, textColor color.RGBA) {
	n.textKey = ""
	n.textArgs = nil
	if n.hidden {
		return
	}
	if path == "" {
		return
	}
	if !n.rebuildTexture(path, fontSize, textColor) {
		return
	}

	n.text = path
	n.fontSize = fontSize
	n.started = time.Now()
	n.visible = true
	n.injected = false
	n.alpha = 0
}

func (n *Notifier) ShowMessage(catalog i18n.Catalog, key i18n.Key, fontSize float64, textColor color.RGBA, args ...any) {
	n.ShowTrack(catalog.Format(key, args...), fontSize, textColor)
	n.textKey = key
	n.textArgs = append([]any(nil), args...)
}

func (n *Notifier) Relocalize(catalog i18n.Catalog, fontSize float64, textColor color.RGBA) {
	if n.textKey == "" {
		return
	}
	text := catalog.Format(n.textKey, n.textArgs...)
	if n.rebuildTexture(text, fontSize, textColor) {
		n.text = text
	}
}

// Retheme rebuilds the cached texture with a new text color.
func (n *Notifier) Retheme(textColor color.RGBA) {
	if n.text == "" {
		return
	}
	n.rebuildTexture(n.text, n.fontSize, textColor)
}

// rebuildTexture rasterizes path into a shadowed texture and uploads it.
// Returns false if rendering fails. Does not touch animation state.
func (n *Notifier) rebuildTexture(path string, fontSize float64, textColor color.RGBA) bool {
	notifSize := fontSize * 3.5
	if notifSize < 20 {
		notifSize = 20
	}
	if n.cachedFont == nil {
		var err error
		n.cachedFont, err = opentype.Parse(unifontData)
		if err != nil {
			slog.Error("font parse", "error", err)
			return false
		}
	}
	face, err := opentype.NewFace(n.cachedFont, &opentype.FaceOptions{
		Size:    notifSize,
		DPI:     72,
		Hinting: font.HintingNone,
	})
	if err != nil {
		slog.Error("font face", "error", err)
		return false
	}
	defer func() { _ = face.Close() }()

	bounds, _ := font.BoundString(face, path)
	contentW := (bounds.Max.X - bounds.Min.X).Ceil()
	contentH := (bounds.Max.Y - bounds.Min.Y).Ceil()
	if contentW == 0 || contentH == 0 {
		return false
	}

	rgba, texW, texH, _ := newShadowedTextRGBA(contentW, contentH, notifSize, textColor, func(rgba *image.RGBA, originX, originY int) {
		d := &font.Drawer{
			Dst:  rgba,
			Src:  image.NewUniform(textColor),
			Face: face,
			Dot:  fixed.Point26_6{X: fixed.I(originX) - bounds.Min.X, Y: fixed.I(originY) - bounds.Min.Y},
		}
		d.DrawString(path)
	})

	tex := glUploadTexture(rgba)
	if tex == 0 {
		return false
	}
	glDeleteTex(n.tex)
	n.tex = tex
	n.texW = texW
	n.texH = texH
	return true
}

func (n *Notifier) Layout(width, height int) (x, y, drawWidth, drawHeight float32) {
	drawHeight = float32(height) * 0.12
	scale := drawHeight / float32(n.texH)
	drawWidth = float32(n.texW) * scale
	maxWidth := float32(width) * 0.85
	if drawWidth > maxWidth {
		scale = maxWidth / float32(n.texW)
		drawWidth = maxWidth
		drawHeight = float32(n.texH) * scale
	}
	x = (float32(width) - drawWidth) / 2
	y = (float32(height) - drawHeight) / 2
	return
}

func (n *Notifier) Render(programText uint32, width, height int) {
	if n.tex == 0 || programText == 0 || width <= 0 || height <= 0 {
		return
	}
	x, y, dw, dh := n.Layout(width, height)
	glDrawOverlayText(programText, n.tex, float32(n.alpha), x, y, dw, dh, width, height, width, height)
}

func (n *Notifier) Inject(programText uint32, winW, winH, viewW, viewH int) {
	if n.hidden || n.injected || n.tex == 0 || programText == 0 || winW <= 0 || winH <= 0 || viewW <= 0 || viewH <= 0 {
		return
	}
	if time.Since(n.started) < fadeInDuration+holdDuration {
		return
	}
	x, y, dw, dh := n.Layout(winW, winH)
	glDrawText(programText, n.tex, 1, x, y, dw, dh, winW, winH, viewW, viewH)
	n.injected = true
}

// Update advances the fade animation.
func (n *Notifier) Update(uiVisible bool) {
	if !n.visible {
		return
	}
	if n.injected && !uiVisible {
		n.Hide()
		return
	}
	elapsed := time.Since(n.started)
	if elapsed >= fadeInDuration+holdDuration {
		if uiVisible {
			n.Hide()
		}
		return
	}
	switch {
	case elapsed < fadeInDuration:
		n.alpha = float64(elapsed) / float64(fadeInDuration)
	default:
		n.alpha = 1.0
	}
}

func (n *Notifier) Toggle() {
	n.hidden = !n.hidden
	if !n.hidden && n.text != "" {
		n.visible = true
		n.started = time.Now()
		n.injected = false
		n.alpha = 0
	}
}

func (n *Notifier) Hide() {
	glDeleteTex(n.tex)
	n.tex = 0
	n.visible = false
	n.text = ""
	n.textKey = ""
	n.textArgs = nil
	n.alpha = 0
}
