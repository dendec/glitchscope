# GlitchScope

**Audio player with MilkDrop-compatible real-time visualization for game consoles and desktop.**

Renders MilkDrop preset visualizations (projectM 4.x) over OpenGL while playing audio files (MP3, FLAC, WAV, Ogg, etc.) and tracker music (MOD, XM, IT, S3M, PT3, YM, etc.). Supports keyboard and gamepad input. Designed for low-power ARM handhelds (PortMaster) but runs on any Linux/Windows desktop.

## Features

- **Audio Playback** — Plays WAV, MP3, FLAC, Ogg Vorbis, Opus, AAC, WMA, APE, WavPack, Musepack, Speex, and more via FFmpeg & SoLoud
- **Tracker & Chip Music** — Full support for MOD, XM, IT, S3M (libopenmpt / libxmp), PT3 (pt3player), VTX (ayumi), YM (StSound), SID (cRSID), and console audio formats (libgme)
- **MilkDrop Visualizations** — Real-time rendering powered by projectM 4.x with 100+ embedded `.milk` presets and compressed `.gsa` preset archives
- **Graphics Settings** — Custom render resolution scaling (e.g., 320x240, 480x360, 640x480) and upscale filters (Smooth/Pixel)
- **Playback Modes** — Shuffle (Album, Source, All) and Repeat (Off, Repeat One, Repeat All). Manual Next honors shuffle. Offline Shuffle All selects only local music; connectivity is rechecked every 30 seconds. Failed loads are skipped, with bounded recovery to avoid an endless loop of broken tracks.
- **Online Module Catalogs** — Browse Modland, yearly ModArchive additions, the 2007 official addendum, and the 1987-2007 ModArchive snapshot; bundled `1980-2007.gsa` and `2007-addendum.gsa` indexes provide offline navigation, while selected tracks are fetched individually with HTTP Range requests
- **Preset Auto-Switch** — Configurable timer-based preset rotation (Off, 15s, 30s, 60s, 2m)
- **UI & Themes** — Clean 2-column interface with nine distinct color themes and customizable overlay transparency
- **Gamepad & Keyboard** — Full controller mapping optimized for PortMaster handhelds (TrimUI Smart Pro, Anbernic, Miyoo, etc.)

## Building

### Linux (AMD64 via Docker)
Building via Docker handles all static C/C++ dependencies (`projectM`, `SoLoud`, `FFmpeg`, `libopenmpt`, `libxmp`, etc.) automatically.
```bash
# Build Docker builder image and AMD64 distribution package
make dist
# Output: dist/linux-amd64/
```

### PortMaster / ARM64 (Cross-build via Docker)
```bash
# Build PortMaster-compatible zip package for ARM64 handhelds
make dist-portmaster
# Output: dist/glitchscope.zip
```

### Development (Lint & Test)
```bash
# Run golangci-lint inside Docker builder environment
make lint

# Run Go unit tests
make test
```

## Quick Start

```bash
# Run local Docker build
make dist

# Launch the visualizer with a music directory
./dist/linux-amd64/glitchscope -music /path/to/your/music/
```

## Configuration

Settings are automatically saved to `config.json` next to the binary:

```json
{
  "graphics": {
    "render_width": 320,
    "render_height": 240,
    "upscale_filter": "pixel",
    "beat_sensitivity": 1
  },
  "playback": {
    "shuffle_mode": "off",
    "repeat": "off",
    "last_position": {
      "path": "/path/to/track.ogg",
      "seconds": 42.5
    }
  },
  "preset_interval": 30,
  "ui": {
    "theme": "dark",
    "transparency": 0
  }
}
```

## License

GPL-2.0+ (See [portmaster/licenses/THIRD_PARTY_LICENSES.md](portmaster/licenses/THIRD_PARTY_LICENSES.md) for third-party component licenses)


## Low-power handheld tuning

- **Tracker seeking** in Settings selects Exact (256 MiB working PCM budget),
  Low memory (128 MiB), or Streaming. Exact remains the default. Tracks that do
  not fit stream natively; backward seeking then depends on the decoder.
- **Visualizer → Off** plays music without running the main visualizer. The
  presets page still provides previews when opened.
- Balanced/Eco avoid presenting duplicate visualization frames. Input continues
  at 60 Hz. Preset previews load after a short cursor pause.
- Stable preset resolutions are remembered during the current run (up to 256
  profiles). Mode changes, window size and changed preset content are isolated.

For a device benchmark, run `./glitchscope -benchmark -benchmark-frames=120 -benchmark-out=benchmark-handheld.csv`. CSV contains preset startup
and load time, mean/p95/p99/max frame times and process-lifetime peak RSS. Use a
new CSV path for the new schema; older reports are not overwritten or mixed.
The benchmark uses a synthetic audio signal and does not measure audio dropouts
or UI rendering. Repeat the same run after warming the device to compare sustained
performance. `-v` logs audio decoding time and process peak RSS.
