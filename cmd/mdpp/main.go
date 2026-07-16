package main

import (
	"bufio"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
	fpsWindow    = 5
	lowFPSThresh = 30.0
)

var (
	flagFullscreen    = flag.Bool("fullscreen", false, "fullscreen mode")
	flagWidth         = flag.Int("w", 1280, "window width")
	flagHeight        = flag.Int("h", 720, "window height")
	flagRenderScale   = flag.Float64("render-scale", 1.0, "internal visualizer render resolution scale (0.2-1.0); values below 1 render at lower resolution and upscale, cutting per-pixel shader cost roughly quadratically")
	flagRenderNearest = flag.Bool("render-nearest", false, "use nearest-neighbour filtering when upscaling the reduced visualizer image")
	flagBenchmark     = flag.Bool("benchmark", false, "benchmark every preset (renders -benchmark-frames frames each, no audio/UI) and write per-preset timings to -benchmark-out, then exit")
	flagBenchFrames   = flag.Int("benchmark-frames", 10, "frames to render per preset in -benchmark mode")
	flagBenchOut      = flag.String("benchmark-out", "benchmark.csv", "output CSV path for -benchmark mode")
	flagBenchWorker   = flag.Bool("benchmark-worker", false, "internal: run as a single benchmark worker subprocess reading preset names from stdin; spawned by -benchmark to isolate crashes to one preset")
)

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	flag.Parse()

	// Track which CLI flags were explicitly set (vs. default values)
	// so that `-render-scale=1.0` correctly overrides settings.json.
	var (
		renderScaleExplicit bool
		renderNearestSet    bool
	)
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "render-scale":
			renderScaleExplicit = true
		case "render-nearest":
			renderNearestSet = true
		}
	})

	// Supervisor mode: no SDL/GL/window needed here. Each preset is
	// benchmarked in an isolated worker subprocess (re-exec of ourselves
	// with -benchmark-worker) so a preset that segfaults libprojectM/the
	// GPU driver only takes down that one subprocess, not the whole run.
	if *flagBenchmark && !*flagBenchWorker {
		if err := presets.Open(presetDirPath()); err != nil {
			slog.Warn("presets dir not found", "error", err)
		}
		runBenchmarkSupervisor(presets.Names(), *flagBenchFrames, *flagBenchOut)
		return
	}

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

	// Load graphics settings (settings.json > defaults).
	gs, err := config.LoadSettings(config.SettingsPath())
	if err != nil {
		slog.Warn("settings load", "error", err)
		gs = config.DefaultGraphics()
	}
	// CLI flags override settings.json.
	if renderNearestSet && *flagRenderNearest {
		gs.UpscaleFilter = config.FilterPixel
	}
	// Compute initial render resolution.
	resolutions := config.ComputeResolutions(int(w), int(h))
	savedRes := config.RenderResolution{Width: gs.RenderWidth, Height: gs.RenderHeight}
	target := config.ClosestResolution(resolutions, savedRes)
	renderW, renderH := target.Width, target.Height
	if renderScaleExplicit {
		renderW, renderH = scaledDim(int(w), *flagRenderScale), scaledDim(int(h), *flagRenderScale)
	}
	gs.RenderWidth, gs.RenderHeight = renderW, renderH

	pm.SetWindowSize(renderW, renderH)
	rt := projectm.NewRenderTarget(renderW, renderH)
	rt.SetNearest(gs.UpscaleFilter.IsNearest())
	defer rt.Destroy()
	slog.Info("render size", "window", fmt.Sprintf("%dx%d", int(w), int(h)), "internal", fmt.Sprintf("%dx%d", renderW, renderH), "scale", *flagRenderScale)

	// Find base dir (where binary lives — music relative to this).
	binDir, _ := os.Executable()
	baseDir := "."
	if binDir != "" {
		baseDir = filepath.Dir(binDir)
	}

	// Load presets from presets/ dir (.mdp archive + user .milk files).
	presetDir := presetDirPath()
	if err := presets.Open(presetDir); err != nil {
		slog.Warn("presets dir not found", "error", err)
	}
	presetNames := presets.Names()
	presetIdx := 0

	if *flagBenchWorker {
		runBenchmarkWorker(pm, rt, window, *flagBenchFrames)
		return
	}

	t1 := time.Now()
	if len(presetNames) > 0 {
		// DEBUG: force specific heavy preset.
		dbgTarget := "men in reverse in reverse time rhyming and stealing roam3"
		for i, n := range presetNames {
			if strings.Contains(n, dbgTarget) {
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

	appLoop(window, pm, p, overlay, inp, lib, presetNames, &presetIdx, rt, &gs, config.SettingsPath())
}

// scaledDim scales a dimension by factor, clamping the factor to a sane
// minimum and the result to the original value (scale > 1 would upsample
// rendering for no benefit).
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
	binDir, _ := os.Executable()
	baseDir := "."
	if binDir != "" {
		baseDir = filepath.Dir(binDir)
	}
	return baseDir + "/presets"
}

// benchmarkWave returns a synthetic "loud music" signal so audio-reactive
// per-frame/per-pixel branches (bass/mid/treb thresholds) exercise their
// typical code path, instead of the near-zero-cost path a silent signal
// would take.
func benchmarkWave() []float32 {
	const sampleCount = 2048
	wave := make([]float32, sampleCount)
	for i := range wave {
		t := float64(i) / 44100.0
		wave[i] = float32(0.6*math.Sin(2*math.Pi*110*t) +
			0.3*math.Sin(2*math.Pi*880*t) +
			0.2*math.Sin(2*math.Pi*4000*t))
	}
	return wave
}

// runBenchmarkWorker is the child process spawned by runBenchmarkSupervisor
// for each preset (or batch of presets, until it crashes). It reads one
// preset name per line from stdin, renders -benchmark-frames frames of it,
// and writes a single result line to a dedicated result pipe (fd 3, set up
// by the supervisor via exec.Cmd.ExtraFiles):
//
//	OK <compile_ms> <steady_ms_per_frame> <steady_fps> <total_ms>
//	ERR <message>          (preset failed to load/read; not a crash)
//
// The result protocol deliberately does NOT use stdout: SDL/the console
// backend (KMSDRM/fbdev) can write raw terminal escape codes (cursor
// hide/show, etc.) to stdout, which would corrupt the protocol and make
// the supervisor mistake a perfectly fine preset for a crash.
//
// If this process segfaults (e.g. a bug in libprojectM/the GPU driver
// triggered by a malformed preset), the result pipe simply closes — the
// supervisor detects that and isolates the blame to the preset it just sent.
func runBenchmarkWorker(pm *projectm.Handle, rt *projectm.RenderTarget, window *sdl.Window, frames int) {
	if frames < 2 {
		frames = 2
	}
	wave := benchmarkWave()
	w, h := window.GLGetDrawableSize()

	resultFile := os.NewFile(uintptr(3), "bench-result")
	if resultFile == nil {
		slog.Error("benchmark worker: no result pipe (fd 3) — must be spawned by -benchmark supervisor")
		return
	}
	out := bufio.NewWriter(resultFile)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		name := scanner.Text()
		if name == "" {
			continue
		}

		for e := sdl.PollEvent(); e != nil; e = sdl.PollEvent() {
			if _, ok := e.(*sdl.QuitEvent); ok {
				return
			}
		}

		data, err := presets.Read(name)
		if err != nil {
			if _, werr := fmt.Fprintf(out, "ERR %v\n", err); werr != nil {
				return
			}
			if err := out.Flush(); err != nil {
				return
			}
			continue
		}

		start := time.Now()
		pm.LoadPresetData(string(data), false)

		var compileMs float64
		var steadyTotal time.Duration

		for frame := 0; frame < frames; frame++ {
			pm.PCMAddFloat(wave, projectm.Mono)
			t0 := time.Now()
			pm.RenderFrame()
			rt.Capture()
			rt.BlitToScreen(int(w), int(h))
			window.GLSwap()
			elapsed := time.Since(t0)
			if frame == 0 {
				// First frame includes preset switch + warp/composite shader
				// compilation, reported separately since it's a one-shot cost.
				compileMs = elapsed.Seconds() * 1000
			} else {
				steadyTotal += elapsed
			}
		}

		totalMs := time.Since(start).Seconds() * 1000
		steadyFrames := frames - 1
		var steadyMs, steadyFPS float64
		if steadyFrames > 0 {
			steadyMs = steadyTotal.Seconds() * 1000 / float64(steadyFrames)
			if steadyMs > 0 {
				steadyFPS = 1000 / steadyMs
			}
		}

		if _, err := fmt.Fprintf(out, "OK %.2f %.2f %.2f %.2f\n", compileMs, steadyMs, steadyFPS, totalMs); err != nil {
			return
		}
		if err := out.Flush(); err != nil {
			return
		}
	}
}

