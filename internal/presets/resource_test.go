package presets

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

func resourceFixture(names, payloads []string) []byte {
	data := make([]byte, 32)
	copy(data, []byte{0, 0, 0, 0, 32, 0, 0, 0, 255, 255, 0, 0, 255, 255, 0, 0})
	for i, name := range names {
		header := make([]byte, 8)
		for _, identifier := range []string{"TEXT", name} {
			for _, value := range append(utf16.Encode([]rune(identifier)), 0) {
				header = binary.LittleEndian.AppendUint16(header, value)
			}
		}
		for len(header)%4 != 0 {
			header = append(header, 0)
		}
		header = append(header, make([]byte, 16)...)
		binary.LittleEndian.PutUint32(header, uint32(len(payloads[i])))
		binary.LittleEndian.PutUint32(header[4:], uint32(len(header)))
		data = append(data, header...)
		data = append(data, payloads[i]...)
		for len(data)%4 != 0 {
			data = append(data, 0)
		}
	}
	return data
}

func TestResourcePackNormalizeAndLoad(t *testing.T) {
	pack, ok := packByID("milkdrop2077")
	if !ok || !pack.SourceResource || pack.PresetCount != 300 || pack.ArchiveBytes != 3383392 {
		t.Fatalf("invalid resource collection: %+v", pack)
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "PRESETS.RES")
	const preset = "MILKDROP_PRESET_VERSION=201\r\n[preset00]\r\nwave_r=1\r\n"
	// Duplicate contents remain separate presets with their original identities.
	if err := os.WriteFile(source, resourceFixture([]string{"MILK0", "MILK299"}, []string{preset, preset}), 0o644); err != nil {
		t.Fatal(err)
	}
	textures := filepath.Join(dir, "textures.zip")
	writePackZIP(t, textures, map[string]string{repositoryTextureRoot + "cloud.jpg": "cloud"})
	archive := filepath.Join(dir, pack.Filename)
	if err := normalizeRepositoryPack(context.Background(), pack, source, textures, archive); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePackArchive(archive); err != nil {
		t.Fatal(err)
	}
	if err := Open(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Close)
	if names := Names(); len(names) != 2 {
		t.Fatalf("Names() = %v", names)
	}
	for _, key := range Names() {
		got, err := Read(key)
		if err != nil || string(got) != preset {
			t.Fatalf("Read(%q) = %q, %v", key, got, err)
		}
	}
	cache, err := EnsureTextureCache(pack.ID, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cache, "cloud.jpg")); err != nil {
		t.Fatal(err)
	}
	for _, status := range PackStatuses(dir) {
		if status.Pack.ID == pack.ID && (!status.Installed || status.PresetCount != 2) {
			t.Fatalf("invalid installed metadata: %+v", status)
		}
	}
	if err := RemovePackAndReload(pack.ID, dir); err != nil {
		t.Fatal(err)
	}
	if len(Names()) != 0 {
		t.Fatal("resource presets remain after removal")
	}
}

func TestResourcePackRejectsMalformedInput(t *testing.T) {
	valid := resourceFixture([]string{"MILK0"}, []string{"[preset00]\n"})
	badBounds := bytes.Clone(valid)
	binary.LittleEndian.PutUint32(badBounds[32:], 0xffffffff)
	badIdentifier := bytes.Clone(valid)
	for i := 40; i < 64; i++ {
		badIdentifier[i] = 1
	}
	cases := map[string][]byte{
		"signature":    []byte("not a resource"),
		"truncated":    valid[:len(valid)-5],
		"bounds":       badBounds,
		"identifier":   badIdentifier,
		"empty":        valid[:32],
		"duplicate":    resourceFixture([]string{"MILK0", "MILK0"}, []string{"[preset00]", "[preset00]"}),
		"invalid name": resourceFixture([]string{"MILK../evil"}, []string{"[preset00]"}),
		"invalid milk": resourceFixture([]string{"MILK0"}, []string{"not a preset"}),
		"binary milk":  resourceFixture([]string{"MILK0"}, []string{"[preset00]\x00"}),
		"unknown only": resourceFixture([]string{"OTHER0"}, []string{"[preset00]"}),
		"missing pad":  valid[:len(valid)-1],
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "source.res")
			if err := os.WriteFile(source, data, 0o644); err != nil {
				t.Fatal(err)
			}
			writer := zip.NewWriter(io.Discard)
			if err := copyResourcePresets(context.Background(), writer, source); err == nil {
				t.Fatal("malformed resource accepted")
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestResourcePackCancellation(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.res")
	if err := os.WriteFile(source, resourceFixture([]string{"MILK0"}, []string{"[preset00]"}), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	writer := zip.NewWriter(io.Discard)
	if err := copyResourcePresets(ctx, writer, source); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}
