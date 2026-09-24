package app

import (
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sync"

	"github.com/dendec/glitchscope/internal/projectm"
	"github.com/veandco/go-sdl2/sdl"
)

// shaderCompiler owns one shared GL context and its locked worker thread.
// Only independent shader/program objects cross the context boundary. Native
// jobs own their inputs/results; cancelling a preset drops its references and
// never waits for an in-progress driver call. Close is the only join point.
type shaderCompiler struct {
	ctx    sdl.GLContext
	window *sdl.Window
	wg     sync.WaitGroup
}

// newShaderCompiler runs on the GL main thread before projectM creation.
func newShaderCompiler(window *sdl.Window, mainContext sdl.GLContext) (*shaderCompiler, error) {
	workerWindow, err := sdl.CreateWindow("shader compiler", 0, 0, 16, 16, sdl.WINDOW_OPENGL|sdl.WINDOW_HIDDEN)
	if err != nil {
		return nil, fmt.Errorf("compiler window: %w", err)
	}
	if err := sdl.GLSetAttribute(sdl.GL_SHARE_WITH_CURRENT_CONTEXT, 1); err != nil {
		_ = workerWindow.Destroy()
		return nil, fmt.Errorf("enable GL sharing: %w", err)
	}
	shared, createErr := workerWindow.GLCreateContext()
	resetErr := sdl.GLSetAttribute(sdl.GL_SHARE_WITH_CURRENT_CONTEXT, 0)
	// SDL_GL_CreateContext makes the new context current on the caller.
	restoreErr := window.GLMakeCurrent(mainContext)
	if err := errors.Join(createErr, resetErr, restoreErr); err != nil {
		if shared != nil {
			sdl.GLDeleteContext(shared)
		}
		_ = workerWindow.Destroy()
		return nil, fmt.Errorf("create shared compiler context: %w", err)
	}

	probe := projectm.CreateShareProbe()
	defer projectm.DeleteShareProbe(probe)
	compiler := &shaderCompiler{ctx: shared, window: workerWindow}
	ready := make(chan error, 1)
	compiler.wg.Add(1)
	go func() {
		defer compiler.wg.Done()
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := workerWindow.GLMakeCurrent(shared); err != nil {
			ready <- fmt.Errorf("bind compiler GL context: %w", err)
			return
		}
		defer func() {
			if err := workerWindow.GLMakeCurrent(nil); err != nil {
				slog.Error("detach compiler GL context", "error", err)
			}
		}()
		if probe == 0 || !projectm.ShareProbeVisible(probe) {
			ready <- errors.New("compiler GL context does not share program objects")
			return
		}
		projectm.EnableShaderWorker()
		ready <- nil
		projectm.RunShaderWorker()
	}()
	if err := <-ready; err != nil {
		compiler.wg.Wait()
		sdl.GLDeleteContext(shared)
		_ = workerWindow.Destroy()
		return nil, err
	}
	slog.Info("shader compiler ready", "mode", projectm.ShaderCompileMode())
	return compiler, nil
}

func (compiler *shaderCompiler) Close() {
	if compiler == nil || compiler.ctx == nil {
		return
	}
	projectm.StopShaderWorker()
	compiler.wg.Wait()
	sdl.GLDeleteContext(compiler.ctx)
	compiler.ctx = nil
	if err := compiler.window.Destroy(); err != nil {
		slog.Warn("destroy compiler window", "error", err)
	}
}
