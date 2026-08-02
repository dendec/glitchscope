package modarchive

import (
	"os"
	"testing"
	"time"
)

func TestCatalog_SaveLoadInit(t *testing.T) {
	ResetMemCache()
	defer ResetMemCache()

	tmpDir, err := os.MkdirTemp("", "modarchive_catalog_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cat := &Catalog{
		Directories: map[string][]DirItem{
			BaseURL: {
				{Name: "modarchive_2023_additions", URL: BaseURL + "modarchive_2023_additions/", Kind: KindDir, CleanName: "2023 Additions"},
			},
			BaseURL + "modarchive_2023_additions/": {
				{Name: "MOD", URL: BaseURL + "modarchive_2023_additions/MOD/", Kind: KindDir, CleanName: "MOD"},
			},
		},
		UpdatedAt: time.Now(),
	}

	// Save catalog
	if err := SaveCatalog(tmpDir, cat); err != nil {
		t.Fatalf("SaveCatalog failed: %v", err)
	}

	// Load catalog
	loadedCat := LoadCatalog(tmpDir)
	if loadedCat == nil {
		t.Fatalf("LoadCatalog returned nil")
	}

	if len(loadedCat.Directories) != 2 {
		t.Fatalf("expected 2 directories in catalog, got %d", len(loadedCat.Directories))
	}

	// Init catalog to populate memory cache
	InitCatalog(tmpDir)

	// Verify memCache contains preloaded directories
	items, ok := FetchDirectoryCached(tmpDir, BaseURL)
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 preloaded root item, got ok=%v, count=%d", ok, len(items))
	}
	if items[0].CleanName != "2023" {
		t.Errorf("unexpected clean name: %s", items[0].CleanName)
	}
}
