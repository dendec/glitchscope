package app

import (
	"time"

	"github.com/dendec/glitchscope/internal/config"
)

type resolutionState struct {
	resolutions   []config.RenderResolution
	index         int
	ceilingIndex  int
	upscaleFloor  int
	floorIndex    int
	downBlocked   bool
	trialAction   adaptiveAction
	trialIndex    int
	trialCost     float64
	trialCadence  float64
	trialConfirm  bool
	searchActive  bool
	searchGood    int
	searchBad     int
	searchConfirm bool
	policy        adaptivePolicy
}

// Configure sets the resolution list and starts at the configured ceiling.
func (s *resolutionState) Configure(winW, winH int, ceiling config.RenderResolution, params adaptiveParams) bool {
	s.resolutions = config.ComputeResolutions(winW, winH)
	s.policy.params = params
	if len(s.resolutions) == 0 {
		s.index, s.ceilingIndex = 0, 0
		s.restartPolicy()
		return false
	}
	s.ceilingIndex = config.ClosestResolutionIndex(s.resolutions, ceiling)
	// Adaptive resolution uses the complete grid. The selected resolution is
	// always the ceiling; the smallest available step is the lower bound.
	s.floorIndex = len(s.resolutions) - 1
	s.index = s.ceilingIndex
	s.upscaleFloor = s.ceilingIndex
	s.restartPolicy()
	return true
}

// Reset starts a new adaptive session at the configured resolution ceiling.
func (s *resolutionState) Reset(winW, winH int, ceiling config.RenderResolution, params adaptiveParams) bool {
	return s.Configure(winW, winH, ceiling, params)
}

// Reconfigure rebuilds the resolution grid while retaining the current
// effective render size when it still fits the new ceiling.
func (s *resolutionState) Reconfigure(winW, winH int, ceiling, warm config.RenderResolution, params adaptiveParams) bool {
	if !s.Configure(winW, winH, ceiling, params) {
		return false
	}
	if warm.Width <= 0 || warm.Height <= 0 {
		return true
	}
	s.index = min(max(config.ClosestResolutionIndex(s.resolutions, warm), s.ceilingIndex), s.floorIndex)
	return true
}

func (s *resolutionState) restartPolicy() {
	s.policy.Restart()
	s.upscaleFloor = s.ceilingIndex
	s.downBlocked = false
	s.trialAction = adaptiveNone
	s.trialConfirm = false
	s.searchActive = false
	s.searchConfirm = false
}

func (s *resolutionState) RestartForPreset() {
	s.restartPolicy()
}

// startResolutionSearch treats the measured minimum as the known-good bound
// and probes the configured ceiling first. If it fails, the remaining interval
// is bisected between that known-bad ceiling and the passing minimum.
func (s *resolutionState) startResolutionSearch(utilization, cadence float64) (config.RenderResolution, adaptiveAction) {
	if len(s.resolutions) < 2 || s.ceilingIndex == s.floorIndex || s.index != s.floorIndex {
		return config.RenderResolution{}, adaptiveNone
	}
	s.searchGood = s.floorIndex
	s.searchBad = s.ceilingIndex
	s.searchConfirm = false
	s.searchActive = false
	if utilization > s.policy.params.utilizationHigh*0.95 || cadence > cadenceTolerance {
		s.upscaleFloor = s.ceilingIndex
		s.policy.Restart()
		return config.RenderResolution{}, adaptiveNone
	}
	s.searchActive = true
	return s.setResolutionSearchTrial(s.ceilingIndex)
}

