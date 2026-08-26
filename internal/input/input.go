// Package input handles SDL gamepad and keyboard events.
package input

import (
	"log/slog"

	"github.com/veandco/go-sdl2/sdl"
)

// Action represents a high-level input operation.
type Action int

const (
	ActionNone Action = iota
	ActionPlayPause
	ActionToggleOverlay
	ActionNextTrack
	ActionPrevTrack
	ActionNextAlbum
	ActionPrevAlbum
	ActionNextPreset
	ActionPrevPreset
	ActionQuit
	// UI navigation actions.
	ActionSelect
	ActionBack
	ActionToggleUI
	ActionFocusLeft
	ActionFocusRight
	ActionCursorUp
	ActionCursorDown
	ActionRandomPreset
	// Seek actions.
	ActionSeekForward
	ActionSeekBackward
)

const axisDeadZone int16 = 8000

// Input manages keyboard and gamepad input.
type Input struct {
	controller *sdl.GameController
	joyIdx     int // -1 = no controller

	// Axis tracking for repeat prevention.
	lAxisY int16
	lAxisX int16
}

// New creates an Input handler and opens the first game controller.
func New() *Input {
	in := &Input{joyIdx: -1}
	in.tryOpenController()
	return in
}

// Close releases the game controller if open.
func (in *Input) Close() {
	if in.controller != nil {
		in.controller.Close()
		in.controller = nil
	}
	in.joyIdx = -1
}

// ProcessEvent translates an SDL event into an Action.
func (in *Input) ProcessEvent(event sdl.Event) Action {
	switch e := event.(type) {
	case *sdl.QuitEvent:
		return ActionQuit

	case *sdl.KeyboardEvent:
		if e.Type != sdl.KEYDOWN {
			return ActionNone
		}
		if e.Repeat != 0 {
			// Ignore OS key-repeat: held-key acceleration is driven by
			// polling GetKeyboardState in Overlay.Update.
			return ActionNone
		}
		return keyToAction(e.Keysym.Sym)

	case *sdl.ControllerButtonEvent:
		if e.Type != sdl.CONTROLLERBUTTONDOWN {
			return ActionNone
		}
		return buttonToAction(e.Button)

	case *sdl.ControllerAxisEvent:
		return axisToAction(in, e)

	case *sdl.ControllerDeviceEvent:
		switch e.Type {
		case sdl.CONTROLLERDEVICEADDED:
			if in.controller == nil {
				in.tryOpenController()
			}
		case sdl.CONTROLLERDEVICEREMOVED:
			if in.joyIdx >= 0 {
				in.Close()
				in.tryOpenController()
			}
		}
	}

	return ActionNone
}

// DPadUpHeld reports whether the D-Pad up button is held.
func (in *Input) DPadUpHeld() bool {
	if in.controller == nil {
		return false
	}
	return in.controller.Button(sdl.CONTROLLER_BUTTON_DPAD_UP) != 0
}

// DPadDownHeld reports whether the D-Pad down button is held.
func (in *Input) DPadDownHeld() bool {
	if in.controller == nil {
		return false
	}
	return in.controller.Button(sdl.CONTROLLER_BUTTON_DPAD_DOWN) != 0
}

// HasController reports whether a game controller is currently connected.
func (in *Input) HasController() bool {
	return in.controller != nil
}

// RightStickX returns the current right stick X axis value (deadzone-filtered).
// Positive = right, negative = left. Returns 0 when no controller is connected.
func (in *Input) RightStickX() float64 {
	if in.controller == nil {
		return 0
	}
	v := float64(in.controller.Axis(sdl.CONTROLLER_AXIS_RIGHTX))
	if v > -float64(axisDeadZone) && v < float64(axisDeadZone) {
		return 0
	}
	return v / 32767.0
}

// tryOpenController opens the first available game controller.
func (in *Input) tryOpenController() {
	if sdl.NumJoysticks() > 0 {
		c := sdl.GameControllerOpen(0)
		if c == nil {
			slog.Warn("game controller open returned nil")
			return
		}
		in.controller = c
		in.joyIdx = 0
		in.lAxisY = 0
		in.lAxisX = 0
		slog.Info("game controller opened")
	}
}

// --- internal ---

