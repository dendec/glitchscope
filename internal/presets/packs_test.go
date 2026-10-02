package presets

import (
	"archive/zip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenIndexesPresetZIPWithoutExtractingPresets(t *testing.T) {
	dir := t.TempDir()
	pack := availablePacks[0]
	archivePath := filepath.Join(dir, pack.Filename)
	writePackZIP(t, archivePath, map[string]string{
		"Presets/Deep/Folder/one.milk":    "[preset00]\nwave_r=1\n",
		"Presets/Deep/Folder/preview.jpg": "preview",
		"Textures/clouds.jpg":             "texture",
	})

	if err := Open(dir); err != nil {
		t.Fatal(err)
	}
	defer Close()
	wantKey := pack.Name + "/Deep/Folder/one.milk"
	if got := Names(); len(got) != 1 || got[0] != wantKey {
		t.Fatalf("Names() = %v, want [%s]", got, wantKey)
	}
	data, err := Read(wantKey)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "[preset00]\nwave_r=1\n" {
		t.Fatalf("Read() = %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "Deep", "Folder", "one.milk")); !os.IsNotExist(err) {
		t.Fatalf("preset unexpectedly extracted: %v", err)
	}
}

func TestSourceFingerprintTracksZIPEntryContents(t *testing.T) {
	dir := t.TempDir()
	pack := availablePacks[0]
	archivePath := filepath.Join(dir, pack.Filename)
	writePackZIP(t, archivePath, map[string]string{
		"Presets/example.milk": "first",
		"Textures/test.jpg":    "texture",
	})
	if err := Open(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Close)
	key := pack.Name + "/example.milk"
	first, err := SourceFingerprint(key)
	if err != nil {
		t.Fatal(err)
	}

	writePackZIP(t, archivePath, map[string]string{
		"Presets/example.milk": "second",
		"Textures/test.jpg":    "texture",
	})
	if err := Open(dir); err != nil {
		t.Fatal(err)
	}
	second, err := SourceFingerprint(key)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("ZIP source fingerprint did not change: %q", first)
	}
}