func (s *resolutionState) Decide(utilization, cadence float64, now time.Time) (config.RenderResolution, adaptiveAction) {
	if s.searchActive {
		return s.decideResolutionSearch(utilization, cadence)
	}
	// The caller supplies a fresh full measurement window after every change.
	cost := utilization
	if s.trialAction != adaptiveNone {
		trial := s.trialAction
		cadenceImproved := cadence <= s.trialCadence*0.92 ||
			(s.trialCadence > cadenceTolerance && cadence <= cadenceTolerance)
		ineffective := trial == adaptiveResolutionDown && cost > s.trialCost*0.92 && !cadenceImproved
		if ineffective && !s.trialConfirm {
			s.trialConfirm = true
			return config.RenderResolution{}, adaptiveResample
		}
		s.trialAction = adaptiveNone
		s.trialConfirm = false
		tooExpensive := trial == adaptiveResolutionUp && (utilization > s.policy.params.utilizationHigh*0.95 || cadence > cadenceTolerance)
		if ineffective || tooExpensive {
			if ineffective {
				s.downBlocked = true
			} else {
				s.upscaleFloor = s.trialIndex
			}
			s.index = s.trialIndex
			s.policy.Restart()
			if ineffective {
				return s.resolutions[s.index], adaptiveResolutionUp
			}
			return s.resolutions[s.index], adaptiveResolutionDown
		}
	}
	count := s.floorIndex + 1
	if s.downBlocked {
		count = s.index + 1
	}
	action := s.policy.Decide(utilization, cadence, now, s.index, max(s.ceilingIndex, s.upscaleFloor), count)
	switch action {
	case adaptiveResolutionDown:
		if s.index < s.floorIndex {
			s.trialAction, s.trialIndex, s.trialCost = action, s.index, cost
			s.trialCadence, s.trialConfirm = cadence, false
			s.index++
			return s.resolutions[s.index], action
		}
	case adaptiveResolutionUp:
		if s.index > s.ceilingIndex && s.index-1 >= s.upscaleFloor {
			s.trialAction, s.trialIndex, s.trialCost = action, s.index, cost
			s.trialCadence, s.trialConfirm = cadence, false
			s.index--
			return s.resolutions[s.index], action
		}
	}
	return config.RenderResolution{}, adaptiveNone
}

func (s *resolutionState) decideResolutionSearch(utilization, cadence float64) (config.RenderResolution, adaptiveAction) {
	tooExpensive := utilization > s.policy.params.utilizationHigh*0.95 || cadence > cadenceTolerance
	if tooExpensive {
		if !s.searchConfirm {
			s.searchConfirm = true
			return config.RenderResolution{}, adaptiveResample
		}
		s.searchBad = s.index
	} else {
		s.searchGood = s.index
	}
	s.searchConfirm = false
	if !tooExpensive && s.index == s.ceilingIndex {
		return s.finishResolutionSearch()
	}
	if s.searchGood-s.searchBad <= 1 {
		return s.finishResolutionSearch()
	}
	return s.nextResolutionSearchTrial()
}

func (s *resolutionState) confirmSearchTrial() {
	if s.searchActive {
		s.searchConfirm = true
	}
}

func (s *resolutionState) nextResolutionSearchTrial() (config.RenderResolution, adaptiveAction) {
	if s.searchGood-s.searchBad <= 1 {
		return s.finishResolutionSearch()
	}
	candidate := s.searchBad + (s.searchGood-s.searchBad)/2
	return s.setResolutionSearchTrial(candidate)
}

func (s *resolutionState) setResolutionSearchTrial(candidate int) (config.RenderResolution, adaptiveAction) {
	previous := s.index
	s.index = candidate
	if candidate < previous {
		return s.resolutions[candidate], adaptiveResolutionUp
	}
	return s.resolutions[candidate], adaptiveResolutionDown
}

func (s *resolutionState) finishResolutionSearch() (config.RenderResolution, adaptiveAction) {
	previous := s.index
	s.index = s.searchGood
	s.upscaleFloor = s.searchGood
	s.searchActive = false
	s.searchConfirm = false
	s.policy.Restart()
	if previous == s.index {
		return config.RenderResolution{}, adaptiveNone
	}
	if s.index < previous {
		return s.resolutions[s.index], adaptiveResolutionUp
	}
	return s.resolutions[s.index], adaptiveResolutionDown
}
