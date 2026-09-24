# Player Architecture Improvement Plan

> Historical design proposal. The player lifecycle and asynchronous loading have
> since been refactored; the sequence below is not current implementation work.
> Use [the architecture guide](../ARCHITECTURE.md) and `internal/player` as
> the current reference.

At the time, the implementation was functional and was not intended to be
reverted. This proposal aimed to make asynchronous playback easier to reason
about, cancel, test, and extend without changing user-visible behavior.

## Goals

The implementation should provide:

- deterministic cancellation of obsolete downloads and decodes;
- safe shutdown while a track is loading;
- one clear owner for every native `AudioSource`;
- correct loading and progress state after success, failure, stop, or shutdown;
- a small, testable boundary between downloading, decoding, and playback;
- regression coverage for remote catalog playback and rapid track switching;
- no dependency from the player package on Modland or ModArchive details.

## Non-goals

This work should not:

- replace SoLoud or the existing audio backends;
- redesign the UI or the main render loop;
- introduce streaming playback;
- change catalog URL formats;
- add a general-purpose event framework;
- perform unrelated cleanup of the native libraries.

## Current Risks

The current implementation in `internal/player/player.go` uses a request generation to reject stale results. This fixes the original virtual-path/local-cache-path bug, but it does not stop obsolete work:

1. An old downloader and decoder continue running after a newer track is requested.
2. An old progress callback can update the global progress value after a newer request has started.
3. `Stop` does not invalidate an in-flight async request. A load that finishes after `Stop` may still be applied.
4. `Close` does not wait for background workers before destroying SoLoud.
5. `PlayFileAsync` stops the current voice but keeps the current native source until the next successful result. On load failure, the source remains owned by the player without active playback.
6. `pendingCh` and `pendingMu` implement a replace-latest queue manually. The ownership and destruction rules are spread across several methods.
7. There are no focused player tests for remote paths, stale requests, stop during loading, or native source cleanup.

## Target Design

### 1. Explicit request and result types

Replace the loose path and generation fields passed between methods with explicit types. The names may be adjusted to match local style, but the information should remain separate:

```go
type TrackRequest struct {
    ID          uint64
    DisplayPath string // virtual catalog path or local path shown to the user
}

type LoadResult struct {
    Request     TrackRequest
    LocalPath   string
    Source      soloud.AudioSource
    BPM         float64
    Channels    int
    Duration    float64
    IsTracker   bool
    TrackCount  int
    Err         error
}
```

`DisplayPath` must never be replaced by the downloaded cache path. `LocalPath` is used only for decoding, bitrate calculation, diagnostics, and cache-related operations.

Keep `LoadResult` private unless tests or another package genuinely need the type.

### 2. Introduce cancellable load jobs

Add a load-job lifecycle to `Player`:

```go
loadCancel context.CancelFunc
loadWG     sync.WaitGroup
```

When `PlayFileAsync` starts:

1. cancel the previous job;
2. increment the request generation;
3. create a new context derived from `context.Background()`;
4. mark loading active and reset progress;
5. stop and release the currently playing source according to the ownership policy;
6. start exactly one worker and add it to `loadWG`.

The worker must check cancellation:

- before starting the download;
- from the downloader progress callback when possible;
- after the download completes;
- before decoding;
- after decoding and before publishing the result.

The downloader callback currently has no context parameter. Preserve the existing public callback shape initially, but make the wrapper check `ctx.Err()` and ignore progress from an obsolete request. If the download package can be changed without breaking callers, add context-aware variants such as `DownloadFileContext` and `DownloadAndExtractContext`; keep the old functions as small compatibility wrappers.

### 3. Define native source ownership

Document and enforce these rules:

- A successfully decoded source is owned by the pending-result queue until `CheckPending` accepts it.
- An accepted source is owned by `Player.current`.
- A rejected, stale, replaced, or failed source is destroyed exactly once.
- `Close` waits for workers before destroying SoLoud.

Create one helper for discarded results, for example:

```go
func destroyResultSource(result LoadResult) {
    if result.Source != nil {
        result.Source.Destroy()
    }
}
```

Use it in every stale-result, queue-replacement, cancellation, and error path. Do not call `Destroy` from multiple ownership paths for the same result.

### 4. Simplify pending-result delivery

Keep the replace-latest behavior, but hide it behind a small private abstraction instead of manipulating `pendingCh` directly from multiple methods.

The abstraction should provide operations equivalent to:

- `Replace(result)`: destroy the previously pending source and store the new result;
- `Take() (result, ok)`: remove the pending result for consumption;
- `Discard()`: destroy any pending source;
- `Close()`: prevent future publication during shutdown.

The abstraction may continue to use a buffered channel and mutex internally. The important result is that `Player` no longer needs to know the queue's replacement and ownership details.

Do not use a blocking send from a worker. A worker must be able to exit after cancellation even when the main loop is no longer consuming results.

### 5. Make player state transitions explicit

Keep all state mutation on the main/player goroutine where possible. Define the expected transitions:

```text
Idle -> Loading
Loading -> Playing       on current successful result
Loading -> Idle          on error or cancellation of the active request
Playing -> Loading       on a new request
Playing -> Idle          on Stop
Any state -> Closed      on Close
```

Required behavior:

- Starting a new request makes the previous voice silent immediately.
- `Stop` cancels the active request, invalidates its generation, clears loading/progress state, and leaves no active voice.
- A cancelled or stale request must never start playback.
- A load error clears loading state and does not leave an owned source unnecessarily retained.
- `Close` is idempotent or clearly documented as single-use, waits for workers, drains/discards pending results, and only then destroys SoLoud.

