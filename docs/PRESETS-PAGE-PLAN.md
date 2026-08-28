# Presets Page Rework — Plan

## Goal

Rework the Presets page navigation to mirror the Library/NC pattern:
- **Left panel**: hierarchical category tree (like NC directory navigation)
- **Right panel**: stable preset detail view + optional thumbnail preview

---

## Current State

The presets page is a flat two-column layout:
- Left: category list (flat, no nesting)
- Right: preset names within selected category
- No metadata, no preview, no info panel

Data model: `PresetCat{Name, Presets []string}` — flat categories built from
preset path prefixes. The new tree must be built from canonical keys returned
by the preset store, not from the current two-level category projection.

---

## Target State

### Left Panel — Hierarchical Tree (NC-style)

```
▸ Fractal
    Loops
    Nested
    Radial
  Dancer
    Infect
    Lasers
  Drawing
    Trails Mirror
    Rorschach
```

Navigation:
- UP/DOWN moves cursor within current level
- RIGHT or ENTER on a category: expand (push children level)
- LEFT or BACKSPACE: collapse (pop to parent)
- ENTER on a .milk: confirm it as active without mutating the main visualizer
- RIGHT on a .milk: no-op
- Right panel never receives focus — it's a derived view of the selected node
- Currently-playing preset marked with `▸` (like Library track indicator)

### Right Panel — Preset Info

The layout remains stable for every selection. A `.milk` entry shows preset
metadata and preview state; a category shows category information or an empty
state, but the right panel never receives focus.

```
Phat-Zoom artifacts.milk

[thumbnail 160×90]

Rating: ★★★★☆        Decay: 0.99
Warp: 1.6             Echo: 1.2x
Wave mode: 7 (Custom)
Shapes: 3   Waves: 1
Per-frame: 12 eq      Per-pixel: 4 eq
```

Scrollable if content exceeds panel height.

---

## Architecture

### 1. Preset Tree Model (`internal/ui/overlay_presets.go`)

New types:

```go
// presetNode is a single node in the preset tree.
type presetNode struct {
    name     string       // display name (category or .milk basename without ext)
    key      string       // full preset key (empty for directories)
    children []presetNode // subcategories + presets (sorted; tree is immutable after build)
    isLeaf   bool         // true = .milk file, false = directory
}

// presetTree is the root of the hierarchical preset structure.
type presetTree struct {
    root []presetNode // top-level categories
}
```

Build the tree from `presetNames []string` (already sorted):
- Split each key by `/` → create intermediate directory nodes
- Leaf nodes are the .milk files
- Keep root-level `.milk` files in the root; do not invent a `Default/` node
- Sort directories before files, then sort each group lexicographically
- Derive leaf-ness from `isLeaf` field, not from `key == ""`. This keeps
  the semantic explicit and safe if virtual categories with keys appear later.

### 2. Left Panel Navigation

The right panel has **no navigation state** of its own. It is a pure
function of the selected node:

```go
selectedNode := navigation.Selected()
detail := buildPresetDetail(selectedNode)
```

This avoids the old problem of two independent cursors under a new name.

```go
// presetNavLevel holds the visible nodes and cursor at one depth level.
type presetNavLevel struct {
    nodes  []presetNode
    cursor int
    scroll int
}

// presetNavigation manages a stack of expansion levels.
// Current level is always len(stack) - 1 — no separate index needed.
type presetNavigation struct {
    stack []presetNavLevel
}

func (n *presetNavigation) current() *presetNavLevel {
    return &n.stack[len(n.stack)-1]
}

func (n *presetNavigation) Selected() *presetNode {
    cur := n.current()
    if cur.cursor < 0 || cur.cursor >= len(cur.nodes) {
        return nil
    }
    return &cur.nodes[cur.cursor]
}

func (n *presetNavigation) Expand(node *presetNode) {
    if len(node.children) == 0 {
        return
    }
    n.stack = append(n.stack, presetNavLevel{nodes: node.children})
}

func (n *presetNavigation) Collapse() bool {
    if len(n.stack) <= 1 {
        return false
    }
    n.stack = n.stack[:len(n.stack)-1]
    return true
}
```

Navigation semantics — **presets page overrides FocusRight/Select/Back**
for the `PagePresets` case. The right panel never receives focus.

