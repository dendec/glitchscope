package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCollectReadsPresetTextureReferencesFromZip(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "isosceles-cream-of-the-crop.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	preset, err := archive.Create("Presets/featured/example.milk")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := preset.Write([]byte("warp_1=`shader_body { sampler sampler_fc_TestTexture; }`\n")); err != nil {
		t.Fatal(err)
	}
	texture, err := archive.Create("Textures/testtexture.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := texture.Write([]byte("texture")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	rows, err := collect(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"Cream of the Crop/featured/example.milk", "sampler_fc_TestTexture", "testtexture", "file"}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("texture report rows = %#v, want %#v", rows, want)
	}
}
