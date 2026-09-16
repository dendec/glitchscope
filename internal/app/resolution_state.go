package app

import (
	"time"

	"github.com/dendec/glitchscope/internal/config"
)

type resolutionState struct {
	resolutions  []config.RenderResolution
	index        int
	ceilingIndex int
	upscaleFloor int
	floorIndex   int
	downBlocked  bool
	trialAction  adaptiveAction
	trialIndex   int
	trialCost    float64
	trialCadence float64
	trialConfirm bool
	policy       adaptivePolicy
}

// Configure sets the resolution list and starts at the configured ceiling.
func (s *resolutionState) Configure(winW, winH int, ceiling config.RenderResolution, params config.ModeParams) bool {
	s.resolutions = config.ComputeResolutions(winW, winH)
	s.policy.params = params
	if len(s.resolutions) == 0 {
		s.index, s.ceilingIndex = 0, 0
		s.restartPolicy()
		return false
	}
	s.ceilingIndex = config.ClosestResolutionIndex(s.resolutions, ceiling)
	s.floorIndex = s.ceilingIndex
	limit := s.resolutions[s.ceilingIndex]
	divisor := max(params.ResolutionDivisor, 1)
	for i := s.ceilingIndex + 1; i < len(s.resolutions); i++ {
		r := s.resolutions[i]
		if r.Width*divisor < limit.Width || r.Height*divisor < limit.Height {
			break
		}
		s.floorIndex = i
	}
	s.index = s.ceilingIndex
	s.upscaleFloor = s.ceilingIndex
	s.restartPolicy()
	return true
}

// Reset starts a new adaptive session at the configured resolution ceiling.
func (s *resolutionState) Reset(winW, winH int, ceiling config.RenderResolution, params config.ModeParams) bool {
	return s.Configure(winW, winH, ceiling, params)
}

func (s *resolutionState) restartPolicy() {
	s.policy.Restart()
	s.upscaleFloor = s.ceilingIndex
	s.downBlocked = false
	s.trialAction = adaptiveNone
	s.trialConfirm = false
}

func (s *resolutionState) RestartForPreset() {
	s.restartPolicy()
}

func (s *resolutionState) Decide(utilization, cadence float64, now time.Time, currentFPS int32) (config.RenderResolution, int32, adaptiveAction) {
	// The caller supplies a fresh full measurement window after every change.
	cost := utilization / float64(max(currentFPS, 1))
	if s.trialAction != adaptiveNone {
		trial := s.trialAction
		cadenceImproved := cadence <= s.trialCadence*0.92 ||
			(s.trialCadence > cadenceTolerance && cadence <= cadenceTolerance)
		ineffective := trial == adaptiveResolutionDown && cost > s.trialCost*0.92 && !cadenceImproved
		if ineffective && !s.trialConfirm {
			s.trialConfirm = true
			return config.RenderResolution{}, currentFPS, adaptiveResample
		}
		s.trialAction = adaptiveNone
		s.trialConfirm = false
		tooExpensive := trial == adaptiveResolutionUp && (utilization > s.policy.params.UtilizationHigh*0.95 || cadence > cadenceTolerance)
		if ineffective || tooExpensive {
			if ineffective {
				s.downBlocked = true
			} else {
				s.upscaleFloor = s.trialIndex
			}
			s.index = s.trialIndex
			s.policy.Restart()
			if ineffective {
				return s.resolutions[s.index], currentFPS, adaptiveResolutionUp
			}
			return s.resolutions[s.index], currentFPS, adaptiveResolutionDown
		}
	}
	count := s.floorIndex + 1
	if s.downBlocked {
		count = s.index + 1
	}
	action := s.policy.Decide(utilization, cadence, now, int(currentFPS), s.index, max(s.ceilingIndex, s.upscaleFloor), count)
	switch action {
	case adaptiveFrequencyDown:
		return config.RenderResolution{}, max(s.policy.params.MinVisualizerFPS, currentFPS-s.policy.params.FrequencyStep), action
	case adaptiveFrequencyUp:
		return config.RenderResolution{}, min(s.policy.params.VisualizerFPS, currentFPS+s.policy.params.FrequencyStep), action
	case adaptiveResolutionDown:
		if s.index < s.floorIndex {
			s.trialAction, s.trialIndex, s.trialCost = action, s.index, cost
			s.trialCadence, s.trialConfirm = cadence, false
			s.index++
			return s.resolutions[s.index], currentFPS, action
		}
	case adaptiveResolutionUp:
		if s.index > s.ceilingIndex && s.index-1 >= s.upscaleFloor {
			s.trialAction, s.trialIndex, s.trialCost = action, s.index, cost
			s.trialCadence, s.trialConfirm = cadence, false
			s.index--
			return s.resolutions[s.index], currentFPS, action
		}
	}
	return config.RenderResolution{}, currentFPS, adaptiveNone
}
