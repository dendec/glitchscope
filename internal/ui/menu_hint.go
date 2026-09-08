package ui

import (
	"time"

	"github.com/dendec/glitchscope/internal/i18n"
)

const menuHintDuration = 10 * time.Second

// menuHint is session state only. Settings owns the persisted acknowledgement.
type menuHint struct {
	enabled bool
	started time.Time
	texture listTex
	text    string
}

func (h *menuHint) visible(now time.Time, menuOpen bool) bool {
	if !h.enabled || menuOpen {
		return false
	}
	if h.started.IsZero() {
		h.started = now
	}
	return now.Sub(h.started) < menuHintDuration
}

func (o *Overlay) SetMenuHintEnabled(enabled bool) {
	o.menuHint.enabled = enabled
}

// The hint is drawn only on the screen, never into projectM feedback.
func (o *Overlay) drawMenuHint(width, height int) {
	if o.face == nil || !o.menuHint.visible(time.Now(), o.uiVisible) {
		return
	}
	text := "[" + o.controlLabel(hintMenu) + "] " + o.catalog.Text(i18n.ActionOpen)
	if text != o.menuHint.text || o.menuHint.texture.tex == 0 {
		o.menuHint.text = text
		o.deleteTex(&o.menuHint.texture.tex)
		o.menuHint.texture.tex, o.menuHint.texture.w, o.menuHint.texture.h = o.renderTextToTex(text, o.textColor())
	}
	t := o.menuHint.texture
	if t.tex == 0 {
		return
	}
	pad := float32(o.scalePx(8))
	x := float32(width-t.w) / 2
	y := float32(height-t.h) - pad*3
	r, g, b := o.panelBgRGB()
	glDrawFilledRect(o.programRect, x-pad, y-pad/2, float32(t.w)+pad*2, float32(t.h)+pad, r, g, b, 0.92, width, height, width, height)
	glDrawOverlayText(o.programText, t.tex, 1, x, y, float32(t.w), float32(t.h), width, height, width, height)
}