Consider adding a small private `resetPlaybackState` helper if that prevents `Stop`, load failure, and `Close` from implementing slightly different cleanup rules.

### 6. Keep the downloader boundary in the application layer

The current callback assignment in `internal/app/app.go` is architecturally appropriate: the application knows how to resolve `modland:` and `modarchive:` paths, while `player` only asks for a local file.

Preserve this dependency direction:

```text
app -> player.Downloader -> modland/modarchive
player -> generic local-path loader
```

Do not import `internal/modland` or `internal/modarchive` into `internal/player`.

If the callback grows additional behavior, replace the function field with a small interface in the player package, for example:

```go
type Downloader interface {
    Download(ctx context.Context, displayPath string, expectedSize int64,
        onProgress func(read, total int64)) (localPath string, err error)
}
```

Only make this change when it improves testability or cancellation. Do not introduce an interface solely for naming purposes.

### 7. Isolate decoding from playback

Extract the format dispatch currently performed by `loadSource` into a private loader component, for example:

```go
type SourceLoader struct{}

func (SourceLoader) Load(ctx context.Context, localPath string) LoadResult
```

The loader should own:

- extension detection;
- reading file bytes;
- selecting SoLoud/XMP/OpenMPT/GME/HVL/Ayumi/PT3/YM/FFmpeg loaders;
- format metadata extraction.

`Player` should own:

- request creation and cancellation;
- pending result handling;
- current source replacement;
- play, pause, seek, stop, and close operations.

Keep this extraction small. It should not change decoder selection or supported extensions.

## Implementation Sequence

### Phase 1: Add regression tests before structural changes

Add `internal/player/player_test.go` with tests that do not require real audio playback where possible.

Use fakes for the downloader and, if needed, a narrow source factory seam for native sources. The tests must cover:

1. A local async load applies successfully.
2. A virtual remote path is retained as the display path while the downloaded local path is used for loading and bitrate calculation.
3. A newer request wins when an older request completes later.
4. A downloader error clears `Loading()` and returns `failed == true` from `CheckPending`.
5. `Stop` prevents a pending load from starting playback.
6. A stale or replaced pending source is destroyed once.
7. Progress from an obsolete request cannot overwrite the current request's progress.

If a test requires native libraries that cannot be faked cleanly, add the smallest test seam rather than weakening production ownership rules.

### Phase 2: Fix lifecycle behavior without changing the public design

Before introducing new abstractions:

1. Add cancellation and `WaitGroup` fields.
2. Invalidate requests in `Stop` and `Close`.
3. Make `Close` wait for all workers.
4. Guard progress updates with request identity and cancellation.
5. Clear or destroy the current source on load failure according to the ownership rules.
6. Ensure cancelled workers cannot publish results.

Run the focused player tests after each logical change, then run the full project test command:

```text
make test
```

### Phase 3: Extract pending-result ownership

Move queue replacement and source destruction into a private helper. Keep behavior unchanged and rerun the async tests after the extraction.

The helper should be tested independently for:

- replacing a pending result;
- taking a result;
- discarding a result;
- rejecting publication after close.

### Phase 4: Separate decoding from playback

Move format dispatch and metadata extraction into the loader component. Preserve the existing functions as wrappers temporarily if that reduces the size of the change.

Verify that:

- all existing supported extensions still compile and load;
- tracker metadata behavior is unchanged;
- FFmpeg channel detection is unchanged;
- bitrate still uses `LocalPath` for downloaded tracks;
- no native source leaks are introduced during fallback from OpenMPT to XMP.

### Phase 5: Consider context-aware download APIs

Only after the player lifecycle tests pass, add context-aware download functions to `internal/modland/download.go` and `internal/modarchive/download.go` if the underlying HTTP and archive operations can be interrupted safely.

The old exported functions should remain as wrappers unless all callers can be updated without compatibility concerns.

## Validation Checklist

Before merging the architecture work:

- [ ] `go test` for the player package passes in the supported Docker toolchain.
- [ ] `make test` passes.
- [ ] `git diff --check` passes.
- [ ] Local catalog playback works for both Modland and ModArchive paths.
- [ ] Switching tracks rapidly never starts an older selection.
- [ ] Pressing Stop during a download leaves the player stopped.
- [ ] Closing the application during a download exits without a crash or hang.
- [ ] A failed download returns the UI to a non-loading state.
- [ ] Progress does not jump backward because of an older request.
- [ ] Native sources are destroyed exactly once on success replacement, cancellation, stale completion, failure, stop, and close.
- [ ] No new dependency from `internal/player` to catalog-specific packages exists.
- [ ] Supported audio extensions and metadata behavior remain unchanged.

## Suggested Commit Structure

Keep the work reviewable and bisectable:

1. `test(player): cover async request cancellation and stale results`
2. `fix(player): cancel obsolete loads and wait during close`
3. `refactor(player): encapsulate pending source ownership`
4. `refactor(player): separate source loading from playback state`
5. `feat(modland): add context-aware downloads` and/or `feat(modarchive): add context-aware downloads`

Do not combine native library warning cleanup or unrelated UI changes with these commits.

## Expected Result

After completing this plan, the player will have a clear and testable asynchronous lifecycle:

- the application supplies a generic download function;
- the player creates and cancels load jobs;
- the loader converts a local file into a metadata-rich source result;
- the pending-result component owns unpublished native sources;
- the player owns only the active source;
- stale, cancelled, failed, and shutdown paths have deterministic cleanup;
- the main loop remains a simple consumer of `CheckPending`;
- remote catalog playback behaves like local playback from the UI's perspective, without path identity bugs or shutdown races.
