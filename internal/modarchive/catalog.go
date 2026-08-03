package modarchive

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/dendec/pmv/internal/util"
)

// Catalog contains pre-crawled directory structures for ModArchive.
type Catalog struct {
	Directories map[string][]DirItem `json:"directories"`
	UpdatedAt   time.Time            `json:"updated_at"`
}

// CatalogPath returns the path to the modarchive catalog file (.cache/modarchive/catalog).
func CatalogPath(baseDir string) string {
	cacheDir, _ := CacheDir(baseDir)
	return filepath.Join(cacheDir, "catalog")
}

// LoadCatalog reads the ModArchive catalog from disk if present.
func LoadCatalog(baseDir string) *Catalog {
	MigrateLegacyCache(baseDir)
	path := CatalogPath(baseDir)

	var cat Catalog
	if err := util.LoadGzipJSON(path, &cat); err != nil {
		return nil
	}

	return &cat
}

// SaveCatalog writes the catalog to disk as gzipped JSON.
func SaveCatalog(baseDir string, cat *Catalog) error {
	path := CatalogPath(baseDir)
	if err := util.SaveGzipJSON(path, cat); err != nil {
		return fmt.Errorf("modarchive catalog save: %w", err)
	}
	return nil
}

// InitCatalog loads the pre-crawled catalog from baseDir and pre-populates in-memory cache.
func InitCatalog(baseDir string) {
	cat := LoadCatalog(baseDir)
	if cat == nil || len(cat.Directories) == 0 {
		slog.Info("modarchive: catalog file not found or empty")
		return
	}

	memCacheMu.Lock()
	for urlPath, items := range cat.Directories {
		// Empty entries may have been generated before a newly supported
		// extension was added. Let FetchDirectory refresh those listings.
		if len(items) == 0 {
			continue
		}
		for i := range items {
			if items[i].Kind == KindDir {
				items[i].CleanName = FormatDirName(items[i].CleanName)
			}
		}
		memCache[urlPath] = items
	}
	memCacheMu.Unlock()

	slog.Info("modarchive: preloaded catalog from disk", "directories", len(cat.Directories), "age", time.Since(cat.UpdatedAt).Round(time.Hour))
}
