package config

import (
	"bytes"
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
	if d.FrameRate != FrameRateMax || !d.Adaptive {
		t.Fatalf("defaults = frame rate %v adaptive=%v, want Max and enabled", d.FrameRate, d.Adaptive)
	}
}

func TestFrameRateChoicesAndEncoding(t *testing.T) {
	want := []FrameRate{FrameRate15, FrameRate20, FrameRate25, FrameRate30, FrameRate40, FrameRate50, FrameRate60, FrameRateMax}
	got := AllFrameRates()
	if len(got) != len(want) {
		t.Fatalf("frame-rate choices = %v, want %v", got, want)
	}
	for i, rate := range want {
		if got[i] != rate || got[i].Validate() != nil {
			t.Fatalf("choice %d = %v", i, got[i])
		}
		data, err := json.Marshal(rate)
		if err != nil {
			t.Fatal(err)
		}
		var decoded FrameRate
		if err := json.Unmarshal(data, &decoded); err != nil || decoded != rate {
			t.Fatalf("round trip %v: data=%s decoded=%v err=%v", rate, data, decoded, err)
		}
	}
	if FrameRateMax.Target(120) != 120 || FrameRateMax.Target(0) != 60 || FrameRate30.Target(120) != 30 {
		t.Fatal("frame-rate target resolution is incorrect")
	}
}

func TestDynamicFrameRateChoices(t *testing.T) {
	tests := []struct {
		refresh int32
		want    []FrameRate
	}{
		{refresh: 60, want: []FrameRate{FrameRate15, FrameRate20, FrameRate25, FrameRate30, FrameRate40, FrameRate50, FrameRateMax}},
		{refresh: 75, want: []FrameRate{FrameRate15, FrameRate20, FrameRate25, FrameRate30, FrameRate40, FrameRate50, FrameRate60, FrameRateMax}},
		{refresh: 120, want: []FrameRate{FrameRate15, FrameRate20, FrameRate25, FrameRate30, FrameRate40, FrameRate50, FrameRate60, FrameRateMax}},
		{refresh: 0, want: []FrameRate{FrameRate15, FrameRate20, FrameRate25, FrameRate30, FrameRate40, FrameRate50, FrameRateMax}},
		{refresh: -1, want: []FrameRate{FrameRate15, FrameRate20, FrameRate25, FrameRate30, FrameRate40, FrameRate50, FrameRateMax}},
	}
	for _, tt := range tests {
		got := FrameRateChoices(tt.refresh)
		if len(got) != len(tt.want) {
			t.Fatalf("refresh %d: choices=%v, want %v", tt.refresh, got, tt.want)
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Fatalf("refresh %d: choice %d=%v, want %v", tt.refresh, i, got[i], tt.want[i])
			}
		}
	}
}

