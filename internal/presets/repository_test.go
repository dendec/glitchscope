package presets

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryPacksNormalizeAndLoad(t *testing.T) {
	for _, id := range []string{"en-d", "milkdrop-original", "projectm-classic", "butterchurn"} {
		t.Run(id, func(t *testing.T) {
			pack, ok := packByID(id)
			if !ok {
				t.Fatalf("collection %s is unavailable", id)
			}
			dir := t.TempDir()
			source := filepath.Join(dir, "source.zip")
			textures := filepath.Join(dir, "textures.zip")
			const data = "[preset00]\nwave_r=1\n"
			writePackZIP(t, source, map[string]string{
				pack.SourcePresetRoot + "Category/example.milk": data,
				pack.SourcePresetRoot + "README.md":             "upstream notice",
				pack.SourcePresetRoot + "preview.jpg":           "preview",
				pack.SourcePresetRoot + "../outside.milk":       "unsafe",
				"other-root/extra.milk":                         "wrong root",
			})
			writePackZIP(t, textures, map[string]string{
				repositoryTextureRoot + "cloud.jpg":      "cloud",
				repositoryTextureRoot + "../outside.jpg": "unsafe",
			})
			archive := filepath.Join(dir, pack.Filename)
			if err := normalizeRepositoryPack(context.Background(), pack, source, textures, archive); err != nil {
				t.Fatal(err)
			}
			if err := ValidatePackArchive(archive); err != nil {
				t.Fatal(err)
			}
			zr, err := zip.OpenReader(archive)
			if err != nil {
				t.Fatal(err)
			}
			noticePath := "Sources/Presets/README.md"
			if id == "butterchurn" {
				noticePath = "Sources/Presets/presets/milkdrop/README.md"
			}
			if id == "milkdrop-original" {
				noticePath = "Sources/Presets/Milkdrop-Original/README.md"
			}
			notice, err := zr.Open(noticePath)
			if err != nil {
				zr.Close()
				t.Fatal("upstream notice was not retained")
			}
			if err := notice.Close(); err != nil {
				t.Fatal(err)
			}
			if err := zr.Close(); err != nil {
				t.Fatal(err)
			}
			if err := Open(dir); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(Close)
			key := pack.Name + "/Category/example.milk"
			if names := Names(); len(names) != 1 || names[0] != key {
				t.Fatalf("Names() = %v, want [%s]", names, key)
			}
			got, err := Read(key)
			if err != nil || string(got) != data {
				t.Fatalf("Read(%q) = %q, %v", key, got, err)
			}
			cache, err := EnsureTextureCache(id, dir)
			if err != nil {
				t.Fatal(err)
			}
			cloud, err := os.ReadFile(filepath.Join(cache, "cloud.jpg"))
			if err != nil || string(cloud) != "cloud" {
				t.Fatalf("cached cloud = %q, %v", cloud, err)
			}
			statuses := PackStatuses(dir)
			for _, status := range statuses {
				if status.Pack.ID == id && (!status.Installed || status.Error != "") {
					t.Fatalf("installed status = %+v", status)
				}
			}
			if err := RemovePackAndReload(id, dir); err != nil {
				t.Fatal(err)
			}
			if len(Names()) != 0 {
				t.Fatalf("presets remain after removal: %v", Names())
			}
		})
	}
}

func TestNormalizeRepositoryPackCancellation(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.zip")
	pack, _ := packByID("en-d")
	writePackZIP(t, source, map[string]string{pack.SourcePresetRoot + "one.milk": "preset"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := normalizeRepositoryPack(ctx, pack, source, source, filepath.Join(dir, "out.zip"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestCopyRepositoryEntriesRejectsOversizedArchive(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.zip")
	file, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	_, err = writer.CreateRaw(&zip.FileHeader{Name: "root/one.milk", UncompressedSize64: maxPackUncompressed + 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	pack := Pack{SourcePresetRoot: "root/"}
	if err := normalizeRepositoryPack(context.Background(), pack, source, source, filepath.Join(dir, "out.zip")); err == nil {
		t.Fatal("oversized repository archive accepted")
	}
}
