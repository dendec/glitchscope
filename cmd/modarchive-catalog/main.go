package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dendec/glitchscope/internal/modarchive"
)

func main() {
	var (
		targetDir    string
		concurrency  int
		verbose      bool
		catalogOnly  bool
		snapshotOnly bool
		addendumOnly bool
	)

	flag.StringVar(&targetDir, "dir", ".", "Base directory to output the catalog to (saves to <dir>/.cache/modarchive/catalog)")
	flag.IntVar(&concurrency, "concurrency", 8, "Number of concurrent HTTP workers")
	flag.BoolVar(&verbose, "v", false, "Verbose logging for each fetched directory")
	flag.BoolVar(&verbose, "verbose", false, "Verbose logging for each fetched directory")
	flag.BoolVar(&catalogOnly, "catalog-only", false, "Build only the additions catalog")
	flag.BoolVar(&snapshotOnly, "snapshot-only", false, "Build only the 1987-2007 snapshot catalog")
	flag.BoolVar(&addendumOnly, "addendum-only", false, "Build only the 2007 addendum catalog")
	flag.Parse()
	if concurrency < 1 {
		fmt.Fprintln(os.Stderr, "concurrency must be at least 1")
		os.Exit(2)
	}
	selectedModes := 0
	for _, selected := range []bool{catalogOnly, snapshotOnly, addendumOnly} {
		if selected {
			selectedModes++
		}
	}
	if selectedModes > 1 {
		fmt.Fprintln(os.Stderr, "catalog-only, snapshot-only, and addendum-only cannot be combined")
		os.Exit(2)
	}

	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
	slog.SetDefault(slog.New(handler))

	slog.Info("starting modarchive catalog crawler", "concurrency", concurrency, "output", targetDir, "verbose", verbose)

	if !snapshotOnly && !addendumOnly {
		startTime := time.Now()
		cat := crawlCatalog(targetDir, concurrency, verbose)
		if err := modarchive.SaveCatalog(targetDir, cat); err != nil {
			fmt.Fprintf(os.Stderr, "error saving catalog: %v\n", err)
			os.Exit(1)
		}
		logCatalogComplete(targetDir, cat, startTime)
	}

	if !catalogOnly && !addendumOnly {
		startTime := time.Now()
		buckets, err := crawlSnapshot(targetDir, concurrency, verbose)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error building snapshot catalog: %v\n", err)
			os.Exit(1)
		}
		if err := modarchive.SaveSnapshotCatalog(targetDir, buckets); err != nil {
			fmt.Fprintf(os.Stderr, "error saving snapshot catalog: %v\n", err)
			os.Exit(1)
		}
		slog.Info("modarchive snapshot build complete",
			"buckets", len(buckets),
			"tracks", countTracks(buckets),
			"elapsed", time.Since(startTime).Round(time.Second),
			"file", modarchive.SnapshotCatalogPath(targetDir),
		)
	}

	if !catalogOnly && !snapshotOnly {
		startTime := time.Now()
		buckets, err := crawlAddendum(targetDir, concurrency, verbose)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error building addendum catalog: %v\n", err)
			os.Exit(1)
		}
		if err := modarchive.SaveAddendumCatalog(targetDir, buckets); err != nil {
			fmt.Fprintf(os.Stderr, "error saving addendum catalog: %v\n", err)
			os.Exit(1)
		}
		slog.Info("modarchive addendum build complete",
			"buckets", len(buckets),
			"tracks", countTracks(buckets),
			"elapsed", time.Since(startTime).Round(time.Second),
			"file", modarchive.AddendumCatalogPath(targetDir),
		)
	}
}

func logCatalogComplete(targetDir string, cat *modarchive.Catalog, startTime time.Time) {
	slog.Info("modarchive catalog build complete",
		"directories", len(cat.Directories),
		"tracks", countTracks(cat.Directories),
		"elapsed", time.Since(startTime).Round(time.Second),
		"file", modarchive.CatalogPath(targetDir),
	)
}

