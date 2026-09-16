package config

import "sort"

type scale struct{ num, den int }

// scaleCandidates are render scale fractions, tried descending (highest res first).
var scaleCandidates = []scale{
	{1, 1},  // 1.0
	{3, 4},  // 0.75
	{5, 8},  // 0.625
	{1, 2},  // 0.5
	{3, 8},  // 0.375
	{1, 4},  // 0.25
	{3, 16}, // 0.1875
	{1, 8},  // 0.125
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

// ClosestResolutionIndex returns the index of the resolution nearest to target by area.
func ClosestResolutionIndex(list []RenderResolution, target RenderResolution) int {
	if len(list) == 0 {
		return 0
	}
	targetArea := target.Width * target.Height
	best := 0
	bestDiff := max(targetArea-list[0].Width*list[0].Height, list[0].Width*list[0].Height-targetArea)
	for i, r := range list[1:] {
		d := max(targetArea-r.Width*r.Height, r.Width*r.Height-targetArea)
		if d < bestDiff {
			bestDiff = d
			best = i + 1
		}
	}
	return best
}

// ClosestResolution returns the resolution nearest to target by area.
func ClosestResolution(list []RenderResolution, target RenderResolution) RenderResolution {
	if len(list) == 0 {
		return target
	}
	return list[ClosestResolutionIndex(list, target)]
}

// ResolutionAtMost returns the largest available resolution whose area does
// not exceed target. The list is sorted from largest to smallest, so the
// first matching entry is the configured quality ceiling. If every available
// entry is larger than target, the smallest entry is returned.
func ResolutionAtMost(list []RenderResolution, target RenderResolution) RenderResolution {
	if len(list) == 0 {
		return target
	}
	targetArea := target.Width * target.Height
	for _, r := range list {
		if r.Width*r.Height <= targetArea {
			return r
		}
	}
	return list[len(list)-1]
}
