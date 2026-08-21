# AGENTS.md

Guidance for AI coding agents working in this repository. This file complements
(and deliberately does not duplicate) the project's own documentation; the
authoritative architecture rules live in [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
and must be followed.

---

## 1. Project at a glance

**What this is.** `pmv` (Portable Music Visualizer) is a real-time audio player +
MilkDrop visualizer. It plays audio (MP3/FLAC/Ogg/WAV/Opus/… via FFmpeg & SoLoud)
and tracker/chip formats (MOD/XM/IT/S3M/PT3/VTX/YM/SID/console audio via
libopenmpt, libxmp, pt3player, ayumi, StSound, libgme) while rendering projectM 4
MilkDrop presets over OpenGL. Target: low-power ARM handhelds (PortMaster), but it
runs on any Linux/Windows desktop.

- **Language:** Go 1.25 (`module github.com/dendec/pmv`). UI is pure Go +
  OpenGL/SDL2; there is no web frontend.
- **Windowing/audio/GL:** `go-sdl2`, OpenGL. All audio decoding is done through
  statically linked C/C++ libraries (see §3 — you cannot build or run tests
  natively).
- **Input:** keyboard + gamepad, mapped to abstract actions (see §5).

### Repository layout

| Path | Purpose |
|---|---|
| `cmd/pmv/` | Main entrypoint. CLI flags, SDL/GL/SoLoud/projectM init, main loop. |
| `cmd/pmv-pack/` | Packs preset/texture directories into `.pmv` archives. |
| `cmd/modland-catalog/` `cmd/modarchive-catalog/` | Build remote music catalogs from Modland / ModArchive. |
| `cmd/validate-catalog/` | Validates a downloaded catalog by loading each format. |
| `cmd/texture-report/` | Reports texture usage of presets (CSV). |
| `internal/app/` | **Orchestration**: app lifecycle, main loop, adaptive resolution, playback state, actions, benchmark, delete service. |
| `internal/ui/` | **Overlay**: SDL/GL rendering of the 2-column UI, navigation, help, presets page, themes. Largest package (~9k lines), split by concern into `overlay*.go`, `render_*.go`, `help.go`, `device.go`, `gl.go`. |
| `internal/player/` | SoLoud-backed audio playback, async loading, music library/scanner, metadata. |
| `internal/config/` | Persisted settings types, validation, atomic JSON storage. |
| `internal/filesystem/` | Shared directory walk with `OK` / `Partial` / `Failed` statuses. |
| `internal/{soloud,projectm,openmpt,xmp,mic}` | Thin cgo bindings over the C/C++ libs (SoLoud, projectM, openmpt, xmp, ALSA mic capture). |
| `internal/{presets,modland,modarchive,archive,input,prof,formats}` | Presets store, remote catalogs, `.pmv` archive reader, input mapping, profiling, format detection. |
| `lib/` | git submodules — the C/C++ sources. **Never modify these.** |
| `portmaster/` | PortMaster packaging (launcher, `port.json`, `gameinfo.xml`). |
| `docs/` | Architecture + feature/design plans (see §8). |
| `Makefile`, `Dockerfile`, `Dockerfile.builder` | Build orchestration (see §3). |
| `settings.json` | Runtime-generated user settings (gitignored). |

---

## 2. Golden rules (read first)

1. **You generally cannot build, run, or `go vet`/`go test` here.** This repo is
   cgo-heavy and depends on static C/C++ libraries compiled inside a Docker
   builder image. Do not attempt a native `go build ./...` and report a failure
   as broken code — it will fail on missing `/opt/*` headers/libs even when the
   code is correct. All verification happens via `make lint` / `make test` /
   `make dist`, which run inside the builder container (requires Docker + network
   on first run).
2. **`docs/ARCHITECTURE.md` is the source of truth for module boundaries and
   invariants.** Read it before changing cross-package behavior. Its "ownership"
   table defines the single owner of each concept; update every projection
   (implementation, tests, docs, packaging) when you change an owned concept.
3. **Preserve the documented invariants.** The two most important:
   - `internal/filesystem` walk statuses: `OK`, `Partial`, `Failed`. An empty
     result is only acceptable after a successful (`OK`) read. Playback must not
     replace the current queue if a scan is `Partial`/`Failed`.
   - Settings: a *missing* settings file is the only normal "absent" case.
     Corrupt JSON or unknown values are surfaced as **errors**, never silently
     masked as valid defaults. (See `config.Validate` / `LoadSettings`.)
4. **Delete flow ordering:** validate the path boundary + symlink policy first,
   then mutate the filesystem; update playback/index only after a successful
   delete; surface a rescan error explicitly. See `internal/app/delete_service.go`
   and `actions.go:deleteNCPath`.
5. **Concurrency pattern is established — reuse it.** Async work uses
   `context.WithCancel`, an atomic `requestID` to discard stale results, a
   buffered channel, and `sync.WaitGroup` for shutdown (see
   `internal/player/player.go`). Follow this; do not introduce ad-hoc goroutine
   patterns.
6. **All C/C++ sources are git submodules** (`lib/projectm`, `lib/soloud`,
   `lib/game-music-emu`, `lib/ayumi`, `lib/pt3player`, `lib/libstsound`,
   `lib/ffmpeg`). Do not edit them, add new ones casually, or commit their build
   output. Add a binding package under `internal/` for any new native backend.

---

## 3. Build, test, lint

Everything is driven by `make` and requires Docker.

```bash
make builder          # compile the C/C++ deps once into the pmv-builder image
make lint             # golangci-lint run inside builder (config: .golangci.yml)
make test             # go test -count=1 ./cmd/... ./internal/... inside builder
make dist             # build amd64 release into dist/linux-amd64
make dist-arm64       # arm64 cross-build
make dist-portmaster  # PortMaster zip -> dist/pmv.zip
make deploy           # push to an adb-connected handheld
make presets          # clone cream-of-the-crop presets repo (~160MB)
make textures         # clone milkdrop texture pack
make catalog          # build modland + modarchive catalogs + validate
```

- `make lint` runs `golangci-lint` with `.golangci.yml` (v2 config). It enables
  extra linters (`bodyclose`, `errorlint`, `gocritic`, `misspell`, `nilerr`,
  `unused`, …) and uses **gofumpt** as the formatter. **Keep `gofumpt`-clean** —
  the formatter is enforced, and the lint config has only a few *documented*
  exclusions; do not add blanket ones.
- `make test` and `make lint` intentionally run in the builder so results don't
  depend on local cgo availability. `make dist` additionally exercises the full
  amd64 packaging path.
- **Fast local checks:** `scripts/dtest.sh test|lint` runs the same checks
  directly against an *existing* `pmv-builder` image without rebuilding it
  (use when the image is already present). It feeds the cgo env vars into the
  container exactly like the Makefile does.
- CI runs `make lint` and `make test` on GitHub Actions (`.github/workflows/ci.yml`).

### Native dev caveat
The sandbox/host here is read-only for Go tooling and lacks the `/opt` cgo
libs, so `go build`/`go test`/`go vet` will fail for environment reasons. That
is **not** evidence of broken code. Reason statically and rely on the Docker
targets for actual verification.

---

## 4. Architecture & module ownership

Ownership is defined centrally in `docs/ARCHITECTURE.md`. Key owners:

| Concept | Single owner | Other projections |
|---|---|---|
| Settings | `internal/config` | JSON storage, Settings UI |
| Local music index | `internal/player` | NC navigation, playback selection |
| Navigation & focus | `internal/ui` | rendered lists, breadcrumbs |
| Playback queue | `internal/app` playback state | player commands, overlay snapshot |
| File deletion | `internal/app/delete_service.go` | confirmation UI, rescan |
| Remote catalogs | `internal/modland`, `internal/modarchive` | provider nav + downloads |

**Layering / dependencies.** Keep dependency flow one-directional:
`internal/ui` and `internal/player` are low-level and should not depend on
`internal/app`; `internal/app` is the wiring layer that owns orchestration,
the main loop, and cross-package glue (e.g. downloader assignment in
`app.go:initAudio`). New modules should not become required owners of several
independent decisions.

**`internal/ui` internal split** (large package): the header comment in
`overlay.go` documents which file owns what — `overlay_nav.go` (tree),
`overlay_input.go` (cursor/pages), `overlay_theme.go` (colors),
`overlay_presets.go` (presets model + marquee), `overlay_render.go` (drawing),
`gl.go` (all GL/cgo calls). **Keep GL/cgo calls isolated in `gl.go`** — do not
scatter raw GL into other files.

**Native bindings** (`internal/soloud`, `internal/projectm`, …): thin, mostly
mechanical wrappers. Keep them thin — put business logic in the caller.

---

## 5. Conventions & style

- **Logging:** use `log/slog` structured logging only (`slog.Info/Debug/Warn/Error`)
  with key/value pairs. No `log.Printf`/global logger, no `fmt.Println` in
  production paths. Verbose flags select `Debug` level (`cmd/pmv/main.go`).
- **Errors:** wrap with context and `%w`
  (`fmt.Errorf("sdl init: %w", err)`). Unwrap with `errors.As`/`errors.Is`.
  Never swallow an error silently; if a non-fatal failure is acceptable, log it
  and degrade gracefully (fall back to `config.DefaultSettings()`, disable
  audio, etc.) rather than crash.
- **Resource lifecycle:** every acquired resource has a matching teardown.
  Mirror the `App.Close()` chain in `internal/app/app.go` and
  `Player.Close()` in `internal/player/player.go`. Cancel background work and
  wait for workers before destroying shared state.
- **Naming:** terse graphics abbreviations are idiomatic here (`pm` = projectM
  handle, `rt` = render target, `pl` = player, `inp` = input, `gs` = graphics
  settings). Follow existing short names within a file; prefer clear names for
  new concepts. Package-level doc comments are expected on each package.
- **Go stdlib idioms:** `min`/`max` builtins, `slices`, `errors`, `sync/atomic`
  for cross-goroutine flags, `sync.Mutex` (not `sync.RWMutex` unless justified).
- **Formatting:** gofumpt. Run `make lint` or gofumpt before finishing.
- **Naked `return`s** are common in simple accessor-style methods — that's fine;
  prefer explicit returns when the signature has multiple named results.

---

## 6. Domain specifics & gotchas

- **Presets:** a preset name with a leading `!` is a *transition* preset used for
  smooth cross-fades (`softCutDuration = 2.5s`); normal presets exclude them.
  Preset switching has a single DRY entry point: `App.transitionPreset` /
  `applyPresetName` in `internal/app/actions.go`. Presets are stored in a `.pmv`
  archive; `internal/presets` is the store.
- **Adaptive resolution** (`internal/app/adaptive.go`, `resolution_state.go`):
  a policy steps the internal render resolution down/up to keep FPS in band
  (`adaptiveThreshLow/Hight`, cooldowns). Tune constants there; keep the policy
  free of UI/player knowledge. When a user explicitly sets a render scale, the
  adaptive path is bypassed (`renderScaleExplicit`).
- **Async playback** (`internal/player/player.go`): `PlayFileAsync` loads in a
  background goroutine, publishes to `pendingCh`, and `CheckPending()` picks it up
  **once per frame**. Stale results are identified by `requestID` and destroyed,
  never applied. `PlayFile` is the synchronous path (used by tests/benchmark).
- **Seek & pre-render layer** (`internal/player/player.go`, see
  `docs/SEEK-DESIGN.md`): formats whose native decoder can't seek cleanly in
  both directions (openmpt, xmp, YM, SID) are **pre-rendered once at load**
  into an in-memory planar float32 `Wav` at `renderSampleRate = 44100`. The
  Wav then replaces the native source and gives exact bidirectional seek.
  `maxRenderSeconds = 360` caps the render (worst-case ~124 MiB stereo) — tracks
  longer than the cap fall back to native streaming (`sourceSeeksBoth`). The
  cap is a loaded constant guarded by `TestMaxRenderFrames`, so raising it
  requires touching the constant, the regression guard, and the doc together.
- **SoLoud voice** (`voice uint`): `0` means "no active voice"; guard all
  voice-scoped calls (`Pause`/`Resume`/`Position`/`Seek`) with `IsValidVoice()`.
- **Mic capture** takes priority and stops SoLoud playback to avoid feedback
  (`startMicCapture` in `actions.go`). The mic stream feeds `pm.PCMAddFloat`
  directly; it is not played through speakers.
- **Settings persistence** is atomic (temp file + rename) — see
  `internal/config/storage.go`. All enum-like settings implement `String()`,
  `MarshalJSON`/`UnmarshalJSON` (store lowercase strings), `Validate()`, and an
  `All*()` accessor. Keep these consistent when adding a new setting; add it to
  `DefaultSettings`, the Settings UI rows, and validation together.
- **Embedded assets** (`internal/ui/`): `help.json`, `assets/licenses/*`, and the
  `unifont.otf` font (subset generated at build; see `FONT_*` in the Makefile).
  New embed files must be added with the `//go:embed` directive and referenced
  from the correct package.
- **Remote catalogs:** Modland/ModArchive are network catalogs downloaded on
  demand (cached under `.cache/`). Connectivity is probed in the background
  (`checkConnectivity` in `app.go`). Treat remote downloads as cancellable work
  (`context`) and track expected sizes in `modlandSizes`.
- **Main loop** (`internal/app/run_loop.go`): fixed-tick (`time.Second/60`)
  loop calling `PollEvent`, rendering via `pm.RenderFrame() -> rt.Capture() ->
  rt.BlitToScreen()`, then the overlay draw. Keep per-frame work cheap; heavy
  work belongs in background goroutines (e.g. audio loading).
- **Platform quirks:** `runtime.LockOSThread()` is required in `main` (SDL/GL
  thread affinity). `sdl.StopTextInput()` is called so Linux input methods
  (ibus/fcitx) don't swallow key events — do not remove it.

---

## 7. Tests

- Go unit tests live next to their packages: `internal/app/*_test.go`,
  `internal/ui/*_test.go`, `internal/player/*_test.go`, `internal/config/*_test.go`.
  Coverage concentrates on state machines and pure logic (playback state,
  adaptive policy, delete service, overlay layout, help wrapping, notifier).
- Tests run only inside Docker: `make test`.
- Use the existing test seams rather than adding new ones where possible
  (e.g. `Player.loadFunc` overrides the loader for unit tests).
- When you add or change behavior in `internal/app` or `internal/ui`, add a
  matching unit test for the pure-logic parts. Do not try to unit-test the SDL/GL
  rendering path itself.
- Test files are subject to the same lint config; the only relaxations are the
  explicitly documented ones in `.golangci.yml`.

---

## 8. Documentation

- `docs/ARCHITECTURE.md` — module boundaries, ownership table, invariants.
  **Update it when you change ownership or invariants.**
- `docs/UI-PLAN.md`, `docs/UI-HELP-PLAN.md`, `docs/NAV-MODE-SWITCH.md`,
  `docs/PLAN*.md` — feature/design plans. Read the relevant one before touching
  UI/navigation/player features.
- `docs/SEEK-DESIGN.md` — the seek/перемотка architecture (accelerating seek
  drivetrain + pre-render layer). Read it before touching seeking or the
  tracker/chip pre-render path in `internal/player`.
- `README.md` — user-facing features and build instructions.
- Keep docs in sync with behavior; a change is "complete" only when its
  implementation, tests, and docs are all updated together.

---

## 9. Workflow for making changes

1. Read the owning doc (`docs/ARCHITECTURE.md` and any relevant `PLAN-*.md`/`UI-*.md`).
2. Find the single owner of the concept you're changing and touch only its
   projections.
3. Follow the concurrency, error, logging, and resource-lifecycle conventions in §5.
4. Add/update unit tests for pure logic.
5. Verify with `make lint` and `make test` (requires Docker); for packaging
   changes, `make dist`.
6. Do not commit build artifacts (`dist/`, `.cache/`, `presets/`, `settings.json`,
   fonts) or anything under `lib/` submodules.
