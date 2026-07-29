package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dendec/pmv/internal/modland"
)

func main() {
	zipPath := filepath.Join("dist", "allmods.zip")
	if len(os.Args) > 1 {
		zipPath = os.Args[1]
	}

	baseDir := filepath.Join(os.ExpandEnv("$HOME"), ".config", "pmv")
	if len(os.Args) > 2 {
		baseDir = os.Args[2]
	}

	data, err := os.ReadFile(zipPath)
	if err != nil {
		fatal(err)
	}

	cat, err := parseZip(data)
	if err != nil {
		fatal(err)
	}

	if err := modland.SaveCatalog(baseDir, cat); err != nil {
		fatal(err)
	}

	n := 0
	for _, a := range cat.Albums {
		n += len(a.Tracks)
	}
	fmt.Printf("%d albums, %d tracks → %s/modland\n", len(cat.Albums), n, baseDir)
}

func parseZip(data []byte) (*modland.Catalog, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("zip open: %w", err)
	}

	var bestFile *zip.File
	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := strings.ToLower(f.Name)
		if strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".lst") {
			bestFile = f
			break
		}
		if bestFile == nil || f.UncompressedSize64 > bestFile.UncompressedSize64 {
			bestFile = f
		}
	}

	if bestFile == nil {
		return nil, fmt.Errorf("no files found in zip")
	}

	rc, err := bestFile.Open()
	if err != nil {
		return nil, fmt.Errorf("zip entry open: %w", err)
	}
	defer rc.Close()

	raw, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("zip entry read: %w", err)
	}

	return modland.ParseListing(raw)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
