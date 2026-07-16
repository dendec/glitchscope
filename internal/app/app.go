package app

import (
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/dendec/mdpp/internal/config"
	"github.com/dendec/mdpp/internal/input"
	"github.com/dendec/mdpp/internal/player"
	"github.com/dendec/mdpp/internal/presets"
	"github.com/dendec/mdpp/internal/projectm"
	"github.com/dendec/mdpp/internal/ui"
	"github.com/veandco/go-sdl2/sdl"
)

const (
	fpsWindow       = 60
	lowFPSThresh    = 30.0
	transitionDelay = 1500 * time.Millisecond
)

type pendingPreset struct {
	name string
	at   time.Time
}

// App holds the full application state.
type App struct {
	window *sdl.Window
	glCtx  sdl.GLContext
	pm     *projectm.Handle
	rt     *projectm.RenderTarget

	pl      *player.Player
	overlay *ui.Overlay
	inp     *input.Input
	lib     *player.Library

	gs           *config.GraphicsSettings
	settingsPath string
	presetNames  []string
	presetIdx    int

	renderScale         float64
	renderScaleExplicit bool

	pending pendingPreset // pending preset name + scheduled load time
}

// New creates an App with display initialised (SDL, window, GL, projectM,
// settings, render target, presets).  The player / overlay / input / library
// are created later by Init().
func New(fullscreen bool, width, height int, renderScale float64, renderNearest bool, renderScaleExplicit, renderNearestSet bool) (*App, error) {
	a := &App{
		settingsPath:        config.SettingsPath(),
		renderScale:         renderScale,
		renderScaleExplicit: renderScaleExplicit,
	}

	if err := sdl.Init(sdl.INIT_VIDEO | sdl.INIT_EVENTS | sdl.INIT_GAMECONTROLLER | sdl.INIT_JOYSTICK); err != nil {
		return nil, fmt.Errorf("sdl init: %w", err)
	}

	winFlags := uint32(sdl.WINDOW_OPENGL | sdl.WINDOW_SHOWN | sdl.WINDOW_RESIZABLE)
	if fullscreen {
		winFlags |= sdl.WINDOW_FULLSCREEN_DESKTOP
	}
	win, err := sdl.CreateWindow("MDPP — MilkDrop Portable Player",
		sdl.WINDOWPOS_UNDEFINED, sdl.WINDOWPOS_UNDEFINED,
		int32(width), int32(height), winFlags)
	if err != nil {
		sdl.Quit()
		return nil, fmt.Errorf("window: %w", err)
	}
	a.window = win

	glCtx, err := win.GLCreateContext()
	if err != nil {
		_ = win.Destroy()
		sdl.Quit()
		return nil, fmt.Errorf("gl context: %w", err)
	}
	a.glCtx = glCtx

	t0 := time.Now()
	pm, err := projectm.Create()
	if err != nil {
		sdl.GLDeleteContext(glCtx)
		_ = win.Destroy()
		sdl.Quit()
		return nil, fmt.Errorf("projectm init: %w", err)
	}
	a.pm = pm
	slog.Info("projectM init", "ms", time.Since(t0).Milliseconds())

	w, h := win.GLGetDrawableSize()

	gs, err := config.LoadSettings(a.settingsPath)
	if err != nil {
		slog.Warn("settings load", "error", err)
		gs = config.DefaultGraphics()
	}
	if renderNearestSet && renderNearest {
		gs.UpscaleFilter = config.FilterPixel
	}

	resolutions := config.ComputeResolutions(int(w), int(h))
	savedRes := config.RenderResolution{Width: gs.RenderWidth, Height: gs.RenderHeight}
	target := config.ClosestResolution(resolutions, savedRes)
	renderW, renderH := target.Width, target.Height
	if renderScaleExplicit {
		renderW, renderH = scaledDim(int(w), renderScale), scaledDim(int(h), renderScale)
	}
	gs.RenderWidth, gs.RenderHeight = renderW, renderH

	pm.SetWindowSize(renderW, renderH)
	rt := projectm.NewRenderTarget(renderW, renderH)
	rt.SetNearest(gs.UpscaleFilter.IsNearest())
	a.rt = rt
	a.gs = &gs

	slog.Info("render size", "window", fmt.Sprintf("%dx%d", int(w), int(h)),
		"internal", fmt.Sprintf("%dx%d", renderW, renderH), "scale", renderScale)

	if err := presets.Open(presetDirPath()); err != nil {
		slog.Warn("presets dir not found", "error", err)
	}
	a.presetNames = presets.Names()

	return a, nil
}

