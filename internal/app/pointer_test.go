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
