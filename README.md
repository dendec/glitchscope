# PMV — Portable Music Visualizer

**Audio player with MilkDrop-compatible real-time visualization for game consoles and desktop.**

Renders MilkDrop preset visualizations (projectM 4.x) over OpenGL while playing audio files (MP3, FLAC, WAV, Ogg, etc.) and tracker music (MOD, XM, IT, S3M, PT3, YM, etc.). Supports keyboard and gamepad input. Designed for low-power ARM handhelds (PortMaster) but runs on any Linux/Windows desktop.

## Features

- **Audio Playback** — Plays WAV, MP3, FLAC, Ogg Vorbis, Opus, AAC, WMA, APE, WavPack, Musepack, Speex, and more via FFmpeg & SoLoud
- **Tracker & Chip Music** — Full support for MOD, XM, IT, S3M (libopenmpt / libxmp), PT3 (pt3player), VTX (ayumi), YM (StSound), SID (cRSID), and console audio formats (libgme)
- **MilkDrop Visualizations** — Real-time rendering powered by projectM 4.x with 100+ embedded `.milk` presets and compressed `.pmv` preset archives
- **Graphics Settings** — Custom render resolution scaling (e.g., 320x240, 480x360, 640x480) and upscale filters (Smooth/Pixel)
- **Playback Modes** — Shuffle (Album, Local, All) and Repeat (Off, Repeat One, Repeat All)
- **Preset Auto-Switch** — Configurable timer-based preset rotation (Off, 15s, 30s, 60s, 2m)
- **UI & Themes** — Clean 2-column interface with Dark/Light themes and customizable overlay transparency
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
# Output: dist/pmv.zip
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
./dist/linux-amd64/pmv -music /path/to/your/music/
```

## Configuration

Settings are automatically saved to `config.json` next to the binary:

```json
{
  "graphics": {
    "render_width": 320,
    "render_height": 240,
    "upscale_filter": "pixel"
  },
  "playback": {
    "shuffle_mode": 0,
    "repeat": 0
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
