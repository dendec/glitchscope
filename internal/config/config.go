// Package config handles settings types, validation, and persistence.
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

// RepeatMode controls what happens when a track finishes.
type RepeatMode int

const (
	RepeatOff    RepeatMode = iota // stop after last track
	RepeatOne                      // restart current track
	RepeatAll                      // loop to first track of next/first album
)

func (m RepeatMode) String() string {
	switch m {
	case RepeatOff:
		return "Off"
	case RepeatOne:
		return "Repeat One"
	case RepeatAll:
		return "Repeat All"
	default:
		return "Unknown"
	}
}

// AllRepeatModes returns every valid RepeatMode in display order.
func AllRepeatModes() []RepeatMode { return []RepeatMode{RepeatOff, RepeatOne, RepeatAll} }

// ShuffleMode controls the scope of random track selection.
type ShuffleMode int

const (
	ShuffleOff   ShuffleMode = iota // sequential
	ShuffleAlbum                     // random within current album
	ShuffleLocal                     // random across local albums only
	ShuffleAll                       // random across all tracks (local + modland)
)

func (m ShuffleMode) String() string {
	switch m {
	case ShuffleOff:
		return "Off"
	case ShuffleAlbum:
		return "Shuffle Album"
	case ShuffleLocal:
		return "Shuffle Local"
	case ShuffleAll:
		return "Shuffle All"
	default:
		return "Unknown"
	}
}

// AllShuffleModes returns every valid ShuffleMode in display order.
func AllShuffleModes() []ShuffleMode { return []ShuffleMode{ShuffleOff, ShuffleAlbum, ShuffleLocal, ShuffleAll} }

// PlaybackSettings holds shuffle/repeat configuration.
type PlaybackSettings struct {
	ShuffleMode ShuffleMode `json:"shuffle_mode"`
	Repeat      RepeatMode  `json:"repeat"`
}

// DefaultPlayback returns sensible playback defaults.
func DefaultPlayback() PlaybackSettings {
	return PlaybackSettings{ShuffleMode: ShuffleOff, Repeat: RepeatOff}
}

// PresetInterval returns the auto-switch interval in seconds.
// 0 means off.
type PresetInterval int

const (
	PresetOff   PresetInterval = 0
	Preset15s   PresetInterval = 15
	Preset30s   PresetInterval = 30
	Preset60s   PresetInterval = 60
	Preset2m    PresetInterval = 120
)

func (p PresetInterval) String() string {
	switch p {
	case PresetOff:
		return "Off"
	case Preset15s:
		return "15s"
	case Preset30s:
		return "30s"
	case Preset60s:
		return "60s"
	case Preset2m:
		return "2m"
	default:
		return "Unknown"
	}
}

// AllPresetIntervals returns every valid PresetInterval in display order.
func AllPresetIntervals() []PresetInterval {
	return []PresetInterval{PresetOff, Preset15s, Preset30s, Preset60s, Preset2m}
}

// Settings is the full persisted settings envelope.
type Settings struct {
	Graphics      GraphicsSettings  `json:"graphics"`
	Playback      PlaybackSettings  `json:"playback"`
	PresetInterval PresetInterval   `json:"preset_interval"`
}

// DefaultSettings returns the full default settings.
func DefaultSettings() Settings {
	return Settings{
		Graphics:       DefaultGraphics(),
		Playback:       DefaultPlayback(),
		PresetInterval: PresetOff,
	}
}
