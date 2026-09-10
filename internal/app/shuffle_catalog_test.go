package app

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/dendec/glitchscope/internal/catalog"
	"github.com/dendec/glitchscope/internal/filesystem"
	"github.com/dendec/glitchscope/internal/player"
)

func testAlbums() []player.Album {
	return []player.Album{
		{Name: "Album1", Path: "/music/Album1", Tracks: []string{"/music/Album1/a.mp3", "/music/Album1/b.mp3"}},
	}
}

func TestUpdateLocalShuffleSourceOnOKScan(t *testing.T) {
	a := &App{}
	a.updateLocalShuffleSource(testAlbums(), filesystem.StatusOK)

	if a.localShuffleSrc == nil {
		t.Fatal("expected localShuffleSrc to be set after an OK scan")
	}
	if a.localShuffleSrc.TrackCount() != 2 {
		t.Fatalf("TrackCount = %d, want 2", a.localShuffleSrc.TrackCount())
	}
	if a.localScanFingerprint == "" {
		t.Fatal("expected a non-empty scan fingerprint")
	}
}

func TestUpdateLocalShuffleSourcePreservesPreviousOnPartialScan(t *testing.T) {
	a := &App{}
	a.updateLocalShuffleSource(testAlbums(), filesystem.StatusOK)
	previous := a.localShuffleSrc
	previousFP := a.localScanFingerprint

	// A Partial/Failed rescan must not replace the known-good source.
	a.updateLocalShuffleSource(nil, filesystem.StatusPartial)

	if a.localShuffleSrc != previous {
		t.Fatal("expected localShuffleSrc to be unchanged after a Partial scan")
	}
	if a.localScanFingerprint != previousFP {
		t.Fatal("expected scan fingerprint to be unchanged after a Partial scan")
	}
}

func TestUpdateLocalShuffleSourcePreservesPreviousOnFailedScan(t *testing.T) {
	a := &App{}
	a.updateLocalShuffleSource(testAlbums(), filesystem.StatusOK)
	previous := a.localShuffleSrc

	a.updateLocalShuffleSource(nil, filesystem.StatusFailed)

	if a.localShuffleSrc != previous {
		t.Fatal("expected localShuffleSrc to be unchanged after a Failed scan")
	}
}

func TestBuildShuffleCatalogWithOnlyLocalSource(t *testing.T) {
	a := &App{}
	a.updateLocalShuffleSource(testAlbums(), filesystem.StatusOK)
	a.buildShuffleCatalog()

	cat := a.shuffleCatalog.Load()
	if cat == nil {
		t.Fatal("expected shuffleCatalog to be built")
	}
	if cat.SourceIndex(catalog.SourceLocal) == nil {
		t.Fatal("expected local source to be registered in the catalog")
	}
	if cat.TotalTrackCount() != 2 {
		t.Fatalf("TotalTrackCount = %d, want 2", cat.TotalTrackCount())
	}
}

func TestBuildShuffleCatalogWithNoSources(t *testing.T) {
	a := &App{}
	a.buildShuffleCatalog()

	cat := a.shuffleCatalog.Load()
	if cat == nil {
		t.Fatal("expected shuffleCatalog to be built even with no sources")
	}
	if len(cat.Available()) != 0 {
		t.Fatalf("Available() = %d, want 0", len(cat.Available()))
	}
}

func TestRefreshOfflineProjectionIncludesCachedRemoteTracks(t *testing.T) {
	base := t.TempDir()
	physical := filepath.Join(base, ".cache", "modland", "files", "MOD", "cached.mod")
	if err := os.MkdirAll(filepath.Dir(physical), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(physical, []byte("module"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &App{trackCache: player.NewTrackCache(base)}
	if err := a.trackCache.ReconcileManifest(); err != nil {
		t.Fatal(err)
	}
	a.refreshOfflineProjection()

	projection := a.offlineProjection.Load()
	if projection == nil {
		t.Fatal("offline projection is nil")
	}
	index := projection.SourceIndex(catalog.SourceModland)
	if index == nil || index.TrackCount() != 1 {
		t.Fatalf("cached Modland index = %#v, want one track", index)
	}
	track, err := projection.RandomTrackFromSource(catalog.SourceModland, rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatal(err)
	}
	if track.Path != player.ModlandPrefix+"MOD/cached.mod" || track.Source != catalog.SourceModland {
		t.Fatalf("cached track = %+v", track)
	}
}
