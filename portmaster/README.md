## Notes

GlitchScope is a music player with real-time MilkDrop visualizations, tracker
and chip music playback, Internet radio, and Modland/ModArchive browsing.

Thanks to dendec for creating GlitchScope, the projectM and MilkDrop preset
creators for the visualizations, and the SoLoud, FFmpeg and tracker/chip decoder
contributors for the audio engines. Thanks to the PortMaster community for
handheld integration and testing.

Copy your music to `glitchscope/music/`. No demo tracks are bundled. Wi-Fi is
needed for Internet radio and downloading tracks from online catalogs; local
music and previously downloaded tracks can be played offline. Presets and
textures are supplied as archives in `glitchscope/presets/`.

## Runtime requirements

The PortMaster package requires compatible 64-bit ARM custom firmware that
provides glibc 2.36 or newer, SDL2, OpenGL ES 2, ALSA, and working graphics and
audio drivers. Codec and C++ runtime libraries are bundled in
`glitchscope/libs.aarch64/`. Firmware requirements vary by device; check the
runtime requirements included in the package and use the firmware's documented
update process when a system library is missing.

## Controls

Use the button labels shown in the menu footer; face-button labels can vary
with the device/controller layout.

| Button | Action |
| --- | --- |
| Start | Open/close menu |
| Select | Toggle information overlay |
| D-pad | Navigate menu |
| Confirm / Back (see footer) | Open selection / go back |
| L1 / R1 | Previous / next preset |
| Start + Select | Exit through PortMaster |

Help in the menu documents playback, seeking, favorites and context controls.
Settings includes language, frame rate and adaptive resolution. Turn the
visualizer off to reduce power use while listening.

## Compile

```sh
git clone --recursive https://github.com/dendec/glitchscope.git
cd glitchscope
make dist-portmaster
```

Requires Docker, Make, Python 3 and binutils on the build host. Output:
`dist/glitchscope.zip`; submission tree:
`dist/portmaster-submit/ports/glitchscope/`.

## Licenses

GlitchScope is GPL-2.0-or-later. Dependency notices are in `glitchscope/licenses/`.
Sources and build recipes: https://github.com/dendec/glitchscope
