package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"

	"github.com/dendec/glitchscope/internal/presets"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "Usage: texture-report <presets-dir> <output.csv>")
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
	if err := presets.Open(root); err != nil {
		return nil, err
	}
	defer presets.Close()

	var rows [][]string
	for _, name := range presets.Names() {
		data, err := presets.Read(name)
		if err != nil {
			return nil, fmt.Errorf("read preset %q: %w", name, err)
		}
		for _, reference := range presets.TextureReferences(data) {
			rows = append(rows, []string{name, reference.Sampler, reference.Texture, reference.Kind})
		}
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

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
