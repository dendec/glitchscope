# GlitchScope

**Audio player with MilkDrop-compatible real-time visualization for game consoles and desktop.**

Renders MilkDrop preset visualizations (projectM 4.x) over OpenGL while playing audio files (MP3, FLAC, WAV, Ogg, etc.) and tracker music (MOD, XM, IT, S3M, PT3, YM, etc.). Supports keyboard, gamepad, mouse, and touch input. Designed for low-power ARM handhelds (PortMaster) but runs on any Linux/Windows desktop.

## Features

- **Audio Playback** — Plays WAV, MP3, FLAC, Ogg Vorbis, Opus, AAC, WMA, APE, WavPack, Musepack, Speex, and more via FFmpeg & SoLoud
- **Tracker & Chip Music** — Full support for MOD, XM, IT, S3M (libopenmpt / libxmp), PT3 (pt3player), VTX (ayumi), YM (StSound), SID (cRSID), and console audio formats (libgme)
- **MilkDrop Visualizations** — Real-time rendering powered by projectM 4.x with 100+ embedded `.milk` presets and compressed `.gsa` preset archives
- **Graphics Settings** — Independent frame-rate cap (including display-bound Max), adaptive-resolution toggle, render-resolution ceiling and upscale filter for output and preset transitions (Bilinear or Nearest neighbor)
- **Playback Modes** — Shuffle (Album, Source, All) and Repeat (Off, Repeat One, Repeat All). Manual Next honors shuffle. Offline Shuffle All selects local music and already-downloaded remote tracks; Shuffle Source is restricted to the cached subset of the selected remote source. Connectivity is rechecked every 30 seconds. Failed loads are skipped, with bounded recovery to avoid an endless loop of broken tracks.
- **Online Module Catalogs** — Browse Modland, yearly ModArchive additions, the 2007 official addendum, and the 1987-2007 ModArchive snapshot; bundled `1980-2007.gsa` and `2007-addendum.gsa` indexes provide offline navigation, while selected tracks are fetched individually with HTTP Range requests
- **Internet Radio** — Browse Radio Browser stations by popularity, random selection, tag, language, or country; station listings are cached locally, MP3/AAC streams use the existing FFmpeg/SoLoud pipeline, and ICY track titles appear in the station panel. Local `.m3u`, `.m3u8`, and `.pls` station playlists are also supported. Basic unencrypted HLS is supported; byte ranges, gaps, discontinuities and changing init segments are rejected explicitly (see [radio review](docs/RADIO-REVIEW.md))
- **Preset Auto-Switch** — Configurable timer-based preset rotation (Off, 15s, 30s, 60s, 2m)
- **UI & Themes** — Clean 2-column interface with nine distinct color themes and customizable overlay transparency
- **Localized UI & Help** — English, Russian, Simplified/Traditional Chinese, Japanese, Korean, Vietnamese, Thai, Indonesian, Malay, Brazilian Portuguese, Spanish, German, French, and Turkish; switch languages in Settings without restarting
- **Gamepad, Keyboard & Pointer** — Full controller mapping optimized for PortMaster handhelds (TrimUI Smart Pro, Anbernic, Miyoo, etc.); mouse and touch taps open the UI, select rows, and scroll lists

## Building

### Linux (AMD64 via Docker)
Building via Docker handles all static C/C++ dependencies (`projectM`, `SoLoud`, `FFmpeg`, `libopenmpt`, `libxmp`, etc.) automatically.
```bash
# Build Docker builder image and AMD64 distribution package
make dist
# Set the version embedded in the binary, window title, and Go HTTP User-Agent.
make VERSION=1.2 dist
# Output: dist/linux-amd64/
```

### Windows (AMD64 via Docker)
Windows uses a separate cross-builder because the Linux builder produces ELF
objects and GLES libraries that cannot be linked into a Windows executable.
```bash
make dist-windows
# Output: dist/windows-amd64/
```
The package contains `glitchscope.exe`, the SDL2 runtime DLL, preset/texture
archives, and offline Modland/ModArchive catalogs. The remaining system DLLs
are provided by Windows itself.

### PortMaster / ARM64 (Cross-build via Docker)
```bash
# Build PortMaster-compatible zip package for ARM64 handhelds
make dist-portmaster
# Output: dist/glitchscope.zip and dist/portmaster-submit/ports/glitchscope/
```

See [PortMaster release checks](docs/PORTMASTER-RELEASE.md) for screenshot,
asset permissions and required device testing before submission.

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
    "adaptive": true,
    "frame_rate": "max",
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
    "transparency": 0,
    "language": "en"
  }
}
```

## License

GPL-2.0+ (See [portmaster/licenses/THIRD_PARTY_LICENSES.md](portmaster/licenses/THIRD_PARTY_LICENSES.md) for third-party component licenses)


## Low-power handheld tuning

- **Tracker seeking** selects its working PCM budget automatically from free
  device memory. Tracks that do not fit stream natively; backward seeking then
  depends on the decoder.
- **Visualizer → Off** plays music without running the main visualizer. The
  presets page still provides previews when opened.
- The visualizer follows the selected frame-rate limit while input remains
  responsive. Adaptive resolution is independent and never exceeds the selected
  resolution ceiling. Preset previews load after a short cursor pause.
- Stable preset resolutions are remembered during the current run (up to 256
  profiles). Frame-rate/adaptive setting changes, window size and changed preset
  content are isolated.

For a device benchmark, run `./glitchscope -benchmark -benchmark-frames=120 -benchmark-out=benchmark-handheld.csv`. CSV contains preset startup
and load time, mean/p95/p99/max frame times and process-lifetime peak RSS. Use a
new CSV path for the new schema; older reports are not overwritten or mixed.
The benchmark uses a synthetic audio signal and does not measure audio dropouts
or UI rendering. Repeat the same run after warming the device to compare sustained
performance. `-v` logs audio decoding time and process peak RSS.


## Finding your way around

On first use, **START — Open menu** (or **TAB** with a keyboard) appears for ten
seconds. Opening the menu dismisses it permanently; missing the hint lets it
reappear next time. Help begins with Quick Start, Controls, Playback, Frame rate and
Battery, and Troubleshooting. Button names match the context hints at the bottom
of the menu. Settings includes short explanations of the selected options.

On desktop, **Ctrl+F** toggles borderless fullscreen; plain **F** keeps its
favorite action.

В настройках визуализации частота кадров, адаптивное разрешение и разрешение
выбираются независимо. Частота предлагает фиксированные значения до частоты
дисплея и пункт «Макс.» с текущей частотой обновления; одинаковые значения не
дублируются. При включённом адаптивном разрешении выбранный размер является
верхним пределом: приложение может понижать и восстанавливать его, чтобы
приблизиться к целевой частоте, но не меняет саму настройку FPS. При выключенной
адаптации разрешение фиксировано. По умолчанию используются «Макс.»,
адаптивное разрешение и максимальный размер экрана.
