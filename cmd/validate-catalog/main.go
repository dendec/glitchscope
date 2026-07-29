// validate-catalog tests each format in the modland catalog by downloading
// the smallest file and trying xmp→openmpt. Formats that fail are excluded.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/dendec/pmv/internal/modland"
	"github.com/dendec/pmv/internal/openmpt"
	"github.com/dendec/pmv/internal/xmp"
)

func main() {
	baseDir := filepath.Join(os.ExpandEnv("$HOME"), ".config", "pmv")
	if len(os.Args) > 1 {
		baseDir = os.Args[1]
	}

	cat := modland.LoadCatalog(baseDir)
	if cat == nil {
		fatal("no catalog found — run modland-catalog first")
	}

	// Group albums by format, track smallest file per format
	type formatInfo struct {
		format   string
	 smallest modland.Track
		album    string // first album in this format
	}
	formats := map[string]*formatInfo{}

	for _, a := range cat.Albums {
		format := modland.FormatName(a.Name)
		if modland.IsExcluded(cat.ExcludedFormats, format) {
			continue
		}
		for _, t := range a.Tracks {
			info, ok := formats[format]
			if !ok {
				formats[format] = &formatInfo{
					format:   format,
					smallest: t,
					album:    a.Name,
				}
				continue
			}
			if t.Size > 0 && (info.smallest.Size == 0 || t.Size < info.smallest.Size) {
				info.smallest = t
				info.album = a.Name
			}
		}
	}

	// Sort formats for deterministic output
	var sorted []*formatInfo
	for _, info := range formats {
		sorted = append(sorted, info)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].format < sorted[j].format
	})

	var excluded []string
	existing := map[string]bool{}
	for _, e := range cat.ExcludedFormats {
		existing[e] = true
	}

	client := &http.Client{Timeout: 30 * time.Second}

	for _, info := range sorted {
		if existing[info.format] {
			continue
		}

		remotePath := info.album + "/" + info.smallest.Name
		fmt.Printf("testing %s: %s (%d bytes)... ", info.format, info.smallest.Name, info.smallest.Size)

		data, err := download(client, remotePath)
		if err != nil {
			fmt.Printf("download failed: %v\n", err)
			continue
		}

		// Try xmp first
		if err := xmp.TryLoad(data); err == nil {
			fmt.Println("ok (xmp)")
			continue
		}

		// Try openmpt fallback
		if err := openmpt.TryLoad(data); err == nil {
			fmt.Println("ok (openmpt)")
			continue
		}

		fmt.Printf("FAIL — excluding format %q\n", info.format)
		excluded = append(excluded, info.format)
	}

	if len(excluded) == 0 {
		fmt.Println("\nall formats OK — nothing to exclude")
		return
	}

	// Merge with existing excluded list
	for _, e := range excluded {
		if !existing[e] {
			cat.ExcludedFormats = append(cat.ExcludedFormats, e)
		}
	}
	sort.Strings(cat.ExcludedFormats)

	if err := modland.SaveCatalog(baseDir, cat); err != nil {
		fatal(err)
	}
	fmt.Printf("\nexcluded %d formats (total %d): %v\n", len(excluded), len(cat.ExcludedFormats), excluded)
}

func download(client *http.Client, remotePath string) ([]byte, error) {
	urls := []string{
		modland.FileURL(remotePath),
		modland.FileURLLower(remotePath),
		modland.FileFallbackURL(remotePath),
		modland.FileFallbackURLLower(remotePath),
	}
	var resp *http.Response
	var err error
	for _, url := range urls {
		resp, err = client.Get(url)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		break
	}
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// Limit to 10MB
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	return data, nil
}

func fatal(args ...interface{}) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(1)
}