| Key | On directory/category | On .milk file |
|---|---|---|
| UP/DOWN | Move cursor within level | Move cursor within level |
| RIGHT | Expand: push children level | No-op (stay on entry) |
| LEFT | Collapse: pop to parent | Collapse: pop to parent |
| ENTER | Expand: push children level | Confirm preset for activation on exit |
| BACKSPACE / B | Collapse: pop to parent | Collapse: pop to parent |

This is the same model as NC directory navigation, applied to the preset
tree. The right panel is a **derived view** — it shows info about
`navigation.Selected()` but never participates in navigation itself.

```go
// In overlay_input.go — Presets page overrides.
case PagePresets:
    switch key {
    case RIGHT, ENTER:
        node := o.presetNav.Selected()
        if node == nil {
            break
        }
        if node.key == "" {
            o.presetNav.Expand(node)
        } else {
            o.confirmPreset(node.key)
        }
    case LEFT, BACKSPACE, 'b':
        o.presetNav.Collapse()
    }
```

### 3. Right Panel — Preset Info

The right panel is **stateless and unfocusable** — it has no cursor, no
scroll state, no independent selection. It is a pure rendering of
`buildPresetDetail(node)` where `node = navigation.Selected()`.

When the selected node changes (cursor moves), the right panel detail is
rebuilt. The panel keeps a stable layout: a directory shows category
information or an empty-state message, while a `.milk` node shows preset
metadata and preview state. The right panel has no cursor or independent
selection.

Scrolling long content reuses the existing `infoMarquee` horizontal
scroll mechanism — no new scroll state needed. The marquee activates
automatically when content overflows, no focus required.

**"Currently-playing" comparison**: use the full preset key (the same
string stored in `presetName` and passed by app). Basename comparison is
unsafe — different categories can contain presets with identical names.
The `▸` indicator in the tree is set when `node.key == playingKey`.

**Metadata struct** (`internal/presets/metadata.go`):

```go
type PresetMeta struct {
    Rating        float64
    Decay         float64
    WarpSpeed     float64
    VideoEchoZoom float64
    WaveMode      int
    Shapes        int        // count of enabled shapecode_N
    Waves         int        // count of enabled wavecode_N
    PerFrameEqs   int        // count of per_frame_N entries
    PerPixelEqs   int        // count of per_pixel_N entries
}
```

**Parsing** (`internal/presets/metadata.go`):

Parser contract — handles real .milk files, not just idealized key=value:
- Recognizes `[preset00]` section headers; keys outside sections ignored
- Accepts whitespace around `=` (`key = value`)
- Ignores empty lines and `//` comment lines
- Handles repeated keys: last value wins (MilkDrop behavior)
- Counts `shapecode_N_enabled=1` (not just presence of `shapecode_N`)
- Distinguishes `*_enabled=0` from absent key (explicit disabled vs unused)
- `per_frame_N` / `per_pixel_N` counting only considers recognized assignment
    lines; code text without a matching assignment is ignored
- Unknown keys are silently ignored (no error)
- Invalid numeric values return partial metadata plus an error; the caller may
    show valid fields with an `incomplete` status
- Does NOT extract WaveColor yet — field added when UI decides how to
  render it (premature data leads to premature coupling)

**Validation against real presets**: before implementation, sample 50+
`.milk` files from an available preset bundle to confirm `fRating`,
`fDecay`, `fWarpAnimSpeed`, `fVideoEchoZoom` are consistently present
and parseable. If a field is missing in >20% of presets, omit it from
the UI rather than showing zeros.

**Cache belongs to the store**, not the UI:
- `presets.go` owns the cache (`metaCache map[string]PresetMeta`)
- `ReadMeta(key)` reads from cache or parses on first access
- Cache is invalidated on store reload (user adds .milk files)
- UI receives `PresetMeta` and an error via callback/provider — no knowledge of
  where or how it was cached
- This enables UI tests to mock metadata without a real preset store:

```go
// In overlay_presets.go — provider callback type.
type presetMetaProvider func(key string) (PresetMeta, error)

// In overlay.go:
presetMeta presetMetaProvider // set by app, mocked in tests
```

**Display** (`internal/ui/render_presets.go`):
- Right panel shows info when cursor is on a .milk node
- Stable category/empty-state detail when cursor is on a directory
- Rebuild texture when cursor moves to a different .milk entry

### 4. Thumbnail Preview