func countTracks(directories map[string][]modarchive.DirItem) int {
	total := 0
	for _, items := range directories {
		for _, item := range items {
			if item.Kind == modarchive.KindFile {
				total++
			}
		}
	}
	return total
}

func crawlSnapshot(baseDir string, concurrency int, verbose bool) (map[string][]modarchive.DirItem, error) {
	snapshotURL := modarchive.BaseURL + modarchive.SnapshotDir + "/"
	letters, err := modarchive.FetchDirectory(baseDir, snapshotURL)
	if err != nil {
		return nil, fmt.Errorf("fetch snapshot root: %w", err)
	}

	letterURLs := make([]string, 0, len(letters))
	for _, item := range letters {
		if item.Kind == modarchive.KindDir {
			letterURLs = append(letterURLs, item.URL)
		}
	}
	letterListings, err := fetchDirectories(letterURLs, baseDir, concurrency, verbose, "snapshot letter")
	if err != nil {
		return nil, err
	}

	archiveURLs := make([]string, 0, 1200)
	for _, items := range letterListings {
		for _, item := range items {
			if item.Kind == modarchive.KindArchive {
				archiveURLs = append(archiveURLs, item.URL)
			}
		}
	}
	if len(archiveURLs) == 0 {
		return nil, fmt.Errorf("snapshot contains no bucket archives")
	}
	sort.Strings(archiveURLs)
	return fetchDirectories(archiveURLs, baseDir, concurrency, verbose, "snapshot bucket")
}

func crawlAddendum(baseDir string, concurrency int, verbose bool) (map[string][]modarchive.DirItem, error) {
	addendumURL := modarchive.BaseURL + modarchive.AddendumDir + "/"
	items, err := modarchive.FetchDirectory(baseDir, addendumURL)
	if err != nil {
		return nil, fmt.Errorf("fetch addendum root: %w", err)
	}

	archiveURLs := make([]string, 0, len(items))
	for _, item := range items {
		if item.Kind == modarchive.KindArchive {
			archiveURLs = append(archiveURLs, item.URL)
		}
	}
	if len(archiveURLs) == 0 {
		return nil, fmt.Errorf("addendum contains no bucket archives")
	}
	sort.Strings(archiveURLs)
	return fetchDirectories(archiveURLs, baseDir, concurrency, verbose, "addendum bucket")
}

func fetchDirectories(urls []string, baseDir string, concurrency int, verbose bool, label string) (map[string][]modarchive.DirItem, error) {
	workCh := make(chan string, len(urls))
	for _, targetURL := range urls {
		workCh <- targetURL
	}
	close(workCh)

	results := make(map[string][]modarchive.DirItem, len(urls))
	var (
		mu        sync.Mutex
		wg        sync.WaitGroup
		firstErr  error
		processed atomic.Int64
	)
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for targetURL := range workCh {
				items, err := modarchive.FetchDirectory(baseDir, targetURL)
				current := processed.Add(1)
				mu.Lock()
				if err != nil {
					if firstErr == nil {
						firstErr = fmt.Errorf("fetch %s %s: %w", label, targetURL, err)
					}
				} else {
					results[targetURL] = items
				}
				mu.Unlock()
				if verbose {
					slog.Debug("fetched "+label, "idx", current, "total", len(urls), "url", targetURL, "items", len(items), "error", err)
				}
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if len(results) != len(urls) {
		return nil, fmt.Errorf("fetched %d of %d %ss", len(results), len(urls), label)
	}
	return results, nil
}

func crawlCatalog(baseDir string, concurrency int, verbose bool) *modarchive.Catalog {
	directories := make(map[string][]modarchive.DirItem)
	visited := make(map[string]bool)
	snapshotURL := modarchive.BaseURL + modarchive.SnapshotDir + "/"
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
						if item.Kind == modarchive.KindDir && item.URL != snapshotURL && !visited[item.URL] {
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
