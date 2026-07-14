package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

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

	if err := sdl.Init(sdl.INIT_VIDEO | sdl.INIT_EVENTS); err != nil {
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

	slog.Info("MDPP skeleton OK. Press ESC to quit.")

	// projectM visualization
	pm, err := projectm.Create()
	if err != nil {
		slog.Error("projectm init", "error", err)
		os.Exit(1)
	}
	defer pm.Destroy()

	slog.Info("projectM initialized")
	w, h := window.GLGetDrawableSize()
	pm.SetWindowSize(int(w), int(h))

	// Load test preset (relative to binary dir, then CWD)
	binDir, _ := os.Executable() // best-effort
	baseDir := ""
	if binDir != "" {
		baseDir = filepath.Dir(binDir) + "/"
	}
	presetPaths := []string{
		baseDir + "presets/test.milk",
		"presets/test.milk",
		baseDir + "test_data/presets/test.milk",
		"test_data/presets/test.milk",
	}
	loaded := false
	for _, p := range presetPaths {
		if _, err := os.Stat(p); err == nil {
			pm.LoadPresetFile(p, false)
			slog.Info("loaded preset", "path", p)
			loaded = true
			break
		}
	}
	if !loaded {
		slog.Warn("no preset found, using idle")
	}

	// Audio player (non-fatal — visualizer works without sound)
	mp3Path := "test_data/song.mp3"
	if len(os.Args) > 1 {
		mp3Path = os.Args[1]
	}

	var p *player.Player
	var overlay *ui.Overlay
	if pl, err := player.New(); err != nil {
		slog.Warn("audio unavailable, running without sound", "error", err)
	} else {
		p = pl
		overlay = ui.New()
		defer overlay.Close()
		if err := p.PlayFile(mp3Path); err != nil {
			slog.Error("play file", "error", err)
		} else {
			overlay.ShowTrack(mp3Path)
		}
		defer p.Close()
	}

	appLoop(window, pm, p, overlay)
}

func appLoop(window *sdl.Window, pm *projectm.Handle, pl *player.Player, overlay *ui.Overlay) {
	ticker := time.NewTicker(time.Second / 60) // ~60 fps
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Process events
			for e := sdl.PollEvent(); e != nil; e = sdl.PollEvent() {
				switch ev := e.(type) {
				case *sdl.QuitEvent:
					return
				case *sdl.KeyboardEvent:
					if ev.Keysym.Sym == sdl.K_ESCAPE && ev.State == sdl.PRESSED {
						return
					}
				}
			}

			// Feed wave data from SoLoud to projectM (no-op if no player)
			if pl != nil {
				if wave := pl.GetWave(); wave != nil {
					pm.PCMAddFloat(wave, projectm.Mono)
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
