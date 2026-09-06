package main

import (
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/dendec/glitchscope/internal/app"
)

var (
	flagFullscreen  = flag.Bool("fullscreen", false, "fullscreen mode")
	flagWidth       = flag.Int("w", 1280, "window width")
	flagHeight      = flag.Int("h", 720, "window height")
	flagBenchmark   = flag.Bool("benchmark", false, "benchmark every preset and write CSV, then exit")
	flagBenchFrames = flag.Int("benchmark-frames", 120, "frames per preset in benchmark mode")
	flagBenchOut    = flag.String("benchmark-out", "benchmark.csv", "output path for benchmark CSV")
	flagBenchWorker = flag.Bool("benchmark-worker", false, "internal: run as benchmark worker subprocess")
	flagFile        = flag.String("file", "", "audio file to play on startup")
	flagVerbose     = flag.Bool("v", false, "verbose debug logging (incl. modarchive navigation)")
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

	if *flagBenchmark && !*flagBenchWorker {
		app.RunBenchmarkSupervisor(*flagBenchFrames, *flagBenchOut)
		return
	}

	a, err := app.New(*flagFullscreen, *flagWidth, *flagHeight, *flagFile)
	if err != nil {
		slog.Error("app init", "error", err)
		return
	}
	defer a.Close()
	quitSignals := make(chan os.Signal, 1)
	signal.Notify(quitSignals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(quitSignals)
	go func() {
		<-quitSignals
		a.RequestQuit()
	}()

	if *flagBenchWorker {
		a.RunBenchmarkWorker(*flagBenchFrames)
		return
	}

	a.Init()
	a.Run()
}
