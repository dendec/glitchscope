# Seek / перемотка — design

Status: implemented (velocity/acceleration drivetrain + pre-render layer).

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

A backward seek on SID and trackers previously returned `NOT_IMPLEMENTED` and a
backward seek **restarted from 0**. That is still the last-resort fallback, but
the primary fix is now **pre-rendering** (see next section), which gives exact
bidirectional seek to formats whose native decoders can't do it.

## Pre-render (decode-to-buffer) layer

Openmpt/Xmp only seek forward (and only by coarse, CPU-heavy sample-discard),
SID has no seek at all, and libstsound's YM seek is sub-format dependent /
unreliable. Rather than fight each native decoder, the player **pre-renders**
these formats into a memory-backed SoLoud `Wav` once, during the existing async
load, and plays from that buffer. A `Wav` seeks exactly in both directions, so
MOD/IT/YM/SID (and any other tracker) get identical, precise scrubbing.

| Piece | Location |
|---|---|
| `NewWavFromSamples` (memory Wav from planar PCM) | `internal/soloud/soloud.go` |
| `Render` (libopenmpt -> interleaved PCM) | `internal/openmpt/openmpt.go` |
| `Render` (libxmp -> interleaved PCM) | `internal/xmp/xmp.go` |
| `RenderYm`, `RenderSid` (libstsound / cRSID -> PCM) | `internal/soloud` |
| `maxRenderFrames`, `interleavedToPlanar`, `renderToSeekable` | `internal/player/player.go` |

**Cap and fallback.** A track is only pre-rendered if its duration is within
`maxRenderSeconds` (`360s`, ~121 MiB of float32 stereo at 44.1 kHz). Longer tracks
keep native streaming to bound RAM on low-power handhelds (backward seek then
restarts). The cap is set above typical chip-track lengths (e.g. a common YM
tune is ~186s) so they pre-render and seek exactly rather than silently falling
back to the unreliable native YM seek. The render pipeline can temporarily hold
multiple full-track PCM buffers while decoding and converting channel layout, so
the cap is based on peak working memory rather than only the final SoLoud buffer.
Unknown-duration tracks default to the cap. Rendering runs in the background load
goroutine, so the UI shows the loading indicator, never stalls. Starting an
async replacement releases the already stopped current source before decoding, so
its memory-backed PCM does not overlap with the temporary render buffers for the
next track. The decoded interleaved PCM is converted directly into a SoLoud-
owned planar buffer, avoiding one additional full-track copy.

**Seek matrix with pre-render** — for pre-rendered tracks all formats become
exact/bidirectional; the "implementation" column applies only to the over-cap
native fallback:

| Source | forward | backward | native implementation |
|---|---|---|---|
| FFmpeg / Wav (lossy & PCM) | exact | exact | `avformat_seek_file`, dr_* / stb_vorbis |
| GME / HVL / PT3 / Ayumi | exact | exact | re-seek / `gme_seek` |
| **Openmpt / Xmp / YM / SID (pre-rendered)** | **exact** | **exact** | `Wav` buffer seek |
| Openmpt / Xmp (over cap, native) | coarse | restart | sample-discard; `rewind()` = NOT_IMPLEMENTED |
| SID (over cap, native) | none | none | `seek()`/`rewind()` = NOT_IMPLEMENTED |


## Velocity + acceleration drivetrain (shared input model)

Replace the naive `Position()+rx*10*dt` with an explicit velocity model in
`internal/app`. The reusable response curve lives in `internal/input/stick.go`
and also drives vertical navigation with the left stick:

```
effectiveSpeed = deflection * baseSeekSpeed          // deflection ∈ [0,1]
if heldMax && holdTime > 0.4s:                        // extreme deflection held
    effectiveSpeed *= accelMultiplier(holdTime)       // doubles each second, cap 8×
```

