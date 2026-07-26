package ui

import "time"

// This file owns the presets-page model (categories/cursors, equality check
// for change detection) and the marquee scroll animation shared by the
// left/right list columns across pages. Struct fields live in overlay.go.

// marqueeState tracks the scrolling animation for a single line that doesn't
// fit its panel. One instance per column (left/right); the active page reuses them.
type marqueeState struct {
	tex    uint32
	texW   int
	texH   int
	offset float32
	start  time.Time
}

func (m *marqueeState) reset() {
	m.offset = 0
	m.start = time.Time{}
}

func (m *marqueeState) invalidate(o *Overlay) {
	o.deleteTex(&m.tex)
	m.tex = 0
	m.offset = 0
	m.start = time.Time{}
}

const (
	marqueeDelay   = 1 * time.Second // pause before scrolling starts
	marqueeSpeed   = 60.0            // pixels per second
	marqueePauseAt = 1 * time.Second // pause at end before resetting
)

// updateMarquee advances the marquee scroll offset for both columns.
func (o *Overlay) updateMarquee(now time.Time) {
	o.updateMarqueeCol(&o.marqueeL, now)
	o.updateMarqueeCol(&o.marqueeR, now)
}

func (o *Overlay) updateMarqueeCol(m *marqueeState, now time.Time) {
	if m.tex == 0 || m.texW <= 0 {
		return
	}
	if m.start.IsZero() {
		m.start = now
		return
	}
	elapsed := now.Sub(m.start)
	if elapsed < marqueeDelay {
		return
	}
	scrollTime := elapsed - marqueeDelay
	// drawMarqueeCol clamps this phase to the active panel width.
	m.offset = float32(scrollTime.Seconds()) * marqueeSpeed
}

// invalidateActiveMarquee resets the marquee for the currently focused column.
func (o *Overlay) invalidateActiveMarquee() {
	if o.focusPanel == 0 {
		o.marqueeL.reset()
	} else {
		o.marqueeR.reset()
	}
}

// SetPresetCategories updates the presets model for the presets page.
// Only marks textures dirty if the content actually changed, since this may
// be called repeatedly (e.g. once at startup) with the same static data.
func (o *Overlay) SetPresetCategories(cats []PresetCat) {
	if presetCatsEqual(o.presetCategories, cats) {
		return
	}
	o.presetCategories = cats
	o.presetsDirty = true
}

func presetCatsEqual(a, b []PresetCat) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || len(a[i].Presets) != len(b[i].Presets) {
			return false
		}
		for j := range a[i].Presets {
			if a[i].Presets[j] != b[i].Presets[j] {
				return false
			}
		}
	}
	return true
}

// PresetCategoryCursor returns the current category cursor index.
func (o *Overlay) PresetCategoryCursor() int { return o.presetCategoryCursor }

// PresetCursor returns the current preset cursor index.
func (o *Overlay) PresetCursor() int { return o.presetCursor }

// SelectedPresetKey returns the full key of the preset the user selected on the presets page.
// Returns empty string if nothing valid is selected.
func (o *Overlay) SelectedPresetKey() string {
	if o.presetCategoryCursor >= len(o.presetCategories) {
		return ""
	}
	cat := o.presetCategories[o.presetCategoryCursor]
	if o.presetCursor >= len(cat.Presets) {
		return ""
	}
	return cat.Presets[o.presetCursor]
}

// syncPresetCursors positions category/preset cursors on the currently
// playing preset and focuses the presets (right) panel, so the Presets page
// opens with the cursor on the active preset — mirroring FocusPlayingTrack
// on the Library page.
func (o *Overlay) syncPresetCursors() {
	key := o.presetName
	if key == "" {
		return
	}
	for ci, cat := range o.presetCategories {
		for pi, p := range cat.Presets {
			if p == key {
				o.presetCategoryCursor = ci
				o.presetCursor = pi
				o.focusPanel = 1
				o.presetsDirty = true
				return
			}
		}
	}
}

// currentCategory returns the currently focused preset category, or nil.
func (o *Overlay) currentCategory() *PresetCat {
	if o.presetCategoryCursor >= len(o.presetCategories) {
		return nil
	}
	return &o.presetCategories[o.presetCategoryCursor]
}
