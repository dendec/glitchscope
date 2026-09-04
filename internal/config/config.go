// Package config handles settings types, validation, and persistence.
package config

import (
	"encoding/json"
	"fmt"
	"math"
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

// PerformanceMode selects a visualizer quality preset that trades frame rate
// for power consumption.
type PerformanceMode int

const (
	PerfModePerformance PerformanceMode = iota // 30 FPS visualizer, default
	PerfModeBalanced                            // 24 FPS, moderate savings
	PerfModeEco                                 // 15 FPS, maximum battery life
)

// ModeParams holds the tuning knobs for one performance mode.
type ModeParams struct {
	VisualizerFPS    int32
	AdaptiveThreshLow  float64
	AdaptiveThreshHigh float64
	AdaptiveLowFrames  int
	AdaptiveHighFrames int
	AdaptiveCooldown   int
	AdaptiveLowSec     float64
	AdaptiveHighSec    float64
	LowFPSThresh       float64
}

// Params returns the tuning parameters for mode m.
func (m PerformanceMode) Params() ModeParams {
	switch m {
	case PerfModeBalanced:
		return ModeParams{
			VisualizerFPS:     24,
			AdaptiveThreshLow:  17.0,
			AdaptiveThreshHigh: 20.0,
			AdaptiveLowFrames:  8,
			AdaptiveHighFrames: 10,
			AdaptiveCooldown:   12,
			AdaptiveLowSec:     2.0,
			AdaptiveHighSec:    4.0,
			LowFPSThresh:       12.0,
		}
	case PerfModeEco:
		return ModeParams{
			VisualizerFPS:     15,
			AdaptiveThreshLow:  11.0,
			AdaptiveThreshHigh: 14.0,
			AdaptiveLowFrames:  6,
			AdaptiveHighFrames: 8,
			AdaptiveCooldown:   15,
			AdaptiveLowSec:     3.0,
			AdaptiveHighSec:    6.0,
			LowFPSThresh:       7.5,
		}
	default: // PerfModePerformance
		return ModeParams{
			VisualizerFPS:     30,
			AdaptiveThreshLow:  20.0,
			AdaptiveThreshHigh: 24.0,
			AdaptiveLowFrames:  10,
			AdaptiveHighFrames: 10,
			AdaptiveCooldown:   10,
			AdaptiveLowSec:     2.0,
			AdaptiveHighSec:    4.0,
			LowFPSThresh:       15.0,
		}
	}
}

func (m PerformanceMode) String() string {
	switch m {
	case PerfModePerformance:
		return "Performance"
	case PerfModeBalanced:
		return "Balanced"
	case PerfModeEco:
		return "Eco"
	default:
		return "Unknown"
	}
}

func (m PerformanceMode) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.String())
}

func (m *PerformanceMode) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	switch s {
	case "Performance", "":
		*m = PerfModePerformance
	case "Balanced":
		*m = PerfModeBalanced
	case "Eco":
		*m = PerfModeEco
	default:
		return fmt.Errorf("unknown performance mode: %s", s)
	}
	return nil
}

func (m PerformanceMode) Validate() error {
	switch m {
	case PerfModePerformance, PerfModeBalanced, PerfModeEco:
		return nil
	default:
		return fmt.Errorf("invalid performance mode %d", m)
	}
}

// AllPerformanceModes returns all valid performance modes.
func AllPerformanceModes() []PerformanceMode {
	return []PerformanceMode{PerfModePerformance, PerfModeBalanced, PerfModeEco}
}

// GraphicsSettings is the persisted user-tunable graphics parameters.
type GraphicsSettings struct {
	RenderWidth     int             `json:"render_width"`
	RenderHeight    int             `json:"render_height"`
	UpscaleFilter   UpscaleFilter   `json:"upscale_filter"`
	Adaptive        bool            `json:"adaptive"`
	BeatSensitivity float64         `json:"beat_sensitivity"`
	PerformanceMode PerformanceMode `json:"performance_mode"`
}

// DefaultGraphics returns sensible defaults.
func DefaultGraphics() GraphicsSettings {
	return GraphicsSettings{
		RenderWidth:     320,
		RenderHeight:    240,
		UpscaleFilter:   FilterPixel,
		Adaptive:        true,
		BeatSensitivity: 1,
		PerformanceMode: PerfModePerformance,
	}
}

func (g *GraphicsSettings) Validate() error {
	if g.RenderWidth < 1 || g.RenderHeight < 1 {
		return fmt.Errorf("render dimensions must be positive, got %dx%d", g.RenderWidth, g.RenderHeight)
	}
	if g.UpscaleFilter < FilterSmooth || g.UpscaleFilter > FilterPixel {
		return fmt.Errorf("invalid upscale filter %d", g.UpscaleFilter)
	}
	if math.IsNaN(g.BeatSensitivity) || math.IsInf(g.BeatSensitivity, 0) || g.BeatSensitivity < 0 || g.BeatSensitivity > 2 {
		return fmt.Errorf("beat sensitivity must be 0..2, got %v", g.BeatSensitivity)
	}
	if err := g.PerformanceMode.Validate(); err != nil {
		return fmt.Errorf("performance mode: %w", err)
	}
	return nil
}

