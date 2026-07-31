# Spectrum Emulator Backend

## Purpose

This is a deferred research project for broad ZX Spectrum music coverage. It
is separate from the direct format integrations in `pmv` and should only be
embedded after a small standalone prototype is proven.

The goal is to play Spectrum music by executing the original loader/player
code in a Spectrum emulator, intercepting writes to the AY-3-8910/YM2149 chip,
and rendering those register changes through `ayumi`.

```text
Spectrum file or snapshot
        -> loader and player routine
        -> Z80/Spectrum emulator
        -> AY/YM register-write events
        -> ayumi chip renderer
        -> PCM stream
```

## Why This Helps

The tracker formats do not share one parser, but their players ultimately
program the same AY/YM chip. An emulator backend can therefore support many
formats without implementing a native parser for every extension:

- PT2/PT3
- STC/STP
- ASC
- SQT
- VT2
- AY containers, snapshots, and tape images where a suitable player is present

This is not a universal file decoder automatically. A bare module still needs
a compatible player routine or loader. A snapshot, tape image, or AY container
may already include enough information to start one.

## Audio Boundary

The emulator must expose AY writes as timed events rather than only returning
one register state per video frame:

```c
typedef struct {
    uint64_t cpu_cycles;
    uint8_t reg;
    uint8_t value;
} spectrum_ay_event;
```

The first prototype may execute one music tick at a time and emit the final
register state for each 50 Hz tick. The accurate implementation should retain
CPU-cycle timestamps so it can preserve rapid register changes, envelope tricks,
and player effects that happen within one frame.

`ayumi` remains the final AY/YM renderer. The backend is responsible for
clocking the emulator and applying register events at the corresponding audio
position.

## Prototype Scope

Start with the smallest useful vertical slice:

1. Select a permissively licensed Z80 core and a 48K Spectrum memory/port model.
2. Load one known PT3 player image and one real PT3 fixture.
3. Place the module at the address expected by that player.
4. Run the Z80 until interrupts or player ticks occur.
5. Intercept AY register selection and data writes.
6. Export an AY event trace and render it to WAV through `ayumi`.
7. Compare its output and timing with the existing direct PT3 player.

Only after this is reproducible should the project add tape loading, snapshots,
128K paging, loop detection, or more player routines.

## What Makes It Difficult

- A module file does not identify the correct player routine by itself.
- Full TAP/TZX loading needs accurate ROM, tape, and timing behavior.
- Different Spectrum models use different memory paging and AY wiring.
- Exact audio requires cycle timing, not just 50 Hz register snapshots.
- Player binaries and loader code have their own copyright and license terms.
- Emulator state, loop detection, seeking, and malformed inputs need explicit
  boundaries for use as a streaming audio decoder.

## Integration Boundary

The standalone prototype should expose a small C API based on the project's
common decoder shape:

```c
SpectrumCodec *spectrum_create(void);
void spectrum_destroy(SpectrumCodec *codec);
int spectrum_load_mem(SpectrumCodec *codec, const unsigned char *data, int size);
int spectrum_read_frames(SpectrumCodec *codec, int frames, int16_t *pcm);
int spectrum_seek_frames(SpectrumCodec *codec, int64_t frame);
```

The eventual SoLoud adapter should know only this API. It should not contain
format-specific parsing or Spectrum CPU details.

## Decision

Keep direct PT3/VTX integrations in `mdpp` while this backend is researched
separately. Defer PT2 and the other Spectrum parser items in plan sections 2b
and 2c until the prototype demonstrates that the emulator, AY event capture,
licensing, and streaming behavior are practical.