func TestEnsureTextureCacheExtractsTexturesOnlyAndAliasesKnownTypo(t *testing.T) {
	dir := t.TempDir()
	pack := availablePacks[0]
	archivePath := filepath.Join(dir, pack.Filename)
	writePackZIP(t, archivePath, map[string]string{
		"Presets/one.milk":       "[preset00]\n",
		"Textures/clouds.jpg":    "cloud",
		"Textures/OIchess1..jpg": "chess",
		"Presets/clouds.jpg":     "preview",
		"Textures/ignored.txt":   "ignored",
	})

	texturePath, err := EnsureTextureCache(pack.ID, dir)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"clouds.jpg":   "cloud",
		"OIchess1.jpg": "chess",
	} {
		got, err := os.ReadFile(filepath.Join(texturePath, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	for _, name := range []string{"one.milk", "ignored.txt", "OIchess1..jpg"} {
		if _, err := os.Stat(filepath.Join(texturePath, name)); !os.IsNotExist(err) {
			t.Errorf("unexpected cache entry %s: %v", name, err)
		}
	}
	if !textureStampMatches(texturePath, archivePath) {
		t.Fatal("texture cache stamp does not match source archive")
	}
}

func TestExtractTexturesRejectsZipSlip(t *testing.T) {
	dir := t.TempDir()
	pack := availablePacks[0]
	archivePath := filepath.Join(dir, pack.Filename)
	writePackZIP(t, archivePath, map[string]string{
		"Presets/one.milk":           "[preset00]\n",
		"Textures/../../outside.jpg": "outside",
		"Textures/safe.jpg":          "safe",
	})

	if err := ValidatePackArchive(archivePath); err != nil {
		t.Fatal(err)
	}
	texturePath, err := EnsureTextureCache(pack.ID, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(texturePath, "safe.jpg")); err != nil {
		t.Fatalf("safe texture not extracted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "outside.jpg")); !os.IsNotExist(err) {
		t.Fatalf("zip-slip target exists: %v", err)
	}
}

func TestAvailablePacksDoesNotExposeSpoutJamming(t *testing.T) {
	for _, pack := range AvailablePacks() {
		if pack.ID == "spout-jamming" || pack.Name == "Spout Jamming" {
			t.Fatal("Spout Jamming must remain unlisted")
		}
	}
}

func TestDownloadPackArchiveEnforcesFinalSizeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("archive exceeds limit"))
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "download.zip")
	err := downloadPackArchive(context.Background(), server.URL, target, 8, nil)
	if err == nil || !strings.Contains(err.Error(), "exceeds 8 bytes") {
		t.Fatalf("downloadPackArchive() error = %v, want size limit error", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("oversized download remains at target: %v", err)
	}
}

func TestPackPresetKeyRejectsUnsafeNames(t *testing.T) {
	pack := availablePacks[0]
	for _, name := range []string{
		"Presets/../outside.milk",
		`Presets\\outside.milk`,
		"/Presets/outside.milk",
	} {
		if key, ok := packPresetKey(pack, name); ok {
			t.Errorf("packPresetKey(%q) = %q, want rejected", name, key)
		}
	}
	if key, ok := packPresetKey(pack, "Presets/Fractal/example.milk"); !ok || key != pack.Name+"/Fractal/example.milk" {
		t.Fatalf("packPresetKey() = %q, %v", key, ok)
	}
}

func TestPackPresetKeyOmitsRedundantCollectionFolder(t *testing.T) {
	pack := availablePacks[2]
	for _, test := range []struct {
		name string
		want string
	}{
		{
			name: "Presets/Mashups 2024/Isosceles Mashup.milk",
			want: "Isosceles Mashups 2024/Isosceles Mashup.milk",
		},
		{
			name: "Presets/Mashups 2024/Deep/Isosceles Mashup.milk",
			want: "Isosceles Mashups 2024/Deep/Isosceles Mashup.milk",
		},
	} {
		got, ok := packPresetKey(pack, test.name)
		if !ok || got != test.want {
			t.Errorf("packPresetKey(%q) = %q, %t; want %q, true", test.name, got, ok, test.want)
		}
	}

	otherPack := availablePacks[1]
	if got, ok := packPresetKey(otherPack, "Presets/Dancer/example.milk"); !ok || got != otherPack.Name+"/Dancer/example.milk" {
		t.Fatalf("other pack key = %q, %t; want original directory preserved", got, ok)
	}
}

func TestOpenReadsPresetAfterOmittingRedundantCollectionFolder(t *testing.T) {
	dir := t.TempDir()
	pack := availablePacks[2]
	archivePath := filepath.Join(dir, pack.Filename)
	const presetData = "[preset00]\nfRating=3.0\n"
	writePackZIP(t, archivePath, map[string]string{
		"Presets/Mashups 2024/example.milk": presetData,
		"Textures/test.jpg":                 "texture",
	})

	if err := Open(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Close)

	key := pack.Name + "/example.milk"
	if names := Names(); len(names) != 1 || names[0] != key {
		t.Fatalf("Names() = %v, want [%s]", names, key)
	}
	got, err := Read(key)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != presetData {
		t.Fatalf("Read(%q) = %q, want %q", key, got, presetData)
	}
}

func TestRemovePackAndReloadPublishesRemainingPresets(t *testing.T) {
	dir := t.TempDir()
	pack := availablePacks[0]
	archivePath := filepath.Join(dir, pack.Filename)
	writePackZIP(t, archivePath, map[string]string{
		"Presets/one.milk":    "[preset00]\n",
		"Textures/clouds.jpg": "texture",
	})
	if err := Open(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Close)
	if len(Names()) != 1 {
		t.Fatalf("before removal, names = %v", Names())
	}
	if _, err := EnsureTextureCache(pack.ID, dir); err != nil {
		t.Fatal(err)
	}

	if err := RemovePackAndReload(pack.ID, dir); err != nil {
		t.Fatal(err)
	}
	if names := Names(); len(names) != 0 {
		t.Fatalf("after removal, names = %v", names)
	}
	for _, path := range []string{archivePath, textureDir(pack.ID, dir)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("removed path still exists %s: %v", path, err)
		}
	}
}

func writePackZIP(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(file)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			file.Close()
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			file.Close()
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
