package app

import (
	"crypto/sha256"

	"github.com/dendec/glitchscope/internal/config"
)

const maxPresetProfiles = 256

type presetProfileKey struct {
	name          string
	mode          config.PerformanceMode
	width, height int
}

type presetProfile struct {
	digest       [sha256.Size]byte
	index, floor int
	stableFrames int
	candidate    int
	ready, heavy bool
}

// Session-local profiles cannot outlive a driver or device change. Mode,
// drawable size and preset contents isolate incompatible measurements.
type presetTuning struct {
	profiles map[presetProfileKey]presetProfile
	active   presetProfileKey
}

func (t *presetTuning) activate(key presetProfileKey, data []byte) presetProfile {
	if t.profiles == nil {
		t.profiles = make(map[presetProfileKey]presetProfile)
	}
	digest := sha256.Sum256(data)
	profile, exists := t.profiles[key]
	if !exists || profile.digest != digest {
		if !exists && len(t.profiles) >= maxPresetProfiles {
			for old := range t.profiles {
				delete(t.profiles, old)
				break
			}
		}
		profile = presetProfile{digest: digest}
	}
	profile.stableFrames = 0
	t.profiles[key] = profile
	t.active = key
	return profile
}

func (t *presetTuning) observe(index, floor int, stable bool) {
	profile, ok := t.profiles[t.active]
	if !ok {
		return
	}
	if !stable || profile.candidate != index {
		profile.stableFrames = 0
	}
	profile.candidate = index
	if stable {
		profile.stableFrames++
	}
	if profile.stableFrames >= 30 {
		profile.index, profile.floor = index, floor
		profile.ready, profile.heavy = true, false
		profile.stableFrames = 30
	}
	t.profiles[t.active] = profile
}

func (t *presetTuning) markHeavy() {
	profile, ok := t.profiles[t.active]
	if !ok {
		return
	}
	profile.heavy = true
	t.profiles[t.active] = profile
}

func (a *App) presetProfileKey(name string) presetProfileKey {
	w, h := a.window.GLGetDrawableSize()
	return presetProfileKey{name: name, mode: a.settings.Graphics.PerformanceMode, width: int(w), height: int(h)}
}

func (a *App) activatePresetProfile(name string, data []byte) {
	profile := a.presetTuning.activate(a.presetProfileKey(name), data)
	if a.settings.Graphics.PerformanceMode == config.PerfModeUltra || !a.settings.Graphics.Adaptive || !profile.ready {
		return
	}
	index := max(profile.index, a.settings.Graphics.PerformanceMode.Params().AdaptiveMaxIndex)
	if index < 0 || index >= len(a.adaptive.resolutions) {
		return
	}
	a.adaptive.index = index
	a.adaptive.upscaleFloor = profile.floor
	a.applyRenderResolution(a.adaptive.resolutions[index])
}
