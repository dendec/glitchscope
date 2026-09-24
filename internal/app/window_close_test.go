package app

import (
	"testing"

	"github.com/veandco/go-sdl2/sdl"
)

func TestPrimaryWindowClose(t *testing.T) {
	closeEvent := &sdl.WindowEvent{WindowID: 7, Event: sdl.WINDOWEVENT_CLOSE}
	if !isPrimaryWindowClose(closeEvent, 7) {
		t.Fatal("close event for primary window was ignored")
	}
	if isPrimaryWindowClose(closeEvent, 8) {
		t.Fatal("close event for a different window quit the app")
	}
	if isPrimaryWindowClose(&sdl.QuitEvent{}, 7) {
		t.Fatal("global quit event was mistaken for a window close")
	}
}