type benchResult struct {
	compileMs, steadyMs, steadyFPS, totalMs float64
	errText                                 string
}

func parseBenchLine(line string) (benchResult, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return benchResult{}, fmt.Errorf("empty response")
	}
	switch fields[0] {
	case "OK":
		if len(fields) != 5 {
			return benchResult{}, fmt.Errorf("malformed OK line: %q", line)
		}
		compileMs, _ := strconv.ParseFloat(fields[1], 64)
		steadyMs, _ := strconv.ParseFloat(fields[2], 64)
		steadyFPS, _ := strconv.ParseFloat(fields[3], 64)
		totalMs, _ := strconv.ParseFloat(fields[4], 64)
		return benchResult{compileMs: compileMs, steadyMs: steadyMs, steadyFPS: steadyFPS, totalMs: totalMs}, nil
	case "ERR":
		return benchResult{errText: strings.Join(fields[1:], " ")}, nil
	default:
		return benchResult{}, fmt.Errorf("unrecognized response: %q", line)
	}
}

// benchWorker manages one worker subprocess.
type benchWorker struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines chan string
	done  chan error
}

func startBenchWorker(frames int) (*benchWorker, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	args := append([]string{}, os.Args[1:]...)
	args = append(args, "-benchmark-worker", fmt.Sprintf("-benchmark-frames=%d", frames))
	cmd := exec.Command(exe, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	// Dedicated result pipe (fd 3 in the child) instead of stdout: SDL/the
	// console backend can write raw terminal escape codes to stdout, which
	// would otherwise corrupt our line-based protocol.
	resultRead, resultWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.ExtraFiles = []*os.File{resultWrite}

	if err := cmd.Start(); err != nil {
		_ = resultRead.Close()
		_ = resultWrite.Close()
		return nil, err
	}
	// The child has its own copy of the write end; close ours so reads on
	// resultRead see EOF when the child exits/crashes.
	_ = resultWrite.Close()

	lines := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(resultRead)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	doneCh := make(chan error, 1)
	go func() { doneCh <- cmd.Wait() }()

	return &benchWorker{cmd: cmd, stdin: stdin, lines: lines, done: doneCh}, nil
}

// run sends one preset name to the worker and waits for its result line,
// a crash (stdout closed / process exited), or a timeout (hang).
func (bw *benchWorker) run(name string, timeout time.Duration) (benchResult, error) {
	if _, err := io.WriteString(bw.stdin, name+"\n"); err != nil {
		return benchResult{}, fmt.Errorf("write to worker: %w", err)
	}
	select {
	case line, ok := <-bw.lines:
		if !ok {
			werr := <-bw.done
			return benchResult{}, fmt.Errorf("worker exited without a result (crash?): %v", werr)
		}
		return parseBenchLine(line)
	case werr := <-bw.done:
		return benchResult{}, fmt.Errorf("worker exited unexpectedly: %v", werr)
	case <-time.After(timeout):
		return benchResult{}, fmt.Errorf("timed out after %s (hang?)", timeout)
	}
}

func (bw *benchWorker) kill() {
	if bw.cmd.Process != nil {
		_ = bw.cmd.Process.Kill()
	}
}

func (bw *benchWorker) close() {
	_ = bw.stdin.Close()
	select {
	case <-bw.done:
	case <-time.After(3 * time.Second):
		bw.kill()
	}
}

// runBenchmarkSupervisor benchmarks every preset in turn, each rendered in a
// worker subprocess. Frame count (not wall-clock time) is used per preset so
// cheap presets are measured quickly and expensive ones naturally take
// longer. If a preset crashes or hangs the worker, that single preset is
// logged as "crashed"/"timeout" in the CSV and a fresh worker subprocess
// continues with the rest — one bad preset can't take down the whole run.
//
// If outPath already contains results (e.g. from a run interrupted by an
// overheating device shutdown), presets already listed there (including
// ones marked crashed) are skipped, so the benchmark can be resumed by
// simply re-running the same command.
func runBenchmarkSupervisor(presetNames []string, frames int, outPath string) {
	if frames < 2 {
		frames = 2
	}

	done := readBenchmarkedPresets(outPath)
	if len(done) > 0 {
		slog.Info("benchmark resume", "already_done", len(done), "out", outPath)
	}

	needHeader := true
	if fi, err := os.Stat(outPath); err == nil && fi.Size() > 0 {
		needHeader = false
	}

	f, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		slog.Error("benchmark: open output", "path", outPath, "error", err)
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			slog.Warn("benchmark: close output", "path", outPath, "error", err)
		}
	}()

	cw := csv.NewWriter(f)
	if needHeader {
		_ = cw.Write([]string{"preset", "status", "compile_ms", "steady_ms_per_frame", "steady_fps", "total_ms"})
		cw.Flush()
	}

	slog.Info("benchmark start", "presets", len(presetNames), "frames_per_preset", frames, "out", outPath)

	var worker *benchWorker
	tested := 0
	for i, name := range presetNames {
		if done[name] {
			continue
		}

		if worker == nil {
			w, err := startBenchWorker(frames)
			if err != nil {
				slog.Error("benchmark: start worker", "error", err)
				return
			}
			worker = w
		}

		slog.Info("benchmark testing", "index", i+1, "total", len(presetNames), "preset", name)

		result, runErr := worker.run(name, 30*time.Second)

		var status string
		var compileMs, steadyMs, steadyFPS, totalMs float64
		switch {
		case runErr != nil:
			status = "crashed"
			slog.Warn("benchmark preset crashed/hung — marking and isolating", "preset", name, "error", runErr)
			worker.kill()
			worker = nil
		case result.errText != "":
			status = "error"
			slog.Warn("benchmark preset read error", "preset", name, "error", result.errText)
		default:
			status = "ok"
			compileMs, steadyMs, steadyFPS, totalMs = result.compileMs, result.steadyMs, result.steadyFPS, result.totalMs
			slog.Info("benchmark result", "preset", name, "steady_fps", fmt.Sprintf("%.1f", steadyFPS))
		}

		_ = cw.Write([]string{
			name, status,
			fmt.Sprintf("%.2f", compileMs),
			fmt.Sprintf("%.2f", steadyMs),
			fmt.Sprintf("%.2f", steadyFPS),
			fmt.Sprintf("%.2f", totalMs),
		})

		tested++
		if tested%20 == 0 {
			cw.Flush()
			slog.Info("benchmark progress", "done", i+1, "total", len(presetNames))
		}
	}

	if worker != nil {
		worker.close()
	}
	cw.Flush()
	slog.Info("benchmark complete", "out", outPath)
}

