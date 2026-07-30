# Rockbox Codecs Integration Plan (v2)

## Overview

Integration of missing audio codecs into mdpp player.
Target: universal audio player with widest possible format support.

**License:** GPL-2.0+ for our code. All imported libraries retain their original licenses.

---

## Corrections from v1

### 1. libgme: Use Upstream, Not Rockbox

Rockbox's `lib/rbcodec/codecs/libgme/gme.h` is a stub (only `gme_err_t` typedef).
The actual API is in upstream https://github.com/libgme/game-music-emu.

**Action:** Clone upstream libgme directly, not from Rockbox.

### 2. libayumi: Chip Emulation Only, No File Loading

libayumi API is `ayumi_set_tone`, `ayumi_set_noise`, `ayumi_process` — chip emulation.
`AyumiRender_LoadFile` only loads VTX format. No PT2/PT3 parsers.

**Action:**
- Use libayumi for VTX files only
- Find separate PT2/PT3 parsers (e.g., https://github.com/alexanderbautro/pt2-player, https://github.com/alexanderbautro/pt3-player)
- Or implement parsers ourselves based on format specs

### 3. cRSID: Global State, Sample-by-Sample API

cRSID uses global `cRSID_C64` instance and `cRSID_generateSample()` (one sample at a time).
No multi-instance support. Thread safety concern.

**Action:**
- Wrap with mutex (only one SID decoder at a time)
- Buffer samples internally (generate 1024 at a time)
- Support subtunes via `cRSID_initSIDtune`

### 4. AAC/M4A: Separate Container from Codec

- `libfaad` decodes AAC (ADTS or raw)
- `libm4a` demuxes MP4/M4A container
- Need to handle: ADTS AAC, AAC-in-M4A, ALAC-in-M4A, multiple tracks
- DRM-protected files will not be supported

**Action:**
- Two-stage pipeline: M4A demux → AAC decode
- Skip DRM-protected files (detect via atom inspection)

### 5. WMA: Need ASF Demuxer

WMA decoding requires:
- `libasf` for ASF/WMA demuxing
- `libwma` for WMA decoding
- `libwmapro` for WMA Pro
- `libwmavoice` for WMA Voice

**Action:**
- Include libasf for container demuxing
- Three separate decoders behind common interface

### 6. Unity Build: C/C++ Compatibility Concerns

Current `soloud_build.cpp` is C++. Adding C sources may cause:
- Global symbol conflicts
- `static` variable duplication
- Missing Rockbox runtime functions (`ci->*`)
- Platform-specific macros (`IBSS_ATTR`, etc.)

**Action:**
- Create separate `codecs_build.c` for pure C codecs
- Use `extern "C"` wrappers where needed
- Strip Rockbox-specific macros (`IBSS_ATTR` → empty)
- Provide stubs for missing Rockbox runtime functions

### 7. License: Preserve Individual Licenses

| Library | License |
|---------|---------|
| libgme | LGPL-2.1 (GPL if using MAME YM2612) |
| libayumi | MIT |
| cRSID | GPL-2.0+ |
| libfaad | GPL-2.0+ (non-GPL usage forbidden) |
| libm4a | LGPL |
| libwma | LGPL (needs verification) |
| libopus | BSD-3-Clause |
| libwavpack | BSD-3-Clause |
| demac | GPL-2.0+ |
| libtta | GPL-2.0+ |
| libmusepack | BSD-3-Clause |
| libspeex | BSD-3-Clause |
| liba52 | GPL-2.0+ |
| libalac | LGPL-2.1 |

**Action:**
- Create `THIRD_PARTY_LICENSES.md` with all license texts
- Preserve copyright notices in source files
- Our code: GPL-2.0+

### 8. File Counts: Reproducible Data

Source: `dist/allmods.zip` → `allmods.txt` (514656 entries)

```
libgme formats:        61,049 files
  .nsf: 5,015  .spc: 36,903  .vgz: 14,169
  .sap: 3,230  .gbs: 918  .hes: 421
  .kss: 393

Spectrum formats:      22,073 files
  .pt3: 6,868  .pt2: 6,284  .stc: 3,639
  .asc: 1,706  .sqt: 884  .vtx: 878
  .stp: 632  .stp2: 632  .vt2: 550

SID formats:           64,174 files
  .sid: 60,634  .rsid: 3,540
```

---

## Architecture: Streaming PCM Decoder Pattern

The common codec API is an internal decoder API. It is not exposed directly as
a SoLoud `AudioSource`. A separate SoLoud adapter owns the codec instance and
connects `getAudio()` and `seek()` to the decoder. This keeps `Player` unaware
of codec-specific details and allows all new codecs to share one playback path.

```text
Player
  -> SoLoud CodecSource / CodecSourceInstance
    -> common codec interface
      -> libgme / cRSID / ayumi / ...
```

The adapter must support both native seek and a fallback seek. The fallback
resets the decoder and decodes-and-discards frames until the requested
position. This is acceptable for formats without an efficient or exact seek.

### Common Codec Interface

```c
// codec_interface.h
#include <stdint.h>

typedef struct Codec Codec;

// Lifecycle
Codec* codec_create(void);
void   codec_destroy(Codec* c);

// Loading
int codec_load_mem(Codec* c, const unsigned char* data, int size);

// Playback
// All positions and counts are interleaved PCM sample frames, not individual
// channel samples. For stereo, one frame contains two int16 values.
int  codec_read_frames(Codec* c, int frames, int16_t* interleaved);
int  codec_seek_frames(Codec* c, int64_t frame);
int64_t codec_tell_frames(Codec* c);
int64_t codec_length_frames(Codec* c);
int  codec_sample_rate(Codec* c);
int  codec_channels(Codec* c);

// Metadata
const char* codec_title(Codec* c);
const char* codec_author(Codec* c);
int codec_track_count(Codec* c);
int codec_current_track(Codec* c);
```

### Go Wrapper Pattern

```go
// codec.go
type Codec interface {
    Destroy()
    ReadFrames(frames int, buffer []int16) int
    SeekFrames(frame int64) error
    TellFrames() int64
    LengthFrames() int64
    SampleRate() int
    Channels() int
    Title() string
    Author() string
}
```

---

## Phase 1: libgme (Game Music Emu)

**Source:** https://github.com/libgme/game-music-emu (upstream)
**License:** LGPL-2.1
**Formats:** AY, NSF, NSFE, SPC, GBS, HES, KSS, SAP, VGM, VGZ
**File count:** 61,049

The AY format is libgme's ZX Spectrum emulator format (`ZXAYEMUL`). It is
separate from VTX: `.ay` files use libgme, while `.vtx` files use the existing
ayumi-based adapter.

### Files
Clone entire `gme/` directory from upstream.

### CGO Bridge
```c
// bridge_gme.h
#include "gme.h"

typedef struct {
    Music_Emu* emu;
    int sample_rate;
} GmeCodec;

GmeCodec* GmeCodec_create(int sample_rate);
void GmeCodec_destroy(GmeCodec* c);
int GmeCodec_load(GmeCodec* c, const unsigned char* data, long size);
int GmeCodec_play(GmeCodec* c, int samples, short* buffer);
void GmeCodec_seek(GmeCodec* c, int msec);
int GmeCodec_tell(GmeCodec* c);
int GmeCodec_length(GmeCodec* c);
int GmeCodec_track_count(GmeCodec* c);
int GmeCodec_set_track(GmeCodec* c, int track);
const char* GmeCodec_title(GmeCodec* c);
const char* GmeCodec_author(GmeCodec* c);
```

### Build
- Clone the complete upstream source tree to `lib/game-music-emu/`
- Build the complete upstream source list; `gme.cpp` alone is not assumed to
  contain all emulator implementations
- Keep libgme C++ sources in a dedicated static library built by upstream CMake;
  do not add them to the pure-C or SoLoud unity builds
- Add `-I/lib/game-music-emu/gme` to the relevant C++ flags

---

## Phase 2: Spectrum Formats

### 2a. VTX via libayumi

**Source:** https://github.com/true-grue/ayumi
**License:** MIT
**Integration:** Git submodule at `lib/ayumi/`

Upstream ayumi provides AY-3-8910/YM2149 chip emulation through
`ayumi_configure` and `ayumi_process`; it does not provide a VTX file loader.
Implement the VTX container parser, LZH unpacking, frame timing, metadata, and
seek layer in the project, then drive the submodule's chip renderer from that
adapter. The `AyumiRender_*` API belongs to Rockbox's codec wrapper and is only
a reference for the behavior to reproduce, not an upstream ayumi API. The
initial adapter may accept uncompressed register data first; LZH support must
be added before considering VTX coverage complete.

### 2b. YM/LHARC via libstsound

**Source:** https://github.com/cpcsdk/libstsound
**License:** BSD-2-Clause
**Integration:** Git submodule at `lib/libstsound/`

libstsound provides a complete YM2/YM3/YM4/YM5/YM6 player, metadata, duration,
seek, loop handling, AY emulation, and LHA/LHARC depacking. The `mdpp` adapter
uses its memory-loading API and PCM renderer, so `.ym`, `.lh`, and `.lha` files
share one implementation without a separate LHA parser. Its built-in emulator
is tuned for AY-3-8912/Amstrad CPC behavior; VTX and PT3 therefore continue to
use the existing ayumi-based paths.

### 2c. PT2/PT3 Parsers

**File count:** .pt2: 6,284, .pt3: 6,868

PT3 currently uses the MIT-licensed `https://github.com/Volutar/pt3player`
core as a Git submodule at `lib/pt3player/`. Its parser/player state is global,
so the adapter serializes decoder operations and supports one active PT3 voice.
The renderer reuses the project `lib/ayumi` static library rather than the
upstream demo's private ayumi copy. Seek is implemented by restart and replay.

PT2 and the remaining format-specific parser work are intentionally deferred.
Rather than adding one player core per extension, the proposed long-term path
is a separate Spectrum emulator backend that runs a compatible loader/player,
intercepts timed AY/YM register writes, and renders them through `ayumi`.
See [Spectrum Emulator Backend](spectrum-emulator-backend.md) for the
architecture, prototype scope, and the reasons this is not being embedded in
the main player yet.

**Action:** Keep direct PT3/VTX integrations stable and prototype the emulator
backend separately before adding PT2 support.

### 2d. Other Spectrum Formats

| Format | Files | Parser needed |
|--------|-------|---------------|
| .stc | 3,639 | ST Song Compiler parser |
| .asc | 1,706 | ASC Sound Master parser |
| .sqt | 880 | SQ Tracker parser |
| .stp | 632 | Sound Tracker Pro parser |
| .vt2 | 550 | Vortex Tracker II parser |

**Action:** Defer these format-specific parsers pending the
[Spectrum Emulator Backend](spectrum-emulator-backend.md). This avoids
committing to a separate parser/player integration for every AY tracker
extension before a shared execution path has been evaluated.

---

## Phase 3: SID (cRSID)

**Source:** Rockbox `lib/rbcodec/codecs/cRSID/`
**License:** GPL-2.0+
**File count:** 64,174

### Challenges
- Global state (`cRSID_C64`)
- Sample-by-sample API
- No multi-instance support

### Solution
```c
// bridge_sid.h
#include "libcRSID.h"

typedef struct {
    cRSID_SIDheader* header;
    int sample_rate;
    int buffer_pos;
    short buffer[1024]; // internal buffer
} SidCodec;

// Mutex for global state
extern pthread_mutex_t sid_mutex;

SidCodec* SidCodec_create(int sample_rate);
void SidCodec_destroy(SidCodec* c);
int SidCodec_load(SidCodec* c, const unsigned char* data, int size);
int SidCodec_play(SidCodec* c, int samples, short* buffer);
void SidCodec_seek(SidCodec* c, int sample);
int SidCodec_subtune_count(SidCodec* c);
int SidCodec_set_subtune(SidCodec* c, int tune);
const char* SidCodec_title(SidCodec* c);
const char* SidCodec_author(SidCodec* c);
```

### Build
- Copy cRSID from Rockbox to `lib/cRSID/`
- Remove Rockbox-specific code (`#ifdef CRSID_PLATFORM_PC`)
- Add stubs for missing functions
- Wrap every operation touching the global cRSID state with a mutex
- Support only one active SID decoder at a time; the internal PCM buffer does
  not make cRSID multi-instance safe

---

## Phase 4: AAC/M4A

**Source:** Rockbox `lib/rbcodec/codecs/libfaad/`, `libm4a/`
**License:** GPL-2.0+ (libfaad), LGPL (libm4a)

### Pipeline
```
M4A file → libm4a (demux) → AAC frames → libfaad (decode) → PCM
ADTS file → libfaad (decode) → PCM
```

### Build
- Copy libfaad and libm4a from Rockbox
- Create bridge_aac.c with two-stage pipeline
- Handle both ADTS and M4A container

---

## Phase 5: WMA

**Source:** Rockbox `lib/rbcodec/codecs/libwma/`, `libwmapro/`, `libwmavoice/`, `libasf/`
**License:** LGPL (needs verification)

### Pipeline
```
WMA file → libasf (demux) → WMA frames → libwma (decode) → PCM
WMA Pro → libwmapro (decode) → PCM
WMA Voice → libwmavoice (decode) → PCM
```

### Build
- Copy all WMA libraries + libasf from Rockbox
- Create bridge_wma.c with ASF demux + decode

---

## Phase 6: Opus

**Source:** https://github.com/xiph/opus (upstream)
**License:** BSD-3-Clause

### Build
- Clone upstream opus to `lib/opus/`
- Standard CMake build
- Create bridge_opus.c wrapper

---

## Phase 7: Lossless Codecs

### 7a. WavPack
**Source:** https://github.com/dbry/WavPack (upstream)
**License:** BSD-3-Clause

### 7b. TTA
**Source:** Rockbox `lib/rbcodec/codecs/libtta/`
**License:** GPL-2.0+

### 7c. APE
**Source:** Rockbox `lib/rbcodec/codecs/demac/`
**License:** GPL-2.0+

---

## Phase 8: Other Codecs

| Codec | Source | License |
|-------|--------|---------|
| Musepack | Rockbox or upstream | BSD-3-Clause |
| Speex | https://github.com/xiph/speex | BSD-3-Clause |
| AC3 | Rockbox `lib/rbcodec/codecs/liba52/` | GPL-2.0+ |
| ALAC | Rockbox `lib/rbcodec/codecs/libalac/` | LGPL-2.1 |

---

## Build System

### File Structure
```
lib/
├── game-music-emu/     # upstream libgme
├── ayumi/              # upstream libayumi
├── cRSID/              # from Rockbox
├── libfaad/            # from Rockbox
├── libm4a/             # from Rockbox
├── libwma/             # from Rockbox
├── libwmapro/          # from Rockbox
├── libwmavoice/        # from Rockbox
├── libasf/             # from Rockbox
├── opus/               # upstream
├── wavpack/            # upstream
├── libtta/             # from Rockbox
├── demac/              # from Rockbox
├── libmusepack/        # from Rockbox
├── libspeex/           # from upstream
├── liba52/             # from Rockbox
└── libalac/            # from Rockbox
```

### Build Files
```
internal/soloud/
├── soloud_build.cpp        # SoLoud C++ unity build (existing)
├── gme_source.cpp/h         # SoLoud adapter; libgme is a separate static library
├── ayumi_source.cpp/h       # VTX SoLoud adapter; libayumi is a separate static library
├── bridge_sid.c/h          # cRSID wrapper
├── bridge_aac.c/h          # libfaad + libm4a wrapper
├── bridge_wma.c/h          # libwma + libasf wrapper
├── bridge_opus.c/h         # libopus wrapper
├── bridge_wavpack.c/h      # libwavpack wrapper
├── bridge_tta.c/h          # libtta wrapper
├── bridge_ape.c/h          # demac wrapper
└── ... (other bridges)
```

Third-party C libraries are compiled as separate static libraries by
`Dockerfile.builder` and linked through the target-specific CGO flags. If
Rockbox sources have conflicting file-local symbols or incompatible runtime
macros, they must be compiled as separate translation units instead of being
included in one unity file. `extern "C"` is used only at C/C++ ABI boundaries;
it does not resolve conflicts inside a C unity build.

### Makefile Changes
```makefile
# Existing C++ build
CGO_CXXFLAGS += -I$(PWD)/lib/soloud/include

# New static codec libraries are built by Dockerfile.builder.
CGO_LDFLAGS += /opt/ayumi/amd64/lib/libayumi.a
CGO_CFLAGS += -I$(PWD)/lib/cRSID
CGO_CFLAGS += -I$(PWD)/lib/libfaad
CGO_CFLAGS += -I$(PWD)/lib/libm4a
CGO_CFLAGS += -I$(PWD)/lib/libwma
CGO_CFLAGS += -I$(PWD)/lib/libasf
CGO_CFLAGS += -I$(PWD)/lib/opus/include
CGO_CFLAGS += -I$(PWD)/lib/wavpack/include
CGO_CFLAGS += -I$(PWD)/lib/libtta
CGO_CFLAGS += -I$(PWD)/lib/demac
```

---

## Implementation Order

There are eight product phases and thirteen codec families. The table below
contains fourteen implementation work items because PT2/PT3 parsers are shown
separately as a sub-stage inside the Spectrum phase.

| Priority | Phase | Formats | Files | Effort |
|----------|-------|---------|-------|--------|
| 1 | libgme | NSF, NSFE, SPC, GBS, HES, KSS, SAP, VGM, VGZ | 61,049 | Medium |
| 2 | cRSID | SID, RSID | 64,174 | Medium |
| 3 | libayumi (VTX) | VTX | 878 | Low |
| 4 | PT2/PT3 parsers | PT2, PT3 | 13,152 | Medium |
| 5 | libfaad + libm4a | AAC, M4A | ~0 (modland) | Medium |
| 6 | libopus | Opus | ~0 | Low |
| 7 | libwma + libasf | WMA | ~0 | Medium |
| 8 | libwavpack | WavPack | ~0 | Low |
| 9 | demac | APE | ~0 | Low |
| 10 | libtta | TTA | ~0 | Low |
| 11 | libmusepack | Musepack | ~0 | Low |
| 12 | libspeex | Speex | ~0 | Low |
| 13 | liba52 | AC3 | ~0 | Medium |
| 14 | libalac | ALAC | ~0 | Low |

---

## Risk Assessment

| Risk | Impact | Mitigation |
|------|--------|------------|
| cRSID global state | Only one SID at a time | Mutex wrapper |
| PT2/PT3 parser availability | Delayed Spectrum support | Find/use existing parsers |
| Rockbox runtime dependencies | Build failures | Stub missing functions |
| C/C++ symbol conflicts | Linker errors | Separate build files |
| License compliance | Legal issues | Preserve all notices |
| DRM-protected M4A | Expected failures | Detect and skip |

---

## Summary

| Metric | Current | Target |
|--------|---------|--------|
| Format count | ~64 | ~90+ |
| Modland coverage | ~200K | ~340K+ |
| Game music | None | NSF, SPC, GBS, HES, KSS, SGC, SAP, VGM |
| Spectrum | AY only | VTX, PT2, PT3 |
| C64 | None | SID, RSID |
| Modern audio | WAV, OGG, MP3, FLAC | + AAC, M4A, Opus, WMA |
| Lossless | FLAC | + WavPack, TTA, APE, ALAC |

**Estimated effort:** 3-4 weeks for full integration.
**Critical path:** libgme → cRSID → PT2/PT3 parsers.