// Close releases all resources in reverse order.
func (a *App) Close() {
	if a.rt != nil {
		a.rt.Destroy()
	}
	if a.overlay != nil {
		a.overlay.Close()
	}
	if a.pl != nil {
		a.pl.Close()
	}
	if a.inp != nil {
		a.inp.Close()
	}
	if a.pm != nil {
		a.pm.Destroy()
	}
	if a.glCtx != nil {
		sdl.GLDeleteContext(a.glCtx)
	}
	if a.window != nil {
		_ = a.window.Destroy()
	}
	sdl.Quit()
}

// Init completes initialisation for normal mode: audio, overlay, library,
// input, preset, and the first track.
func (a *App) Init() {
	a.initAudio()
	a.initLibrary()
	a.initInput()
	a.initPreset()
	a.playFirst()
}

func (a *App) initAudio() {
	t := time.Now()
	pl, err := player.New()
	if err != nil {
		slog.Warn("audio unavailable, running without sound", "error", err)
		return
	}
	a.pl = pl
	a.overlay = ui.New()
	w, h := a.window.GLGetDrawableSize()
	a.overlay.SetScreenSize(int(w), int(h))
	slog.Info("audio init", "ms", time.Since(t).Milliseconds())
}

func (a *App) initLibrary() {
	musicDir := a.findMusicDir()
	t := time.Now()
	lib, err := player.NewLibrary(musicDir)
	if err != nil {
		slog.Warn("music scan failed", "error", err)
		return
	}
	a.lib = lib
	slog.Info("music scan", "albums", lib.AlbumCount(), "ms", time.Since(t).Milliseconds())
}

func (a *App) initInput() {
	a.inp = input.New()
}

func (a *App) initPreset() {
	if len(a.presetNames) == 0 {
		a.pm.LoadPresetData(string(presets.DefaultPreset()), false)
		slog.Warn("no external presets, using minimal built-in")
		return
	}
	// Pick a random non-transition (! prefix) preset for startup.
	var normals []string
	for _, n := range a.presetNames {
		if n[0] != '!' {
			normals = append(normals, n)
		}
	}
	if len(normals) == 0 {
		normals = a.presetNames
	}
	normIdx := rand.Intn(len(normals))
	for i, n := range a.presetNames {
		if n == normals[normIdx] {
			a.presetIdx = i
			break
		}
	}
	d, err := presets.Read(a.presetNames[a.presetIdx])
	if err != nil {
		slog.Warn("preset read error", "name", a.presetNames[a.presetIdx], "error", err)
		a.pm.LoadPresetData(string(presets.DefaultPreset()), false)
		return
	}
	a.pm.LoadPresetData(string(d), false)
	slog.Info("preset loaded", "name", a.presetNames[a.presetIdx], "count", len(a.presetNames))
}

func (a *App) playFirst() {
	if a.pl == nil || a.lib == nil {
		return
	}
	if first := a.lib.PlayCurrent(); first != "" {
		a.playTrack(first, a.lib.CurrentAlbum().Name)
	}
}

// baseDir returns the directory containing the running executable.
func baseDir() string {
	if binDir, err := os.Executable(); err == nil && binDir != "" {
		return filepath.Dir(binDir)
	}
	return "."
}

// findMusicDir locates the music directory relative to the binary.
func (a *App) findMusicDir() string {
	for _, candidate := range []string{baseDir() + "/music", baseDir() + "/test_data/music", baseDir()} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return baseDir()
}

