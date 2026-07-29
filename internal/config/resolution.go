package config

import "sort"

type scale struct{ num, den int }

// scaleCandidates are render scale fractions, tried descending (highest res first).
var scaleCandidates = []scale{
	{1, 1},   // 1.0
	{3, 4},   // 0.75
	{5, 8},   // 0.625
	{1, 2},   // 0.5
	{2, 5},   // 0.4
	{3, 8},   // 0.375
	{1, 4},   // 0.25
	{1, 5},   // 0.2
	{1, 8},   // 0.125
}

// ComputeResolutions generates valid RenderResolutions for a base window size.
// A candidate is included only when base×scale is an exact integer.
func ComputeResolutions(baseW, baseH int) []RenderResolution {
	seen := make(map[RenderResolution]bool)
	for _, s := range scaleCandidates {
		if (baseW*s.num)%s.den != 0 || (baseH*s.num)%s.den != 0 {
			continue
		}
		r := RenderResolution{Width: baseW * s.num / s.den, Height: baseH * s.num / s.den}
		if seen[r] {
			continue
		}
		seen[r] = true
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]RenderResolution, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Width*out[i].Height > out[j].Width*out[j].Height
	})
	return out
}

// ClosestResolution returns the resolution nearest to target by area.
func ClosestResolution(list []RenderResolution, target RenderResolution) RenderResolution {
	if len(list) == 0 {
		return target
	}
	targetArea := target.Width * target.Height
	best := list[0]
	bestDiff := abs(targetArea - best.Width*best.Height)
	for _, r := range list[1:] {
		d := abs(targetArea - r.Width*r.Height)
		if d < bestDiff {
			bestDiff = d
			best = r
		}
	}
	return best
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
