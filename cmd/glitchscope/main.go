package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/dendec/glitchscope/internal/app"
)

var (
	flagFullscreen = flag.Bool("fullscreen", false, "fullscreen mode")
	flagWidth      = flag.Int("w", 1280, "window width")
	flagHeight     = flag.Int("h", 720, "window height")
	flagVerbose    = flag.Bool("v", false, "verbose debug logging (incl. modarchive navigation)")
)

func init() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options] [path]\n", os.Args[0])
		fmt.Fprintln(flag.CommandLine.Output(), "path is an optional audio file or folder to open on startup")
		fmt.Fprintln(flag.CommandLine.Output(), "Options:")
		flag.PrintDefaults()
	}
}

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	flag.Parse()

	logLevel := slog.LevelInfo
	if *flagVerbose {
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

	args := flag.Args()
	if len(args) > 1 {
		slog.Error("expected at most one path argument", "arguments", args)
		flag.Usage()
		return
	}
	var startupPath string
	if len(args) == 1 {
		startupPath = args[0]
	}

	a, err := app.New(*flagFullscreen, *flagWidth, *flagHeight, startupPath)
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

	a.Init()
	a.Run()
}
