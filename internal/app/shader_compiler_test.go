package app

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/dendec/glitchscope/internal/projectm"
	"github.com/veandco/go-sdl2/sdl"
)

// Run with scripts/dtest.sh gl. Mesa offscreen exercises the production SDL
// context lifecycle and native program handoff without a display or GPU.
func TestShaderCompilerGL(t *testing.T) {
	if os.Getenv("GLITCHSCOPE_GL_TEST") != "1" {
		t.Skip("requires opt-in software OpenGL integration run")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := sdl.Init(sdl.INIT_VIDEO); err != nil {
		t.Fatal(err)
	}
	defer sdl.Quit()
	for _, attr := range []struct {
		key   sdl.GLattr
		value int
	}{
		{sdl.GL_CONTEXT_MAJOR_VERSION, 3},
		{sdl.GL_CONTEXT_MINOR_VERSION, 3},
		{sdl.GL_CONTEXT_PROFILE_MASK, sdl.GL_CONTEXT_PROFILE_COMPATIBILITY},
	} {
		if err := sdl.GLSetAttribute(attr.key, attr.value); err != nil {
			t.Fatal(err)
		}
	}
	window, err := sdl.CreateWindow("shader test", 0, 0, 64, 64, sdl.WINDOW_OPENGL|sdl.WINDOW_HIDDEN)
	if err != nil {
		t.Fatal(err)
	}
	defer window.Destroy() //nolint:errcheck // SDL teardown after the test.
	mainContext, err := window.GLCreateContext()
	if err != nil {
		t.Fatal(err)
	}
	defer sdl.GLDeleteContext(mainContext)
	if err := projectm.InitOpenGL(); err != nil {
		t.Fatal(err)
	}
	compiler, err := newShaderCompiler(window, mainContext)
	if err != nil {
		t.Fatal(err)
	}
	defer compiler.Close()
	if mode := projectm.ShaderCompileMode(); mode != "shared-context" {
		t.Fatalf("compiler mode = %s", mode)
	}
	pm, err := projectm.Create()
	if err != nil {
		t.Fatal(err)
	}
	defer pm.Destroy()
	pm.SetWindowSize(64, 64)
	// Two instances with independent pending sets: committing the main preset
	// must not depend on a pending/cancelled preview job.
	preview, err := projectm.Create()
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Destroy()
	preview.SetWindowSize(32, 32)
	for i := range 8 {
		data := fmt.Sprintf("[preset00]\nfRating=3.0\nfDecay=0.9\nPSVERSION=2\nPSVERSION_WARP=2\nPSVERSION_COMP=2\nwarp_1=`shader_body { ret = tex2D(sampler_main, uv).xyz; }\ncomp_1=`shader_body { ret = float3(%f, 0.3, 0.7); }\n", float64(i)/10)
		started := time.Now()
		if !pm.BeginPresetLoad(data, false) || !preview.BeginPresetLoad(data, false) {
			t.Fatalf("begin: main=%s preview=%s", pm.PresetLoadError(), preview.PresetLoadError())
		}
		preview.CancelPresetLoad()
		polls := 0
		for pm.PollPresetLoad() != projectm.PresetLoadReady {
			if pm.PollPresetLoad() == projectm.PresetLoadFailed || time.Since(started) > 10*time.Second {
				t.Fatalf("prepare: %s", pm.PresetLoadError())
			}
			pm.RenderFrame() // Old preset keeps rendering on the main context.
			polls++
			runtime.Gosched()
		}
		if !pm.CommitPresetLoad() {
			t.Fatalf("commit: %s", pm.PresetLoadError())
		}
		pm.RenderFrame() // Newly compiled programs must work in this context.
		t.Logf("switch %d: %s, old frames while loading=%d", i, time.Since(started), polls)
	}
}
