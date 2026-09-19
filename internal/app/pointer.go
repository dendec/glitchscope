package app

import (
	"log/slog"

	"github.com/dendec/glitchscope/internal/input"
	"github.com/dendec/glitchscope/internal/player"
	"github.com/dendec/glitchscope/internal/ui"
)

// pointerOpenGesture owns the special gesture used while the overlay is
// hidden. It deliberately lives in app: opening the UI must consume the whole
// down/up pair before the overlay can see any pointer input.
type pointerOpenGesture struct {
	active  bool
	device  input.PointerDevice
	pointer int64
	downX   float32
	downY   float32
	dragged bool
}

func (g *pointerOpenGesture) handle(event input.PointerEvent, screenH int) bool {
	if event.Phase == input.PointerCancel {
		*g = pointerOpenGesture{}
		return false
	}

	if !g.active {
		if event.Phase == input.PointerDown && event.Button == input.PointerButtonPrimary {
			*g = pointerOpenGesture{
				active:  true,
				device:  event.Device,
				pointer: event.PointerID,
				downX:   event.X,
				downY:   event.Y,
			}
		}
		return false
	}

	if event.Device != g.device || event.PointerID != g.pointer {
		return false
	}
	if event.Phase == input.PointerMove {
		dx := event.X - g.downX
		dy := event.Y - g.downY
		if dx*dx+dy*dy > ui.PointerTapSlop(screenH)*ui.PointerTapSlop(screenH) {
			g.dragged = true
		}
		return false
	}
	if event.Phase != input.PointerUp || event.Button != input.PointerButtonPrimary {
		return false
	}

	dx := event.X - g.downX
	dy := event.Y - g.downY
	tap := !g.dragged && dx*dx+dy*dy <= ui.PointerTapSlop(screenH)*ui.PointerTapSlop(screenH)
	*g = pointerOpenGesture{}
	return tap
}

func (a *App) handlePointerEvent(event input.PointerEvent, winW, winH int) {
	if a.overlay == nil {
		return
	}
	if a.inp != nil {
		caps := a.inp.PointerCapabilities()
		a.overlay.SetPointerCapabilities(caps.Keyboard, caps.Mouse, caps.Touch)
	}
	// An opening gesture retains ownership until its release.
	if !a.pointerOpen.active {
		r := a.overlay.HandlePlayerBarPointer(event, winW, winH)
		if r.Consumed {
			switch r.Action {
			case input.ActionPlayPause:
				a.handleAction(r.Action, winW, winH)
			case input.ActionPrevTrack:
				if path, album, ok := a.previousTrack(); ok {
					a.playTrack(path, album)
				}
			case input.ActionNextTrack:
				if path, album, ok := a.manualNext(a.settings.Playback); ok {
					a.playTrack(path, album)
				}
			}
			if r.Seek && a.pl != nil && a.pl.TrackPath() == r.Path && !player.IsRadio(r.Path) {
				if err := a.pl.Seek(r.Seconds); err != nil {
					slog.Warn("pointer seek failed", "error", err)
				}
			}
			return
		}
	}
	if !a.overlay.UIVisible() {
		if a.pointerOpen.handle(event, winH) {
			a.handleAction(input.ActionToggleUI, winW, winH)
		}
		return
	}

	wasSettings := a.overlay.IsSettingsPage()
	switch a.overlay.HandlePointer(event, winW, winH) {
	case ui.PointerResultSelect:
		pointerPanel := a.overlay.FocusPanel()
		a.handleAction(input.ActionSelect, winW, winH)
		a.overlay.RestorePointerFocus(pointerPanel)
	case ui.PointerResultClose:
		a.overlay.CloseUI()
	}
	if !wasSettings && a.overlay.IsSettingsPage() {
		a.refreshSettingsRows(winW, winH, a.displayRefreshRate(), a.overlay.SettingsCursor())
	}
}