// readBenchmarkedPresets reads preset names already recorded in an existing
// benchmark CSV (any status, including "crashed"), so a re-run can skip
// them. Returns an empty (non-nil) map if the file doesn't exist or can't
// be parsed.
func readBenchmarkedPresets(outPath string) map[string]bool {
	done := make(map[string]bool)
	f, err := os.Open(outPath)
	if err != nil {
		return done
	}
	defer func() {
		if err := f.Close(); err != nil {
			slog.Debug("readBenchmarkedPresets: close", "path", outPath, "error", err)
		}
	}()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil || len(records) == 0 {
		return done
	}
	// Skip header row.
	for _, rec := range records[1:] {
		if len(rec) > 0 {
			done[rec[0]] = true
		}
	}
	return done
}

type pendingPreset struct {
	name string
	at   time.Time
}

func appLoop(window *sdl.Window, pm *projectm.Handle, pl *player.Player, overlay *ui.Overlay, inp *input.Input, lib *player.Library, presetNames []string, presetIdx *int, rt *projectm.RenderTarget, gs *config.GraphicsSettings, settingsPath string) {
	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()

	lastFrame := time.Now()
	fpsBuf := make([]float64, 0, fpsWindow)
	lowFPSWarned := false

	var lastAlbumIdx = -1
	var pending pendingPreset
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

		// Warn once per low-FPS episode; reset once FPS recovers so a later
		// preset that also struggles logs again.
		if len(fpsBuf) == fpsWindow {
			if fpsAvg < lowFPSThresh && !lowFPSWarned {
				presetName := ""
				if *presetIdx >= 0 && *presetIdx < len(presetNames) {
					presetName = presetNames[*presetIdx]
				}
				slog.Warn("low fps", "fps", fpsAvg, "threshold", lowFPSThresh, "preset", presetName)
				lowFPSWarned = true
			} else if fpsAvg >= lowFPSThresh {
				lowFPSWarned = false
			}
		}

		w32, h32 := window.GLGetDrawableSize()
		w, h := int(w32), int(h32)
		winChanged := w != prevW || h != prevH

		// On window resize, adapt render resolution: recompute available
		// candidates and pick the closest saved resolution by area. This
		// preserves the user's intended resolution as closely as possible
		// without tying it to a transient scale factor. If the saved
		// resolution is still valid it is kept unchanged.
		if winChanged && prevW > 0 && prevH > 0 {
			resolutions := config.ComputeResolutions(w, h)
			saved := config.RenderResolution{Width: gs.RenderWidth, Height: gs.RenderHeight}
			target := config.ClosestResolution(resolutions, saved)
			gs.RenderWidth, gs.RenderHeight = target.Width, target.Height

			if overlay != nil && overlay.IsSettingsPage() {
				rows := ui.BuildSettingsRows(*gs, w, h)
				overlay.SetSettingsRows(rows, overlay.SettingsCursor())
			}
		}
		prevW, prevH = w, h

		// Resize the internal render target if the window size or settings
		// resolution changed, and let projectM know its new viewport.
		renderW, renderH := gs.RenderWidth, gs.RenderHeight
		if rw, rh := rt.Size(); rw != renderW || rh != renderH {
			rt.Resize(renderW, renderH)
			pm.SetWindowSize(renderW, renderH)
		}

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
				randPreset(pm, overlay, presetNames, presetIdx, &pending)
			} else {
				handleAction(act, pm, pl, overlay, lib, presetNames, presetIdx, gs, settingsPath, int(w), int(h), rt)
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
				if overlay != nil {
					overlay.SetPresetName(pending.name)
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

		// Feed real FPS to presets so time-dependent effects scale correctly.
		pm.SetFPS(int32(fpsAvg))

		// projectM always renders its final composite into the real screen
		// framebuffer, sized to whatever SetWindowSize was given (the
		// bottom-left renderW x renderH corner here). Capture that reduced
		// image and upscale it to fill the window — this is what cuts
		// per-pixel shader cost for heavy presets.
		pm.RenderFrame()
		rt.Capture()
		rt.BlitToScreen(int(w), int(h))

		if overlay != nil {
			rw, rh := rt.Size()
			overlay.Update()
			overlay.Draw(int(w), int(h))
			pm.BindFeedbackFramebuffer()
			overlay.Inject(rw, rh)
		}

		window.GLSwap()
	}
}