While this page is visible, the main visualizer does not render. The window
background is cleared to black before the overlay and dedicated preview are
drawn. Both timer-based switching and projectM automatic hard cuts are paused;
they resume with a fresh timer interval after leaving the page.

Moving the cursor only changes metadata and preview. Pressing ENTER confirms a
preset for activation. The confirmed preset is loaded into the main projectM
instance, and only then updates the bottom bar and playing indicator in the
tree, after the user leaves the Presets page or closes the menu. Leaving without
confirmation keeps the previously playing preset.

The main `pm` handle owns the active visualization state:
- `SetWindowSize` mutates global GL viewport + projection
- `LoadPresetData` replaces the active preset and its feedback history
- `RenderFrame` mutates feedback buffers and temporal dynamics
- Any of these mid-frame would cause visible glitches

#### Why NOT a separate goroutine

OpenGL context is bound to the main thread via `runtime.LockOSThread()` in
`main()`. All GL calls (`glGenFramebuffers`, `glCopyTexSubImage2D`,
`glTexImage2D`, etc.) must execute on that same thread. A goroutine calling
GL functions would corrupt the context or crash.

#### Solution: dedicated preview instance + main-loop job queue

Create a **second projectM instance** (`pmPreview`) used exclusively for
thumbnail rendering. All operations on it happen on the main GL thread,
processed as a bounded job queue inside the existing frame loop.

```
┌─────────────────────────────────────────────────┐
│ main loop (run_loop.go)                         │
│                                                 │
│ 1. Process one preview step per main frame       │
│    ├─ load job and preset (first step)          │
│    ├─ feed deterministic preview PCM            │
│    ├─ pmPreview.RenderFrame()                    │
│    ├─ capture via FBO → GL texture              │
│    └─ store texture ID in thumbCache            │
│                                                 │
│ 2. Presets page: clear black → preview → overlay│
└─────────────────────────────────────────────────┘
```

**Key constraints:**
- Preview jobs are serialized with the main render — never concurrent
- One preview step is processed per main frame. The first rendered frame is
    captured immediately so the thumbnail appears without a warmup delay.
- Every preview render is timed. The displayed FPS is the uncapped moving
    average render capacity over the last 10 frames on the current hardware.
- Preview animation is paced at a maximum of 15 FPS; the 60 Hz main loop continues to
    process input and draw the overlay on ticks where preview rendering is skipped.
    `SetFPS` exposes the measured moving-average value to MilkDrop expressions;
    the main-loop scheduler independently enforces the actual render interval.
- `pmPreview` is created once at init, destroyed at close
- FBO is created once at thumbnail size, reused for all captures
- Preview queue depth capped at 10; older requests discarded when full

`projectM` advances preset time from its internal wall clock on each
`RenderFrame()` call. `SetFPS()` only sets the `fps` value exposed to MilkDrop
expressions; it does not set the time step or make several immediate frames
simulate elapsed time. A preview must not use `sleep` to advance time because
that would block the main render loop.

The main loop remains at 60 Hz and samples the current audio waveform once per
tick. The scheduler renders at most one preview frame every 1/15 second.
Ticks without preview rendering still process input and draw the UI. Rendering
multiple preview frames per tick would repeatedly analyze the same PCM snapshot
and distort beat attenuation. The displayed FPS is updated from the measured
render cost after every preview frame. The preview render target uses one eighth
of the window width and height; the UI scales that low-resolution image up when
placing it in the right panel.

**Preview renderer** (`internal/app/preview.go`):

```go
type previewRenderer struct {
    pm        *projectm.Handle   // dedicated projectM instance
    queue     []previewJob       // pending render requests
    maxQueue  int
    thumbW    int
    thumbH    int
}

type previewJob struct {
    key      string  // preset key (for cache lookup)
    data     []byte  // .milk content
    priority int     // lower = more urgent (selected preset > visible > background)
}

// ProcessNext runs one preview step. Called once per main frame from Run().
// It loads a job, renders and captures a frame, and records its render time.
// Returns the GL texture ID only when a new thumbnail was rendered.
func (r *previewRenderer) ProcessNext() uint32 {
    if len(r.queue) == 0 {
        return 0
    }

    // Each call performs one bounded render and capture step.
    // GL/FBO operations are delegated to a thin GL helper.
    return r.processStep()
}

The preview renderer feeds a deterministic short PCM signal to `pmPreview`
before each frame. A 1000 Hz sine wave is a valid minimal signal, but a
small composite signal with bass, mid, treble, and periodic transients gives
more representative results for beat- and spectrum-dependent presets. This
PCM is sent only to `pmPreview`; it is never played through the speakers.
```

