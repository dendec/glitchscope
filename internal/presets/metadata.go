// Package presets manages the preset archive store and user preset files.
package presets

import (
	"bufio"
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

var samplerRE = regexp.MustCompile(`(?i)\bsampler\s+sampler_([a-z0-9_]+)\b`)

// TextureReference describes one external MilkDrop sampler declaration.
type TextureReference struct {
	Sampler string
	Texture string
	Kind    string
}

// PresetMeta holds metadata extracted from a .milk preset file, including
// values from [preset00] and texture dependencies declared in shader code.
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
	Textures      int     // count of unique external texture names
}

// metaCache stores parsed metadata keyed by preset key.
// Populated lazily by ReadMeta; invalidated on store reload.
var metaCache = make(map[string]PresetMeta)

// ReadMeta returns cached metadata for key, parsing on first access.
// Safe to call from any goroutine (cache is populated once per key,
// never mutated after; map reads are safe when no concurrent writes).
func ReadMeta(key string) PresetMeta {
	if m, ok := metaCache[key]; ok {
		return m
	}
	data, err := Read(key)
	if err != nil {
		return PresetMeta{}
	}
	m := ParseMeta(data)
	metaCache[key] = m
	return m
}

// InvalidateMetaCache clears the metadata cache (call on store reload).
func InvalidateMetaCache() {
	metaCache = make(map[string]PresetMeta)
}

// ParseMeta extracts metadata from .milk preset content.
// It handles real-world .milk files:
//   - Recognizes [preset00] section header; keys outside sections are ignored
//   - Accepts whitespace around =
//   - Ignores empty lines and // comment lines
//   - Handles repeated keys: last value wins
//   - Counts *_enabled=1 for shapecode and wavecode
//   - Counts per_frame_N and per_pixel_N assignment lines
//   - Counts unique external textures declared by shader samplers
//   - Unknown keys are silently ignored
//
// Returns partial metadata with nil error for unparseable files.
func ParseMeta(data []byte) PresetMeta {
	m := PresetMeta{Textures: countUniqueTextures(TextureReferences(data))}

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

// TextureReferences extracts unique external sampler declarations from a
// MilkDrop preset. Built-in render targets are not texture-file dependencies.
func TextureReferences(data []byte) []TextureReference {
	seen := make(map[string]bool)
	var references []TextureReference
	for _, match := range samplerRE.FindAllSubmatch(data, -1) {
		name := string(match[1])
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		texture, kind := normalizeTexture(name)
		if kind == "builtin" {
			continue
		}
		references = append(references, TextureReference{
			Sampler: "sampler_" + name,
			Texture: texture,
			Kind:    kind,
		})
	}
	return references
}

func countUniqueTextures(references []TextureReference) int {
	seen := make(map[string]bool)
	for _, reference := range references {
		seen[strings.ToLower(reference.Texture)] = true
	}
	return len(seen)
}

func normalizeTexture(name string) (string, string) {
	lower := strings.ToLower(name)
	if lower == "main" || lower == "blur1" || lower == "blur2" || lower == "blur3" || strings.HasPrefix(lower, "noise") {
		return lower, "builtin"
	}
	if strings.HasPrefix(lower, "rand") && len(lower) >= 6 && lower[4] >= '0' && lower[4] <= '9' && lower[5] >= '0' && lower[5] <= '9' {
		return lower, "random"
	}
	if len(lower) > 3 && lower[2] == '_' {
		prefix := lower[:3]
		if prefix == "fc_" || prefix == "cf_" || prefix == "fw_" || prefix == "wf_" || prefix == "pc_" || prefix == "cp_" || prefix == "pw_" || prefix == "wp_" {
			return lower[3:], "file"
		}
	}
	return lower, "file"
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func parseInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
