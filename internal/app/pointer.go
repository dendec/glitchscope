package app

import (
	"log/slog"

	"github.com/dendec/glitchscope/internal/i18n"
	"github.com/dendec/glitchscope/internal/input"
	"github.com/dendec/glitchscope/internal/player"
	"github.com/dendec/glitchscope/internal/ui"
	"github.com/veandco/go-sdl2/sdl"
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
			if r.Volume && a.pl != nil {
				a.pl.SetVolume(r.Level)
			}
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
		a.setLinkCursor(false)
		if a.pointerOpen.handle(event, winH) {
			a.handleAction(input.ActionToggleUI, winW, winH)
		}
		return
	}

	wasSettings := a.overlay.IsSettingsPage()
	result := a.overlay.HandlePointer(event, winW, winH)
	if text := a.overlay.ConsumePointerCopy(); text != "" {
		if err := sdl.SetClipboardText(text); err != nil {
			slog.Warn("copy text to clipboard", "error", err)
			a.overlay.ShowMessage(i18n.InfoCopyFailed)
		} else {
			a.overlay.ShowMessage(i18n.InfoCopied)
		}
	}
	if targetURL := a.overlay.ConsumePointerURL(); targetURL != "" {
		if err := openExternalURL(targetURL); err != nil {
			slog.Warn("open external URL", "url", targetURL, "error", err)
		}
	}
	if event.Device == input.PointerMouse && event.Phase == input.PointerMove {
		a.setLinkCursor(a.overlay.PointerOverLink(event.X, event.Y))
	}
	switch result {
	case ui.PointerResultSelect:
		pointerPanel := a.overlay.FocusPanel()
		a.handleAction(input.ActionSelect, winW, winH)
		a.overlay.RestorePointerFocus(pointerPanel)
	case ui.PointerResultClose:
		a.overlay.CloseUI()
		a.setLinkCursor(false)
	}
	if !wasSettings && a.overlay.IsSettingsPage() {
		a.refreshSettingsRows(winW, winH, a.displayRefreshRate(), a.overlay.SettingsCursor())
	}
}

func (a *App) setLinkCursor(active bool) {
	if a.linkCursorActive == active {
		return
	}
	if active {
		if a.linkCursor == nil {
			a.linkCursor = sdl.CreateSystemCursor(sdl.SYSTEM_CURSOR_HAND)
		}
		if a.linkCursor == nil {
			return
		}
		sdl.SetCursor(a.linkCursor)
	} else {
		sdl.SetCursor(sdl.GetDefaultCursor())
	}
	a.linkCursorActive = active
}
