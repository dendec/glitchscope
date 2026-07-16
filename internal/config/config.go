// Package config handles graphics settings types, validation, and persistence.
package config

import (
	"encoding/json"
	"fmt"
)

// RenderResolution represents a fixed render size.
type RenderResolution struct {
	Width  int
	Height int
}

func (r RenderResolution) String() string {
	return fmt.Sprintf("%dx%d", r.Width, r.Height)
}

// UpscaleFilter selects the GL texture filter for the final upscale pass.
type UpscaleFilter int

const (
	FilterSmooth UpscaleFilter = iota // GL_LINEAR
	FilterPixel                        // GL_NEAREST
)

func (f UpscaleFilter) String() string {
	switch f {
	case FilterSmooth:
		return "Smooth"
	case FilterPixel:
		return "Pixel"
	default:
		return "Unknown"
	}
}

// IsNearest returns true when the filter requires GL_NEAREST.
func (f UpscaleFilter) IsNearest() bool { return f == FilterPixel }

// MarshalJSON outputs the lower-case label used in settings.json.
func (f UpscaleFilter) MarshalJSON() ([]byte, error) {
	var s string
	switch f {
	case FilterSmooth:
		s = "smooth"
	case FilterPixel:
		s = "pixel"
	default:
		s = "unknown"
	}
	return json.Marshal(s)
}

// UnmarshalJSON reads the lower-case label from settings.json.
func (f *UpscaleFilter) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	switch s {
	case "smooth":
		*f = FilterSmooth
	case "pixel":
		*f = FilterPixel
	default:
		return fmt.Errorf("unknown upscale filter: %s", s)
	}
	return nil
}

// AllFilters returns every valid UpscaleFilter in display order.
func AllFilters() []UpscaleFilter { return []UpscaleFilter{FilterSmooth, FilterPixel} }

// GraphicsSettings is the persisted set of user-tunable graphics parameters.
type GraphicsSettings struct {
	RenderWidth   int           `json:"render_width"`
	RenderHeight  int           `json:"render_height"`
	UpscaleFilter UpscaleFilter `json:"upscale_filter"`
}

// DefaultGraphics returns sensible defaults, matching the current
// projectM preset domain and the smallest commonly valid resolution.
func DefaultGraphics() GraphicsSettings {
	return GraphicsSettings{
		RenderWidth:   320,
		RenderHeight:  240,
		UpscaleFilter: FilterPixel,
	}
}

// Validate returns an error if any field is out of range.
func (g *GraphicsSettings) Validate() error {
	if g.RenderWidth < 1 || g.RenderHeight < 1 {
		return fmt.Errorf("render dimensions must be positive, got %dx%d", g.RenderWidth, g.RenderHeight)
	}
	if g.UpscaleFilter < FilterSmooth || g.UpscaleFilter > FilterPixel {
		return fmt.Errorf("invalid upscale filter %d", g.UpscaleFilter)
	}
	return nil
}
