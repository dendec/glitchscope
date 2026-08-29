package ui

import (
	"testing"

	"github.com/dendec/glitchscope/internal/config"
)

func TestThemePalettesCoverAllThemes(t *testing.T) {
	themes := config.AllThemes()
	if len(themePalettes) != len(themes) {
		t.Fatalf("themePalettes has %d entries, want %d", len(themePalettes), len(themes))
	}

	seen := make(map[themePalette]config.Theme, len(themes))
	for _, theme := range themes {
		palette, ok := themePalettes[theme]
		if !ok {
			t.Errorf("missing palette for %s", theme)
			continue
		}
		if previous, duplicate := seen[palette]; duplicate {
			t.Errorf("%s and %s use identical palettes", previous, theme)
		}
		seen[palette] = theme
	}
}

func TestUnknownThemeUsesDarkPalette(t *testing.T) {
	o := &Overlay{theme: config.Theme(999)}
	if got, want := o.palette(), themePalettes[config.ThemeDark]; got != want {
		t.Fatalf("unknown theme palette = %#v, want dark %#v", got, want)
	}
}
