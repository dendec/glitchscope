# PortMaster release gate

`make dist-portmaster` builds an ARM64 release, stages the PortMaster-New
submission directory under `dist/portmaster-submit/ports/glitchscope/`, and
creates `dist/glitchscope.zip`. Packaging fails on missing required inputs.
No third-party music is downloaded or bundled during packaging.

## Remaining external verification

- `portmaster/screenshot.png` is now a 640x480 English capture of the player,
  local music list and visualization. Keep this composition when refreshing
  the image on the device.
- Review bundled preset/texture redistribution with their maintainers and
  PortMaster. The preset repository's LICENSE.md describes assumed permission,
  not explicit grants from every author; the texture README is not a license.
  Copies of both notices are packaged; this does not resolve permission.
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
