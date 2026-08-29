package modarchive

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dendec/glitchscope/internal/util"
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
// Reports whether a non-empty catalog was found.
func InitCatalog(baseDir string) bool {
	cat := LoadCatalog(baseDir)
	hasCatalog := cat != nil && len(cat.Directories) > 0
	if !hasCatalog {
		slog.Info("modarchive: catalog file not found or empty")
	} else {
		rootItems := cat.Directories[BaseURL]
		for _, required := range []DirItem{snapshotRootItem(), addendumRootItem()} {
			if !hasRootItem(rootItems, required.Name) {
				rootItems = append(rootItems, required)
			}
		}
		if len(rootItems) != len(cat.Directories[BaseURL]) {
			sort.Slice(rootItems, func(i, j int) bool {
				return strings.ToLower(rootItems[i].CleanName) < strings.ToLower(rootItems[j].CleanName)
			})
			cat.Directories[BaseURL] = rootItems
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

	hasOfflineCatalog := InitSnapshotCatalog(baseDir)
	if !hasCatalog && hasOfflineCatalog {
		cacheOfflineOnlyRoot()
	}
	return hasCatalog || hasOfflineCatalog
}

func snapshotRootItem() DirItem {
	return DirItem{
		Name:      SnapshotDir,
		URL:       BaseURL + SnapshotDir + "/",
		Kind:      KindDir,
		CleanName: SnapshotLabel,
	}
}

func addendumRootItem() DirItem {
	return DirItem{
		Name:      AddendumDir,
		URL:       BaseURL + AddendumDir + "/",
		Kind:      KindDir,
		CleanName: AddendumLabel,
	}
}

func hasRootItem(items []DirItem, name string) bool {
	for _, item := range items {
		if strings.Trim(item.Name, "/") == name {
			return true
		}
	}
	return false
}
