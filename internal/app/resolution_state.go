package app

import (
	"time"

	"github.com/dendec/pmv/internal/config"
)

type resolutionState struct {
	resolutions []config.RenderResolution
	index       int
	policy      adaptivePolicy
}

func (s *resolutionState) Configure(winW, winH int, current config.RenderResolution) bool {
	s.resolutions = config.ComputeResolutions(winW, winH)
	if len(s.resolutions) == 0 {
		s.index = 0
		s.policy.Reset()
		return false
	}
	s.index = config.ClosestResolutionIndex(s.resolutions, current)
	s.policy.Reset()
	return true
}

func (s *resolutionState) Reset(winW, winH int) bool {
	s.resolutions = config.ComputeResolutions(winW, winH)
	if len(s.resolutions) == 0 {
		s.index = 0
		s.policy.Reset()
		return false
	}
	s.index = 0
	s.policy.Reset()
	return true
}

func (s *resolutionState) Decide(now time.Time, fps float64) (config.RenderResolution, int, bool) {
	next, changed := s.policy.Decide(now, fps, s.index, len(s.resolutions))
	if !changed {
		return config.RenderResolution{}, 0, false
	}
	direction := next - s.index
	s.index = next
	return s.resolutions[next], direction, true
}
