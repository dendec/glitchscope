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
