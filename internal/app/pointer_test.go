package app

import (
	"testing"

	"github.com/dendec/glitchscope/internal/input"
	"github.com/dendec/glitchscope/internal/ui"
)

func TestPointerOpenGestureConsumesTap(t *testing.T) {
	var gesture pointerOpenGesture
	down := input.PointerEvent{
		Device:    input.PointerMouse,
		Phase:     input.PointerDown,
		PointerID: 4,
		X:         100,
		Y:         200,
		Button:    input.PointerButtonPrimary,
	}
	up := down
	up.Phase = input.PointerUp

	if gesture.handle(down, 480) {
		t.Fatal("pointer down opened the hidden UI")
	}
	if !gesture.handle(up, 480) {
		t.Fatal("pointer tap did not open the hidden UI")
	}
	if gesture.active {
		t.Fatal("open gesture remained armed after tap")
	}
}

func TestPointerOpenGestureDoesNotOpenAfterDrag(t *testing.T) {
	var gesture pointerOpenGesture
	down := input.PointerEvent{
		Device:    input.PointerTouch,
		Phase:     input.PointerDown,
		PointerID: 2,
		X:         100,
		Y:         100,
		Button:    input.PointerButtonPrimary,
	}
	move := down
	move.Phase = input.PointerMove
	move.X += ui.PointerTapSlop(480) + 1
	up := move
	up.Phase = input.PointerUp

	gesture.handle(down, 480)
	gesture.handle(move, 480)
	if gesture.handle(up, 480) {
		t.Fatal("drag opened the hidden UI")
	}
}

func TestBottomPanelTapDoesNotOpenMenu(t *testing.T) {
	a := &App{overlay: &ui.Overlay{}}
	a.overlay.SetShowPlayerBar(true)
	a.overlay.SetPlayingInfo("album", "track.mp3")
	down := input.PointerEvent{Phase: input.PointerDown, X: 200, Y: 479, Button: input.PointerButtonPrimary}
	a.handlePointerEvent(down, 640, 480)
	down.Phase = input.PointerUp
	a.handlePointerEvent(down, 640, 480)
	if a.pointerOpen.active || a.overlay.UIVisible() {
		t.Fatal("panel tap opened menu")
	}
	// The rest of the window still starts an opening gesture.
	e := input.PointerEvent{Phase: input.PointerDown, X: 200, Y: 100, Button: input.PointerButtonPrimary}
	a.handlePointerEvent(e, 640, 480)
	if !a.pointerOpen.active {
		t.Fatal("outside press did not start opening gesture")
	}
	a.handlePointerEvent(input.PointerEvent{Phase: input.PointerCancel}, 640, 480)
	if a.pointerOpen.active {
		t.Fatal("opening gesture survived cancellation")
	}
}
