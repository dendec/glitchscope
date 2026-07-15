package main

import (
	"flag"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/dendec/mdpp/internal/input"
	"github.com/dendec/mdpp/internal/player"
	"github.com/dendec/mdpp/internal/presets"
	"github.com/dendec/mdpp/internal/projectm"
	"github.com/dendec/mdpp/internal/ui"
	"github.com/veandco/go-sdl2/sdl"
)

const fpsWindow = 60

var (
	flagFullscreen = flag.Bool("fullscreen", false, "fullscreen mode")
	flagWidth      = flag.Int("w", 1280, "window width")
	flagHeight     = flag.Int("h", 720, "window height")
)

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	flag.Parse()

	if err := sdl.Init(sdl.INIT_VIDEO | sdl.INIT_EVENTS | sdl.INIT_GAMECONTROLLER | sdl.INIT_JOYSTICK); err != nil {
		slog.Error("sdl init", "error", err)
		os.Exit(1)
	}
	defer sdl.Quit()

	winFlags := uint32(sdl.WINDOW_OPENGL | sdl.WINDOW_SHOWN | sdl.WINDOW_RESIZABLE)
	if *flagFullscreen {
		winFlags |= sdl.WINDOW_FULLSCREEN_DESKTOP
	}
	window, err := sdl.CreateWindow("MDPP — MilkDrop Portable Player",
		sdl.WINDOWPOS_UNDEFINED, sdl.WINDOWPOS_UNDEFINED,
		int32(*flagWidth), int32(*flagHeight),
		winFlags)
	if err != nil {
		slog.Error("window", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := window.Destroy(); err != nil {
			slog.Debug("window destroy", "error", err)
		}
	}()

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

	// Find base dir (where binary lives — music relative to this).
	binDir, _ := os.Executable()
	baseDir := "."
	if binDir != "" {
		baseDir = filepath.Dir(binDir)
	}

	// Load presets from presets/ dir (.mdp archive + user .milk files).
	presetDir := baseDir + "/presets"
	if err := presets.Open(presetDir); err != nil {
		slog.Warn("presets dir not found", "error", err)
	}
	presetNames := presets.Names()
	presetIdx := 0
	t1 := time.Now()
	if len(presetNames) > 0 {
		// Build list of non-transition presets for startup.
		var normals []string
		for _, n := range presetNames {
			if n[0] != '!' {
				normals = append(normals, n)
			}
		}
		if len(normals) == 0 {
			normals = presetNames
		}
		// Random initial preset.
		normIdx := rand.Intn(len(normals))
		for i, n := range presetNames {
			if n == normals[normIdx] {
				presetIdx = i
				break
			}
		}
		if d, err := presets.Read(presetNames[presetIdx]); err == nil {
			pm.LoadPresetData(string(d), false)
			slog.Info("preset loaded", "name", presetNames[presetIdx], "count", len(presetNames))
		} else {
			slog.Warn("preset read error", "name", presetNames[presetIdx], "error", err)
		}
	} else {
		pm.LoadPresetData(string(presets.DefaultPreset()), false)
		slog.Warn("no external presets, using minimal built-in")
	}
	slog.Info("preset load", "ms", time.Since(t1).Milliseconds())
	slog.Info("keys", "nav", "arrows", "select", "enter", "back", "bsp", "next-preset", "n", "prev-preset", "p", "random", "r", "toggle-ui", "tab", "play/pause", "space")

	// Audio player (non-fatal).
	t2 := time.Now()
	var p *player.Player
	var overlay *ui.Overlay
	if pl, err := player.New(); err != nil {
		slog.Warn("audio unavailable, running without sound", "error", err)
	} else {
		p = pl
		overlay = ui.New()
		w, h := window.GLGetDrawableSize()
		overlay.SetScreenSize(int(w), int(h))
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

	appLoop(window, pm, p, overlay, inp, lib, presetNames, &presetIdx)
}

type pendingPreset struct {
	name string
	at   time.Time
}

func appLoop(window *sdl.Window, pm *projectm.Handle, pl *player.Player, overlay *ui.Overlay, inp *input.Input, lib *player.Library, presetNames []string, presetIdx *int) {
	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()

	lastFrame := time.Now()
	fpsBuf := make([]float64, 0, fpsWindow)

	var lastAlbumIdx = -1
	var pending pendingPreset

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

		w, h := window.GLGetDrawableSize()

		if overlay != nil {
			overlay.SetScreenSize(int(w), int(h))
		}

		// Push playback data to overlay each frame.
		if overlay != nil {
			overlay.SetFPS(fpsAvg)
			if pl != nil {
				overlay.SetPlayback(pl.Position(), pl.Duration(), pl.SampleRate(), pl.Channels(), pl.BPM(), pl.IsPaused())
			}
			if lib != nil {
				albumNames := make([]string, lib.AlbumCount())
				for i := 0; i < lib.AlbumCount(); i++ {
					albumNames[i] = lib.Albums[i].Name
				}
				overlay.SetAlbums(albumNames, lib.CurrentAlbumIndex())

				// In UI mode, show tracks for the album under the UI cursor;
				// otherwise show the album currently playing.
				trackAlbumIdx := lib.CurrentAlbumIndex()
				if overlay.UIVisible() {
					trackAlbumIdx = overlay.AlbumCursor()
				}
				if trackAlbumIdx != lastAlbumIdx {
					trackCursor := 0
					if trackAlbumIdx == lib.CurrentAlbumIndex() {
						trackCursor = lib.CurrentTrackIndex()
					}
					overlay.SetTrackInfos(lib.GetAlbumTracks(trackAlbumIdx), trackCursor)
					lastAlbumIdx = trackAlbumIdx
				}

				if pl != nil {
					curTrack := pl.TrackPath()
					curAlbum := lib.CurrentAlbum().Name
					overlay.SetPlaying(curAlbum, curTrack)
				}
			}
		}

		// Process events → dispatch actions.
		for e := sdl.PollEvent(); e != nil; e = sdl.PollEvent() {
			act := inp.ProcessEvent(e)
			if act == input.ActionQuit {
				return
			}
			if act == input.ActionRandomPreset {
				randPreset(pm, presetNames, presetIdx, &pending)
			} else {
				handleAction(act, pm, pl, overlay, lib, presetNames, presetIdx)
			}
		}

		// Complete pending transition: load target preset.
		if pending.name != "" && !now.Before(pending.at) {
			if d, err := presets.Read(pending.name); err == nil {
				pm.LoadPresetData(string(d), true)
				for i, n := range presetNames {
					if n == pending.name {
						*presetIdx = i
						break
					}
				}
				slog.Info("preset", "name", pending.name)
			}
			pending.name = ""
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

// randPreset picks a random non-transition preset, transitions through a random
// "!" preset, and loads the target after ~1.5s.
func randPreset(pm *projectm.Handle, presetNames []string, presetIdx *int, pending *pendingPreset) {
	if len(presetNames) == 0 {
		return
	}

	// Filter: non-transition and transition presets.
	var normal, trans []string
	for _, n := range presetNames {
		if n[0] == '!' {
			trans = append(trans, n)
		} else {
			normal = append(normal, n)
		}
	}
	if len(normal) == 0 {
		normal = presetNames // fallback: include all
	}

	// Pick target different from current.
	target := normal[rand.Intn(len(normal))]
	if len(normal) > 1 {
		for target == presetNames[*presetIdx] {
			target = normal[rand.Intn(len(normal))]
		}
	}

	// Load transition if available, then schedule target.
	if len(trans) > 0 {
		t := trans[rand.Intn(len(trans))]
		if d, err := presets.Read(t); err == nil {
			pm.LoadPresetData(string(d), false)
			slog.Info("transition", "name", t)
		}
		pending.name = target
		return
	}

	// No transition presets: load target directly with smooth transition.
	d, err := presets.Read(target)
	if err == nil {
		pm.LoadPresetData(string(d), true)
		for i, n := range presetNames {
			if n == target {
				*presetIdx = i
				break
			}
		}
		slog.Info("preset", "name", target)
	}
}

// handleAction dispatches an input action.
// When UI is visible: actions navigate the overlay UI.
// When UI is hidden: actions control playback directly (cursor→album, focus→track).
func handleAction(act input.Action, pm *projectm.Handle, pl *player.Player, overlay *ui.Overlay, lib *player.Library, presetNames []string, presetIdx *int) {
	if overlay != nil && overlay.UIVisible() {
		handleUIAction(act, pm, pl, overlay, lib, presetNames, presetIdx)
	} else {
		handleNormalAction(act, pm, pl, overlay, lib, presetNames, presetIdx)
	}
}

func handleUIAction(act input.Action, pm *projectm.Handle, pl *player.Player, overlay *ui.Overlay, lib *player.Library, presetNames []string, presetIdx *int) {
	switch act {
	case input.ActionQuit:
		return

	case input.ActionPlayPause:
		if pl != nil {
			pl.TogglePause()
		}

	case input.ActionToggleUI:
		overlay.ToggleUI()

	case input.ActionCursorUp:
		overlay.CursorUp()

	case input.ActionCursorDown:
		overlay.CursorDown()

	case input.ActionFocusLeft:
		overlay.FocusLeft()

	case input.ActionFocusRight:
		overlay.FocusRight()

	case input.ActionSelect:
		if overlay.Select() && lib != nil && pl != nil {
			if overlay.FocusPanel() == 0 {
				if path := lib.SelectAlbum(overlay.AlbumCursor()); path != "" {
					playTrack(pl, overlay, path, lib.CurrentAlbum().Name)
				}
			} else {
				lib.SelectAlbum(overlay.AlbumCursor())
				if path := lib.SelectTrack(overlay.TrackCursor()); path != "" {
					playTrack(pl, overlay, path, lib.CurrentAlbum().Name)
				}
			}
		}

	case input.ActionBack:
		overlay.Back()

	case input.ActionToggleOverlay:
		if overlay != nil {
			overlay.ToggleVisibility()
		}

	case input.ActionNextPreset:
		if len(presetNames) == 0 {
			return
		}
		*presetIdx = (*presetIdx + 1) % len(presetNames)
		d, err := presets.Read(presetNames[*presetIdx])
		if err == nil {
			pm.LoadPresetData(string(d), true)
			slog.Info("preset", "name", presetNames[*presetIdx])
		}

	case input.ActionPrevPreset:
		if len(presetNames) == 0 {
			return
		}
		*presetIdx = (*presetIdx - 1 + len(presetNames)) % len(presetNames)
		d, err := presets.Read(presetNames[*presetIdx])
		if err == nil {
			pm.LoadPresetData(string(d), true)
			slog.Info("preset", "name", presetNames[*presetIdx])
		}
	}
}

func handleNormalAction(act input.Action, pm *projectm.Handle, pl *player.Player, overlay *ui.Overlay, lib *player.Library, presetNames []string, presetIdx *int) {
	// Map gamepad cursor/focus → album/track when UI is hidden.
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

	case input.ActionToggleUI:
		if overlay != nil {
			overlay.ToggleUI()
		}

	case input.ActionSelect:
		if overlay != nil {
			overlay.ToggleUI()
		}

	case input.ActionBack:
		// ignored in normal mode

	case input.ActionCursorUp, input.ActionPrevAlbum:
		if lib == nil || pl == nil {
			return
		}
		if path := lib.AlbumPrev(); path != "" {
			playTrack(pl, overlay, path, lib.CurrentAlbum().Name)
		}

	case input.ActionCursorDown, input.ActionNextAlbum:
		if lib == nil || pl == nil {
			return
		}
		if path := lib.AlbumNext(); path != "" {
			playTrack(pl, overlay, path, lib.CurrentAlbum().Name)
		}

	case input.ActionFocusLeft, input.ActionPrevTrack:
		if lib == nil || pl == nil {
			return
		}
		if path := lib.TrackPrev(); path != "" {
			playTrack(pl, overlay, path, lib.CurrentAlbum().Name)
		}

	case input.ActionFocusRight, input.ActionNextTrack:
		if lib == nil || pl == nil {
			return
		}
		if path := lib.TrackNext(); path != "" {
			playTrack(pl, overlay, path, lib.CurrentAlbum().Name)
		}

	case input.ActionNextPreset:
		if len(presetNames) == 0 {
			return
		}
		*presetIdx = (*presetIdx + 1) % len(presetNames)
		d, err := presets.Read(presetNames[*presetIdx])
		if err == nil {
			pm.LoadPresetData(string(d), true)
			slog.Info("preset", "name", presetNames[*presetIdx])
		}

	case input.ActionPrevPreset:
		if len(presetNames) == 0 {
			return
		}
		*presetIdx = (*presetIdx - 1 + len(presetNames)) % len(presetNames)
		d, err := presets.Read(presetNames[*presetIdx])
		if err == nil {
			pm.LoadPresetData(string(d), true)
			slog.Info("preset", "name", presetNames[*presetIdx])
		}
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