**UI integration** (`internal/ui/render_presets.go`):
- On cursor move to a `.milk` entry, update detail state and request one preview
    job if the key is not already cached. Scheduling happens in navigation/update
    code, never from the renderer.
- Draw the cached texture in the right panel above metadata text
- If not yet rendered: show a "loading..." placeholder, re-check next frame
- Apply a result only when its request ID and key still match the selected node
- On preset store reload: flush the cache and pending requests

**Memory budget:**
- A staging FBO/texture may be reused, but every cached thumbnail needs its own
    texture. One shared capture texture must never be stored under multiple keys.
- `thumbCache`: up to 50 textures × 160×90×4 = 2.7 MiB (LRU eviction)
- If per-thumbnail copies are too expensive, v1 uses one current preview
    texture instead of a multi-entry cache.

#### Fallback: metadata-only (no thumbnails)

If FBO is not available (some ARM GL ES 2.0 drivers don't support
`GL_FRAMEBUFFER_COMPLETE`), or if performance is unacceptable:
- Skip thumbnail rendering entirely
- Right panel shows only metadata text
- `previewRenderer` gracefully degrades: `ProcessNext()` returns 0
- Log a warning once at init

**Preview delivery to UI**:

`ProcessNext()` is called in `Run()` (app layer) and stores the result
internally. The UI checks it during render:

```go
// In previewRenderer:
type previewRenderer struct {
    ...
    resultKey string   // key of the last rendered thumbnail
    resultTex uint32   // GL texture ID of the last rendered thumbnail
    resultID  uint64   // request ID that produced this result
}

func (r *previewRenderer) HasResult(key string) (tex uint32, ok bool) {
    if r.resultKey == key && r.resultTex != 0 {
        return r.resultTex, true
    }
    return 0, false
}
```

The UI never calls GL directly — it only draws the texture ID returned
by `HasResult`. The app layer owns the full lifecycle.

#### Concurrency model

v1: all metadata and preview work happens on the main thread (via
provider callbacks and `ProcessNext`). No mutex needed.

If background metadata loading is added later, `ReadMeta` must be
wrapped with `sync.Mutex` or use `sync.Map`. This is explicitly
documented as a future concern, not implemented now.

#### Recursive user scan depth limit

`scanUser()` recursively discovers `.milk` files in the user presets
directory. Maximum depth is **3** (Category/Subcategory/File). This
prevents stalls on slow ARM storage and is enforced with a depth
parameter:

```go
func scanUser(dir string, depth int, maxDepth int, out *presetStore) {
    if depth > maxDepth {
        return
    }
    ...
}
```

#### GL spike checklist (must pass before Phase 4b)

All items must be checked on both desktop and target ARM:

```
□ pmPreview created without errors on existing GL context
□ pmPreview.RenderFrame() does not change main pm preset or feedback
□ FBO status = GL_FRAMEBUFFER_COMPLETE at thumbnail size
□ GL state fully restored after capture (viewport, FBO, active texture, program)
□ App.Close() destroys pmPreview before main pm (correct teardown order)
□ No GL errors reported after 10 consecutive preview renders
□ Main visualizer remains stopped during preview rendering
□ Window viewport is restored after preview rendering
□ No visible flicker or feedback reset occurs when leaving the page
□ Preview FPS reflects measured render time after every frame
□ Preview latency and main-loop frame time measured on desktop and target ARM
```

If any check fails → ship metadata-only detail view, preview disabled.

#### Required GL spike before implementation

Before adding the full preview feature, run the checklist above in a
small prototype. If any check fails, ship metadata-only and keep
preview disabled.

### 5. File Changes

