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
	PerfModeBalanced                           // 24 FPS, moderate savings
	PerfModeEco                                // 24 FPS, lower resolution for battery life
)

// ModeParams holds the tuning knobs for one performance mode.
type ModeParams struct {
	VisualizerFPS      int32
	AdaptiveThreshLow  float64
	AdaptiveThreshHigh float64
	AdaptiveLowFrames  int
	AdaptiveHighFrames int
	AdaptiveCooldown   int
	AdaptiveLowSec     float64
	AdaptiveHighSec    float64
	LowFPSThresh       float64
	// AdaptiveMaxIndex is the highest resolution index the adaptive algorithm
	// may scale up to. 0 = no limit (may reach native resolution). Higher
	// indices are lower resolutions, so this caps the "quality ceiling".
	AdaptiveMaxIndex int
}

// Params returns the tuning parameters for mode m.
func (m PerformanceMode) Params() ModeParams {
	switch m {
	case PerfModeBalanced:
		return ModeParams{
			VisualizerFPS:      24,
			AdaptiveThreshLow:  18.0,
			AdaptiveThreshHigh: 21.0,
			AdaptiveLowFrames:  8,
			AdaptiveHighFrames: 10,
			AdaptiveCooldown:   12,
			AdaptiveLowSec:     2.0,
			AdaptiveHighSec:    4.0,
			LowFPSThresh:       12.0,
			AdaptiveMaxIndex:   1, // cap at 0.75× (960×540)
		}
	case PerfModeEco:
		return ModeParams{
			VisualizerFPS:      24,
			AdaptiveThreshLow:  16.0,
			AdaptiveThreshHigh: 19.0,
			AdaptiveLowFrames:  6,
			AdaptiveHighFrames: 8,
			AdaptiveCooldown:   15,
			AdaptiveLowSec:     3.0,
			AdaptiveHighSec:    6.0,
			LowFPSThresh:       10.0,
			AdaptiveMaxIndex:   3, // cap at 0.5× (640×360)
		}
	default: // PerfModePerformance
		return ModeParams{
			VisualizerFPS:      30,
			AdaptiveThreshLow:  23.0,
			AdaptiveThreshHigh: 27.0,
			AdaptiveLowFrames:  10,
			AdaptiveHighFrames: 10,
			AdaptiveCooldown:   10,
			AdaptiveLowSec:     2.0,
			AdaptiveHighSec:    4.0,
			LowFPSThresh:       15.0,
			AdaptiveMaxIndex:   0, // no cap — may reach native
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
	VisualizerOff   bool            `json:"visualizer_off"`
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

// RepeatMode JSON: lowercase strings with integer fallback for legacy files.

func (m RepeatMode) MarshalJSON() ([]byte, error) {
	var s string
	switch m {
	case RepeatOff:
		s = "off"
	case RepeatOne:
		s = "repeat_one"
	case RepeatAll:
		s = "repeat_all"
	default:
		s = "unknown"
	}
	return json.Marshal(s)
}

func (m *RepeatMode) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		switch s {
		case "off":
			*m = RepeatOff
			return nil
		case "repeat_one":
			*m = RepeatOne
			return nil
		case "repeat_all":
			*m = RepeatAll
			return nil
		default:
			return fmt.Errorf("unknown repeat mode: %s", s)
		}
	}
	// Legacy: accept integer.
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("invalid repeat mode: %w", err)
	}
	if n < int(RepeatOff) || n > int(RepeatAll) {
		return fmt.Errorf("invalid repeat mode: %d", n)
	}
	*m = RepeatMode(n)
	return nil
}

// ShuffleMode controls the scope of random track selection.
type ShuffleMode int

const (
	ShuffleOff    ShuffleMode = iota // sequential
	ShuffleAlbum                     // random within current album
	ShuffleSource                    // random across the current source
	ShuffleAll                       // random across all tracks and sources
)

func (m ShuffleMode) String() string {
	switch m {
	case ShuffleOff:
		return "Off"
	case ShuffleAlbum:
		return "Shuffle Album"
	case ShuffleSource:
		return "Shuffle Source"
	case ShuffleAll:
		return "Shuffle All"
	default:
		return "Unknown"
	}
}

// ShuffleMode JSON: lowercase strings with integer fallback for legacy files.

func (m ShuffleMode) MarshalJSON() ([]byte, error) {
	var s string
	switch m {
	case ShuffleOff:
		s = "off"
	case ShuffleAlbum:
		s = "shuffle_album"
	case ShuffleSource:
		s = "shuffle_source"
	case ShuffleAll:
		s = "shuffle_all"
	default:
		s = "unknown"
	}
	return json.Marshal(s)
}

func (m *ShuffleMode) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		switch s {
		case "off":
			*m = ShuffleOff
			return nil
		case "shuffle_album":
			*m = ShuffleAlbum
			return nil
		case "shuffle_source", "shuffle_local":
			// shuffle_local was the previous name for this mode.
			*m = ShuffleSource
			return nil
		case "shuffle_all":
			*m = ShuffleAll
			return nil
		default:
			return fmt.Errorf("unknown shuffle mode: %s", s)
		}
	}
	// Legacy: accept integer.
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("invalid shuffle mode: %w", err)
	}
	if n < int(ShuffleOff) || n > int(ShuffleAll) {
		return fmt.Errorf("invalid shuffle mode: %d", n)
	}
	*m = ShuffleMode(n)
	return nil
}

func AllShuffleModes() []ShuffleMode {
	return []ShuffleMode{ShuffleOff, ShuffleAlbum, ShuffleSource, ShuffleAll}
}

