# PortMaster release gate

`make dist-portmaster` builds an ARM64 release, stages the PortMaster-New
submission directory under `dist/portmaster-submit/ports/glitchscope/`, and
creates `dist/glitchscope.zip`. Packaging fails on missing required inputs.
No third-party music or preset collections are bundled during packaging.
Users can download optional preset collections from the Presets page, including
En D, MilkDrop Original and projectM Classic from the projectM repositories.
These repository collections download the MilkDrop texture pack during installation.
Butterchurn is optional and downloads its official repository ZIP, retaining
only `presets/milkdrop/` plus the standard texture pack and upstream notices.
MilkDrop2077 is also optional: its `PRESETS.RES` is converted to a managed ZIP
with the same texture pack. The resource file and resulting ZIP are not bundled.
Release archives must contain an empty `presets/` directory only: never
include downloaded ZIP collections, extracted texture caches, or legacy
`presets.gsa` / `textures.gsa` archives. ModArchive catalog snapshots remain
separate GSA assets under `.cache/modarchive/`.

## Remaining external verification

- MilkDrop2077's 300 resources were checked for ZIP conversion and store reads;
  rendering still needs device verification. Five source presets reference
  `grad3` or `rose`, absent from the standard texture pack; projectM uses its
  placeholder for missing textures, so their appearance may differ.
- `portmaster/screenshot.png` is now a 640x480 English capture of the player,
  local music list and visualization. Keep this composition when refreshing
  the image on the device.
- The preset author authorized direct, optional downloads for Cream of the Crop,
  Isosceles Mashups 2020, and Isosceles Mashups 2024. Keep attribution and this
  distinction from bundled GPL application code in the third-party notice.
- Do not add Spout Jamming until its compatibility and availability are reviewed.
- Publish corresponding release sources and dependency versions/build recipes
  alongside the binary. Font and Device-Info notices are included explicitly.
- Inspect `glitchscope/runtime-requirements.txt` in the staged directory after each build.
  The verified Bookworm package needs glibc 2.36 or newer.
  `min_glibc` documents the known floor, not universal CFW compatibility.
  SDL2, GLES and ALSA must be available on the target. Codec libraries, zlib,
  libstdc++ and libgcc are bundled in `libs.aarch64/`; packaging derives the
  glibc floor from the executable and these libraries.
  For older CFWs, rebuild with a compatible toolchain/sysroot; do not bundle
  glibc or GPU drivers to bypass this.
- Test startup, controller mapping, exit, local audio, radio, Wi-Fi loss,
  long pause, station switching and sustained visualization on real devices.
  Capture firmware/device/version, resolution and results, including audio.
- Open the required PortMaster Discord #testing-n-dev thread and record its
  link and the CFW/resolution test matrix before submitting a PR.
- Copy the staged directory into PortMaster-New `ports/`, run
  `python3 tools/build_release.py --do-check`, and resolve its findings.
  Use `tools/build_data.py` for large files as required by PortMaster.

The launcher respects the CFW's audio configuration. It no longer writes
hardware-specific mixer IDs or forces ALSA on every device. A future mic routing
fix must be restricted to a verified device/codec and restore changed state.

Packaging reference: https://portmaster.games/packaging.html

## Suggested catalog screenshot

The submitted capture uses the main player/library menu in English, with a
local track playing and `Waveform/Wire Flat/Geiss - Game of Life.milk` as the
visualization. The dark theme keeps the music list, track details and control
footer readable while showing the visualizer behind the overlay. Capture at
640x480 after the preset transition completes; do not add a logo or replace
the visualization with generated artwork. A second optional image can show
the radio list.
