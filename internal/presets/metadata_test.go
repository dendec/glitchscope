package presets

import (
	"os"
	"testing"
)

func TestParseMetaMinimal(t *testing.T) {
	data := []byte(`MILKDROP_PRESET_VERSION=201
[preset00]
fRating=3.000000
fDecay=0.950
fWarpAnimSpeed=1.200
fVideoEchoZoom=0.900
nWaveMode=7
`)
	m := ParseMeta(data)
	if m.Rating != 3.0 {
		t.Errorf("Rating = %v, want 3", m.Rating)
	}
	if m.Decay != 0.95 {
		t.Errorf("Decay = %v, want 0.95", m.Decay)
	}
	if m.WarpSpeed != 1.2 {
		t.Errorf("WarpSpeed = %v, want 1.2", m.WarpSpeed)
	}
	if m.VideoEchoZoom != 0.9 {
		t.Errorf("VideoEchoZoom = %v, want 0.9", m.VideoEchoZoom)
	}
	if m.WaveMode != 7 {
		t.Errorf("WaveMode = %v, want 7", m.WaveMode)
	}
}

func TestParseMetaShapes(t *testing.T) {
	data := []byte(`[preset00]
shapecode_0_enabled=1
shapecode_0_sides=32
shapecode_1_enabled=0
shapecode_2_enabled=1
wavecode_0_enabled=1
wavecode_1_enabled=1
wavecode_2_enabled=0
wavecode_3_enabled=1
`)
	m := ParseMeta(data)
	if m.Shapes != 2 {
		t.Errorf("Shapes = %v, want 2", m.Shapes)
	}
	if m.Waves != 3 {
		t.Errorf("Waves = %v, want 3", m.Waves)
	}
}

func TestParseMetaPerFramePerPixel(t *testing.T) {
	data := []byte(`[preset00]
per_frame_1=q1=sin(time);
per_frame_2=q2=cos(time);
per_frame_3=
per_frame_4=monitor=q1;
per_pixel_1=dx=sin(x);
per_pixel_2=dy=cos(y);
`)
	m := ParseMeta(data)
	if m.PerFrameEqs != 4 {
		t.Errorf("PerFrameEqs = %v, want 4", m.PerFrameEqs)
	}
	if m.PerPixelEqs != 2 {
		t.Errorf("PerPixelEqs = %v, want 2", m.PerPixelEqs)
	}
}

func TestParseMetaCountsUniqueExternalTextures(t *testing.T) {
	data := []byte(`[preset00]
warp_1=` + "`" + `shader_body {
sampler sampler_main;
sampler sampler_blur1;
sampler sampler_noise_lq;
sampler sampler_fc_clouds;
sampler sampler_clouds;
sampler sampler_rand00;
sampler sampler_RAND00;
}` + "`" + `
`)

	m := ParseMeta(data)
	if m.Textures != 2 {
		t.Fatalf("Textures = %d, want 2 unique external textures", m.Textures)
	}
	references := TextureReferences(data)
	if len(references) != 3 {
		t.Fatalf("texture references = %d, want 3 unique sampler declarations", len(references))
	}
}

func TestParseMetaWhitespaceAroundEquals(t *testing.T) {
	data := []byte(`[preset00]
fRating = 4.500000
fDecay  =  0.990
`)
	m := ParseMeta(data)
	if m.Rating != 4.5 {
		t.Errorf("Rating = %v, want 4.5", m.Rating)
	}
	if m.Decay != 0.99 {
		t.Errorf("Decay = %v, want 0.99", m.Decay)
	}
}

func TestParseMetaIgnoresCommentsAndEmptyLines(t *testing.T) {
	data := []byte(`// This is a comment
[preset00]

// Another comment
fRating=2.000000

fDecay=0.800
`)
	m := ParseMeta(data)
	if m.Rating != 2.0 {
		t.Errorf("Rating = %v, want 2", m.Rating)
	}
	if m.Decay != 0.8 {
		t.Errorf("Decay = %v, want 0.8", m.Decay)
	}
}

func TestParseMetaRepeatedKeysLastWins(t *testing.T) {
	data := []byte(`[preset00]
fRating=1.000000
fRating=5.000000
`)
	m := ParseMeta(data)
	if m.Rating != 5.0 {
		t.Errorf("Rating = %v, want 5 (last wins)", m.Rating)
	}
}

func TestParseMetaOutsidePresetSectionIgnored(t *testing.T) {
	data := []byte(`MILKDROP_PRESET_VERSION=201
PSVERSION=2
fRating=1.000000
[preset00]
fRating=4.000000
`)
	m := ParseMeta(data)
	if m.Rating != 4.0 {
		t.Errorf("Rating = %v, want 4 (only preset00 section)", m.Rating)
	}
}

func TestParseMetaOtherSectionsSkipped(t *testing.T) {
	data := []byte(`[preset00]
fRating=3.000000
[en]
fRating=1.000000
`)
	m := ParseMeta(data)
	if m.Rating != 3.0 {
		t.Errorf("Rating = %v, want 3 (en section ignored)", m.Rating)
	}
}

func TestParseMetaUnknownKeysIgnored(t *testing.T) {
	data := []byte(`[preset00]
fRating=2.500000
unknown_key=42
another_thing=true
`)
	m := ParseMeta(data)
	if m.Rating != 2.5 {
		t.Errorf("Rating = %v, want 2.5", m.Rating)
	}
}

func TestParseMetaEmptyInput(t *testing.T) {
	m := ParseMeta(nil)
	if m.Rating != 0 || m.Shapes != 0 || m.PerFrameEqs != 0 {
		t.Errorf("expected zero values for empty input, got %+v", m)
	}
}

func TestParseMetaInvalidNumericValues(t *testing.T) {
	data := []byte(`[preset00]
fRating=not_a_number
fDecay=NaN
nWaveMode=abc
`)
	m := ParseMeta(data)
	// Invalid parse returns 0 — partial metadata with nil error.
	if m.Rating != 0 {
		t.Errorf("Rating = %v, want 0 for invalid", m.Rating)
	}
	if m.WaveMode != 0 {
		t.Errorf("WaveMode = %v, want 0 for invalid", m.WaveMode)
	}
}

// TestParseMetaRealPreset validates parsing against a real .milk file
// from the cream-of-the-crop collection.
func TestParseMetaRealPreset(t *testing.T) {
	files := []string{
		"../../dist/presets-cream-of-the-crop/Dancer/Infect/LuxXx - Flame Dance Iib.milk",
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("preset file not available: %v", err)
			continue
		}
		m := ParseMeta(data)
		if m.Rating != 5.0 {
			t.Errorf("%s: Rating = %v, want 5", path, m.Rating)
		}
		if m.Decay != 0.5 {
			t.Errorf("%s: Decay = %v, want 0.5", path, m.Decay)
		}
		if m.WarpSpeed != 0.037 {
			t.Errorf("%s: WarpSpeed = %v, want 0.037", path, m.WarpSpeed)
		}
		if m.VideoEchoZoom != 0.952 {
			t.Errorf("%s: VideoEchoZoom = %v, want 0.952", path, m.VideoEchoZoom)
		}
		if m.WaveMode != 7 {
			t.Errorf("%s: WaveMode = %v, want 7", path, m.WaveMode)
		}
		if m.Waves < 1 {
			t.Errorf("%s: Waves = %v, want >= 1", path, m.Waves)
		}
		if m.PerFrameEqs < 1 {
			t.Errorf("%s: PerFrameEqs = %v, want >= 1", path, m.PerFrameEqs)
		}
	}
}
