# Seek / перемотка — design

Status: proposed (agreed with maintainer on 2 open decisions).

## Goal

Make track seeking (перемотка) feel like the rest of the UI: proportional to
how hard the analog stick is pushed, and *accelerating* the longer you hold.
This mirrors the existing held-key scroll acceleration used for navigation
(`Overlay.updateScrollHold` in `internal/ui/overlay_input.go`).

## What already exists

| Piece | Location |
|---|---|
| `Player.Seek(seconds)`, `Position()`, `Duration()` | `internal/player/player.go` |
| `Soloud_seek(voice, seconds)` (sync, immediate) | `internal/soloud/soloud.go` |
| Continuous seek off right-stick X | `internal/app/run_loop.go` (`rx * 10 * dt`) |
| Discrete ±5s seek (`,` / `.` keys) | `internal/app/actions.go` (`ActionSeekForward`/`Backward`) |
| Progress bar + `pos/dur` text | `internal/ui/overlay_render.go` |

## Root problem this design fixes: stale position after a seek

SoLoud's `getStreamTime` returns `mStreamTime`, which only advances by wall
clock each audio mix (`mStreamTime += buffertime`). A `Soloud_seek(...)` does
**not** set `mStreamTime` (the FFmpeg/chip `seek()` overrides never touch it;
the base `AudioSourceInstance::seek` sets `mStreamPosition`, not
`mStreamTime`).

Consequences today:

- After any seek the progress bar and `Position()` keep showing the pre-seek
  wall clock until it "catches up" — wrong for the jump and for the progress bar.
- The continuous-seek loop builds each new target on a stale `Position()`,
  which makes absolute scrubbing inaccurate (it behaves like a relative scrub).

### Fix: authoritative position in `Player`

`Player` owns a logical playback clock:

- `posMu sync.Mutex`, `pos float64`, `posLast time.Time`, `posValid bool`.
- Advanced on `Seek` (set `pos = target`) and advanced lazily on `Position()`
  (`pos += since(posLast)` only while a valid voice is playing and **not**
  paused). `posLast` is refreshed on **every** read — including while paused —
  so a resume after pause (or a long stall) never jumps the clock by the idle
  span. A single `dt` is capped to avoid giant wall-clock gaps.
- Reset to `0` / `posValid = false` on source change and `Stop()`.
- `Position()` therefore always reflects the last seek immediately and the
  progress bar never lags.

This keeps `TrackFinished()` and the `playbackState.snapshot` consistent.

## Backend seek support matrix (what the user hears during seek)

| Source | forward | backward | implementation |
|---|---|---|---|
| FFmpeg (MP3/FLAC/Ogg/Wav/Opus/M4A…) | yes | yes | `avformat_seek_file` + flush (exact) |
| Wav / WavStream (fallback decoder) | yes | yes | dr_* / stb_vorbis seek |
| GME (nsf/spc/vgm/gbs/ay…) | yes | yes | `gme_seek` |
| HVL/AHX, PT3, YM, Ayumi(VTX) | yes | yes | exact re-seek |
| Openmpt / Xmp (trackers) | only forward, coarse | **no** | generic sample-discard seek; `rewind()` = NOT_IMPLEMENTED |
| SID | **no** | **no** | `seek()`/`rewind()` return NOT_IMPLEMENTED |

During a discrete ± step you simply continue playing from the new position
(clean jump). During *continuous* analog scrubbing, each frame re-seeks the
decoder and resumes, producing a rapid succession of short audio fragments —
a garbled fast-forward (not pitch-shifted).

### Handling formats without backward seek

A backward seek on SID and trackers currently returns `NOT_IMPLEMENTED` and is
silently dropped (only a debug log). Design: when the player knows a source
cannot seek backward, a backward seek **restarts from 0** (stop + re-seek to
the exact fraction is not possible without decoder support, so restart is the
safe, predictable fallback). Implement by reporting seek capability from the
source and, on unsupported-backward, calling `Seek(0)` (which for these
sources means rewind) instead of erroring silently.

## Velocity + acceleration drivetrain (app layer)

Replace the naive `Position()+rx*10*dt` with an explicit velocity model in
`internal/app`:

```
effectiveSpeed = deflection * baseSeekSpeed          // deflection ∈ [0,1]
if heldMax && holdTime > 0.4s:                        // extreme deflection held
    effectiveSpeed *= accelMultiplier(holdTime)       // doubles each second, cap 8×
```

- `baseSeekSpeed` ≈ 10 s/s, cap on accelerated speed ≈ 80 s/s (8×).
- `deflection = |RightStickX()|` (already deadzone-filtered).
- Direction from sign of `RightStickX()`.
- Accelerated speed is applied only to the *continuous* (stick) path; the
  discrete `,`/`.` button still does one ±5s step per press, and if the key is
  **held** the same hold-acceleration drives repeated steps of growing size
  (polled from `sdl.GetKeyboardState`, matching `updateScrollHold`).

### Clamping & batching

- The target is clamped to `[0, Duration()]`; `Player.Seek` re-clamps.
- To avoid re-seeking the decoder every frame (churn of
  `avformat_seek_file`), the app accumulates requested delta and only issues a
  `Seek` when it has built up ≥ a small threshold (e.g. 0.1s) or the direction
  changes. The authoritative `Position()` keeps the rest exact.

## Interaction summary

- **Right analog stick** — continuous proportional scrub, accelerates if held
  at max deflection.
- **`,` / `.`** — discrete ±5s; held → accelerated repeated steps.
- Everything else (prev/next track, play/pause, etc.) unchanged.

## Files touched

- `internal/player/player.go` — position clock, seek-capability reporting,
  backward-seek fallback.
- `internal/app/run_loop.go` — velocity + acceleration drivetrain.
- `internal/app/actions.go` — discrete hold-aware seek.
- `internal/input/input.go` — expose held-seek button state if keyboard polling
  is the mechanism (or reuse `sdl.GetKeyboardState` in run_loop directly).
- `internal/player/*_test.go`, `internal/app/*_test.go` — pure-logic tests.

## Verification

`make lint` + `scripts/dtest.sh test` (sequential docker jobs; shared
go-build cache is fragile). Unit tests cover the position clock and the
velocity/acceleration math; the SDL/GL path is not unit-tested.
