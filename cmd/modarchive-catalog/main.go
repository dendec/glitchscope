package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dendec/pmv/internal/modarchive"
)

func main() {
	var (
		targetDir   string
		concurrency int
		verbose     bool
	)

	flag.StringVar(&targetDir, "dir", ".", "Base directory to output the catalog to (saves to <dir>/.cache/modarchive/catalog)")
	flag.IntVar(&concurrency, "concurrency", 8, "Number of concurrent HTTP workers")
	flag.BoolVar(&verbose, "v", false, "Verbose logging for each fetched directory")
	flag.BoolVar(&verbose, "verbose", false, "Verbose logging for each fetched directory")
	flag.Parse()

	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
	slog.SetDefault(slog.New(handler))

	slog.Info("starting modarchive catalog crawler", "concurrency", concurrency, "output", targetDir, "verbose", verbose)

	startTime := time.Now()
	cat := crawlCatalog(targetDir, concurrency, verbose)

	if err := modarchive.SaveCatalog(targetDir, cat); err != nil {
		fmt.Fprintf(os.Stderr, "error saving catalog: %v\n", err)
		os.Exit(1)
	}

	catalogFile := modarchive.CatalogPath(targetDir)
	totalTracks := 0
	for _, items := range cat.Directories {
		for _, item := range items {
			if item.Kind == modarchive.KindFile {
				totalTracks++
			}
		}
	}

	slog.Info("modarchive catalog build complete",
		"directories", len(cat.Directories),
		"tracks", totalTracks,
		"elapsed", time.Since(startTime).Round(time.Second),
		"file", catalogFile,
	)
}

func crawlCatalog(baseDir string, concurrency int, verbose bool) *modarchive.Catalog {
	directories := make(map[string][]modarchive.DirItem)
	visited := make(map[string]bool)
	var mu sync.Mutex
	var processedCount int64

	queue := []string{modarchive.BaseURL}
	visited[modarchive.BaseURL] = true

	for len(queue) > 0 {
		currentBatch := queue
		batchSize := len(currentBatch)

		slog.Info("crawling level batch", "batch_size", batchSize, "total_directories_so_far", len(directories))

		workCh := make(chan string, batchSize)
		for _, u := range currentBatch {
			workCh <- u
		}
		close(workCh)

		var batchNext []string
		var wg sync.WaitGroup

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for u := range workCh {
					items, err := modarchive.FetchDirectory(baseDir, u)
					idx := atomic.AddInt64(&processedCount, 1)

					if err != nil {
						slog.Warn("fetch directory failed", "idx", idx, "url", u, "error", err)
						continue
					}

					dirCount := 0
					fileCount := 0
					for _, item := range items {
						if item.Kind == modarchive.KindDir {
							dirCount++
						} else {
							fileCount++
						}
					}

					if verbose {
						slog.Debug("fetched directory", "idx", idx, "url", u, "subdirs", dirCount, "files", fileCount)
					}

					mu.Lock()
					directories[u] = items
					for _, item := range items {
						if item.Kind == modarchive.KindDir && !visited[item.URL] {
							visited[item.URL] = true
							batchNext = append(batchNext, item.URL)
						}
					}
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		queue = batchNext
	}

	return &modarchive.Catalog{
		Directories: directories,
		UpdatedAt:   time.Now(),
	}
}
