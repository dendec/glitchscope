# Internet radio and stream support

Radio playback is implemented. The network stream is opened by the Go HTTP
layer, passed to FFmpeg through custom I/O, and decoded away from the UI and
SoLoud audio callback. Compressed input is bounded to 2 MiB; decoded audio uses
a preallocated four-second stereo PCM ring. Stream requests are cancellable,
and changing stations or stopping playback detaches and tears down the old
stream off the UI thread.

## HLS support

The player handles master and media playlists, redirects, media-sequence
deduplication, and TS or fMP4 segments, including an `EXT-X-MAP` init segment.
For a master playlist it follows the first variant.

The following HLS features are unsupported and return an error:

- encrypted streams;
- byte ranges, gaps, and discontinuities;
- changing the initialization segment during playback;
- choosing a bitrate or audio rendition from a master playlist.

Live radio has no seekable timeline. HTTP/HLS parsing, cancellation, redirects,
sequence handling, and stream teardown have regression coverage in
`internal/radio/radio_test.go` and `internal/player/stream_test.go`.

## Device verification

Automated tests do not replace a real-station and handheld smoke/soak run. Before
a PortMaster binary release, verify startup, station switching, Wi-Fi loss,
long pauses, audio continuity, and sustained visualization on target devices.
Record results in the [PortMaster release checklist](PORTMASTER-RELEASE.md).

The original implementation plan is preserved in
[`archive/RADIO-STREAMING-PLAN.md`](archive/RADIO-STREAMING-PLAN.md).
