# Windows port plan

## Goal

Produce a reproducible `windows-amd64` build of GlitchScope from Docker, using
the existing Go + cgo + SDL2 architecture. The first useful milestone is a
native Windows executable that opens an SDL/OpenGL window, renders projectM,
loads presets, and plays at least the existing local audio formats.

This is a native desktop port, not a WASM port. Linux/ARM GLES builds must
remain unchanged.

## Current assessment

The repository now has a first reproducible Windows build pipeline:

- `go-sdl2` already has Windows cgo support.
- projectM has a Windows desktop OpenGL/GLEW configuration.
- SoLoud and the local decoder bridges are C/C++ sources that can be built with
  a MinGW-compatible toolchain.
- `Dockerfile.builder` remains Linux-only; `Dockerfile.windows` and
  `make dist-windows` provide the separate Windows amd64 path.
- Several Go/C files contain Linux-only assumptions that must be isolated
  before the Windows build can compile.

The historical `PLAN.md` also mentions `Dockerfile.windows`; this plan remains
the current, normative port roadmap.

## Milestones

### 0. Baseline and dependency inventory

- Keep the current Linux build path unchanged.
- Enumerate every object/library required by the cgo link line.
- Confirm the Windows file set with `GOOS=windows GOARCH=amd64` package
  discovery and identify Linux-only files before running a native build.
- Decide whether GLEW is linked statically or shipped as a DLL. Prefer static
  GLEW and static third-party libraries where licensing and build systems allow
  it; runtime SDL2 remains a normal DLL.

Acceptance: a documented dependency list and a Windows-specific link plan.

### 1. Platform graphics compatibility layer

Preserve the existing Go-facing rendering API while selecting the graphics
implementation at compile time:

- Linux/ARM: keep GLES2 headers and `libGLESv2`.
- Windows: use desktop OpenGL headers, GLEW, and `opengl32`.
- Add one shared compatibility header for the C helpers used by
  `internal/ui`, `internal/projectm`, and the preview renderer.
- Move direct GL calls that rely on header macros/function pointers into C
  helper functions where necessary for GLEW.
- Call `glewInit` after `SDL_GL_CreateContext` and before `projectm.Create`.
- Add desktop GLSL shader variants. The current `#version 100` and
  `precision mediump float` shaders remain the GLES variants.
- Request a compatible Windows OpenGL context explicitly before creating the
  SDL window.

Acceptance: a minimal Windows executable creates a context, initializes GLEW,
creates projectM, and renders a frame without unresolved GL symbols.

### 2. Go platform cleanup

- Exclude the ALSA microphone backend from Windows and provide a Windows
  fallback that continues to use SDL capture.
- Split system/device information that currently uses `/proc` or
  `syscall.Sysinfo_t`; Windows may return a reduced but valid device report.
- Review profiler and benchmark code. Linux `/proc` metrics should degrade
  gracefully; benchmark subprocess features that depend on Unix file
  descriptors should be disabled or given a Windows implementation.
- Verify path handling, settings location, executable-relative presets, and
  cache directories on Windows.

Acceptance: the application package and all production dependencies compile
for Windows without Linux-only headers, symbols, or syscall types.

### 3. Windows native dependency builder

Implemented in `Dockerfile.windows` following the `zimlite` pattern:

- Debian build container.
- Go toolchain and Zig cross-compiler.
- Windows SDL2 development archive and runtime DLL.
- A Windows sysroot under `/opt/win32`.
- Static builds of projectM/projectM-eval, SoLoud bridges, FFmpeg, OpenMPT,
  XMP, GME, cRSID, libstsound, ayumi, and pt3player.
- CMake/Meson/autotools cross files as appropriate for each dependency.
- One final `go build` with `CGO_ENABLED=1`, `GOOS=windows`, `GOARCH=amd64`,
  Zig `CC/CXX`, and a Windows-specific cgo link line.

The Windows builder must not reuse Linux `.a` files from
`glitchscope-builder`; object format and system libraries differ.

Acceptance: Docker produces `glitchscope.exe` and a complete runtime
directory containing every required DLL.

### 4. Packaging and Makefile integration

- `dist-windows` extracts `/dist/glitchscope` from the artifact image, as
  `zimlite` does, then adds the same preset/texture archives and offline
  Modland/ModArchive catalogs as the Linux release.
- Runtime-created data such as downloaded module files, station caches,
  `cached-tracks.json`, and shuffle indexes is not part of a clean release.
- A release zip and Windows-specific documentation/licenses remain optional
  packaging work for a later pass.
- Do not add generated build output, caches, or release archives to Git.

Acceptance for this milestone: one command creates a Windows amd64 runtime
directory containing the PE executable and every non-system DLL it imports.

### 5. Verification

- Run normal Linux `make lint` and `make test` after platform changes.
- Run Windows package/build checks inside the Windows Docker builder.
- Inspect the PE import table for unintended runtime dependencies.
- If Wine is available, run a smoke test; otherwise validate the artifact
  statically and test it on a real Windows machine.
- Manually verify: startup, window resize, preset load/switch, WAV/MP3/FLAC,
  tracker playback, keyboard/gamepad input, settings persistence, and clean
  shutdown.

## Deliberate non-goals for the first pass

- Windows ARM.
- WASM or browser support.
- Replacing SDL2 or the existing UI.
- Rewriting the audio architecture.
- Making Linux-specific profiler details identical on Windows.

## Main risks

1. Desktop OpenGL/GLEW versus the current GLES2 helper code and shader source.
2. Cross-compiling the trimmed FFmpeg configuration with Zig.
3. pthread usage in the SID bridge and threading in the streaming decoder.
4. Static-link order and MinGW runtime DLLs.
5. Runtime differences in executable-relative paths and file deletion.

The first implementation slice should therefore be the graphics compatibility
layer plus a build-only Windows skeleton, before spending time on all decoder
variants and packaging polish.
