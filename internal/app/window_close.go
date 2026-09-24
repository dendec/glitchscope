package app

import "github.com/veandco/go-sdl2/sdl"

// isPrimaryWindowClose converts the main window's close-button event into an
// app quit request. SDL_WINDOWEVENT_CLOSE is per-window; it does not reliably
// become SDL_QUIT while the hidden shared-context compiler window still exists.
func isPrimaryWindowClose(event sdl.Event, windowID uint32) bool {
	closeEvent, ok := event.(*sdl.WindowEvent)
	return ok && closeEvent.Event == sdl.WINDOWEVENT_CLOSE && closeEvent.WindowID == windowID
}
