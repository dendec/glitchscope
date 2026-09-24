# Adaptive Render Resolution

> Historical implementation plan. Adaptive rendering is implemented and has
> since evolved beyond the fixed 25–30 FPS policy described below. Treat
> [the architecture guide](../ARCHITECTURE.md) and current code as authoritative.

## Goal
Keep visualizer FPS in 25–30 range by dynamically adjusting render resolution per-preset performance.

## Target
- **Target FPS**: 30 (hardcoded)
- **Lower bound**: < 25 FPS → step DOWN resolution
- **Upper bound**: > 30 FPS sustained → step UP resolution
- **Exactly 25 or 30**: both counters reset (neutral zone)
- **Hysteresis**: down after 15 consecutive frames below 25 (~0.25s); up after 30 consecutive frames above 30 (~0.5s). FPS averaging window: 10 frames.
- **Cooldown**: 2s between resolution changes
- **Total reaction time**: down ~0.4s, up ~0.7s (well within 1-2s target for 15s preset switches)
- **Floor**: minimum available resolution (not necessarily 1/8 — some window sizes don't produce integer-scale at 1/8)
- **Both counters reset** after every successful resolution change (prevents stale state carryover through cooldown)

## CLI: -render-scale
`renderScaleExplicit` (from `-render-scale` flag) is a **process-only runtime override**.
- When set: adaptive list/index/counters are not used; resolution computed via `scaledDim()` each frame; `Adaptive` field in settings is **not modified** (persisted value preserved).
- When not set: normal adaptive or fixed behavior.
- Persisted `Adaptive` is untouched by the CLI override — relaunching without `-render-scale` restores saved behavior.

## Startup Behavior
- **Adaptive=true, no -render-scale**: ignore saved render size, start at maximum available resolution (`resolutions[0]`).
- **Adaptive=false or renderScaleExplicit**: existing behavior unchanged.

## UI: Resolution Row
`BuildSettingsRows` receives `renderScaleExplicit bool` parameter.

| renderScaleExplicit | List contents | Index mapping |
|---------------------|--------------|---------------|
| false | Auto, 480x272, 320x180, ... | Adaptive=true → 0; Adaptive=false → fixedIdx+1 |
| true | 480x272, 320x180, ... (no Auto) | direct index into resolutions |

## Files to modify

### 1. `internal/config/config.go`
- Add `Adaptive bool` to `GraphicsSettings` (JSON: `"adaptive"`)
- Default: `false`

### 2. `internal/config/storage.go`
- Add `Adaptive *bool` to raw struct in `LoadSettings` (matches partial-loader pattern)
- Handle `nil` → `false` fallback

### 3. `internal/config/config_test.go`
- Lines 23–26: replace positional literals with named fields:
  ```go
  GraphicsSettings{RenderWidth: 320, RenderHeight: 240, UpscaleFilter: FilterPixel}
  GraphicsSettings{RenderWidth: 0,   RenderHeight: 240, UpscaleFilter: FilterPixel}
  GraphicsSettings{RenderWidth: 320, RenderHeight: 0,   UpscaleFilter: FilterPixel}
  GraphicsSettings{RenderWidth: 320, RenderHeight: 240, UpscaleFilter: 99}
  ```
- Line 171 already uses named fields — no change needed.

### 4. `internal/app/app.go` — App struct
Add fields:
```go
adaptiveResIdx       int                      // index into adaptiveResolutions
adaptiveCooldown     time.Time                // last resolution change timestamp
adaptiveLowCount     int                      // consecutive frames with fpsAvg < 25
adaptiveHighCount    int                      // consecutive frames with fpsAvg > 30
adaptiveResolutions  []config.RenderResolution // cached list for current window size
```

### 5. `internal/app/app.go` — New()
```go
if a.renderScaleExplicit {
    // CLI override — compute via scaledDim, skip adaptive init
    renderW = scaledDim(int(w), renderScale)
    renderH = scaledDim(int(h), renderScale)
} else if gs.Graphics.Adaptive {
    // Adaptive — start at max, ignore saved size
    resolutions := config.ComputeResolutions(int(w), int(h))
    if len(resolutions) > 0 {
        a.adaptiveResolutions = resolutions
        a.adaptiveResIdx = 0
        renderW, renderH = resolutions[0].Width, resolutions[0].Height
    }
    // else: fall through to saved/default
} else {
    // Fixed — use saved size, snap to closest
    resolutions := config.ComputeResolutions(int(w), int(h))
    target := config.ClosestResolution(resolutions, savedRes)
    renderW, renderH = target.Width, target.Height
}
```
`Adaptive` field is never modified here — it stays as loaded from settings.

### 6. `internal/app/run_loop.go` — main loop

#### Adaptive check (after FPS averaging, ~line 69):
```go
if a.settings.Graphics.Adaptive && !a.renderScaleExplicit {
    switch {
    case fpsAvg < 25:
        a.adaptiveLowCount++
        a.adaptiveHighCount = 0
    case fpsAvg > 30:
        a.adaptiveHighCount++
        a.adaptiveLowCount = 0
    default: // 25 <= fpsAvg <= 30: neutral zone
        a.adaptiveLowCount = 0
        a.adaptiveHighCount = 0
    }

    now := time.Now()
    if a.adaptiveLowCount >= 60 && now.Sub(a.adaptiveCooldown) > 2*time.Second {
        if a.stepDown() {
            a.adaptiveLowCount = 0
            a.adaptiveHighCount = 0
            a.adaptiveCooldown = now
        }
    } else if a.adaptiveHighCount >= 180 && now.Sub(a.adaptiveCooldown) > 2*time.Second {
        if a.stepUp() {
            a.adaptiveLowCount = 0
            a.adaptiveHighCount = 0
            a.adaptiveCooldown = now
        }
    }
}
```

#### Window resize handler (existing `winChanged` block, ~line 88):
```go
if a.renderScaleExplicit {
    // CLI override — recalc via scaledDim
    renderW = scaledDim(int(w), a.renderScale)
    renderH = scaledDim(int(h), a.renderScale)
} else if a.settings.Graphics.Adaptive {
    // Adaptive — recompute list, snap, reset state
    a.adaptiveResolutions = config.ComputeResolutions(w, h)
    if len(a.adaptiveResolutions) > 0 {
        a.adaptiveResIdx = a.closestAdaptiveIdx(renderW, renderH)
        renderW = a.adaptiveResolutions[a.adaptiveResIdx].Width
        renderH = a.adaptiveResolutions[a.adaptiveResIdx].Height
    }
    a.resetAdaptiveCounters()
} else {
    // Fixed — snap to closest
    resolutions := config.ComputeResolutions(w, h)
    target := config.ClosestResolution(resolutions, config.RenderResolution{renderW, renderH})
    renderW, renderH = target.Width, target.Height
}
```

#### Step functions (new, on App):
Return `true` if resolution actually changed.
```go
func (a *App) stepDown() bool {
    if len(a.adaptiveResolutions) == 0 || a.adaptiveResIdx >= len(a.adaptiveResolutions)-1 {
        return false
    }
    a.adaptiveResIdx++
    r := a.adaptiveResolutions[a.adaptiveResIdx]
    a.settings.Graphics.RenderWidth = r.Width
    a.settings.Graphics.RenderHeight = r.Height
    a.rt.Resize(r.Width, r.Height)
    a.pm.SetWindowSize(r.Width, r.Height)
    slog.Info("adaptive: step down", "resolution", r)
    return true
}

func (a *App) stepUp() bool {
    if len(a.adaptiveResolutions) == 0 || a.adaptiveResIdx <= 0 {
        return false
    }
    a.adaptiveResIdx--
    r := a.adaptiveResolutions[a.adaptiveResIdx]
    a.settings.Graphics.RenderWidth = r.Width
    a.settings.Graphics.RenderHeight = r.Height
    a.rt.Resize(r.Width, r.Height)
    a.pm.SetWindowSize(r.Width, r.Height)
    slog.Info("adaptive: step up", "resolution", r)
    return true
}

func (a *App) closestAdaptiveIdx(w, h int) int {
    best := 0
    bestDist := abs(a.adaptiveResolutions[0].Width*a.adaptiveResolutions[0].Height - w*h)
    for i, r := range a.adaptiveResolutions[1:] {
        dist := abs(r.Width*r.Height - w*h)
        if dist < bestDist {
            bestDist = dist
            best = i + 1
        }
    }
    return best
}
```

### 7. `internal/ui/settings_rows.go`
- `BuildSettingsRows` signature gains `renderScaleExplicit bool`
- Caller sites: `switchScreen`, `applySettings`, window resize handler — all pass `a.renderScaleExplicit`

```go
func BuildSettingsRows(s config.Settings, winW, winH int, renderScaleExplicit bool) []SettingRow {
    resolutions := config.ComputeResolutions(winW, winH)

    var resValues []string
    var resIndex int

    if !renderScaleExplicit {
        // Auto first
        resValues = append(resValues, "Auto")
        if s.Graphics.Adaptive {
            resIndex = 0
        } else {
            resIndex = 1 // default to first fixed res
            for i, r := range resolutions {
                if r.Width == s.Graphics.RenderWidth && r.Height == s.Graphics.RenderHeight {
                    resIndex = i + 1
                    break
                }
            }
        }
    } else {
        // No Auto — fixed-only numbering
        resIndex = 0
        for i, r := range resolutions {
            if r.Width == s.Graphics.RenderWidth && r.Height == s.Graphics.RenderHeight {
                resIndex = i
                break
            }
        }
    }

    for _, r := range resolutions {
        resValues = append(resValues, r.String())
    }

    // ... rest of row building unchanged
}
```

### 8. `internal/app/actions.go` — applySettings
```go
resIndex := rows[ui.SettingResolution].Index
resolutions := config.ComputeResolutions(winW, winH)

if a.renderScaleExplicit {
    // CLI override — apply fixed resolution, do NOT modify Adaptive field
    if resIndex >= 0 && resIndex < len(resolutions) {
        r := resolutions[resIndex]
        a.settings.Graphics.RenderWidth = r.Width
        a.settings.Graphics.RenderHeight = r.Height
        a.rt.Resize(r.Width, r.Height)
        a.pm.SetWindowSize(r.Width, r.Height)
    }
    a.resetAdaptiveCounters()
} else if resIndex == 0 {
    // Auto selected
    a.settings.Graphics.Adaptive = true
    a.resetAdaptiveState(winW, winH)
} else {
    // Fixed resolution selected (index 1..N maps to resolutions[0..N-1])
    a.settings.Graphics.Adaptive = false
    fixedIdx := resIndex - 1
    if fixedIdx >= 0 && fixedIdx < len(resolutions) {
        r := resolutions[fixedIdx]
        a.settings.Graphics.RenderWidth = r.Width
        a.settings.Graphics.RenderHeight = r.Height
        a.rt.Resize(r.Width, r.Height)
        a.pm.SetWindowSize(r.Width, r.Height)
    }
    a.resetAdaptiveCounters()
}
```

Helper on App:
```go
func (a *App) resetAdaptiveCounters() {
    a.adaptiveLowCount = 0
    a.adaptiveHighCount = 0
    a.adaptiveCooldown = time.Time{}
}

func (a *App) resetAdaptiveState(winW, winH int) {
    a.adaptiveResolutions = config.ComputeResolutions(winW, winH)
    if len(a.adaptiveResolutions) == 0 {
        a.resetAdaptiveCounters()
        return
    }
    a.adaptiveResIdx = 0
    r := a.adaptiveResolutions[0]
    a.settings.Graphics.RenderWidth = r.Width
    a.settings.Graphics.RenderHeight = r.Height
    a.rt.Resize(r.Width, r.Height)
    a.pm.SetWindowSize(r.Width, r.Height)
    a.resetAdaptiveCounters()
}
```

## Edge Cases
- **Window resize**: recompute list, snap index, reset counters+cooldown, apply if changed
- **Preset change**: no reset — different presets have different GPU cost, adaptation handles it
- **UI open**: still adapts (overlay is cheap, main cost is projectM render)
- **Manual switch Auto↔Fixed**: reset all adaptive state
- **Single available resolution**: adaptive does nothing (stepUp/stepDown return false)
- **ComputeResolutions returns nil/empty**: skip adaptive, log warning, don't touch resolution
- **-render-scale flag**: process-only override, `Adaptive` field preserved in settings, adaptive not used this run

## Verification
- `go build ./...` — compiles
- `go vet ./...` — no issues
- `go test ./...` — existing tests pass (after fixing positional literals)
- Unit tests:
  - Auto present when renderScaleExplicit=false, absent when true
  - Adaptive=true → index 0; Adaptive=false → index = fixedIdx + 1 (when Auto shown)
  - Fixed-only list direct-indexed when renderScaleExplicit=true
  - Startup at max resolution when Adaptive=true
  - stepUp returns false at index 0, stepDown returns false at last index
  - stepDown/stepUp return false on empty list
  - Window resize recomputes list and resets counters
  - resetAdaptiveState handles nil ComputeResolutions gracefully
  - Save/load roundtrip preserves Adaptive field
  - -render-scale hides Auto, skips adaptive, does not modify Adaptive in settings