// randPreset picks a random non-transition preset, transitions through a random
// "!" preset, and loads the target after ~1.5s.
func randPreset(pm *projectm.Handle, overlay *ui.Overlay, presetNames []string, presetIdx *int, pending *pendingPreset) {
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
		if overlay != nil {
			overlay.SetPresetName(target)
		}
		slog.Info("preset", "name", target)
	}
}

// handleAction dispatches an input action.
// When UI is visible: actions navigate the overlay UI.
// When UI is hidden: actions control playback directly (cursor→album, focus→track).
func handleAction(act input.Action, pm *projectm.Handle, pl *player.Player, overlay *ui.Overlay, lib *player.Library, presetNames []string, presetIdx *int, gs *config.GraphicsSettings, settingsPath string, winW, winH int, rt *projectm.RenderTarget) {
	if overlay != nil && overlay.UIVisible() {
		handleUIAction(act, pm, pl, overlay, lib, presetNames, presetIdx, gs, settingsPath, winW, winH, rt)
	} else {
		handleNormalAction(act, pm, pl, overlay, lib, presetNames, presetIdx)
	}
}

func handleUIAction(act input.Action, pm *projectm.Handle, pl *player.Player, overlay *ui.Overlay, lib *player.Library, presetNames []string, presetIdx *int, gs *config.GraphicsSettings, settingsPath string, winW, winH int, rt *projectm.RenderTarget) {
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
		if overlay.IsSettingsPage() {
			if overlay.Select() {
				// A value was confirmed — read and apply.
				applySettings(overlay, gs, settingsPath, winW, winH, pm, rt)
			}
		} else if overlay.Select() && lib != nil && pl != nil {
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
		// When settings page first opens, populate its rows.
		if overlay.IsSettingsPage() && !overlay.IsSettingsEditing() && !overlay.SelectEntered() {
			rows := ui.BuildSettingsRows(*gs, winW, winH)
			overlay.SetSettingsRows(rows, 0)
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
			overlay.SetPresetName(presetNames[*presetIdx])
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
			overlay.SetPresetName(presetNames[*presetIdx])
			slog.Info("preset", "name", presetNames[*presetIdx])
		}
	}
}