// Run enters the main loop.  Must be called after Init().
func (a *App) Run() {
	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()

	lastFrame := time.Now()
	fpsBuf := make([]float64, 0, fpsWindow)
	lowFPSWarned := false

	var lastAlbumIdx = -1
	var prevW, prevH int

	for range ticker.C {
		now := time.Now()
		dt := now.Sub(lastFrame).Seconds()
		lastFrame = now

		// FPS sliding window.
		if dt > 0 {
			fpsBuf = append(fpsBuf, 1.0/dt)
			if len(fpsBuf) > fpsWindow {
				fpsBuf = fpsBuf[1:]
			}
		}
		var fpsAvg float64
		for _, v := range fpsBuf {
			fpsAvg += v
		}
		fpsAvg /= float64(len(fpsBuf))

		if len(fpsBuf) == fpsWindow {
			if fpsAvg < lowFPSThresh && !lowFPSWarned {
				presetName := ""
				if a.presetIdx >= 0 && a.presetIdx < len(a.presetNames) {
					presetName = a.presetNames[a.presetIdx]
				}
				slog.Warn("low fps", "fps", fpsAvg, "threshold", lowFPSThresh, "preset", presetName)
				lowFPSWarned = true
			} else if fpsAvg >= lowFPSThresh {
				lowFPSWarned = false
			}
		}

		w32, h32 := a.window.GLGetDrawableSize()
		w, h := int(w32), int(h32)
		winChanged := w != prevW || h != prevH

		if winChanged && prevW > 0 && prevH > 0 {
			if a.renderScaleExplicit {
				a.gs.RenderWidth = scaledDim(w, a.renderScale)
				a.gs.RenderHeight = scaledDim(h, a.renderScale)
			} else {
				resolutions := config.ComputeResolutions(w, h)
				saved := config.RenderResolution{Width: a.gs.RenderWidth, Height: a.gs.RenderHeight}
				target := config.ClosestResolution(resolutions, saved)
				a.gs.RenderWidth, a.gs.RenderHeight = target.Width, target.Height
			}

			if a.overlay != nil && a.overlay.IsSettingsPage() {
				rows := ui.BuildSettingsRows(*a.gs, w, h)
				a.overlay.SetSettingsRows(rows, a.overlay.SettingsCursor())
			}
		}
		prevW, prevH = w, h

		renderW, renderH := a.gs.RenderWidth, a.gs.RenderHeight
		if rw, rh := a.rt.Size(); rw != renderW || rh != renderH {
			a.rt.Resize(renderW, renderH)
			a.pm.SetWindowSize(renderW, renderH)
		}

		if a.overlay != nil {
			a.overlay.SetScreenSize(w, h)
		}

		// Push UI data each frame.
		if a.overlay != nil {
			a.overlay.SetFPS(fpsAvg)
			if a.pl != nil {
				a.overlay.SetPlayback(a.pl.Position(), a.pl.Duration(), a.pl.SampleRate(), a.pl.Channels(), a.pl.BPM(), a.pl.IsPaused())
			}
			if a.lib != nil {
				albumNames := make([]string, a.lib.AlbumCount())
				for i := 0; i < a.lib.AlbumCount(); i++ {
					albumNames[i] = a.lib.Albums[i].Name
				}
				a.overlay.SetAlbums(albumNames, a.lib.CurrentAlbumIndex())

				trackAlbumIdx := a.lib.CurrentAlbumIndex()
				if a.overlay.UIVisible() {
					trackAlbumIdx = a.overlay.AlbumCursor()
				}
				if trackAlbumIdx != lastAlbumIdx {
					trackCursor := 0
					if trackAlbumIdx == a.lib.CurrentAlbumIndex() {
						trackCursor = a.lib.CurrentTrackIndex()
					}
					a.overlay.SetTrackInfos(a.lib.GetAlbumTracks(trackAlbumIdx), trackCursor)
					lastAlbumIdx = trackAlbumIdx
				}

				if a.pl != nil {
					curTrack := a.pl.TrackPath()
					curAlbum := a.lib.CurrentAlbum().Name
					a.overlay.SetPlaying(curAlbum, curTrack)
				}
			}
		}

		// Process events.
		for e := sdl.PollEvent(); e != nil; e = sdl.PollEvent() {
			act := a.inp.ProcessEvent(e)
			if act == input.ActionQuit {
				return
			}
			if act == input.ActionRandomPreset {
				a.randPreset()
			} else {
				a.handleAction(act, w, h)
			}
		}

		// Complete pending transition (load target after delay).
		if a.pending.name != "" && now.After(a.pending.at) {
			if d, err := presets.Read(a.pending.name); err == nil {
				a.pm.LoadPresetData(string(d), true)
				for i, n := range a.presetNames {
					if n == a.pending.name {
						a.presetIdx = i
						break
					}
				}
				if a.overlay != nil {
					a.overlay.SetPresetName(a.pending.name)
				}
				slog.Info("preset", "name", a.pending.name)
			}
			a.pending = pendingPreset{}
		}

		// Feed wave data.
		if a.pl != nil {
			if wave := a.pl.GetWave(); wave != nil {
				a.pm.PCMAddFloat(wave, projectm.Mono)
			}
		}

		// Auto-advance track.
		if a.pl != nil && a.lib != nil && a.pl.Voice() != 0 && !a.pl.IsValidVoice() {
			if path := a.lib.TrackNext(); path != "" {
				a.playTrack(path, a.lib.CurrentAlbum().Name)
			}
		}

		a.pm.SetFPS(int32(fpsAvg))

		a.pm.RenderFrame()
		a.rt.Capture()
		a.rt.BlitToScreen(w, h)

		if a.overlay != nil {
			rw, rh := a.rt.Size()
			a.overlay.Update()
			a.overlay.Draw(w, h)
			a.pm.BindFeedbackFramebuffer()
			a.overlay.Inject(rw, rh)
		}

		a.window.GLSwap()
	}
}

// scaledDim scales a dimension by factor, clamping to sane bounds.
func scaledDim(v int, scale float64) int {
	if scale >= 1.0 {
		return v
	}
	if scale < 0.1 {
		scale = 0.1
	}
	d := int(float64(v) * scale)
	if d > v {
		d = v
	}
	return d
}

// presetDirPath returns the presets/ directory next to the running binary.
func presetDirPath() string {
	return baseDir() + "/presets"
}
