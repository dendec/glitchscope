package main

import (
	"encoding/csv"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dendec/glitchscope/internal/archive"
)

const (
	presetKind  = "presets"
	textureKind = "textures"
)

func main() {
	if len(os.Args) != 4 && len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "Usage: glitchscope-pack <presets|textures> <input-dir> <output.gsa> [benchmark.csv]")
		os.Exit(1)
	}
	kind, inputDir, outputPath := os.Args[1], os.Args[2], os.Args[3]
	if kind != presetKind && kind != textureKind {
		fatal(fmt.Errorf("unknown archive kind %q", kind))
	}

	blacklist := map[string]bool{}
	if len(os.Args) == 5 {
		var err error
		blacklist, err = loadBlacklist(os.Args[4], 20.0)
		if err != nil {
			fatal(fmt.Errorf("load benchmark: %w", err))
		}
	}
	entries, err := collect(inputDir, kind, blacklist)
	if err != nil {
		fatal(err)
	}
	if len(entries) == 0 {
		fatal(fmt.Errorf("no %s files found in %s", kind, inputDir))
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		fatal(err)
	}
	if err := archive.Write(outputPath, entries); err != nil {
		fatal(fmt.Errorf("write %s: %w", outputPath, err))
	}
	fmt.Printf("wrote %d %s to %s\n", len(entries), kind, outputPath)
}

func collect(root, kind string, blacklist map[string]bool) ([]archive.SourceEntry, error) {
	var entries []archive.SourceEntry
	err := filepath.WalkDir(root, func(path string, item fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if item.IsDir() || !allowed(item.Name(), kind) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if blacklist[key] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries = append(entries, archive.SourceEntry{Name: key, Data: data})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

func allowed(name, kind string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	if kind == presetKind {
		return ext == ".milk"
	}
	switch ext {
	case ".jpg", ".jpeg", ".png", ".dds", ".tga", ".bmp", ".dib":
		return true
	default:
		return false
	}
}

func loadBlacklist(path string, minFPS float64) (map[string]bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 || len(records[0]) != 6 || records[0][0] != "preset" || records[0][1] != "status" {
		return nil, fmt.Errorf("invalid benchmark CSV header")
	}

	blacklist := make(map[string]bool)
	for i, row := range records[1:] {
		if len(row) != 6 {
			return nil, fmt.Errorf("invalid benchmark CSV row %d", i+2)
		}
		if row[1] != "ok" {
			blacklist[row[0]] = true
			continue
		}
		fps, err := strconv.ParseFloat(row[4], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid FPS in benchmark CSV row %d: %w", i+2, err)
		}
		if fps < minFPS {
			blacklist[row[0]] = true
		}
	}
	return blacklist, nil
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
