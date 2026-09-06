package app

import "github.com/dendec/glitchscope/internal/config"

type resolutionState struct {
	resolutions  []config.RenderResolution
	index        int
	policy       adaptivePolicy
	upscaleTrial int
	upscaleFloor int
}

// Configure sets the resolution list and snaps to the closest match.
// params controls the adaptive thresholds for the current performance mode.
func (s *resolutionState) Configure(winW, winH int, current config.RenderResolution, params config.ModeParams) bool {
	s.resolutions = config.ComputeResolutions(winW, winH)
	if len(s.resolutions) == 0 {
		s.index = 0
		s.policy.params = params
		s.restartPolicy()
		return false
	}
	s.index = config.ClosestResolutionIndex(s.resolutions, current)
	s.policy.params = params
	s.restartPolicy()
	return true
}

// Reset snaps to the mode's starting resolution and resets adaptive state.
// The starting resolution is AdaptiveMaxIndex (0 = native, 1 = 0.75×, etc.).
func (s *resolutionState) Reset(winW, winH int, params config.ModeParams) bool {
	s.resolutions = config.ComputeResolutions(winW, winH)
	s.policy.params = params
	if len(s.resolutions) == 0 {
		s.index = 0
		s.restartPolicy()
		return false
	}
	s.index = params.AdaptiveMaxIndex
	if s.index >= len(s.resolutions) {
		s.index = len(s.resolutions) - 1
	}
	s.restartPolicy()
	return true
}

func (s *resolutionState) restartPolicy() {
	s.policy.Restart()
	s.upscaleTrial = -1
	s.upscaleFloor = 0
}

func (s *resolutionState) RestartForPreset() {
	s.restartPolicy()
}

func (s *resolutionState) Decide(fps float64) (config.RenderResolution, int, bool, bool) {
	next, changed, minReached := s.policy.Decide(fps, s.index, len(s.resolutions))
	if !changed {
		return config.RenderResolution{}, 0, false, minReached
	}
	// Enforce quality ceiling: don't scale up past AdaptiveMaxIndex.
	if next < s.policy.params.AdaptiveMaxIndex {
		next = s.policy.params.AdaptiveMaxIndex
		if next == s.index {
			return config.RenderResolution{}, 0, false, false
		}
	}
	direction := next - s.index
	if direction < 0 && next < s.upscaleFloor {
		s.policy.Reset()
		return config.RenderResolution{}, 0, false, false
	}
	if direction > 0 && s.upscaleTrial == s.index {
		s.upscaleFloor = next
		s.upscaleTrial = -1
	}
	if direction < 0 {
		s.upscaleTrial = next
	}
	s.index = next
	return s.resolutions[next], direction, true, false
}