// applySettings reads the confirmed settings rows and applies changes.
func applySettings(overlay *ui.Overlay, gs *config.GraphicsSettings, settingsPath string, winW, winH int, pm *projectm.Handle, rt *projectm.RenderTarget) {
	rows := overlay.SettingsRows()
	if len(rows) < 2 {
		return
	}

	// Row 0: Render resolution.
	resValues := rows[0].Values
	resIndex := rows[0].Index
	if resIndex >= 0 && resIndex < len(resValues) {
		resolutions := config.ComputeResolutions(winW, winH)
		if resIndex < len(resolutions) {
			r := resolutions[resIndex]
			gs.RenderWidth = r.Width
			gs.RenderHeight = r.Height
			rt.Resize(r.Width, r.Height)
			pm.SetWindowSize(r.Width, r.Height)
		}
	}

	// Row 1: Upscale filter.
	filterValues := rows[1].Values
	filterIndex := rows[1].Index
	if filterIndex >= 0 && filterIndex < len(filterValues) {
		filters := config.AllFilters()
		if filterIndex < len(filters) {
			gs.UpscaleFilter = filters[filterIndex]
			rt.SetNearest(gs.UpscaleFilter.IsNearest())
		}
	}

	// Persist.
	if err := config.SaveSettings(settingsPath, *gs); err != nil {
		slog.Warn("settings save", "error", err)
	} else {
		slog.Debug("settings saved", "path", settingsPath)
	}

	// Refresh rows so the UI shows the newly saved state.
	rows = ui.BuildSettingsRows(*gs, winW, winH)
	overlay.SetSettingsRows(rows, overlay.SettingsCursor())
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
			if overlay != nil {
				overlay.SetPresetName(presetNames[*presetIdx])
			}
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
			if overlay != nil {
				overlay.SetPresetName(presetNames[*presetIdx])
			}
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