func keyToAction(key sdl.Keycode) Action {
	switch key {
	case sdl.K_ESCAPE, sdl.K_q:
		return ActionQuit
	case sdl.K_SPACE:
		return ActionPlayPause
	case sdl.K_LEFT:
		return ActionFocusLeft
	case sdl.K_RIGHT:
		return ActionFocusRight
	case sdl.K_UP:
		return ActionCursorUp
	case sdl.K_DOWN:
		return ActionCursorDown
	case sdl.K_n:
		return ActionNextPreset
	case sdl.K_r:
		return ActionRandomPreset
	case sdl.K_p:
		return ActionPrevPreset
	case sdl.K_b:
		return ActionToggleOverlay
	case sdl.K_TAB:
		return ActionToggleUI
	case sdl.K_RETURN, sdl.K_KP_ENTER:
		return ActionSelect
	case sdl.K_BACKSPACE:
		return ActionBack
	case sdl.K_COMMA:
		return ActionSeekBackward
	case sdl.K_PERIOD:
		return ActionSeekForward
	}
	return ActionNone
}

func buttonToAction(btn uint8) Action {
	// SDL normalizes to Xbox layout. Nintendo labels are offset:
	//   Nintendo A (right) = SDL B,  Nintendo B (bottom) = SDL A
	//   Nintendo X (top)   = SDL Y,  Nintendo Y (left)  = SDL X
	switch btn {
	case sdl.CONTROLLER_BUTTON_A:
		return ActionBack // Nintendo B (bottom) = back
	case sdl.CONTROLLER_BUTTON_B:
		return ActionSelect // Nintendo A (right) = confirm
	case sdl.CONTROLLER_BUTTON_X:
		return ActionRandomPreset // Nintendo Y (left) = random
	case sdl.CONTROLLER_BUTTON_Y:
		return ActionPlayPause // Nintendo X (top) = play/pause
	case sdl.CONTROLLER_BUTTON_DPAD_UP:
		return ActionCursorUp
	case sdl.CONTROLLER_BUTTON_DPAD_DOWN:
		return ActionCursorDown
	case sdl.CONTROLLER_BUTTON_DPAD_LEFT:
		return ActionFocusLeft
	case sdl.CONTROLLER_BUTTON_DPAD_RIGHT:
		return ActionFocusRight
	case sdl.CONTROLLER_BUTTON_START:
		return ActionToggleUI
	case sdl.CONTROLLER_BUTTON_BACK:
		return ActionToggleOverlay
	case sdl.CONTROLLER_BUTTON_LEFTSHOULDER:
		return ActionPrevPreset
	case sdl.CONTROLLER_BUTTON_RIGHTSHOULDER:
		return ActionNextPreset
	}
	return ActionNone
}

func axisToAction(in *Input, e *sdl.ControllerAxisEvent) Action {
	switch e.Axis {
	case sdl.CONTROLLER_AXIS_LEFTY:
		// Up/Down: cursor navigation (maps to album nav when UI hidden)
		if e.Value > axisDeadZone && in.lAxisY <= axisDeadZone {
			in.lAxisY = e.Value
			return ActionCursorDown
		}
		if e.Value < -axisDeadZone && in.lAxisY >= -axisDeadZone {
			in.lAxisY = e.Value
			return ActionCursorUp
		}
		if e.Value > -axisDeadZone && e.Value < axisDeadZone {
			in.lAxisY = 0
		} else {
			in.lAxisY = e.Value
		}

	case sdl.CONTROLLER_AXIS_LEFTX:
		// Left/Right: focus navigation (maps to track nav when UI hidden)
		if e.Value > axisDeadZone && in.lAxisX <= axisDeadZone {
			in.lAxisX = e.Value
			return ActionFocusRight
		}
		if e.Value < -axisDeadZone && in.lAxisX >= -axisDeadZone {
			in.lAxisX = e.Value
			return ActionFocusLeft
		}
		if e.Value > -axisDeadZone && e.Value < axisDeadZone {
			in.lAxisX = 0
		} else {
			in.lAxisX = e.Value
		}
	}
	// Right stick X is NOT routed through actions: it drives continuous seek
	// directly from the app main loop (internal/app/run_loop.go) so seek speed
	// can be proportional to deflection and accelerate on hold.
	return ActionNone
}
