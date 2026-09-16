package app

import (
	"crypto/sha256"

	"github.com/dendec/glitchscope/internal/config"
)

const maxPresetProfiles = 256

type presetProfileKey struct {
	name          string
	frameRate     config.FrameRate
	targetFPS     int32
	adaptive      bool
	width, height int
	ceilingIndex  int
}

type presetProfile struct {
	digest       [sha256.Size]byte
	index, floor int
	stableFrames int
	candidate    int
	ready, heavy bool
}

// Session-local profiles cannot outlive a driver or device change. Frame rate,
// resolved target FPS, adaptive mode, drawable size and preset contents isolate
// incompatible measurements.
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
	return a.presetProfileKeyAt(name, int(w), int(h))
}

func (a *App) presetProfileKeyAt(name string, width, height int) presetProfileKey {
	return presetProfileKey{
		name:         name,
		frameRate:    a.settings.Graphics.FrameRate,
		targetFPS:    a.profileTargetFPS(),
		adaptive:     a.settings.Graphics.Adaptive,
		width:        width,
		height:       height,
		ceilingIndex: a.adaptive.ceilingIndex,
	}
}

func (a *App) activatePresetProfile(name string, data []byte) {
	profile := a.presetTuning.activate(a.presetProfileKey(name), data)
	if !a.settings.Graphics.Adaptive {
		return
	}
	warmIndex := a.adaptive.index
	a.renderCost.Reset()
	a.adaptive.startPreset(profile, warmIndex)
	if len(a.adaptive.resolutions) > 0 {
		a.applyRenderResolution(a.adaptive.resolutions[a.adaptive.index])
	}
}

// profileTargetFPS returns the resolved target used by the active profile key.
// The fallback keeps profile construction safe before the first loop tick.
func (a *App) profileTargetFPS() int32 {
	if a.targetVisualizerFPS > 0 {
		return a.targetVisualizerFPS
	}
	refreshRate := a.displayRefreshRate()
	return config.NormalizeFrameRate(a.settings.Graphics.FrameRate, refreshRate).Target(refreshRate)
}

// startPreset keeps a new preset at the current effective quality. A learned
// profile may choose a lower resolution, but never causes an immediate upscale.
func (s *resolutionState) startPreset(profile presetProfile, warmIndex int) {
	s.RestartForPreset()
	if len(s.resolutions) == 0 {
		return
	}
	warmIndex = min(max(warmIndex, s.ceilingIndex), s.floorIndex)
	s.index = warmIndex
	if profile.ready {
		profileIndex := min(max(profile.index, s.ceilingIndex), s.floorIndex)
		if profileIndex >= s.index {
			if profileIndex > s.index {
				s.index = profileIndex
			}
			s.upscaleFloor = min(max(profile.floor, s.ceilingIndex), s.index)
		}
	}
}
