package input

import "github.com/veandco/go-sdl2/sdl"

// PointerDevice identifies the physical source of a pointer event.
type PointerDevice uint8

const (
	PointerMouse PointerDevice = iota
	PointerTouch
)

// PointerPhase identifies the lifecycle stage of a pointer event.
type PointerPhase uint8

const (
	PointerMove PointerPhase = iota
	PointerDown
	PointerUp
	PointerWheel
	PointerCancel
)

// PointerButton is deliberately smaller than SDL's button namespace. Pointer
// consumers only need to distinguish the primary button from unsupported
// buttons; the latter are still useful for capability detection.
type PointerButton uint8

const (
	PointerButtonNone    PointerButton = 0
	PointerButtonPrimary PointerButton = sdl.BUTTON_LEFT
)

// PointerEvent is the SDL-independent pointer event passed to the UI. X/Y and
// DX/DY are already expressed in drawable/UI pixels. Touch coordinates are
// converted from SDL's normalized 0..1 representation by Input.
type PointerEvent struct {
	Device    PointerDevice
	Phase     PointerPhase
	PointerID int64
	X         float32
	Y         float32
	// PositionValid is false only for a mouse wheel event received before SDL
	// has reported a mouse position. Wheel events do not carry coordinates in
	// SDL2, so consumers can fall back to the active panel in that case.
	PositionValid bool
	DX            float32
	DY            float32
	ScrollY       float32
	Button        PointerButton
}

// PointerCapabilities describes input devices observed by the application.
// Keyboard, mouse, and touch are sticky for the session because SDL does not
// expose a portable physical-device enumeration for those devices. The
// controller flag is a live snapshot of the already managed SDL controller.
type PointerCapabilities struct {
	Keyboard   bool
	Controller bool
	Mouse      bool
	Touch      bool
}

// PointerSpace describes the conversion between SDL window coordinates and
// the drawable coordinates used by the OpenGL overlay.
type PointerSpace struct {
	WindowW   int
	WindowH   int
	DrawableW int
	DrawableH int
}

func (s PointerSpace) point(x, y float32) (float32, float32) {
	if s.WindowW <= 0 || s.WindowH <= 0 || s.DrawableW <= 0 || s.DrawableH <= 0 {
		return x, y
	}
	return x * float32(s.DrawableW) / float32(s.WindowW),
		y * float32(s.DrawableH) / float32(s.WindowH)
}

func (s PointerSpace) normalizedPoint(x, y float32) (float32, float32) {
	if s.WindowW > 0 && s.WindowH > 0 {
		return s.point(x*float32(s.WindowW), y*float32(s.WindowH))
	}
	if s.DrawableW > 0 && s.DrawableH > 0 {
		return x * float32(s.DrawableW), y * float32(s.DrawableH)
	}
	return x, y
}

func (s PointerSpace) normalizedDelta(dx, dy float32) (float32, float32) {
	if s.WindowW > 0 && s.WindowH > 0 {
		return s.delta(dx*float32(s.WindowW), dy*float32(s.WindowH))
	}
	if s.DrawableW > 0 && s.DrawableH > 0 {
		return dx * float32(s.DrawableW), dy * float32(s.DrawableH)
	}
	return dx, dy
}

func (s PointerSpace) delta(dx, dy float32) (float32, float32) {
	if s.WindowW <= 0 || s.WindowH <= 0 || s.DrawableW <= 0 || s.DrawableH <= 0 {
		return dx, dy
	}
	return dx * float32(s.DrawableW) / float32(s.WindowW),
		dy * float32(s.DrawableH) / float32(s.WindowH)
}
