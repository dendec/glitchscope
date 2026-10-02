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
	t.Cleanup(Close)

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
	dir := t.TempDir()
	writeFake(t, dir, "Particles", "Blobby", "royal.milk")
	writeFake(t, dir, "Particles", "Blobby", "sparkle.milk")
	writeFake(t, dir, "Particles", "Fireflies", "glow.milk")
	if err := Open(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Close)

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

func TestReadMetaCacheInvalidatedOnStoreReload(t *testing.T) {
	dir := t.TempDir()
	presetPath := filepath.Join(dir, "alpha.milk")
	if err := os.WriteFile(presetPath, []byte("[preset00]\nfRating=1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Open(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Close)
	if got := ReadMeta("alpha.milk").Rating; got != 1 {
		t.Fatalf("initial rating = %v, want 1", got)
	}
	if err := os.WriteFile(presetPath, []byte("[preset00]\nfRating=4.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Open(dir); err != nil {
		t.Fatal(err)
	}
	if got := ReadMeta("alpha.milk").Rating; got != 4 {
		t.Fatalf("reloaded rating = %v, want 4", got)
	}
}

func TestSourceFingerprintTracksIndexedPresetContents(t *testing.T) {
	dir := t.TempDir()
	presetPath := filepath.Join(dir, "example.milk")
	if err := os.WriteFile(presetPath, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Open(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Close)
	first, err := SourceFingerprint("example.milk")
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(presetPath, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Open(dir); err != nil {
		t.Fatal(err)
	}
	second, err := SourceFingerprint("example.milk")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("source fingerprint did not change: %q", first)
	}
}

func TestOpenFailureKeepsCurrentStore(t *testing.T) {
	dir := t.TempDir()
	writeFake(t, dir, "alpha.milk")
	if err := Open(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Close)

	filePath := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(filePath, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Open(filePath); err == nil {
		t.Fatal("Open() unexpectedly accepted a file as the preset directory")
	}
	if names := Names(); len(names) != 1 || names[0] != "alpha.milk" {
		t.Fatalf("failed Open replaced active names: %v", names)
	}
}

func TestCloseClearsPresetStore(t *testing.T) {
	dir := t.TempDir()
	writeFake(t, dir, "alpha.milk")
	if err := Open(dir); err != nil {
		t.Fatal(err)
	}

	Close()
	if names := Names(); len(names) != 0 {
		t.Fatalf("Names() after Close = %v, want empty", names)
	}
	if _, err := Read("alpha.milk"); err == nil {
		t.Fatal("Read() succeeded after Close")
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
