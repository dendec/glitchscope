package ui

// SetPresetCategories updates the presets model. Only marks dirty if changed.
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

func (o *Overlay) PresetCategoryCursor() int { return o.presetCategoryCursor }

func (o *Overlay) PresetCursor() int { return o.presetCursor }

// SelectedPresetKey returns the full key of the selected preset.
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

// syncPresetCursors positions cursors on the currently playing preset.
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

func (o *Overlay) currentCategory() *PresetCat {
	if o.presetCategoryCursor >= len(o.presetCategories) {
		return nil
	}
	return &o.presetCategories[o.presetCategoryCursor]
}
