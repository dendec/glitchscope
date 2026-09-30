// Package config handles settings types, validation, and persistence.
package config

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
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

// FrameRate is the user-selected visualizer cadence. Max follows the display
// refresh rate; every numeric value is an explicit upper bound in frames per
// second.
type FrameRate int32

const (
	FrameRateMax FrameRate = 0
	FrameRate15  FrameRate = 15
	FrameRate20  FrameRate = 20
	FrameRate25  FrameRate = 25
	FrameRate30  FrameRate = 30
	FrameRate40  FrameRate = 40
	FrameRate50  FrameRate = 50
	FrameRate60  FrameRate = 60
)

func (r FrameRate) IsMax() bool { return r == FrameRateMax }

func (r FrameRate) Target(refreshRate int32) int32 {
	if r.IsMax() {
		return NormalizeRefreshRate(refreshRate)
	}
	return int32(r)
}

// NormalizeRefreshRate returns a usable display refresh rate.
func NormalizeRefreshRate(refreshRate int32) int32 {
	if refreshRate <= 0 {
		return 60
	}
	return refreshRate
}

// NormalizeFrameRate maps a fixed cap above the active display refresh to the
// display-bound Max choice. A cap equal to refresh remains persisted so it can
// become a distinct fixed choice when the window moves to a faster display.
func NormalizeFrameRate(rate FrameRate, refreshRate int32) FrameRate {
	refreshRate = NormalizeRefreshRate(refreshRate)
	if !rate.IsMax() && int32(rate) > refreshRate {
		return FrameRateMax
	}
	return rate
}

func (r FrameRate) String() string {
	if r.IsMax() {
		return "Max"
	}
	if r.Validate() != nil {
		return "Unknown"
	}
	return strconv.Itoa(int(r)) + " FPS"
}

func (r FrameRate) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if r.IsMax() {
		return json.Marshal("max")
	}
	return json.Marshal(strconv.Itoa(int(r)))
}

func (r *FrameRate) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "max", "maximum", "unlimited":
			*r = FrameRateMax
			return nil
		default:
			value := strings.TrimSpace(strings.ToLower(s))
			value = strings.TrimSpace(strings.TrimSuffix(value, "fps"))
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid frame rate %q", s)
			}
			*r = FrameRate(n)
			return r.Validate()
		}
	}
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("invalid frame rate: %w", err)
	}
	*r = FrameRate(n)
	return r.Validate()
}

func (r FrameRate) Validate() error {
	switch r {
	case FrameRateMax, FrameRate15, FrameRate20, FrameRate25, FrameRate30,
		FrameRate40, FrameRate50, FrameRate60:
		return nil
	default:
		return fmt.Errorf("invalid frame rate %d", r)
	}
}

// AllFrameRates returns every persisted frame-rate value.
func AllFrameRates() []FrameRate {
	return []FrameRate{FrameRate15, FrameRate20, FrameRate25, FrameRate30, FrameRate40, FrameRate50, FrameRate60, FrameRateMax}
}

// FrameRateChoices returns the settings values supported by the active
// display. Max is display-bound; a numeric cap equal to the refresh rate is
// omitted because it would have the same target.
func FrameRateChoices(refreshRate int32) []FrameRate {
	refreshRate = NormalizeRefreshRate(refreshRate)
	choices := make([]FrameRate, 0, len(AllFrameRates()))
	for _, rate := range AllFrameRates() {
		if rate.IsMax() {
			continue
		}
		if int32(rate) < refreshRate {
			choices = append(choices, rate)
		}
	}
	return append(choices, FrameRateMax)
}

// GraphicsSettings is the persisted user-tunable graphics parameters.
type GraphicsSettings struct {
	VisualizerOff bool          `json:"visualizer_off"`
	RenderWidth   int           `json:"render_width"`
	RenderHeight  int           `json:"render_height"`
	UpscaleFilter UpscaleFilter `json:"upscale_filter"`
	// Adaptive enables dynamic resolution while keeping RenderWidth/Height as
	// the user-selected ceiling.
	Adaptive        bool      `json:"adaptive"`
	FrameRate       FrameRate `json:"frame_rate"`
	BeatSensitivity float64   `json:"beat_sensitivity"`
}

