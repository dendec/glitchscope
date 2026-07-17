package ui

import (
	"image"
	"image/color"
	"log/slog"
	"time"

	"github.com/dendec/mdpp/internal/player"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Notifier manages the notification fade-in/out overlay.
type Notifier struct {
	tex      uint32
	texW     int
	texH     int
	text     string
	alpha    float64
	started  time.Time
	visible  bool
	injected bool
	hidden   bool
}

// Visible reports whether the notification is currently shown.
func (n *Notifier) Visible() bool { return n.visible }

// Hidden reports whether the notification is suppressed.
func (n *Notifier) Hidden() bool { return n.hidden }

// Injected reports whether the notification was stamped into the feedback FBO.
func (n *Notifier) Injected() bool { return n.injected }

// Tex returns the notification texture ID (0 if none).
func (n *Notifier) Tex() uint32 { return n.tex }

// Alpha returns the current notification opacity [0..1].
func (n *Notifier) Alpha() float64 { return n.alpha }

// ShowTrack begins the fade-in animation for the given track path.
func (n *Notifier) ShowTrack(path string, fontSize float64) {
	if n.hidden {
		return
	}
	notifSize := fontSize * 3.5
	if notifSize < 20 {
		notifSize = 20
	}
	f, err := opentype.Parse(unifontData)
	if err != nil {
		slog.Error("font parse", "error", err)
		return
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    notifSize,
		DPI:     72,
		Hinting: font.HintingNone,
	})
	if err != nil {
		slog.Error("font face", "error", err)
		return
	}
	defer func() { _ = face.Close() }()

	title := player.TrackTitle(path)
	if title == "" {
		return
	}

	bounds, _ := font.BoundString(face, title)
	const padding = 4
	n.texW = (bounds.Max.X - bounds.Min.X).Ceil() + padding*2
	n.texH = (bounds.Max.Y - bounds.Min.Y).Ceil() + padding*2
	if n.texW == 0 || n.texH == 0 {
		return
	}

	rgba := image.NewRGBA(image.Rect(0, 0, n.texW, n.texH))
	d := &font.Drawer{
		Dst:  rgba,
		Src:  image.NewUniform(color.RGBA{255, 255, 255, 255}),
		Face: face,
		Dot:  fixed.Point26_6{X: fixed.I(padding) - bounds.Min.X, Y: fixed.I(padding) - bounds.Min.Y},
	}
	d.DrawString(title)

	glDeleteTex(n.tex)
	n.tex = glUploadTexture(rgba)
	if n.tex == 0 {
		return
	}

	n.text = title
	n.started = time.Now()
	n.visible = true
	n.injected = false
	n.alpha = 0
}

// Layout returns the centered draw coordinates for the notification texture.
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

// Render draws the notification to the screen (FBO 0).
func (n *Notifier) Render(programText uint32, width, height int) {
	if n.tex == 0 || programText == 0 || width <= 0 || height <= 0 {
		return
	}
	x, y, dw, dh := n.Layout(width, height)
	glDrawOverlayText(programText, n.tex, float32(n.alpha), x, y, dw, dh, width, height)
}

// Inject stamps the notification text once into projectM's feedback framebuffer.
func (n *Notifier) Inject(programText uint32, width, height int) {
	if n.hidden || n.injected || n.tex == 0 || programText == 0 || width <= 0 || height <= 0 {
		return
	}
	if time.Since(n.started) < fadeInDuration+holdDuration {
		return
	}
	x, y, dw, dh := n.Layout(width, height)
	glDrawText(programText, n.tex, 1, x, y, dw, dh, width, height)
	n.injected = true
}

// Update advances the fade animation. Call every frame.
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

// Toggle shows or hides the notification (B button legacy).
func (n *Notifier) Toggle() {
	n.hidden = !n.hidden
	if !n.hidden && n.text != "" {
		n.visible = true
		n.started = time.Now()
		n.injected = false
		n.alpha = 0
	}
}

// Hide removes the notification texture and resets state.
func (n *Notifier) Hide() {
	glDeleteTex(n.tex)
	n.tex = 0
	n.visible = false
	n.text = ""
	n.alpha = 0
}
