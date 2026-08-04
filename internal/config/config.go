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
	FilterPixel                       // GL_NEAREST
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

func (f UpscaleFilter) IsNearest() bool { return f == FilterPixel }

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

func AllFilters() []UpscaleFilter { return []UpscaleFilter{FilterSmooth, FilterPixel} }

// GraphicsSettings is the persisted user-tunable graphics parameters.
type GraphicsSettings struct {
	RenderWidth   int           `json:"render_width"`
	RenderHeight  int           `json:"render_height"`
	UpscaleFilter UpscaleFilter `json:"upscale_filter"`
	Adaptive      bool          `json:"adaptive"`
}

// DefaultGraphics returns sensible defaults.
func DefaultGraphics() GraphicsSettings {
	return GraphicsSettings{
		RenderWidth:   320,
		RenderHeight:  240,
		UpscaleFilter: FilterPixel,
		Adaptive:      true,
	}
}

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
	RepeatOff RepeatMode = iota // stop after last track
	RepeatOne                   // restart current track
	RepeatAll                   // loop to first track of next/first album
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

func AllRepeatModes() []RepeatMode { return []RepeatMode{RepeatOff, RepeatOne, RepeatAll} }

// ShuffleMode controls the scope of random track selection.
type ShuffleMode int

const (
	ShuffleOff   ShuffleMode = iota // sequential
	ShuffleAlbum                    // random within current album
	ShuffleLocal                    // random across local albums only
	ShuffleAll                      // random across all tracks (local + modland)
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

func AllShuffleModes() []ShuffleMode {
	return []ShuffleMode{ShuffleOff, ShuffleAlbum, ShuffleLocal, ShuffleAll}
}

// PlaybackSettings holds shuffle/repeat configuration.
type PlaybackSettings struct {
	ShuffleMode ShuffleMode `json:"shuffle_mode"`
	Repeat      RepeatMode  `json:"repeat"`
}

func DefaultPlayback() PlaybackSettings {
	return PlaybackSettings{ShuffleMode: ShuffleOff, Repeat: RepeatOff}
}

// PresetInterval returns the auto-switch interval in seconds (0 = off).
type PresetInterval int

const (
	PresetOff PresetInterval = 0
	Preset15s PresetInterval = 15
	Preset30s PresetInterval = 30
	Preset60s PresetInterval = 60
	Preset2m  PresetInterval = 120
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

func AllPresetIntervals() []PresetInterval {
	return []PresetInterval{PresetOff, Preset15s, Preset30s, Preset60s, Preset2m}
}

// Theme selects the UI color scheme.
type Theme int

const (
	ThemeDark  Theme = iota // dark background, light text (default)
	ThemeLight              // light background, dark text
)

func (t Theme) String() string {
	switch t {
	case ThemeDark:
		return "Dark"
	case ThemeLight:
		return "Light"
	default:
		return "Unknown"
	}
}

func AllThemes() []Theme { return []Theme{ThemeDark, ThemeLight} }

func (t Theme) MarshalJSON() ([]byte, error) {
	var s string
	switch t {
	case ThemeDark:
		s = "dark"
	case ThemeLight:
		s = "light"
	default:
		s = "unknown"
	}
	return json.Marshal(s)
}

func (t *Theme) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	switch s {
	case "dark":
		*t = ThemeDark
	case "light":
		*t = ThemeLight
	default:
		return fmt.Errorf("unknown theme: %s", s)
	}
	return nil
}

// Transparency sets UI overlay opacity (0–100).
type Transparency int

func (t Transparency) String() string { return fmt.Sprintf("%d%%", int(t)) }

// AllTransparencies returns every valid Transparency in display order.
func AllTransparencies() []Transparency {
	return []Transparency{0, 10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
}

func (t Transparency) Validate() error {
	if t < 0 || t > 100 {
		return fmt.Errorf("transparency must be 0..100, got %d", int(t))
	}
	return nil
}

// UISettings holds theme and transparency.
type UISettings struct {
	Theme        Theme        `json:"theme"`
	Transparency Transparency `json:"transparency"`
}

func DefaultUI() UISettings {
	return UISettings{Theme: ThemeDark, Transparency: 0}
}

// Settings is the full persisted settings envelope.
type Settings struct {
	Graphics       GraphicsSettings `json:"graphics"`
	Playback       PlaybackSettings `json:"playback"`
	PresetInterval PresetInterval   `json:"preset_interval"`
	UI             UISettings       `json:"ui"`
}

func DefaultSettings() Settings {
	return Settings{
		Graphics:       DefaultGraphics(),
		Playback:       DefaultPlayback(),
		PresetInterval: PresetOff,
		UI:             DefaultUI(),
	}
}