// DefaultGraphics returns sensible defaults.
func DefaultGraphics() GraphicsSettings {
	return GraphicsSettings{
		// The oversized value is a startup sentinel. App.New clamps it to
		// the largest resolution available on the current display, making
		// the first-run choice effectively "maximum" on any normal screen.
		RenderWidth:     8192,
		RenderHeight:    8192,
		UpscaleFilter:   FilterPixel,
		Adaptive:        true,
		FrameRate:       FrameRateMax,
		BeatSensitivity: 1,
	}
}

func (g *GraphicsSettings) Validate() error {
	if g.RenderWidth < 1 || g.RenderHeight < 1 {
		return fmt.Errorf("render dimensions must be positive, got %dx%d", g.RenderWidth, g.RenderHeight)
	}
	if g.UpscaleFilter < FilterSmooth || g.UpscaleFilter > FilterPixel {
		return fmt.Errorf("invalid upscale filter %d", g.UpscaleFilter)
	}
	if err := g.FrameRate.Validate(); err != nil {
		return fmt.Errorf("frame rate: %w", err)
	}
	if math.IsNaN(g.BeatSensitivity) || math.IsInf(g.BeatSensitivity, 0) || g.BeatSensitivity < 0 || g.BeatSensitivity > 2 {
		return fmt.Errorf("beat sensitivity must be 0..2, got %v", g.BeatSensitivity)
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

// SortOrder controls the display order used by every browsable music source.
type SortOrder int

const (
	SortSource SortOrder = iota
	SortAZ
	SortZA
)

func (o SortOrder) String() string {
	switch o {
	case SortSource:
		return "Source order"
	case SortAZ:
		return "A–Z"
	case SortZA:
		return "Z–A"
	default:
		return "Unknown"
	}
}

func (o SortOrder) MarshalJSON() ([]byte, error) {
	switch o {
	case SortSource:
		return json.Marshal("source")
	case SortAZ:
		return json.Marshal("a-z")
	case SortZA:
		return json.Marshal("z-a")
	default:
		return nil, fmt.Errorf("invalid sort order %d", o)
	}
}

func (o *SortOrder) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("invalid sort order: %w", err)
	}
	switch value {
	case "source":
		*o = SortSource
	case "a-z":
		*o = SortAZ
	case "z-a":
		*o = SortZA
	default:
		return fmt.Errorf("unknown sort order: %s", value)
	}
	return nil
}

func (o SortOrder) Validate() error {
	if o < SortSource || o > SortZA {
		return fmt.Errorf("invalid sort order %d", o)
	}
	return nil
}

func AllSortOrders() []SortOrder { return []SortOrder{SortSource, SortAZ, SortZA} }

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

// UISettings holds persisted menu and appearance preferences.
type UISettings struct {
	MenuOpened    bool         `json:"menu_opened"`
	Language      Language     `json:"language"`
	Theme         Theme        `json:"theme"`
	Transparency  Transparency `json:"transparency"`
	ShowStats     bool         `json:"show_stats"`
	ShowPlayerBar bool         `json:"show_player_bar"`
	SortOrder     SortOrder    `json:"sort_order"`
}

func DefaultUI() UISettings {
	return UISettings{Language: English, Theme: ThemeDark, Transparency: 0, ShowStats: false, ShowPlayerBar: true, SortOrder: SortSource}
}

// Settings is the full persisted settings envelope.
type Settings struct {
	Graphics       GraphicsSettings   `json:"graphics"`
	Playback       PlaybackSettings   `json:"playback"`
	TrackCache     TrackCacheSettings `json:"track_cache"`
	PresetInterval PresetInterval     `json:"preset_interval"`
	UI             UISettings         `json:"ui"`
}

func DefaultSettings() Settings {
	return Settings{
		Graphics:       DefaultGraphics(),
		Playback:       DefaultPlayback(),
		TrackCache:     DefaultTrackCache(),
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
	if err := s.TrackCache.Validate(); err != nil {
		return fmt.Errorf("track cache: %w", err)
	}
	if err := s.PresetInterval.Validate(); err != nil {
		return fmt.Errorf("preset interval: %w", err)
	}
	if _, ok := s.UI.Theme.spec(); !ok {
		return fmt.Errorf("ui theme: invalid theme %d", s.UI.Theme)
	}
	if err := s.UI.Language.Validate(); err != nil {
		return fmt.Errorf("ui language: %w", err)
	}
	if err := s.UI.Transparency.Validate(); err != nil {
		return fmt.Errorf("ui transparency: %w", err)
	}
	if err := s.UI.SortOrder.Validate(); err != nil {
		return fmt.Errorf("ui sort order: %w", err)
	}
	return nil
}
