package main

import (
	"encoding/csv"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var samplerRE = regexp.MustCompile(`(?i)\bsampler\s+sampler_([a-z0-9_]+)\b`)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "Usage: texture-report <preset-dir> <output.csv>")
		os.Exit(1)
	}

	rows, err := collect(os.Args[1])
	if err != nil {
		fatal(err)
	}
	file, err := os.Create(os.Args[2])
	if err != nil {
		fatal(err)
	}
	defer func() { _ = file.Close() }()

	writer := csv.NewWriter(file)
	if err := writer.Write([]string{"preset", "sampler", "texture", "kind"}); err != nil {
		fatal(err)
	}
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			fatal(err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %d texture references to %s\n", len(rows), os.Args[2])
}

func collect(root string) ([][]string, error) {
	var rows [][]string
	err := filepath.WalkDir(root, func(path string, item fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if item.IsDir() || !strings.EqualFold(filepath.Ext(item.Name()), ".milk") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		seen := make(map[string]bool)
		for _, match := range samplerRE.FindAllStringSubmatch(string(data), -1) {
			name := match[1]
			key := strings.ToLower(name)
			if seen[key] {
				continue
			}
			seen[key] = true
			texture, kind := normalize(name)
			if kind == "builtin" {
				continue
			}
			rows = append(rows, []string{filepath.ToSlash(rel), "sampler_" + name, texture, kind})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool {
		for column := range rows[i] {
			if rows[i][column] != rows[j][column] {
				return rows[i][column] < rows[j][column]
			}
		}
		return false
	})
	return rows, nil
}

func normalize(name string) (string, string) {
	lower := strings.ToLower(name)
	if lower == "main" || lower == "blur1" || lower == "blur2" || lower == "blur3" || strings.HasPrefix(lower, "noise") {
		return lower, "builtin"
	}
	if strings.HasPrefix(lower, "rand") && len(lower) >= 6 && lower[4] >= '0' && lower[4] <= '9' && lower[5] >= '0' && lower[5] <= '9' {
		return lower, "random"
	}
	if len(lower) > 3 && lower[2] == '_' {
		prefix := lower[:3]
		if prefix == "fc_" || prefix == "cf_" || prefix == "fw_" || prefix == "wf_" || prefix == "pc_" || prefix == "cp_" || prefix == "pw_" || prefix == "wp_" {
			return lower[3:], "file"
		}
	}
	return lower, "file"
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
