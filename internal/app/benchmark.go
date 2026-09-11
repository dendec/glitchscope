package app

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dendec/glitchscope/internal/presets"
	"github.com/dendec/glitchscope/internal/prof"
	"github.com/dendec/glitchscope/internal/projectm"
	"github.com/veandco/go-sdl2/sdl"
)

var benchmarkHeader = []string{"preset", "status", "startup_ms", "steady_ms_per_frame", "steady_fps", "total_ms", "load_ms", "p95_ms", "p99_ms", "max_ms", "process_peak_rss_kib"}

// RunBenchmarkSupervisor benchmarks every preset in an isolated subprocess.
func RunBenchmarkSupervisor(frames int, outPath string) {
	if frames < 2 {
		frames = 2
	}

	if err := presets.Open(presetDirPath()); err != nil {
		slog.Warn("presets dir not found", "error", err)
	}
	presetNames := presets.Names()

	done := readBenchmarkedPresets(outPath)
	if len(done) > 0 {
		slog.Info("benchmark resume", "already_done", len(done), "out", outPath)
	}

	needHeader := true
	if fi, err := os.Stat(outPath); err == nil && fi.Size() > 0 {
		existing, openErr := os.Open(outPath)
		if openErr != nil {
			slog.Error("benchmark report open", "error", openErr)
			return
		}
		header, readErr := csv.NewReader(existing).Read()
		_ = existing.Close()
		if readErr != nil || !slices.Equal(header, benchmarkHeader) {
			slog.Error("benchmark report schema differs; select a new -benchmark-out path", "path", outPath)
			return
		}
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
		_ = cw.Write(benchmarkHeader)
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
			fmt.Sprintf("%.2f", result.loadMs),
			fmt.Sprintf("%.2f", result.p95Ms),
			fmt.Sprintf("%.2f", result.p99Ms),
			fmt.Sprintf("%.2f", result.maxMs),
			fmt.Sprintf("%.0f", result.peakRSS),
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

// RunBenchmarkWorker runs as a child process inside the benchmark supervisor.
func (a *App) RunBenchmarkWorker(frames int) {
	runBenchmarkWorker(a.pm, a.rt, a.window, frames)
}

// ---------------------------------------------------------------------------
// internal
// ---------------------------------------------------------------------------

type benchResult struct {
	loadMs, p95Ms, p99Ms, maxMs, peakRSS    float64
	compileMs, steadyMs, steadyFPS, totalMs float64
	errText                                 string
}

type benchWorker struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines chan string
	done  chan error
}

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
		loadMs := float64(time.Since(start)) / float64(time.Millisecond)
		samples := make([]float64, 0, frames-1)

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
				compileMs = loadMs + elapsed.Seconds()*1000
			} else {
				steadyTotal += elapsed
				samples = append(samples, elapsed.Seconds()*1000)
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

		p95, p99, maxFrame := frameQuantiles(samples)
		peakRSS, memErr := prof.PeakMemoryKB()
		if memErr != nil {
			slog.Debug("benchmark peak RSS unavailable", "error", memErr)
			peakRSS = -1
		}
		if _, err := fmt.Fprintf(out, "OK %.2f %.2f %.2f %.2f %.2f %.2f %.2f %.2f %d\n", compileMs, steadyMs, steadyFPS, totalMs, loadMs, p95, p99, maxFrame, peakRSS); err != nil {
			return
		}
		if err := out.Flush(); err != nil {
			return
		}
	}
}

func parseBenchLine(line string) (benchResult, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return benchResult{}, fmt.Errorf("empty response")
	}
	switch fields[0] {
	case "OK":
		if len(fields) != 10 {
			return benchResult{}, fmt.Errorf("malformed OK line: %q", line)
		}
		values := make([]float64, 9)
		for i := range values {
			v, err := strconv.ParseFloat(fields[i+1], 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return benchResult{}, fmt.Errorf("invalid benchmark value %q", fields[i+1])
			}
			values[i] = v
		}
		return benchResult{compileMs: values[0], steadyMs: values[1], steadyFPS: values[2], totalMs: values[3], loadMs: values[4], p95Ms: values[5], p99Ms: values[6], maxMs: values[7], peakRSS: values[8]}, nil
	case "ERR":
		return benchResult{errText: strings.Join(fields[1:], " ")}, nil
	default:
		return benchResult{}, fmt.Errorf("unrecognized response: %q", line)
	}
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

func (bw *benchWorker) run(name string, timeout time.Duration) (benchResult, error) {
	if _, err := io.WriteString(bw.stdin, name+"\n"); err != nil {
		return benchResult{}, fmt.Errorf("write to worker: %w", err)
	}
	select {
	case line, ok := <-bw.lines:
		if !ok {
			werr := <-bw.done
			return benchResult{}, fmt.Errorf("worker exited without a result (crash?): %w", werr)
		}
		return parseBenchLine(line)
	case werr := <-bw.done:
		return benchResult{}, fmt.Errorf("worker exited unexpectedly: %w", werr)
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

func readBenchmarkedPresets(outPath string) map[string]bool {
	done := make(map[string]bool)
	f, err := os.Open(outPath)
	if err != nil {
		return done
	}
	defer func() {
		if err := f.Close(); err != nil {
			slog.Warn("readBenchmarkedPresets: close failed", "path", outPath, "error", err)
		}
	}()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil || len(records) == 0 {
		return done
	}
	// CSV columns: preset, status, compile_ms, steady_ms_per_frame, steady_fps, total_ms
	for _, rec := range records[1:] {
		if len(rec) > 0 {
			done[rec[0]] = true
		}
	}
	return done
}

// Nearest-rank percentiles, excluding the preset's first frame. Sort in place.
func frameQuantiles(samples []float64) (p95, p99, maximum float64) {
	if len(samples) == 0 {
		return 0, 0, 0
	}
	slices.Sort(samples)
	return samples[int(math.Ceil(float64(len(samples))*0.95))-1], samples[int(math.Ceil(float64(len(samples))*0.99))-1], samples[len(samples)-1]
}
