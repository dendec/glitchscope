# Codec Integration Status

This document tracks the codec decisions for `mdpp`. It supersedes the
original Rockbox import plan: specialized music formats use dedicated
adapters, while ordinary audio formats use the audio-only FFmpeg build.

**License:** The project and imported components retain their respective
licenses. The FFmpeg build disables GPL and nonfree components.

## Current Decisions

| Area | Decision | Status | Remaining work or reason |
|------|----------|--------|--------------------------|
| libgme | Do | Complete | Upstream libgme adapter supports AY, NSF, NSFE, SPC, GBS, HES, KSS, SAP, VGM, and VGZ. |
| cRSID | Do | Complete | SID/RSID adapter, global-state mutex, buffering, metadata, and subtunes are implemented. |
| VTX / ayumi | Do | Partial | Raw-register VTX works; LZH-compressed VTX payloads are not supported yet. |
| YM / LHARC / StSound | Do | Complete | `.ym`, `.lh`, and `.lha` use the existing StSound adapter. |
| PT3 | Do | Complete | The direct `pt3player` adapter is implemented; seeking restarts and replays. |
| PT2, STC, ASC, SQT, STP, VT2 | Not now | Deferred | Wait for the Spectrum emulator prototype instead of adding one parser per extension. |
| Spectrum emulator backend | Maybe later | Research only | Build and validate the standalone Z80/AY event prototype before integrating it into the player. |
| AAC, M4A, ALAC | Do | Complete via FFmpeg | The planned Rockbox `libfaad`/`libm4a` route was abandoned. |
| WMA / ASF | Do | Complete via FFmpeg | The planned Rockbox `libwma`/`libasf` route was abandoned. |
| Opus | Do | Complete via FFmpeg | A separate libopus wrapper is unnecessary for the current player. |
| AC3 / E-AC3 | Do | Complete via FFmpeg | The planned Rockbox `liba52` route was replaced by FFmpeg decoders. |
| TTA | Do | Complete via FFmpeg | The planned Rockbox `libtta` route was replaced by FFmpeg. |
| APE | Do | Complete via FFmpeg | The planned Rockbox `demac` route was replaced by FFmpeg. |
| WavPack | Do | Complete via FFmpeg | `.wv` uses the built-in WavPack decoder and raw WavPack demuxer. |
| Musepack | Do | FFmpeg enabled | `.mpc` routing and decoders are enabled; a local encoder/fixture is still needed for regression coverage. |
| Speex | Do | Complete via FFmpeg | `.spx` is exclusively routed to FFmpeg's Speex decoder through the Ogg demuxer. |
| Generic Rockbox Codec API | No | Replaced | Specialized SoLoud adapters plus FFmpeg already cover the current needs; a second generic C/Go API would add complexity without a current consumer. |

## Explicitly Out Of Scope

These Rockbox components are not part of the current project and must not be
added as cleanup work:

`libfaad`, `libm4a`, `libwma`, `libwmapro`, `libwmavoice`, `libasf`, `libtta`,
`demac`, `liba52`, and `libalac`.

Their ordinary-audio use cases are covered by FFmpeg, avoiding duplicate
decoders and the maintenance and licensing cost of importing the Rockbox
copies. The old `libfaad` working-tree artifact was removed.

## Current Architecture

```text
Player
  -> specialized SoLoud source
    -> libgme / cRSID / ayumi / StSound / pt3player

Player
  -> FfmpegSource
    -> FFmpeg demuxer and decoder
    -> libswresample
    -> stereo planar float PCM
```

FFmpeg is built as a pinned audio-only submodule in `lib/ffmpeg`. The build
enables the required demuxers, parsers, decoders, and `libswresample`, while
excluding video, encoders, filters, network, GPL, and nonfree components.

The current specialized adapters are:

- `internal/soloud/gme_source.cpp` for libgme;
- `internal/soloud/sid_source.cpp` and `bridge_sid.c` for cRSID;
- `internal/soloud/ayumi_source.cpp` for VTX;
- the StSound source for YM/LHARC;
- `internal/soloud/pt3_source.cpp` for PT3;
- `internal/soloud/ffmpeg_source.cpp` for ordinary audio formats.

## Remaining Work

### LZH-compressed VTX

**Decision:** Do later.

The current VTX adapter accepts raw register data. To complete VTX coverage it
needs an LZH unpacker, validation of compressed metadata, and fixtures for both
compressed and uncompressed files. This is isolated from the FFmpeg work.

### Spectrum Emulator Backend

**Decision:** Research only, not a current player feature.

The proposed backend is described in
[spectrum-emulator-backend.md](spectrum-emulator-backend.md). Before adding
PT2, STC, ASC, SQT, STP, or VT2, it needs a standalone prototype that can:

1. run a permissively licensed Z80 core with a 48K Spectrum model;
2. load one known PT3 player and module;
3. capture timed AY register writes;
4. render the events through ayumi and compare them with the direct PT3 path;
5. demonstrate acceptable seeking, looping, malformed-input handling, and
   licensing boundaries.

Until that prototype exists, adding individual parsers is deliberately
deferred.

## Validation Expectations

Every new decoder route should have:

- at least one fixture readable by `ffprobe` or the relevant native parser;
- playback verification through the `-file` command-line option;
- correct duration and channel metadata in the library;
- cleanup verification for asynchronous and stale loads;
- license and build-matrix review.

The FFmpeg integration currently has 30 fixtures and has been validated with
`ffprobe`, metadata duration checks, and `make dist`. Musepack routing is built
and enabled, but still lacks a local regression fixture because the available
FFmpeg encoder cannot produce Musepack files.
