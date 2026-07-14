package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/dendec/mdpp/internal/input"
	"github.com/dendec/mdpp/internal/player"
	"github.com/dendec/mdpp/internal/projectm"
	"github.com/dendec/mdpp/internal/ui"
	"github.com/veandco/go-sdl2/sdl"
)

const (
	winTitle = "MDPP — MilkDrop Portable Player"
	winW     = 640
	winH     = 480
)

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := sdl.Init(sdl.INIT_VIDEO | sdl.INIT_EVENTS | sdl.INIT_GAMECONTROLLER | sdl.INIT_JOYSTICK); err != nil {
		slog.Error("sdl init", "error", err)
		os.Exit(1)
	}
	defer sdl.Quit()

	window, err := sdl.CreateWindow(winTitle,
		sdl.WINDOWPOS_UNDEFINED, sdl.WINDOWPOS_UNDEFINED,
		winW, winH,
		sdl.WINDOW_OPENGL|sdl.WINDOW_SHOWN|sdl.WINDOW_FULLSCREEN_DESKTOP)
	if err != nil {
		slog.Error("window", "error", err)
		os.Exit(1)
	}
	defer window.Destroy()

	glCtx, err := window.GLCreateContext()
	if err != nil {
		slog.Error("gl context", "error", err)
		os.Exit(1)
	}
	defer sdl.GLDeleteContext(glCtx)

	// projectM visualization
	t0 := time.Now()
	pm, err := projectm.Create()
	if err != nil {
		slog.Error("projectm init", "error", err)
		os.Exit(1)
	}
	defer pm.Destroy()
	slog.Info("projectM init", "ms", time.Since(t0).Milliseconds())

	w, h := window.GLGetDrawableSize()
	pm.SetWindowSize(int(w), int(h))

	// Find base dir (where binary lives — presets, music relative to this).
	binDir, _ := os.Executable()
	baseDir := "."
	if binDir != "" {
		baseDir = filepath.Dir(binDir)
	}

	// Load first preset from dirs in priority order.
	presetPaths := []string{
		baseDir + "/presets/test.milk",
		"presets/test.milk",
		baseDir + "/test_data/presets/test.milk",
		"test_data/presets/test.milk",
	}
	loaded := false
	t1 := time.Now()
	for _, p := range presetPaths {
		if _, err := os.Stat(p); err == nil {
			pm.LoadPresetFile(p, false)
			slog.Info("loaded preset", "path", p)
			loaded = true
			break
		}
	}
	if loaded {
		slog.Info("preset load", "ms", time.Since(t1).Milliseconds())
	} else {
		slog.Warn("no preset found, using idle")
	}

	// Audio player (non-fatal).
	t2 := time.Now()
	var p *player.Player
	var overlay *ui.Overlay
	if pl, err := player.New(); err != nil {
		slog.Warn("audio unavailable, running without sound", "error", err)
	} else {
		p = pl
		overlay = ui.New()
		defer p.Close()
		defer overlay.Close()
	}
	slog.Info("audio init", "ms", time.Since(t2).Milliseconds())

	// Music library scan — prefer music/ subdirs, fallback to base dir.
	musicDir := baseDir + "/music"
	if _, err := os.Stat(musicDir); os.IsNotExist(err) {
		musicDir = baseDir + "/test_data/music"
	}
	if _, err := os.Stat(musicDir); os.IsNotExist(err) {
		musicDir = baseDir
	}
	t3 := time.Now()
	lib, err := player.NewLibrary(musicDir)
	if err != nil {
		slog.Warn("music scan failed", "error", err)
	}
	if lib != nil {
		slog.Info("music scan", "albums", lib.AlbumCount(), "ms", time.Since(t3).Milliseconds())
	}

	// Start playing first track if available.
	if p != nil && lib != nil {
		if first := lib.PlayCurrent(); first != "" {
			playTrack(p, overlay, first, lib.CurrentAlbum().Name)
		}
	}

	// Input handler.
	inp := input.New()
	defer inp.Close()

	appLoop(window, pm, p, overlay, inp, lib)
}

func appLoop(window *sdl.Window, pm *projectm.Handle, pl *player.Player, overlay *ui.Overlay, inp *input.Input, lib *player.Library) {
	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Process events → dispatch actions.
			for e := sdl.PollEvent(); e != nil; e = sdl.PollEvent() {
				act := inp.ProcessEvent(e)
				handleAction(act, pm, pl, overlay, lib)
				if act == input.ActionQuit {
					return
				}
			}

			// Feed wave data → projectM.
			if pl != nil {
				if wave := pl.GetWave(); wave != nil {
					pm.PCMAddFloat(wave, projectm.Mono)
				}
			}

			// Auto-advance to next track when current one finishes.
			if pl != nil && lib != nil && pl.Voice() != 0 && !pl.IsValidVoice() {
				if path := lib.TrackNext(); path != "" {
					playTrack(pl, overlay, path, lib.CurrentAlbum().Name)
				}
			}

			w, h := window.GLGetDrawableSize()
			pm.RenderFrame()

			if overlay != nil {
				overlay.Update()
				overlay.Draw(int(w), int(h))
				pm.BindFeedbackFramebuffer()
				overlay.Inject(int(w), int(h))
			}

			window.GLSwap()
		}
	}
}

// handleAction dispatches an input action to the appropriate player/library/overlay logic.
func handleAction(act input.Action, pm *projectm.Handle, pl *player.Player, overlay *ui.Overlay, lib *player.Library) {
	switch act {
	case input.ActionQuit:
		return

	case input.ActionPlayPause:
		if pl != nil {
			pl.TogglePause()
		}

	case input.ActionToggleOverlay:
		if overlay != nil {
			overlay.ToggleVisibility()
		}

	case input.ActionNextTrack:
		if lib == nil || pl == nil {
			return
		}
		if path := lib.TrackNext(); path != "" {
			playTrack(pl, overlay, path, lib.CurrentAlbum().Name)
		}

	case input.ActionPrevTrack:
		if lib == nil || pl == nil {
			return
		}
		if path := lib.TrackPrev(); path != "" {
			playTrack(pl, overlay, path, lib.CurrentAlbum().Name)
		}

	case input.ActionNextAlbum:
		if lib == nil || pl == nil {
			return
		}
		if path := lib.AlbumNext(); path != "" {
			playTrack(pl, overlay, path, lib.CurrentAlbum().Name)
		}

	case input.ActionPrevAlbum:
		if lib == nil || pl == nil {
			return
		}
		if path := lib.AlbumPrev(); path != "" {
			playTrack(pl, overlay, path, lib.CurrentAlbum().Name)
		}

	case input.ActionNextPreset:
		pm.RenderFrame() // refresh before switching
		// projectM hardcode cycling: just render triggers slow preset change via callback.
		// We can't force next preset without direct API, skip for now.
		slog.Debug("next preset — projectM auto-cycles")

	case input.ActionPrevPreset:
		slog.Debug("prev preset — projectM auto-cycles")
	}
}

func playTrack(pl *player.Player, overlay *ui.Overlay, path string, album string) {
	if err := pl.PlayFile(path); err != nil {
		slog.Error("play", "path", path, "error", err)
		return
	}
	if overlay != nil {
		label := album
		if album != "" {
			label = album + " — " + player.TrackTitle(path)
		}
		overlay.ShowTrack(label)
	}
	slog.Info("now playing", "track", path, "album", album)
}