// PlaybackSettings holds shuffle/repeat configuration.
type PlaybackSettings struct {
	SeekMemory   SeekMemory       `json:"seek_memory"`
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
	return PlaybackSettings{ShuffleMode: ShuffleOff, Repeat: RepeatOff, SeekMemory: SeekMemoryFull}
}

func (p PlaybackSettings) Validate() error {
	if err := p.SeekMemory.Validate(); err != nil {
		return err
	}
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
	// gamepad-osk themes
	ThemeAyuDark
	ThemeCandy
	ThemeCatppuccin
	ThemeCatppuccinFrappe
	ThemeCGA
	ThemeChalk
	ThemeCobalt
	ThemeCopper
	ThemeCoral
	ThemeCyberpunk
	ThemeDracula
	ThemeEmber
	ThemeEverforest
	ThemeFjord
	ThemeGameboy
	ThemeGold
	ThemeGotham
	ThemeGruvbox
	ThemeHorizon
	ThemeIce
	ThemeKanagawa
	ThemeLavender
	ThemeMaterial
	ThemeMatrix
	ThemeMellow
	ThemeMidnight
	ThemeMonokai
	ThemeMoss
	ThemeNavy
	ThemeNeon
	ThemeNightfox
	ThemeNord
	ThemeOcean
	ThemeOlive
	ThemeOneDark
	ThemeOxocarbon
	ThemePalenight
	ThemePaper
	ThemePlum
	ThemeRetro
	ThemeRosePine
	ThemeSakura
	ThemeSand
	ThemeSlate
	ThemeSolarizedLight
	ThemeSteamGreen
	ThemeSunset
	ThemeTeal
	ThemeTerminal
	ThemeTokyoNight
	ThemeTokyoStorm
	ThemeVapor
	ThemeVirtualBoy
	ThemeWine
	ThemeZXSpectrum
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
	{ThemeAyuDark, "Ayu Dark", "ayu-dark"},
	{ThemeCandy, "Candy", "candy"},
	{ThemeCatppuccin, "Catppuccin", "catppuccin"},
	{ThemeCatppuccinFrappe, "Catppuccin Frappe", "catppuccin-frappe"},
	{ThemeCGA, "CGA", "cga"},
	{ThemeChalk, "Chalk", "chalk"},
	{ThemeCobalt, "Cobalt", "cobalt"},
	{ThemeCopper, "Copper", "copper"},
	{ThemeCoral, "Coral", "coral"},
	{ThemeCyberpunk, "Cyberpunk", "cyberpunk"},
	{ThemeDracula, "Dracula", "dracula"},
	{ThemeEmber, "Ember", "ember"},
	{ThemeEverforest, "Everforest", "everforest"},
	{ThemeFjord, "Fjord", "fjord"},
	{ThemeGameboy, "Gameboy", "gameboy"},
	{ThemeGold, "Gold", "gold"},
	{ThemeGotham, "Gotham", "gotham"},
	{ThemeGruvbox, "Gruvbox", "gruvbox"},
	{ThemeHorizon, "Horizon", "horizon"},
	{ThemeIce, "Ice", "ice"},
	{ThemeKanagawa, "Kanagawa", "kanagawa"},
	{ThemeLavender, "Lavender", "lavender"},
	{ThemeMaterial, "Material", "material"},
	{ThemeMatrix, "Matrix", "matrix"},
	{ThemeMellow, "Mellow", "mellow"},
	{ThemeMidnight, "Midnight", "midnight"},
	{ThemeMonokai, "Monokai", "monokai"},
	{ThemeMoss, "Moss", "moss"},
	{ThemeNavy, "Navy", "navy"},
	{ThemeNeon, "Neon", "neon"},
	{ThemeNightfox, "Nightfox", "nightfox"},
	{ThemeNord, "Nord", "nord"},
	{ThemeOcean, "Ocean", "ocean"},
	{ThemeOlive, "Olive", "olive"},
	{ThemeOneDark, "One Dark", "onedark"},
	{ThemeOxocarbon, "Oxocarbon", "oxocarbon"},
	{ThemePalenight, "Palenight", "palenight"},
	{ThemePaper, "Paper", "paper"},
	{ThemePlum, "Plum", "plum"},
	{ThemeRetro, "Retro", "retro"},
	{ThemeRosePine, "Rose Pine", "rose-pine"},
	{ThemeSakura, "Sakura", "sakura"},
	{ThemeSand, "Sand", "sand"},
	{ThemeSlate, "Slate", "slate"},
	{ThemeSolarizedLight, "Solarized Light", "solarized-light"},
	{ThemeSteamGreen, "Steam Green", "steam-green"},
	{ThemeSunset, "Sunset", "sunset"},
	{ThemeTeal, "Teal", "teal"},
	{ThemeTerminal, "Terminal", "terminal"},
	{ThemeTokyoNight, "Tokyo Night", "tokyo-night"},
	{ThemeTokyoStorm, "Tokyo Storm", "tokyo-storm"},
	{ThemeVapor, "Vapor", "vapor"},
	{ThemeVirtualBoy, "Virtual Boy", "virtual-boy"},
	{ThemeWine, "Wine", "wine"},
	{ThemeZXSpectrum, "ZX Spectrum", "zx-spectrum"},
}

func (t Theme) spec() (themeSpec, bool) {
	for _, spec := range &themeSpecs {
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
	for i, spec := range &themeSpecs {
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
	for _, spec := range &themeSpecs {
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
	ShowStats    bool         `json:"show_stats"`
}

func DefaultUI() UISettings {
	return UISettings{Theme: ThemeDark, Transparency: 0, ShowStats: false}
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