func TestNormalizeFrameRateForDisplay(t *testing.T) {
	tests := []struct {
		name    string
		rate    FrameRate
		refresh int32
		want    FrameRate
	}{
		{name: "equal refresh", rate: FrameRate60, refresh: 60, want: FrameRate60},
		{name: "above refresh", rate: FrameRate60, refresh: 50, want: FrameRateMax},
		{name: "below refresh", rate: FrameRate50, refresh: 60, want: FrameRate50},
		{name: "max", rate: FrameRateMax, refresh: 60, want: FrameRateMax},
	}
	for _, tt := range tests {
		if got := NormalizeFrameRate(tt.rate, tt.refresh); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
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
		{"bad frame rate", GraphicsSettings{RenderWidth: 320, RenderHeight: 240, UpscaleFilter: FilterPixel, FrameRate: 27}, false},
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
	// Half size remains exact for this nonstandard aspect ratio.
	found := false
	for _, r := range res {
		if r.Width == 200 && r.Height == 150 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("400x300 should yield 200x150 (scale 0.5)")
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

func TestResolutionAtMost(t *testing.T) {
	list := []RenderResolution{{1920, 1080}, {1280, 720}, {960, 540}, {640, 360}}
	if got, want := ResolutionAtMost(list, RenderResolution{1000, 700}), (RenderResolution{960, 540}); got != want {
		t.Fatalf("ResolutionAtMost = %s, want %s", got, want)
	}
	if got, want := ResolutionAtMost(list, RenderResolution{320, 240}), (RenderResolution{640, 360}); got != want {
		t.Fatalf("ResolutionAtMost fallback = %s, want %s", got, want)
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
		UI:             DefaultUI(),
	}
	orig.UI.ShowPlayerBar = true
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
		UI:       DefaultUI(),
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

func TestLoadSettingsMigratesLegacyPerformanceMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	data := `{"graphics":{"performance_mode":"Eco","render_width":640,"render_height":360}}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	settings, err := LoadSettings(path)
	if err != nil {
		t.Fatal("load:", err)
	}
	if settings.Graphics.FrameRate != FrameRate20 || !settings.Graphics.Adaptive {
		t.Fatalf("legacy Eco migration = frame rate %v adaptive=%v", settings.Graphics.FrameRate, settings.Graphics.Adaptive)
	}

	data = `{"graphics":{"performance_mode":"Ultra","render_width":640,"render_height":360}}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	settings, err = LoadSettings(path)
	if err != nil {
		t.Fatal("load Ultra:", err)
	}
	if settings.Graphics.FrameRate != FrameRateMax || settings.Graphics.Adaptive {
		t.Fatalf("legacy Ultra migration = frame rate %v adaptive=%v", settings.Graphics.FrameRate, settings.Graphics.Adaptive)
	}
}

func TestLoadSettingsRejectsUnknownLegacyPerformanceMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"graphics":{"performance_mode":"Turbo"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSettings(path); err == nil {
		t.Fatal("unknown legacy performance mode was accepted")
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

func TestFrameRateJSONInSettings(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	s := DefaultSettings()
	s.Graphics.FrameRate = FrameRate20
	s.Graphics.Adaptive = true
	if err := SaveSettings(p, s); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || !json.Valid(data) || !bytes.Contains(data, []byte(`"frame_rate":"20"`)) {
		t.Fatalf("settings did not use frame_rate schema: %s", data)
	}
	if bytes.Contains(data, []byte(`performance_mode`)) {
		t.Fatalf("legacy performance_mode leaked into new settings: %s", data)
	}
	got, err := LoadSettings(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Graphics.FrameRate != FrameRate20 || !got.Graphics.Adaptive {
		t.Fatalf("got frame rate %v adaptive=%v", got.Graphics.FrameRate, got.Graphics.Adaptive)
	}
	if err := os.WriteFile(p, []byte(`{"graphics":{"frame_rate":60}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = LoadSettings(p)
	if err != nil || got.Graphics.FrameRate != FrameRate60 {
		t.Fatalf("numeric frame-rate compatibility: got=%v err=%v", got.Graphics.FrameRate, err)
	}
}

func TestShuffleModeRoundTrip(t *testing.T) {
	for _, m := range AllShuffleModes() {
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal %v: %v", m, err)
		}
		var got ShuffleMode
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", b, err)
		}
		if got != m {
			t.Fatalf("round trip: got %v, want %v", got, m)
		}
	}
}

func TestShuffleModeCanonicalFormat(t *testing.T) {
	// MarshalJSON must produce lowercase strings.
	want := map[ShuffleMode]string{
		ShuffleOff:    `"off"`,
		ShuffleAlbum:  `"shuffle_album"`,
		ShuffleSource: `"shuffle_source"`,
		ShuffleAll:    `"shuffle_all"`,
	}
	for mode, expected := range want {
		b, err := json.Marshal(mode)
		if err != nil {
			t.Fatalf("marshal %v: %v", mode, err)
		}
		if string(b) != expected {
			t.Fatalf("marshal %v = %s, want %s", mode, b, expected)
		}
	}
}

func TestShuffleModeLegacyInt(t *testing.T) {
	// Legacy settings.json stored shuffle_mode as an integer.
	var m ShuffleMode
	if err := json.Unmarshal([]byte(`3`), &m); err != nil {
		t.Fatal("legacy int unmarshal:", err)
	}
	if m != ShuffleAll {
		t.Fatalf("got %v, want ShuffleAll", m)
	}
}

func TestShuffleModeInvalid(t *testing.T) {
	var m ShuffleMode
	if err := json.Unmarshal([]byte(`"Turbo"`), &m); err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestShuffleModeLegacyLocalName(t *testing.T) {
	var mode ShuffleMode
	if err := json.Unmarshal([]byte(`"shuffle_local"`), &mode); err != nil {
		t.Fatal("legacy shuffle name unmarshal:", err)
	}
	if mode != ShuffleSource {
		t.Fatalf("got %v, want ShuffleSource", mode)
	}
}

func TestRepeatModeRoundTrip(t *testing.T) {
	for _, m := range AllRepeatModes() {
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal %v: %v", m, err)
		}
		var got RepeatMode
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", b, err)
		}
		if got != m {
			t.Fatalf("round trip: got %v, want %v", got, m)
		}
	}
}

func TestRepeatModeCanonicalFormat(t *testing.T) {
	// MarshalJSON must produce lowercase strings.
	want := map[RepeatMode]string{
		RepeatOff: `"off"`,
		RepeatOne: `"repeat_one"`,
		RepeatAll: `"repeat_all"`,
	}
	for mode, expected := range want {
		b, err := json.Marshal(mode)
		if err != nil {
			t.Fatalf("marshal %v: %v", mode, err)
		}
		if string(b) != expected {
			t.Fatalf("marshal %v = %s, want %s", mode, b, expected)
		}
	}
}

func TestRepeatModeLegacyInt(t *testing.T) {
	var m RepeatMode
	if err := json.Unmarshal([]byte(`2`), &m); err != nil {
		t.Fatal("legacy int unmarshal:", err)
	}
	if m != RepeatAll {
		t.Fatalf("got %v, want RepeatAll", m)
	}
}

func TestRepeatModeInvalid(t *testing.T) {
	var m RepeatMode
	if err := json.Unmarshal([]byte(`"Turbo"`), &m); err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestShuffleModeJSONInSettings(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	s := DefaultSettings()
	s.Playback.ShuffleMode = ShuffleAll
	s.Playback.Repeat = RepeatAll
	if err := SaveSettings(p, s); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Playback.ShuffleMode != ShuffleAll {
		t.Fatalf("got %v, want ShuffleAll", got.Playback.ShuffleMode)
	}
	if got.Playback.Repeat != RepeatAll {
		t.Fatalf("got %v, want RepeatAll", got.Playback.Repeat)
	}
}

func TestResolutionGrid720p(t *testing.T) {
	resolutions := ComputeResolutions(1280, 720)
	heights := []int{720, 540, 450, 360, 270, 180, 135, 90}
	if len(resolutions) != len(heights) {
		t.Fatalf("grid has %d entries, want %d", len(resolutions), len(heights))
	}
	for i, height := range heights {
		if resolutions[i].Height != height || resolutions[i].Width != height*16/9 {
			t.Fatalf("step %d: %v, want %dp at 16:9", i, resolutions[i], height)
		}
	}
}
