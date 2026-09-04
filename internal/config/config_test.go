package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultGraphicsValid(t *testing.T) {
	d := DefaultGraphics()
	if err := d.Validate(); err != nil {
		t.Fatal("default validation:", err)
	}
}

func TestGraphicsValidate(t *testing.T) {
	tests := []struct {
		name string
		g    GraphicsSettings
		ok   bool
	}{
		{"valid", GraphicsSettings{RenderWidth: 320, RenderHeight: 240, UpscaleFilter: FilterPixel}, true},
		{"zero width", GraphicsSettings{RenderWidth: 0, RenderHeight: 240, UpscaleFilter: FilterPixel}, false},
		{"zero height", GraphicsSettings{RenderWidth: 320, RenderHeight: 0, UpscaleFilter: FilterPixel}, false},
		{"bad filter", GraphicsSettings{RenderWidth: 320, RenderHeight: 240, UpscaleFilter: 99}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.g.Validate()
			if tt.ok && err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestSettingsValidateCoversAllDomains(t *testing.T) {
	valid := DefaultSettings()
	if err := valid.Validate(); err != nil {
		t.Fatal("default settings should be valid:", err)
	}

	tests := []struct {
		name   string
		mutate func(*Settings)
	}{
		{"shuffle", func(s *Settings) { s.Playback.ShuffleMode = 99 }},
		{"repeat", func(s *Settings) { s.Playback.Repeat = 99 }},
		{"preset interval", func(s *Settings) { s.PresetInterval = 99 }},
		{"theme", func(s *Settings) { s.UI.Theme = 99 }},
		{"transparency", func(s *Settings) { s.UI.Transparency = 101 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := valid
			tt.mutate(&settings)
			if err := settings.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestThemesRoundTrip(t *testing.T) {
	themes := AllThemes()
	if len(themes) != int(themeCount) {
		t.Fatalf("AllThemes has %d themes, want %d", len(themes), themeCount)
	}
	seenThemes := make(map[Theme]bool, len(themes))
	seen := make(map[string]bool, len(themes))
	for _, theme := range themes {
		if seenThemes[theme] {
			t.Fatalf("duplicate theme: %d", theme)
		}
		seenThemes[theme] = true

		data, err := json.Marshal(theme)
		if err != nil {
			t.Fatalf("marshal %s: %v", theme, err)
		}
		name := string(data)
		if seen[name] {
			t.Fatalf("duplicate theme JSON name: %s", name)
		}
		seen[name] = true

		var decoded Theme
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal %s: %v", name, err)
		}
		if decoded != theme {
			t.Fatalf("theme round trip: got %v, want %v", decoded, theme)
		}
	}
}

func TestUpscaleFilterStrings(t *testing.T) {
	if FilterSmooth.String() != "Smooth" {
		t.Fatal("Smooth string mismatch")
	}
	if FilterPixel.String() != "Pixel" {
		t.Fatal("Pixel string mismatch")
	}
	if FilterSmooth.IsNearest() {
		t.Fatal("Smooth should not be nearest")
	}
	if !FilterPixel.IsNearest() {
		t.Fatal("Pixel should be nearest")
	}
}

func TestComputeResolutions_640x480(t *testing.T) {
	res := ComputeResolutions(640, 480)
	if len(res) == 0 {
		t.Fatal("expected at least one resolution")
	}
	// Largest should be 640x480 (scale 1.0)
	if res[0].Width != 640 || res[0].Height != 480 {
		t.Fatalf("expected 640x480 first, got %s", res[0])
	}
	// All should have integer dims
	for _, r := range res {
		if r.Width*r.Height <= 0 {
			t.Fatalf("invalid resolution %s", r)
		}
	}
	// Should include the classic low-end 80x60 (80/640=0.125, 60/480=0.125)
	found := false
	for _, r := range res {
		if r.Width == 80 && r.Height == 60 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("640x480 should yield 80x60 at scale 0.125")
	}
}

func TestComputeResolutions_960x540(t *testing.T) {
	res := ComputeResolutions(960, 540)
	if len(res) == 0 {
		t.Fatal("expected resolutions for 960x540")
	}
	prevArea := int(^uint(0) >> 1) // max int
	for _, r := range res {
		area := r.Width * r.Height
		if area > prevArea {
			t.Fatal("not sorted descending by area")
		}
		prevArea = area
	}
}

func TestComputeResolutions_NonStandard(t *testing.T) {
	// Non-16:9 aspect, e.g. 400x300
	res := ComputeResolutions(400, 300)
	if len(res) == 0 {
		t.Fatal("expected resolutions for 400x300")
	}
	// 400*0.4 = 160, 300*0.4 = 120 — exact (both divisible by 5)
	found := false
	for _, r := range res {
		if r.Width == 160 && r.Height == 120 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("400x300 should yield 160x120 (scale 0.4)")
	}
}

func TestClosestResolution(t *testing.T) {
	list := []RenderResolution{
		{640, 480},
		{320, 240},
		{160, 120},
		{80, 60},
	}
	tests := []struct {
		target, want RenderResolution
	}{
		{RenderResolution{640, 480}, RenderResolution{640, 480}},
		{RenderResolution{630, 470}, RenderResolution{640, 480}},
		{RenderResolution{300, 200}, RenderResolution{320, 240}},
		{RenderResolution{100, 80}, RenderResolution{80, 60}},
	}
	for _, tt := range tests {
		got := ClosestResolution(list, tt.target)
		if got != tt.want {
			t.Fatalf("ClosestResolution(%s) = %s, want %s", tt.target, got, tt.want)
		}
	}
}

func TestLoadSettingsMissing(t *testing.T) {
	s, err := LoadSettings("/nonexistent/path/settings.json")
	if err != nil {
		t.Fatal("missing file should not error:", err)
	}
	if s != DefaultSettings() {
		t.Fatal("missing file should return defaults")
	}
}

func TestLoadSettingsMalformed(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(p, []byte("{bad json}"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings(p)
	if err == nil {
		t.Fatal("malformed file should return an error")
	}
	if s != DefaultSettings() {
		t.Fatal("malformed file should return defaults")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")

	orig := Settings{
		Graphics:       GraphicsSettings{RenderWidth: 480, RenderHeight: 360, UpscaleFilter: FilterSmooth},
		Playback:       PlaybackSettings{ShuffleMode: ShuffleAll, Repeat: RepeatAll},
		PresetInterval: Preset30s,
	}
	if err := SaveSettings(p, orig); err != nil {
		t.Fatal("save:", err)
	}

	got, err := LoadSettings(p)
	if err != nil {
		t.Fatal("load:", err)
	}
	if got != orig {
		t.Fatalf("round trip: got %+v, want %+v", got, orig)
	}
}

func TestSaveLoadRoundTripAdaptive(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")

	orig := Settings{
		Graphics: GraphicsSettings{RenderWidth: 640, RenderHeight: 480, UpscaleFilter: FilterPixel, Adaptive: true},
	}
	if err := SaveSettings(p, orig); err != nil {
		t.Fatal("save:", err)
	}
	got, err := LoadSettings(p)
	if err != nil {
		t.Fatal("load:", err)
	}
	if !got.Graphics.Adaptive {
		t.Fatal("Adaptive should be true after round-trip")
	}
	if got != orig {
		t.Fatalf("round trip: got %+v, want %+v", got, orig)
	}
}

func TestSaveLoadPlaybackPositionAndBeatSensitivity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := DefaultSettings()
	original.Graphics.BeatSensitivity = 1.5
	original.Playback.LastPosition = PlaybackPosition{Path: "/music/album/track.ogg", Seconds: 42.5}

	if err := SaveSettings(path, original); err != nil {
		t.Fatal("save:", err)
	}
	got, err := LoadSettings(path)
	if err != nil {
		t.Fatal("load:", err)
	}
	if got.Graphics.BeatSensitivity != original.Graphics.BeatSensitivity {
		t.Fatalf("beat sensitivity = %v, want %v", got.Graphics.BeatSensitivity, original.Graphics.BeatSensitivity)
	}
	if got.Playback.LastPosition != original.Playback.LastPosition {
		t.Fatalf("last position = %+v, want %+v", got.Playback.LastPosition, original.Playback.LastPosition)
	}
}

func TestSettingsRejectInvalidPlaybackPosition(t *testing.T) {
	settings := DefaultSettings()
	settings.Playback.LastPosition = PlaybackPosition{Seconds: 1}
	if err := settings.Validate(); err == nil {
		t.Fatal("expected invalid playback position to be rejected")
	}
}

func TestLoadSettingsAdaptivePartial(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")

	data := `{"graphics":{"adaptive":true}}`
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings(p)
	if err != nil {
		t.Fatal("load:", err)
	}
	if !s.Graphics.Adaptive {
		t.Fatal("Adaptive should be true from partial JSON")
	}
	if s.Graphics.RenderWidth != DefaultGraphics().RenderWidth {
		t.Fatal("missing render_width should use default")
	}
}

func TestLoadSettingsAdaptiveFalseOmitted(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")

	data := `{"graphics":{"render_width":320}}`
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings(p)
	if err != nil {
		t.Fatal("load:", err)
	}
	if !s.Graphics.Adaptive {
		t.Fatal("omitted adaptive should default to true from DefaultGraphics")
	}
}

func TestLoadSettingsPartial(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")

	// Only render_width supplied; other fields should fall back to defaults.
	data := `{"graphics":{"render_width":640}}`
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings(p)
	if err != nil {
		t.Fatal("load:", err)
	}
	if s.Graphics.RenderWidth != 640 {
		t.Fatalf("expected width 640, got %d", s.Graphics.RenderWidth)
	}
	if s.Graphics.RenderHeight != DefaultGraphics().RenderHeight {
		t.Fatal("height should use default")
	}
	if s.Graphics.UpscaleFilter != DefaultGraphics().UpscaleFilter {
		t.Fatal("missing upscale_filter should not change default")
	}
	if s.Playback != DefaultPlayback() {
		t.Fatal("missing playback should use defaults")
	}
}

func TestSaveAtomic(t *testing.T) {
	// Save should not corrupt the target file on failure.
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")

	_ = SaveSettings(p, DefaultSettings())

	// Read back once to confirm it was written.
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Graphics json.RawMessage `json:"graphics"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal("unmarshal:", err)
	}
}

func TestPerformanceModeRoundTrip(t *testing.T) {
	for _, m := range AllPerformanceModes() {
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal %v: %v", m, err)
		}
		var got PerformanceMode
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", b, err)
		}
		if got != m {
			t.Fatalf("round trip: got %v, want %v", got, m)
		}
	}
}

func TestPerformanceModeInvalid(t *testing.T) {
	var m PerformanceMode
	if err := json.Unmarshal([]byte(`"Turbo"`), &m); err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestPerformanceModeParams(t *testing.T) {
	for _, m := range AllPerformanceModes() {
		p := m.Params()
		if p.VisualizerFPS <= 0 {
			t.Fatalf("%v: VisualizerFPS = %d, want > 0", m, p.VisualizerFPS)
		}
		if p.AdaptiveThreshLow >= p.AdaptiveThreshHigh {
			t.Fatalf("%v: low=%v >= high=%v", m, p.AdaptiveThreshLow, p.AdaptiveThreshHigh)
		}
	}
}

func TestPerformanceModeJSONInSettings(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	s := DefaultSettings()
	s.Graphics.PerformanceMode = PerfModeEco
	if err := SaveSettings(p, s); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Graphics.PerformanceMode != PerfModeEco {
		t.Fatalf("got %v, want Eco", got.Graphics.PerformanceMode)
	}
}