- `baseSeekSpeed` ≈ 10 s/s, cap on accelerated speed ≈ 80 s/s (8×).
- `deflection = |RightStickX()|` (deadzone-filtered by `internal/input`).
- Left-stick vertical navigation uses the same signed deflection curve and hold
  multiplier, with list steps per second as its base-speed unit.
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
- **Left analog stick** — proportional vertical list scroll, with the same
  max-hold acceleration curve.
- **`,` / `.`** — discrete ±5s; held → accelerated repeated steps.
- Everything else (prev/next track, play/pause, etc.) unchanged.

## Files touched

- `internal/player/player.go` — position clock, seek-capability reporting,
  backward-seek fallback, **pre-render layer** (`maxRenderFrames`,
  `interleavedToPlanar`, `renderToSeekable`, wired into `loadTracker`/`loadChip`).
- `internal/app/run_loop.go` — velocity + acceleration drivetrain.
- `internal/input/stick.go` — shared analog deflection and hold acceleration.
- `internal/ui/overlay_input.go` — left-stick list scroll scheduling.
- `internal/app/actions.go` — discrete hold-aware seek.
- `internal/input/input.go` — expose held-seek button state if keyboard polling
  is the mechanism (or reuse `sdl.GetKeyboardState` in run_loop directly).
- `internal/soloud/soloud.go` — `NewWavFromSamples` (memory Wav from PCM),
  `RenderYm`, `RenderSid` render entry points.
- `internal/openmpt/openmpt.go`, `internal/xmp/xmp.go` — `Render` decoders.
- `internal/soloud/{ym,sid}_source.cpp` — C render loops.
- `internal/player/*_test.go`, `internal/app/*_test.go` — pure-logic tests.

## Verification

`make lint` + `scripts/dtest.sh test` (sequential docker jobs; shared
go-build cache is fragile). Unit tests cover the position clock, the
velocity/acceleration math, the pre-render cap decision and the
interleave→planar conversion; the SDL/GL path is not unit-tested.


## Handheld memory and cancellation

At audio startup, `internal/prof` reads `MemAvailable` and the player selects a
working PCM budget: 256 MiB when at least 512 MiB is free, 128 MiB when at least
256 MiB is free, otherwise native streaming. This keeps the budget at or below
half of currently available memory. The existing 360-second guard remains in place.
The budget reserves space for both the interleaved decoded buffer and the final
SoLoud planar buffer, plus 1 MiB for small blocks. It is not a process RSS limit:
module data, native decoder state, the Go heap and graphics resources are extra.
A known track that exceeds either limit streams natively. For unknown duration,
reaching the buffer limit falls back to native playback instead of truncating
the song. Streaming retains each backend's seek limitations.

Openmpt uses 4096-frame temporary channel blocks; YM writes interleaved samples
directly. Neither retains two full channel arrays before interleaving anymore.
A 360-second stereo track needs about 121 MiB for the decoded PCM and another
121 MiB for SoLoud conversion, rather than additional full-track channel copies.

All asynchronous native loads share one decoding lock. Queued requests check
cancellation after acquiring it, so only the newest pending request decodes.
Openmpt/Xmp check the context between decoding blocks. YM/SID check via a
synchronous cgo.Handle callback between native blocks; the handle is deleted
only after the native call returns. Cancellation does not interrupt library
initialization or a single native block. Stale results are still discarded
through requestID, and Close cancels and waits for all workers.

Verbose logging reports decode time and the kernel's process peak RSS (including
native allocations). Peak RSS is a process-lifetime high-water mark, not memory
attributed exclusively to the most recent track.

## Live radio

Radio has no seekable timeline. `Player.Seek` rejects radio before the
non-seekable tracker restart fallback; keyboard/stick seek and saved-position
restoration skip live sources. Resume restores the station and queue, not an
offset inside the stream.

## Pointer timeline

The bottom timeline previews an absolute time on press/drag and submits one
seek on release, retaining pause state. UI owns geometry and gesture capture;
app validates the track identity and invokes Player.Seek. Cancellation or a
track replacement discards the preview. Live radio and unknown durations have
no interactive timeline. See PLAN-POINTER-INPUT.md for the panel contract.
