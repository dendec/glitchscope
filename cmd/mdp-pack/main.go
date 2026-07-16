package main

import (
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
)

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}

func main() {
	if len(os.Args) != 3 && len(os.Args) != 4 {
		fmt.Fprintf(os.Stderr, "Usage: mdp-pack <input-dir> <output.mdp> [benchmark.csv]\n")
		os.Exit(1)
	}
	inputDir := os.Args[1]
	outputPath := os.Args[2]
	blacklist := make(map[string]bool)
	if len(os.Args) == 4 {
		var err error
		blacklist, err = loadBlacklist(os.Args[3], 20.0)
		if err != nil {
			fatal(fmt.Errorf("load benchmark: %w", err))
		}
	}

	type entry struct {
		name string
		data []byte
	}
	var entries []entry

	if err := filepath.WalkDir(inputDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".milk") {
			return nil
		}
		rel, err := filepath.Rel(inputDir, path)
		if err != nil {
			return err
		}
		if blacklist[filepath.ToSlash(rel)] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries = append(entries, entry{name: rel, data: data})
		return nil
	}); err != nil {
		fatal(err)
	}

	if len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "warning: no .milk files in %s\n", inputDir)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	enc, err := zstd.NewWriter(nil)
	if err != nil {
		fatal(fmt.Errorf("zstd encoder: %w", err))
	}

	type ce struct {
		name  string
		cdata []byte
	}
	compressed := make([]ce, len(entries))
	for i, e := range entries {
		compressed[i] = ce{name: e.name, cdata: enc.EncodeAll(e.data, nil)}
	}

	f, err := os.Create(outputPath)
	if err != nil {
		fatal(fmt.Errorf("create %s: %w", outputPath, err))
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close %s: %v\n", outputPath, err)
		}
	}()

	mustWrite := func(p []byte) {
		if _, err := f.Write(p); err != nil {
			fatal(fmt.Errorf("write: %w", err))
		}
	}
	mustBinaryWrite := func(data any) {
		if err := binary.Write(f, binary.LittleEndian, data); err != nil {
			fatal(fmt.Errorf("write: %w", err))
		}
	}

	mustWrite([]byte("MDP\x00"))
	mustBinaryWrite(uint32(1))               // version
	mustBinaryWrite(uint32(len(compressed))) // count

	for _, e := range compressed {
		nb := []byte(e.name)
		mustBinaryWrite(uint16(len(nb)))
		mustWrite(nb)
		mustBinaryWrite(uint32(len(e.cdata)))
	}

	for _, e := range compressed {
		mustWrite(e.cdata)
	}

	// Sync before close.
	if err := f.Sync(); err != nil {
		fmt.Fprintf(os.Stderr, "sync %s: %v\n", outputPath, err)
	}

	fmt.Printf("wrote %d presets to %s\n", len(compressed), outputPath)
}

func loadBlacklist(path string, minFPS float64) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	records, err := csv.NewReader(f).ReadAll()
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
