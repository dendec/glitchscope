// Package presets manages the preset archive store and user preset files.
package presets

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
)

// PresetMeta holds metadata extracted from a .milk preset file.
// Fields reflect values available in the [preset00] section.
type PresetMeta struct {
	Rating        float64 // fRating (0–5)
	Decay         float64 // fDecay
	WarpSpeed     float64 // fWarpAnimSpeed
	VideoEchoZoom float64 // fVideoEchoZoom
	WaveMode      int     // nWaveMode (0–10)
	Shapes        int     // count of shapecode_N_enabled=1
	Waves         int     // count of wavecode_N_enabled=1
	PerFrameEqs   int     // count of per_frame_N= lines
	PerPixelEqs   int     // count of per_pixel_N= lines
}

// ParseMeta extracts metadata from .milk preset content.
// It handles real-world .milk files:
//   - Recognizes [preset00] section header; keys outside sections are ignored
//   - Accepts whitespace around =
//   - Ignores empty lines and // comment lines
//   - Handles repeated keys: last value wins
//   - Counts *_enabled=1 for shapecode and wavecode
//   - Counts per_frame_N and per_pixel_N assignment lines
//   - Unknown keys are silently ignored
//
// Returns partial metadata with nil error for unparseable files.
func ParseMeta(data []byte) PresetMeta {
	var m PresetMeta

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(data, 64*1024) // large presets may have long lines

	inPreset := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		// Section header.
		if line == "[preset00]" {
			inPreset = true
			continue
		}
		// Other sections (e.g. [en]) — skip their content.
		if strings.HasPrefix(line, "[") {
			inPreset = false
			continue
		}
		if !inPreset {
			continue
		}

		// Split key=value with whitespace around =.
		eqIdx := strings.Index(line, "=")
		if eqIdx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eqIdx])
		val := strings.TrimSpace(line[eqIdx+1:])

		switch {
		case key == "fRating":
			m.Rating = parseFloat(val)
		case key == "fDecay":
			m.Decay = parseFloat(val)
		case key == "fWarpAnimSpeed":
			m.WarpSpeed = parseFloat(val)
		case key == "fVideoEchoZoom":
			m.VideoEchoZoom = parseFloat(val)
		case key == "nWaveMode":
			m.WaveMode = parseInt(val)
		case strings.HasPrefix(key, "shapecode_") && strings.HasSuffix(key, "_enabled"):
			if val == "1" {
				m.Shapes++
			}
		case strings.HasPrefix(key, "wavecode_") && strings.HasSuffix(key, "_enabled"):
			if val == "1" {
				m.Waves++
			}
		case strings.HasPrefix(key, "per_frame_"):
			if _, err := strconv.Atoi(key[len("per_frame_"):]); err == nil {
				m.PerFrameEqs++
			}
		case strings.HasPrefix(key, "per_pixel_"):
			if _, err := strconv.Atoi(key[len("per_pixel_"):]); err == nil {
				m.PerPixelEqs++
			}
		}
	}

	return m
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func parseInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
