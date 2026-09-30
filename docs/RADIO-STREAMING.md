# Internet radio and stream support

Radio playback is implemented. The network stream is opened by the Go HTTP
layer, passed to FFmpeg through custom I/O, and decoded away from the UI and
SoLoud audio callback. Compressed input is bounded to 2 MiB; decoded audio uses
a preallocated four-second stereo PCM ring. Stream requests are cancellable,
and changing stations or stopping playback detaches and tears down the old
stream off the UI thread.

ICY `StreamTitle` metadata is exposed as the current stream title. Percent-
encoded values are decoded when they form valid UTF-8; malformed values are
kept as received.

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

## Recently played

Radio exposes a local **Recently played** list. A station is added only after
its stream starts successfully, is moved to the front when replayed, and is
stored in the radio cache for the next launch. The list keeps up to 50 station
descriptors with the successful start time and requires no directory request
to browse. Failed stations remain in history, playback failures are reported,
and the list can be cleared from its menu.

Favorites keep stable `radio:<uuid>` paths. When a playlist is opened, station
descriptors are fetched asynchronously by UUID when the local radio cache lacks
a station name or stream URL. A favorite radio row is shown only when both are
available, so an incomplete descriptor cannot be selected for playback. Results
with a name and stream URL are persisted; if the directory is offline or the UUID
is no longer available, the favorite remains hidden until metadata is available.

### Localized country names

Country filters use ISO 3166-1 alpha-2 codes as query values and as the source
for localized labels. Station details localize country names when a recognized
code is available. Older cached country names remain readable and continue to
use the legacy name-based query until the filter catalog is refreshed. Values
without a recognized code keep their Radio Browser name. Station language is
shown as supplied by Radio Browser.

## Device verification

Automated tests do not replace a real-station and handheld smoke/soak run. Before
a PortMaster binary release, verify startup, station switching, Wi-Fi loss,
long pauses, audio continuity, and sustained visualization on target devices.
Record results in the [PortMaster release checklist](PORTMASTER-RELEASE.md).

The original implementation plan is preserved in
[`archive/RADIO-STREAMING-PLAN.md`](archive/RADIO-STREAMING-PLAN.md).
