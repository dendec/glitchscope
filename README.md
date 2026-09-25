# GlitchScope

**Bring MilkDrop visuals back to your music.**

GlitchScope is an open-source music player for Linux and Windows desktops and
low-power PortMaster handhelds. Play a local library, classic tracker and chip
music, or internet radio with real-time MilkDrop-compatible visuals powered by
projectM 4.

![GlitchScope running on a handheld](portmaster/screenshot.png)

## What you can do

- **Watch your music.** Browse MilkDrop presets, preview them before switching,
  and set automatic rotation. Graphics options include adaptive resolution and
  frame-rate limits for slower devices.
- **Play more than standard audio.** FFmpeg handles common formats such as MP3,
  FLAC, WAV, Ogg Vorbis, Opus, and AAC. Tracker and chip support includes MOD,
  XM, IT, S3M, PT3, VTX, YM, SID, and console music formats.
- **Explore music online.** Browse Modland and ModArchive catalogs or find
  stations through Radio Browser. Save tracks to three favorites playlists;
  local music and cached downloads remain available offline.
- **Use the controls that suit your device.** The interface supports gamepads,
  keyboards, mice, and touch. It includes 64 color themes and 15 UI languages.
- **Listen without the visualizer.** Turn visualization off to save power while
  keeping audio playback and library controls available.

Radio plays MP3/AAC streams and basic unencrypted HLS. See the
[radio compatibility notes](docs/RADIO-STREAMING.md) for unsupported HLS
features and other limits.

## Data and asset sources

- MilkDrop presets: [projectM Cream of the Crop](https://github.com/projectM-visualizer/presets-cream-of-the-crop).
- Preset textures: [projectM Milkdrop Texture Pack](https://github.com/projectM-visualizer/presets-milkdrop-texture-pack).
- Tracker and chip music catalogs: [Modland](https://modland.com/) (the build
  uses its [`allmods.zip` listing](https://modland.antarctica.no/allmods.zip))
  and [The Mod Archive](https://modarchive.org/) (the app browses the
  [module mirror](http://modarchive.textfiles.com/)).
- Internet radio directory: [Radio Browser](https://www.radio-browser.info/).

Preset and texture collections retain their upstream terms. See the
[release checklist](docs/PORTMASTER-RELEASE.md) for their redistribution status.

## Build and run

### Linux desktop (AMD64)

Requirements: Git with submodule support, Docker, Make, Python 3, and an internet
connection for the first build. Docker builds the native audio and graphics
dependencies; the packaging step also downloads the preset, texture, and online
catalog data it needs.

```sh
git clone --recurse-submodules https://github.com/dendec/glitchscope.git
cd glitchscope
make dist
```

The package is created in `dist/linux-amd64/`. Put music in the `music/`
directory beside the executable and launch `./dist/linux-amd64/glitchscope`,
or start with one track:

```sh
./dist/linux-amd64/glitchscope -file /path/to/track.flac
```

### Other release packages

```sh
make dist-windows    # Windows AMD64 package
make dist-portmaster # PortMaster ARM64 zip and submission directory
make release-archives VERSION=1.0 # Versioned archives for all supported targets
```

`make release-archives` builds Linux AMD64, Linux ARM64, Windows AMD64, and
PortMaster packages, then writes versioned assets to `dist/releases/`. Linux
archives use `.tar.gz`; Windows and PortMaster use `.zip`. The Linux ARM64
archive is a regular Linux package with its own launcher; the PortMaster ZIP is
prepared separately by the PortMaster packager. `SHA256SUMS` contains checksums
for all four release archives. Desktop archives include the collected
third-party licenses and omit local music, settings, favorites, and the local
track cache.

For PortMaster packaging, device checks, and asset redistribution notes, see
[the release checklist](docs/PORTMASTER-RELEASE.md).

### Development checks

```sh
make lint
make test
```

Both commands run in the Docker builder so they use the project's native
dependencies. More commands and repository conventions are in [AGENTS.md](AGENTS.md).

## Controls and settings

Press **Start** on a handheld or **Tab** on a keyboard to open the menu. The
footer shows the controls available on the current screen; the Help page covers
playback and navigation. **Ctrl+F** toggles borderless fullscreen on desktop.

Settings and favorites are saved as `settings.json` and `favorites.json`. When
`XDG_DATA_HOME` is set, the files are stored there; otherwise they are written
to the current working directory.

## Documentation

- [Project documentation index](docs/README.md)
- [Architecture and ownership](docs/ARCHITECTURE.md)
- [PortMaster release checklist](docs/PORTMASTER-RELEASE.md)
- [Seeking and tracker playback design](docs/SEEK-DESIGN.md)

## License

GlitchScope is licensed under the GNU GPL, version 2 or later; see [LICENSE](LICENSE).
Third-party component notices are in
[portmaster/licenses/THIRD_PARTY_LICENSES.md](portmaster/licenses/THIRD_PARTY_LICENSES.md).
