package ui

import (
	"image/color"

	"github.com/dendec/glitchscope/internal/input"
	"github.com/dendec/glitchscope/internal/player"
	"golang.org/x/image/font"
)

// PlayerBarResult is a playback command; app owns its execution.
type PlayerBarResult struct {
	Consumed bool
	Action   input.Action
	Seek     bool
	Seconds  float64
	Path     string
}

type playerBarPress struct {
	active  bool
	device  input.PointerDevice
	pointer int64
	target  int
	x, y    float32
	seconds float64
	path    string
}

func (o *Overlay) playerBarPosition() float64 {
	if o.barPress.active && o.barPress.target == 4 && o.barPress.path == o.playingPath && !o.loading {
		return o.barPress.seconds
	}
	return o.position
}

func (o *Overlay) playerBarPrefix() string {
	status := "▶"
	if o.paused {
		status = "⏸"
	}
	if o.pointerMouse || o.pointerTouch {
		status = "⏸"
		if o.paused {
			status = "▶"
		}
		return " ⏮   " + status + "   ⏭ "
	}
	return status + " "
}

// playerBarBounds also describes the persistent panel when the menu is hidden.
func (o *Overlay) playerBarBounds(w, h int) (top, end, statusY int) {
	l := o.overlayLayout(w, h)
	end = h
	if o.uiVisible {
		end -= l.hintRowH
	}
	statusY = end - o.scalePx(4) - l.statusRowH
	top = statusY - l.presetLineH
	return
}

func (o *Overlay) playerBarTarget(x, y float32, w, h int) int {
	top, end, statusY := o.playerBarBounds(w, h)
	if x < 0 || x >= float32(w) || y < float32(top) || y >= float32(end) {
		return -1
	}
	if y >= float32(statusY) && y < float32(end-o.scalePx(4)) && !o.loading && o.playingPath != "" && (o.pointerMouse || o.pointerTouch) && o.face != nil {
		// Each padded glyph is both a rendered button and its hit region.
		status := "⏸"
		if o.paused {
			status = "▶"
		}
		prefixes := []string{" ⏮  ", " ⏮   " + status + "  ", o.playerBarPrefix()}
		for i, prefix := range prefixes {
			if x < float32(font.MeasureString(o.face, prefix).Ceil()) {
				return i + 1
			}
		}
	}
	if y >= float32(end-o.scalePx(12)) {
		if !o.loading && o.duration > 0 && !player.IsRadio(o.playingPath) {
			return 4
		}
		return 0
	}

	return 0
}

// HandlePlayerBarPointer captures the entire gesture, including releases outside
// the panel, so neither scrubbing nor empty panel taps can open the menu.
func (o *Overlay) HandlePlayerBarPointer(e input.PointerEvent, w, h int) PlayerBarResult {
	result := PlayerBarResult{}
	if e.Phase == input.PointerCancel {
		result.Consumed = o.barPress.active
		o.barPress = playerBarPress{}
		o.bottomDirty = true
		return result
	}
	visible := o.showPlayerBar && (o.playingPath != "" || o.loading || o.presetNameTex != 0)
	target := -1
	if visible {
		target = o.playerBarTarget(e.X, e.Y, w, h)
	}
	result.Consumed = target >= 0 || o.barPress.active
	if o.barPress.active {
		p := &o.barPress
		if e.Device != p.device || e.PointerID != p.pointer {
			return result
		}
		if !visible || p.path != o.playingPath || o.loading {
			o.barPress = playerBarPress{}
			o.bottomDirty = true
			return result
		}
		if p.target == 4 && w > 0 {
			p.seconds = min(max(float64(e.X)/float64(w), 0), 1) * o.duration
			o.bottomDirty = true
		}
		if e.Phase == input.PointerUp && e.Button == input.PointerButtonPrimary {
			if p.target == 4 {
				result.Seek = true
				result.Seconds = p.seconds
				result.Path = p.path
			} else if target == p.target {
				dx, dy := e.X-p.x, e.Y-p.y
				if dx*dx+dy*dy <= PointerTapSlop(h)*PointerTapSlop(h) {
					switch target {
					case 1:
						result.Action = input.ActionPrevTrack
					case 2:
						result.Action = input.ActionPlayPause
					case 3:
						result.Action = input.ActionNextTrack
					}
				}
			}
			o.barPress = playerBarPress{}
			o.bottomDirty = true
		}
		return result
	}
	if target >= 0 && e.Phase == input.PointerDown && e.Button == input.PointerButtonPrimary {
		if o.uiVisible && o.pointerModalBlocks(pointerTarget{}) {
			target = 0
		}
		o.barPress = playerBarPress{active: true, device: e.Device, pointer: e.PointerID, target: target, x: e.X, y: e.Y, path: o.playingPath}
		if target == 4 && w > 0 {
			o.barPress.seconds = min(max(float64(e.X)/float64(w), 0), 1) * o.duration
			o.bottomDirty = true
		}
	}
	return result
}

func (o *Overlay) playbackProgressColors() (dark, light color.RGBA) {
	p := o.palette()
	dark, light = p.background, p.text
	brightness := func(c color.RGBA) int { return 299*int(c.R) + 587*int(c.G) + 114*int(c.B) }
	if brightness(dark) > brightness(light) {
		dark, light = light, dark
	}
	dark.A, light.A = 255, 255
	return
}

func (o *Overlay) playbackProgressState() (visible, seekable bool, progress float64) {
	visible = o.showPlayerBar && (o.playingPath != "" || o.loading || o.presetNameTex != 0)
	if !visible || o.playingPath == "" || o.loading || o.duration <= 0 || player.IsRadio(o.playingPath) {
		return visible, false, 0
	}
	return true, true, min(max(o.playerBarPosition()/o.duration, 0), 1)
}

func (o *Overlay) drawPlaybackProgress(w, h, viewW, viewH int) {
	visible, seekable, progress := o.playbackProgressState()
	if !visible {
		return
	}
	_, end, _ := o.playerBarBounds(w, h)
	barH := o.scalePx(4)
	y := float32(end - barH)
	dark, light := o.playbackProgressColors()
	glDrawFilledRect(o.programRect, 0, y, float32(w), float32(barH), float32(dark.R)/255, float32(dark.G)/255, float32(dark.B)/255, 1, w, h, viewW, viewH)
	if !seekable {
		return
	}
	inset := o.scalePx(1)
	glDrawFilledRect(o.programRect, 0, y+float32(inset), float32(float64(w)*progress), float32(max(1, barH-2*inset)), float32(light.R)/255, float32(light.G)/255, float32(light.B)/255, 1, w, h, viewW, viewH)
}
