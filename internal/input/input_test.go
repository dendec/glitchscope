package input

import (
	"testing"
	"time"

	"github.com/veandco/go-sdl2/sdl"
)

func TestProcessEventCtrlF(t *testing.T) {
	in := &Input{joyIdx: -1}
	now := time.Now()

	ctrlF := &sdl.KeyboardEvent{
		Type:   sdl.KEYDOWN,
		Keysym: sdl.Keysym{Sym: sdl.K_f, Mod: uint16(sdl.KMOD_CTRL)},
	}
	if got := in.ProcessEvent(ctrlF, false, now); got != ActionToggleFullscreen {
		t.Fatalf("Ctrl+F action = %v, want %v", got, ActionToggleFullscreen)
	}

	plainF := &sdl.KeyboardEvent{
		Type:   sdl.KEYDOWN,
		Keysym: sdl.Keysym{Sym: sdl.K_f},
	}
	if got := in.ProcessEvent(plainF, false, now); got != ActionFavorite {
		t.Fatalf("plain F action = %v, want %v", got, ActionFavorite)
	}

	ctrlF.Repeat = 1
	if got := in.ProcessEvent(ctrlF, false, now); got != ActionNone {
		t.Fatalf("repeated Ctrl+F action = %v, want %v", got, ActionNone)
	}
}

func TestPointerMouseUsesDrawableCoordinates(t *testing.T) {
	in := &Input{joyIdx: -1, pointerSpace: PointerSpace{
		WindowW: 640, WindowH: 480, DrawableW: 1280, DrawableH: 960,
	}}
	in.ProcessEvent(&sdl.MouseButtonEvent{
		Type: sdl.MOUSEBUTTONDOWN, Which: 7, Button: sdl.BUTTON_LEFT, X: 320, Y: 240,
	}, false, time.Time{})

	event, ok := in.PollPointerEvent()
	if !ok {
		t.Fatal("mouse event was not queued")
	}
	if event.Device != PointerMouse || event.Phase != PointerDown || event.PointerID != 7 {
		t.Fatalf("mouse event = %#v", event)
	}
	if event.X != 640 || event.Y != 480 {
		t.Fatalf("mouse coordinates = (%.0f, %.0f), want (640, 480)", event.X, event.Y)
	}
}

func TestPointerTouchIsNotDuplicatedAsMouse(t *testing.T) {
	in := &Input{joyIdx: -1, pointerSpace: PointerSpace{
		WindowW: 640, WindowH: 480, DrawableW: 1280, DrawableH: 960,
	}}
	in.ProcessEvent(&sdl.MouseButtonEvent{
		Type: sdl.MOUSEBUTTONDOWN, Which: sdl.TOUCH_MOUSEID, Button: sdl.BUTTON_LEFT, X: 10, Y: 20,
	}, false, time.Time{})
	in.ProcessEvent(&sdl.TouchFingerEvent{
		Type: sdl.FINGERDOWN, FingerID: 11, X: 0.25, Y: 0.5,
	}, false, time.Time{})

	event, ok := in.PollPointerEvent()
	if !ok {
		t.Fatal("touch event was not queued")
	}
	if event.Device != PointerTouch || event.Phase != PointerDown || event.PointerID != 11 {
		t.Fatalf("touch event = %#v", event)
	}
	if event.X != 320 || event.Y != 480 {
		t.Fatalf("touch coordinates = (%.0f, %.0f), want (320, 480)", event.X, event.Y)
	}
	if _, ok := in.PollPointerEvent(); ok {
		t.Fatal("synthetic touch mouse event was queued")
	}
	if caps := in.PointerCapabilities(); !caps.Touch || caps.Mouse {
		t.Fatalf("pointer capabilities = %#v, want touch only", caps)
	}
}

func TestPointerMotionCoalescesWithinMouse(t *testing.T) {
	in := &Input{joyIdx: -1}
	in.ProcessEvent(&sdl.MouseMotionEvent{Which: 1, X: 10, Y: 20, XRel: 2, YRel: 3}, false, time.Time{})
	in.ProcessEvent(&sdl.MouseMotionEvent{Which: 1, X: 15, Y: 24, XRel: 5, YRel: 4}, false, time.Time{})

	event, ok := in.PollPointerEvent()
	if !ok {
		t.Fatal("mouse motion was not queued")
	}
	if event.X != 15 || event.Y != 24 || event.DX != 7 || event.DY != 7 {
		t.Fatalf("coalesced motion = %#v", event)
	}
	if _, ok := in.PollPointerEvent(); ok {
		t.Fatal("coalesced motion produced multiple events")
	}
}

func TestPointerWheelCarriesLastMousePosition(t *testing.T) {
	in := &Input{joyIdx: -1, pointerSpace: PointerSpace{
		WindowW: 640, WindowH: 480, DrawableW: 1280, DrawableH: 960,
	}}
	in.ProcessEvent(&sdl.MouseMotionEvent{Which: 1, X: 100, Y: 50}, false, time.Time{})
	if _, ok := in.PollPointerEvent(); !ok {
		t.Fatal("mouse motion was not queued")
	}
	in.ProcessEvent(&sdl.MouseWheelEvent{Which: 1, PreciseY: -1}, false, time.Time{})
	event, ok := in.PollPointerEvent()
	if !ok {
		t.Fatal("mouse wheel event was not queued")
	}
	if event.Phase != PointerWheel || !event.PositionValid || event.X != 200 || event.Y != 100 {
		t.Fatalf("mouse wheel position = %#v, want (200, 100) valid", event)
	}
}

func TestNintendoControllerMapping(t *testing.T) {
	in := &Input{joyIdx: -1}
	if got := in.buttonDown(sdl.CONTROLLER_BUTTON_A, false, time.Time{}); got != ActionBack {
		t.Fatalf("SDL A/Nintendo B action = %v, want %v", got, ActionBack)
	}
	if got := in.buttonDown(sdl.CONTROLLER_BUTTON_B, false, time.Time{}); got != ActionSelect {
		t.Fatalf("SDL B/Nintendo A action = %v, want %v", got, ActionSelect)
	}
}
