package main

import (
	"flag"
	"log/slog"
	"os"
	"runtime"

	"github.com/dendec/pmv/internal/app"
)

var (
	flagFullscreen    = flag.Bool("fullscreen", false, "fullscreen mode")
	flagWidth         = flag.Int("w", 1280, "window width")
	flagHeight        = flag.Int("h", 720, "window height")
	flagRenderScale   = flag.Float64("render-scale", 1.0, "internal visualizer render resolution scale (0.2-1.0)")
	flagRenderNearest = flag.Bool("render-nearest", false, "nearest-neighbour filtering for visualizer upscale")
	flagBenchmark     = flag.Bool("benchmark", false, "benchmark every preset and write CSV, then exit")
	flagBenchFrames   = flag.Int("benchmark-frames", 10, "frames per preset in benchmark mode")
	flagBenchOut      = flag.String("benchmark-out", "benchmark.csv", "output path for benchmark CSV")
	flagBenchWorker   = flag.Bool("benchmark-worker", false, "internal: run as benchmark worker subprocess")
	flagFile          = flag.String("file", "", "audio file to play on startup")
	flagShowFPS       = flag.Bool("show-fps", false, "always show FPS counter, even when UI is hidden")
	flagVerbose       = flag.Bool("v", false, "verbose debug logging (incl. modarchive navigation)")
)

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	flag.Parse()

	logLevel := slog.LevelInfo
	if *flagVerbose {
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

	renderScaleExplicit, renderNearestSet := trackExplicitFlags()

	if *flagBenchmark && !*flagBenchWorker {
		app.RunBenchmarkSupervisor(*flagBenchFrames, *flagBenchOut)
		return
	}

	a, err := app.New(*flagFullscreen, *flagWidth, *flagHeight,
		*flagRenderScale, *flagRenderNearest,
		renderScaleExplicit, renderNearestSet, *flagFile, *flagShowFPS)
	if err != nil {
		slog.Error("app init", "error", err)
		return
	}
	defer a.Close()

	if *flagBenchWorker {
		a.RunBenchmarkWorker(*flagBenchFrames)
		return
	}

	a.Init()
	a.Run()
}

func trackExplicitFlags() (renderScaleExplicit, renderNearestSet bool) {
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "render-scale":
			renderScaleExplicit = true
		case "render-nearest":
			renderNearestSet = true
		}
	})
	return
}
