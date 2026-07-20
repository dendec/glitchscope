package presets

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestCategories_and_PresetsInCategory(t *testing.T) {
	dir := t.TempDir()
	// Root presets → Default.
	writeFake(t, dir, "alpha.milk")
	writeFake(t, dir, "beta.milk")
	// Subdir presets.
	writeFake(t, dir, "featured", "gamma.milk")
	writeFake(t, dir, "featured", "delta.milk")
	writeFake(t, dir, "custom", "epsilon.milk")

	if err := Open(dir); err != nil {
		t.Fatal(err)
	}

	cats := Categories()
	if len(cats) != 3 {
		t.Fatalf("expected 3 categories, got %d: %v", len(cats), cats)
	}
	sort.Strings(cats) // stable sort already, but be explicit
	want := []string{"Default", "custom", "featured"}
	for i, c := range want {
		if cats[i] != c {
			t.Errorf("cats[%d] = %q, want %q", i, cats[i], c)
		}
	}

	defaultPresets := PresetsInCategory("Default")
	if len(defaultPresets) != 2 {
		t.Fatalf("Default: expected 2, got %d", len(defaultPresets))
	}

	featuredPresets := PresetsInCategory("featured")
	if len(featuredPresets) != 2 {
		t.Fatalf("featured: expected 2, got %d", len(featuredPresets))
	}
	for _, want := range []string{"featured/gamma.milk", "featured/delta.milk"} {
		found := false
		for _, p := range featuredPresets {
			if p == want {
				found = true
			}
		}
		if !found {
			t.Errorf("featured: missing %q", want)
		}
	}

	customPresets := PresetsInCategory("custom")
	if len(customPresets) != 1 || customPresets[0] != "custom/epsilon.milk" {
		t.Errorf("custom: got %v, want [custom/epsilon.milk]", customPresets)
	}
}

func TestCategories_nestedSubcategory(t *testing.T) {
	// scanUser only walks two directory levels, but presets.mdp can
	// contain deeper keys, so set up the store directly to exercise that.
	names := []string{
		"Particles/Blobby/royal.milk",
		"Particles/Blobby/sparkle.milk",
		"Particles/Fireflies/glow.milk",
	}
	s := &presetStore{entries: make(map[string]entry)}
	for _, n := range names {
		s.entries[n] = entry{name: n, dataOff: -1}
	}
	s.names = append(s.names, names...)
	sort.Strings(s.names)
	store = s

	cats := Categories()
	sort.Strings(cats)
	want := []string{"Particles/Blobby", "Particles/Fireflies"}
	if len(cats) != len(want) {
		t.Fatalf("expected %d categories, got %d: %v", len(want), len(cats), cats)
	}
	for i, c := range want {
		if cats[i] != c {
			t.Errorf("cats[%d] = %q, want %q", i, cats[i], c)
		}
	}

	blobby := PresetsInCategory("Particles/Blobby")
	if len(blobby) != 2 {
		t.Fatalf("Particles/Blobby: expected 2, got %d: %v", len(blobby), blobby)
	}
}

func writeFake(t *testing.T, dirs ...string) {
	t.Helper()
	p := filepath.Join(dirs...)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
}
