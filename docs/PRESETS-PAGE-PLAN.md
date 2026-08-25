# Presets Page Rework — Plan

## Goal

Rework the Presets page navigation to mirror the Library/NC pattern:
- **Left panel**: hierarchical category tree (like NC directory navigation)
- **Right panel**: preset info + thumbnail preview (visible only when a .milk file is selected)

---

## Current State

The presets page is a flat two-column layout:
- Left: category list (flat, no nesting)
- Right: preset names within selected category
- No metadata, no preview, no info panel

Data model: `PresetCat{Name, Presets []string}` — flat categories built from
preset path prefixes (`Category/Subcategory/preset.milk` → two levels).

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
- ENTER on a .milk: load the preset
- RIGHT on a .milk: no-op
- Right panel never receives focus — it's a derived view of the selected node
- Currently-playing preset marked with `▸` (like Library track indicator)

### Right Panel — Preset Info

Visible only when cursor is on a .milk entry (not a category folder).

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
    children []presetNode // subcategories + presets (sorted)
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
- Sort children alphabetically at each level

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
| ENTER | Expand: push children level | Load preset |
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
        if !node.isLeaf {
            o.presetNav.Expand(node)
        } else {
            o.loadPresetByKey(node.key)
        }
    case LEFT, BACKSPACE, 'b':
        o.presetNav.Collapse()
    }
```

### 3. Right Panel — Preset Info

The right panel is **stateless and unfocusable** — it has no cursor, no
scroll state, no independent selection. It is a pure rendering of
`buildPresetDetail(node)` where `node = navigation.Selected()`.

When the selected node changes (cursor moves), the right panel texture
is rebuilt. When the node is a directory, the right panel is hidden
(no texture).

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

**Parsing** (`internal/presets/parse.go`):

Parser contract — handles real .milk files, not just idealized key=value:
- Recognizes `[preset00]` section headers; keys outside sections ignored
- Accepts whitespace around `=` (`key = value`)
- Ignores empty lines and `//` comment lines
- Handles repeated keys: last value wins (MilkDrop behavior)
- Counts `shapecode_N_enabled=1` (not just presence of `shapecode_N`)
- Distinguishes `*_enabled=0` from absent key (explicit disabled vs unused)
- `per_frame_N` / `per_pixel_N` counting skips entries inside code blocks
  (lines that are clearly shader code, not key=value assignments)
- Unknown keys are silently ignored (no error)
- Corrupted/unparseable files return partial metadata with nil error
  (caller decides whether to show partial data)
- Does NOT extract WaveColor yet — field added when UI decides how to
  render it (premature data leads to premature coupling)

**Validation against real presets**: before implementation, sample 50+
.milk files from `dist/presets-cream-of-the-crop/` to confirm `fRating`,
`fDecay`, `fWarpAnimSpeed`, `fVideoEchoZoom` are consistently present
and parseable. If a field is missing in >20% of presets, omit it from
the UI rather than showing zeros.

**Cache belongs to the store**, not the UI:
- `presets.go` owns the cache (`metaCache map[string]*PresetMeta`)
- `ReadMeta(key)` reads from cache or parses on first access
- Cache is invalidated on store reload (user adds .milk files)
- UI receives `*PresetMeta` via callback/provider — no knowledge of
  where or how it was cached
- This enables UI tests to mock metadata without a real preset store:

```go
// In overlay_presets.go — provider callback type.
type presetMetaProvider func(key string) *PresetMeta

// In overlay.go:
presetMeta presetMetaProvider // set by app, mocked in tests
```

**Display** (`internal/ui/render_presets.go`):
- Right panel shows info when cursor is on a .milk node
- Hidden (no texture) when cursor is on a directory
- Rebuild texture when cursor moves to a different .milk entry

### 4. Thumbnail Preview