| File | Changes |
|---|---|
| `internal/presets/metadata.go` | **New** — PresetMeta struct + ParseMeta function |
| `internal/presets/metadata_test.go` | **New** — parser fixtures and malformed-value tests |
| `internal/presets/presets.go` | Add Keys(), ReadMeta() with cache, invalidation on reload, recursive user scan |
| `internal/ui/overlay_presets.go` | Rework: presetTree, presetNavigation, tree building |
| `internal/ui/overlay_presets_test.go` | **New** — tests for tree building + navigation |
| `internal/ui/render_presets.go` | Rework: left panel tree rendering, right panel info + thumbnail |
| `internal/ui/overlay.go` | Update Overlay struct: presetNavigation, presetMeta callback |
| `internal/ui/overlay_input.go` | Update cursor/selection handlers for tree navigation |
| `internal/app/preview.go` | **New** — preview job lifecycle and dedicated projectM instance; no raw GL calls |
| `internal/projectm/preview.go` | **New** — thin projectM/GL preview helper after the GL spike |
| `internal/app/app.go` | Create previewRenderer, wire presetMeta callback |
| `internal/app/run_loop.go` | Add preview job processing step before main render |

### 6. Preset Store Changes

`internal/presets/presets.go`:
- Add `func Keys() []string` — returns sorted preset keys (already available as `store.names`)
- Add `func ReadMeta(key string) (PresetMeta, error)` — reads from cache or parses on first access
- Cache stored in `presetStore.metaCache map[string]PresetMeta`
- Cache invalidated in `Open()` (store reload)
- Synchronize cache access if metadata is loaded asynchronously; a Go map is
    not safe for concurrent reads and writes
- `scanUser()` recursively discovers supported `.milk` files, or the maximum
    supported depth is explicitly documented and tested

### 7. Preset Page Entry Point

When entering the Presets page:
1. Build tree from `presetNames`
2. Push root level onto stack
3. If a preset is currently playing, auto-expand to its category and position cursor on it

---

## Implementation Order

1. **Phase 1 — Metadata parsing** (no UI changes)
   - `internal/presets/metadata.go` + tests
   - Parse .milk files, extract PresetMeta
   - Unit tests with real .milk samples

2. **Phase 2 — Tree model + navigation** (left panel)
   - `presetNode` / `presetTree` types
   - Build tree from sorted keys
   - `presetStack` navigation (push/pop/cursor)
   - Left panel renders tree with indentation
   - RIGHT/LEFT expand/collapse
   - UP/DOWN cursor movement
   - Tests for tree building + navigation

3. **Phase 3 — Right panel info** (metadata display)
   - Right panel renders PresetMeta for selected .milk
    - Stable category/empty-state detail when cursor is on a directory
   - Rating as stars, complexity metrics, wave mode name
   - Scrollable for small screens

4. **Phase 4a — GL preview spike** (before production thumbnail code)
    - Verify second projectM instance, FBO completeness and GL state restore
    - Verify the main visualizer is unchanged after one preview render
    - Verify creation/destruction on desktop and target ARM
    - Decide between multi-entry cache and one-current-preview fallback

5. **Phase 4b — Thumbnail rendering** (only if the spike passes)
    - `internal/app/preview.go` — preview job lifecycle and bounded queue
    - Thin GL/projectM helper for offscreen render and capture
        - Main-loop integration with one bounded preview step per frame
        - Capture and display the first preview frame immediately
        - Measure each render and report uncapped capacity over a 10-frame window
        - Feed deterministic preview PCM before each preview frame
        - Do not use `SetFPS` or `sleep` as a substitute for a controllable time step
    - Per-key texture ownership with LRU eviction, or one-current-preview v1
    - Draw thumbnail in the stable right detail panel
    - Fallback: metadata-only if FBO is unavailable or too slow
    - Performance tuning on ARM (job frequency)

6. **Phase 5 — Polish**
   - Auto-expand to playing preset on page entry
   - Smooth expand/collapse animation (optional)
   - Currently-playing indicator in tree
    - Keyboard hints for tree navigation and passive detail state

---

## Risk Mitigation

| Risk | Mitigation |
|---|---|
| FBO unavailable on ARM | Run the spike first; fallback to metadata-only; check `glCheckFramebufferStatus` |
| Thumbnail render too slow on ARM | Process one preview step per 2–3 main frames; skip if load > 80% |
| Preview does not advance far enough in time | Accept early real-time state for v1; do not add `sleep`; a controllable time step would require changing projectM's `TimeKeeper` API |
| GL state leaking from preview | Encapsulate save/restore in the GL helper and test the main visualization afterward |
| Preview instance conflicts with main | Separate `projectm.Handle`, main-thread serialization, and spike validation |
| Stale preview replaces current detail | Match both request ID and preset key before applying result |
| Cache memory growth | LRU with max 50 entries and explicit texture ownership; otherwise one-current-preview v1 |
