package app

import (
	"log/slog"

	"github.com/veandco/go-sdl2/sdl"
)

// toggleFullscreen switches between a resizable window and borderless desktop
// fullscreen. The latter avoids a display-mode change and is more reliable on
// Windows when the OpenGL context is already active.
func (a *App) toggleFullscreen() {
	if a.window == nil {
		return
	}

	flags := a.window.GetFlags()
	fullscreen := flags&(sdl.WINDOW_FULLSCREEN|sdl.WINDOW_FULLSCREEN_DESKTOP) != 0
	var target uint32
	if !fullscreen {
		target = sdl.WINDOW_FULLSCREEN_DESKTOP
	}
	if err := a.window.SetFullscreen(target); err != nil {
		slog.Warn("toggle fullscreen failed", "fullscreen", !fullscreen, "error", err)
		return
	}
	a.presentRequested = true
}