func AllBeatSensitivities() []float64 {
	return []float64{0, 0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2}
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
	ShuffleMode  ShuffleMode      `json:"shuffle_mode"`
	Repeat       RepeatMode       `json:"repeat"`
	LastPosition PlaybackPosition `json:"last_position"`
}

// PlaybackPosition identifies the track and offset to restore on next launch.
type PlaybackPosition struct {
	Path    string  `json:"path"`
	Seconds float64 `json:"seconds"`
}

func (p PlaybackPosition) Validate() error {
	if p.Path == "" {
		if p.Seconds != 0 {
			return fmt.Errorf("playback position requires a path")
		}
		return nil
	}
	if math.IsNaN(p.Seconds) || math.IsInf(p.Seconds, 0) || p.Seconds < 0 {
		return fmt.Errorf("playback position must be non-negative, got %v", p.Seconds)
	}
	return nil
}

func DefaultPlayback() PlaybackSettings {
	return PlaybackSettings{ShuffleMode: ShuffleOff, Repeat: RepeatOff}
}

func (p PlaybackSettings) Validate() error {
	if p.ShuffleMode < ShuffleOff || p.ShuffleMode > ShuffleAll {
		return fmt.Errorf("invalid shuffle mode %d", p.ShuffleMode)
	}
	if p.Repeat < RepeatOff || p.Repeat > RepeatAll {
		return fmt.Errorf("invalid repeat mode %d", p.Repeat)
	}
	if err := p.LastPosition.Validate(); err != nil {
		return err
	}
	return nil
}

// PresetInterval selects the preset auto-switch mode.
type PresetInterval int

const (
	PresetAuto PresetInterval = -1
	PresetOff  PresetInterval = 0
	Preset15s  PresetInterval = 15
	Preset30s  PresetInterval = 30
	Preset60s  PresetInterval = 60
	Preset2m   PresetInterval = 120
)

func (p PresetInterval) String() string {
	switch p {
	case PresetAuto:
		return "Auto"
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
	return []PresetInterval{PresetOff, PresetAuto, Preset15s, Preset30s, Preset60s, Preset2m}
}

func (p PresetInterval) Validate() error {
	for _, valid := range AllPresetIntervals() {
		if p == valid {
			return nil
		}
	}
	return fmt.Errorf("invalid preset interval %d", p)
}

// Theme selects the UI color scheme.
type Theme int

const (
	ThemeDark         Theme = iota // dark background, light text (default)
	ThemeLight                     // light background, dark text
	ThemeAmber                     // warm terminal palette
	ThemeCyan                      // cool night palette
	ThemeSolarized                 // Solarized dark palette
	ThemeHighContrast              // black background, yellow focus
	ThemeForest                    // deep green palette with a warm focus
	ThemeSynthwave                 // neon cyan and pink palette
	ThemeCherry                    // dark red palette with a mint focus
	themeCount
)

type themeSpec struct {
	theme Theme
	name  string
	json  string
}

var themeSpecs = [...]themeSpec{
	{ThemeDark, "Dark", "dark"},
	{ThemeLight, "Light", "light"},
	{ThemeAmber, "Amber", "amber"},
	{ThemeCyan, "Cyan", "cyan"},
	{ThemeSolarized, "Solarized", "solarized"},
	{ThemeHighContrast, "High Contrast", "high-contrast"},
	{ThemeForest, "Forest", "forest"},
	{ThemeSynthwave, "Synthwave", "synthwave"},
	{ThemeCherry, "Cherry", "cherry"},
}

func (t Theme) spec() (themeSpec, bool) {
	for _, spec := range themeSpecs {
		if spec.theme == t {
			return spec, true
		}
	}
	return themeSpec{}, false
}

func (t Theme) String() string {
	if spec, ok := t.spec(); ok {
		return spec.name
	}
	return "Unknown"
}

func AllThemes() []Theme {
	themes := make([]Theme, len(themeSpecs))
	for i, spec := range themeSpecs {
		themes[i] = spec.theme
	}
	return themes
}

func (t Theme) MarshalJSON() ([]byte, error) {
	if spec, ok := t.spec(); ok {
		return json.Marshal(spec.json)
	}
	return json.Marshal("unknown")
}

func (t *Theme) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	for _, spec := range themeSpecs {
		if spec.json == s {
			*t = spec.theme
			return nil
		}
	}
	return fmt.Errorf("unknown theme: %s", s)
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
		PresetInterval: PresetAuto,
		UI:             DefaultUI(),
	}
}

func (s Settings) Validate() error {
	if err := s.Graphics.Validate(); err != nil {
		return fmt.Errorf("graphics: %w", err)
	}
	if err := s.Playback.Validate(); err != nil {
		return fmt.Errorf("playback: %w", err)
	}
	if err := s.PresetInterval.Validate(); err != nil {
		return fmt.Errorf("preset interval: %w", err)
	}
	if _, ok := s.UI.Theme.spec(); !ok {
		return fmt.Errorf("ui theme: invalid theme %d", s.UI.Theme)
	}
	if err := s.UI.Transparency.Validate(); err != nil {
		return fmt.Errorf("ui transparency: %w", err)
	}
	return nil
}
