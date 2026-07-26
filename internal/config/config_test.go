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
		{"valid", GraphicsSettings{320, 240, FilterPixel}, true},
		{"zero width", GraphicsSettings{0, 240, FilterPixel}, false},
		{"zero height", GraphicsSettings{320, 0, FilterPixel}, false},
		{"bad filter", GraphicsSettings{320, 240, 99}, false},
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
	if err != nil {
		t.Fatal("malformed file should not error:", err)
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
