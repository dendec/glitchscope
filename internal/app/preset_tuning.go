package app

import (
	"crypto/sha256"
	"time"

	"github.com/dendec/glitchscope/internal/config"
)

const maxPresetProfiles = 30_000

const (
	heavyPresetMinFPS      = 20
	heavyPresetFrameBudget = time.Second / heavyPresetMinFPS
	presetProbeConfirm     = 500 * time.Millisecond
	presetProbeMaxGap      = 500 * time.Millisecond
	presetProbeFastFrame   = 2 * heavyPresetFrameBudget
	presetProbeWarmup      = 5
	presetProbeSamples     = 3
	presetFastUpshiftAfter = 500 * time.Millisecond
)

type presetProbeResult uint8

const (
	presetProbePending presetProbeResult = iota
	presetProbePassed
	presetProbeHeavy
)

// presetPerformanceProbe requires sustained frame cost over a short rolling
// median so isolated driver or system stalls do not blacklist a preset.
type presetPerformanceProbe struct {
	active      bool
	warmup      int
	values      [presetProbeSamples]time.Duration
	count, next int
	slowSince   time.Time
	normalSince time.Time
	lastSample  time.Time
	lastCost    time.Duration
	fastValues  [presetProbeSamples]time.Duration
	fastCount   int
	fastNext    int
}

func (p *presetPerformanceProbe) Start() {
	*p = presetPerformanceProbe{active: true, warmup: presetProbeWarmup}
}

func (p *presetPerformanceProbe) Stop() {
	p.active = false
	p.resetWindow()
}

func (p *presetPerformanceProbe) resetWindow() {
	p.values = [presetProbeSamples]time.Duration{}
	p.count, p.next = 0, 0
	p.slowSince, p.normalSince = time.Time{}, time.Time{}
	p.lastSample = time.Time{}
	p.lastCost = 0
	p.fastValues = [presetProbeSamples]time.Duration{}
	p.fastCount, p.fastNext = 0, 0
}

func (p *presetPerformanceProbe) Observe(cost time.Duration, now time.Time) presetProbeResult {
	if !p.active || cost <= 0 {
		return presetProbePending
	}
	if !p.lastSample.IsZero() && now.Sub(p.lastSample) > max(presetProbeMaxGap, 2*p.lastCost) {
		p.resetWindow()
	}
	p.lastSample = now
	p.lastCost = cost
	p.fastValues[p.fastNext] = cost
	p.fastNext = (p.fastNext + 1) % len(p.fastValues)
	if p.fastCount < len(p.fastValues) {
		p.fastCount++
	}
	if p.fastCount == len(p.fastValues) {
		median := medianDuration3(p.fastValues)
		if median > heavyPresetFrameBudget && median >= presetProbeFastFrame {
			p.Stop()
			return presetProbeHeavy
		}
	}
	if p.warmup > 0 {
		p.warmup--
		return presetProbePending
	}
	p.values[p.next] = cost
	p.next = (p.next + 1) % len(p.values)
	if p.count < len(p.values) {
		p.count++
	}
	if p.count < len(p.values) {
		return presetProbePending
	}

	if medianDuration3(p.values) > heavyPresetFrameBudget {
		p.normalSince = time.Time{}
		if p.slowSince.IsZero() {
			p.slowSince = now
		}
		if now.Sub(p.slowSince) >= presetProbeConfirm {
			p.Stop()
			return presetProbeHeavy
		}
		return presetProbePending
	}

	p.slowSince = time.Time{}
	if p.normalSince.IsZero() {
		p.normalSince = now
	}
	if now.Sub(p.normalSince) >= presetProbeConfirm {
		p.Stop()
		return presetProbePassed
	}
	return presetProbePending
}

func medianDuration3(values [presetProbeSamples]time.Duration) time.Duration {
	if values[0] > values[1] {
		values[0], values[1] = values[1], values[0]
	}
	if values[1] > values[2] {
		values[1], values[2] = values[2], values[1]
	}
	if values[0] > values[1] {
		values[0], values[1] = values[1], values[0]
	}
	return values[1]
}

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
	ready        bool
	screened     bool
	heavy        bool
}

// Persisted profiles are isolated by frame rate, resolved target FPS, adaptive
// mode, drawable size, resolution ceiling and preset contents.
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
		profile.ready = true
		profile.stableFrames = 30
	}
	t.profiles[t.active] = profile
}

func (t *presetTuning) markHeavy() {
	profile, ok := t.profiles[t.active]
	if !ok {
		return
	}
	profile.screened, profile.heavy = true, true
	t.profiles[t.active] = profile
}

func (t *presetTuning) markScreened() {
	profile, ok := t.profiles[t.active]
	if !ok {
		return
	}
	profile.screened = true
	t.profiles[t.active] = profile
}

func (t *presetTuning) recordResolution(index, floor int) {
	profile, ok := t.profiles[t.active]
	if !ok {
		return
	}
	profile.index, profile.floor = index, floor
	profile.stableFrames, profile.candidate = 30, index
	profile.ready = true
	t.profiles[t.active] = profile
}

func (t *presetTuning) isHeavy(key presetProfileKey) bool {
	return t.profiles[key].heavy
}

func presetNeedsPerformanceProbe(adaptive bool, resolutionCount int, profile presetProfile, known bool) bool {
	return adaptive && resolutionCount > 0 && (!known || (!profile.screened && !profile.heavy))
}

func samePresetProfileContext(a, b presetProfileKey) bool {
	a.name, b.name = "", ""
	return a == b
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
	key := a.presetProfileKey(name)
	contextChanged := !samePresetProfileContext(key, a.presetTuning.active)
	profile := a.presetTuning.activate(key, data)
	if contextChanged {
		a.refreshPresetTree()
	}
	a.presetProbe.Stop()
	if !a.settings.Graphics.Adaptive && !a.presetPackCalibrationMeasuring() {
		return
	}
	a.adaptive.policy.params = defaultAdaptiveParams()
	warmIndex := a.adaptive.index
	a.renderCost.Reset()
	a.adaptive.startPreset(profile, warmIndex)
	forceProbe := a.presetPackCalibrationMeasuring() && a.presetPackTest.current == name
	if len(a.adaptive.resolutions) > 0 && name != "" && (forceProbe || (!profile.screened && !profile.heavy)) {
		a.adaptive.startPresetProbe()
		a.presetProbe.Start()
	}
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

// startPreset restores a compatible learned resolution, otherwise retaining
// the current effective quality until the new profile is measured.
func (s *resolutionState) startPreset(profile presetProfile, warmIndex int) {
	s.RestartForPreset()
	if len(s.resolutions) == 0 {
		return
	}
	warmIndex = min(max(warmIndex, s.ceilingIndex), s.floorIndex)
	s.index = warmIndex
	if profile.ready {
		profileIndex := min(max(profile.index, s.ceilingIndex), s.floorIndex)
		s.index = profileIndex
		s.upscaleFloor = min(max(profile.floor, s.ceilingIndex), s.index)
	}
}

func (s *resolutionState) startPresetProbe() {
	if len(s.resolutions) == 0 {
		return
	}
	s.RestartForPreset()
	s.index = s.floorIndex
}