#### Why NOT the main projectM instance

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
│ 1. Process preview jobs (budget: 1 per frame)   │
│    ├─ load next job from previewQueue           │
│    ├─ pmPreview.SetWindowSize(thumbW, thumbH)   │
│    ├─ pmPreview.LoadPresetData(data, false)     │
│    ├─ pmPreview.RenderFrame() × N warmup        │
│    ├─ Capture via FBO → GL texture              │
│    └─ store texture ID in thumbCache            │
│                                                 │
│ 2. Normal frame: pm.RenderFrame() → rt → overlay│
└─────────────────────────────────────────────────┘
```

**Key constraints:**
- Preview jobs are serialized with the main render — never concurrent
- Budget: 1 thumbnail per frame (~16ms on 60fps). On ARM may need 1 per 2–3 frames.
- `pmPreview` is created once at init, destroyed at close
- FBO is created once at thumbnail size, reused for all captures
- Preview queue depth capped at 10; older requests discarded when full

**Preview renderer** (`internal/app/preview.go`):

```go
type previewRenderer struct {
    pm        *projectm.Handle   // dedicated projectM instance
    fbo       C.GLuint           // framebuffer object
    tex       C.GLuint           // capture texture
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

// ProcessNext runs one preview job if available. Called once per frame from Run().
// Returns the GL texture ID if a new thumbnail was rendered, 0 otherwise.
func (r *previewRenderer) ProcessNext() uint32 {
    if len(r.queue) == 0 {
        return 0
    }
    job := r.queue[0]
    r.queue = r.queue[1:]

    // Bind FBO, set small viewport
    C.glBindFramebuffer(C.GL_FRAMEBUFFER, r.fbo)
    C.glViewport(0, 0, C.GLsizei(r.thumbW), C.GLsizei(r.thumbH))

    // Load + render on the dedicated instance
    r.pm.SetWindowSize(r.thumbW, r.thumbH)
    r.pm.LoadPresetData(string(job.data), false)
    for i := 0; i < 3; i++ { // warmup frames
        r.pm.RenderFrame()
    }

    // Capture to texture
    C.glBindTexture(C.GL_TEXTURE_2D, r.tex)
    C.glCopyTexSubImage2D(C.GL_TEXTURE_2D, 0, 0, 0, 0, 0,
        C.GLsizei(r.thumbW), C.GLsizei(r.thumbH))
    C.glBindTexture(C.GL_TEXTURE_2D, 0)

    // Restore main FBO
    C.glBindFramebuffer(C.GL_FRAMEBUFFER, 0)

    return uint32(r.tex)
}
```

**UI integration** (`internal/ui/render_presets.go`):
- `thumbCache map[string]uint32` — preset key → GL texture ID
- On cursor move to .milk entry: check cache, if miss → enqueue preview job
- Draw cached texture in right panel above metadata text
- If not yet rendered: show "loading..." placeholder, re-check next frame
- On preset store reload: flush thumb cache + queue

**Memory budget:**
- 1 FBO + 1 capture texture at 160×90×4 = 57.6 KiB (reused for all renders)
- thumbCache: up to 50 textures × 160×90×4 = 2.7 MiB (LRU eviction)
- Total: ~2.8 MiB — acceptable on ARM handhelds

#### Fallback: metadata-only (no thumbnails)

If FBO is not available (some ARM GL ES 2.0 drivers don't support
`GL_FRAMEBUFFER_COMPLETE`), or if performance is unacceptable:
- Skip thumbnail rendering entirely
- Right panel shows only metadata text
- `previewRenderer` gracefully degrades: `ProcessNext()` returns 0
- Log a warning once at init

### 5. File Changes

| File | Changes |
|---|---|
| `internal/presets/metadata.go` | **New** — PresetMeta struct + ParseMeta function |
| `internal/presets/metadata_test.go` | **New** — tests for .milk parsing (real samples) |
| `internal/presets/presets.go` | Add Keys(), ReadMeta() with cache, invalidation on reload |
| `internal/ui/overlay_presets.go` | Rework: presetTree, presetNavigation, tree building |
| `internal/ui/overlay_presets_test.go` | **New** — tests for tree building + navigation |
| `internal/ui/render_presets.go` | Rework: left panel tree rendering, right panel info + thumbnail |
| `internal/ui/overlay.go` | Update Overlay struct: presetNavigation, presetMeta callback |
| `internal/ui/overlay_input.go` | Update cursor/selection handlers for tree navigation |
| `internal/app/preview.go` | **New** — previewRenderer with dedicated projectM + FBO |
| `internal/app/app.go` | Create previewRenderer, wire presetMeta callback |
| `internal/app/run_loop.go` | Add preview job processing step before main render |

### 6. Preset Store Changes

`internal/presets/presets.go`:
- Add `func Keys() []string` — returns sorted preset keys (already available as `store.names`)
- Add `func ReadMeta(key string) *PresetMeta` — reads from cache or parses on first access
- Cache stored in `presetStore.metaCache map[string]*PresetMeta`
- Cache invalidated in `Open()` (store reload)
- `ReadMeta` is safe to call from any goroutine (cache is populated once
  at store load, never mutated after; if concurrency is needed later,
  add `sync.Once` per key)

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
   - Hidden when cursor on directory
   - Rating as stars, complexity metrics, wave mode name
   - Scrollable for small screens

4. **Phase 4 — Thumbnail rendering** (FBO + preview instance)
   - `internal/app/preview.go` — previewRenderer (dedicated projectM + FBO)
   - Main-loop integration (1 job per frame budget)
   - thumbCache in Overlay (LRU, max 50)
   - Draw thumbnail in right panel above metadata
   - Fallback: metadata-only if FBO unavailable
   - Performance tuning on ARM (warmup frames, job frequency)

5. **Phase 5 — Polish**
   - Auto-expand to playing preset on page entry
   - Smooth expand/collapse animation (optional)
   - Currently-playing indicator in tree
   - Keyboard hints for right panel scroll

---

## Risk Mitigation

| Risk | Mitigation |
|---|---|
| FBO unavailable on ARM | Fallback to metadata-only; check `glCheckFramebufferStatus` at init |
| Thumbnail render too slow on ARM | Reduce warmup to 1 frame; process 1 job per 3 frames; skip if load > 80% |
| GL state leaking from preview | Strict save/restore: bind FBO, viewport, then restore after capture |
| Preview instance conflicts with main | Separate `projectm.Handle` — no shared mutable state between instances |
| Cache memory growth | LRU with max 50 entries; evict oldest on overflow |
