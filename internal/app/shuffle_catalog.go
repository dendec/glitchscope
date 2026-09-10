package app

import (
	"log/slog"
	"net/url"
	"path"
	"strings"

	"github.com/dendec/glitchscope/internal/catalog"
	"github.com/dendec/glitchscope/internal/filesystem"
	"github.com/dendec/glitchscope/internal/modarchive"
	"github.com/dendec/glitchscope/internal/modland"
	"github.com/dendec/glitchscope/internal/player"
)

// updateLocalShuffleSource rebuilds the local shuffle source from a scan
// result. Only a successful (OK) scan may replace a known-good previous
// source; a Partial/Failed scan is reported and the previous source (which
// may be nil on first run) is kept untouched, per the filesystem-walk
// invariant in AGENTS.md §2.
func (a *App) updateLocalShuffleSource(albums []player.Album, status filesystem.Status) {
	if status != filesystem.StatusOK {
		available := a.localShuffleSrc != nil && a.localShuffleSrc.TrackCount() > 0
		slog.Warn("local shuffle index scan not OK, keeping previous source",
			"status", status, "available", available)
		return
	}
	a.localScanFingerprint = player.ScanFingerprint(albums)
	a.localShuffleSrc = player.NewLocalShuffleSource(albums, a.localScanFingerprint)
	slog.Info("local shuffle index ready", "tracks", a.localShuffleSrc.TrackCount(),
		"directories", a.localShuffleSrc.DirectoryCount())
}

// buildShuffleCatalog opens each provider's persistent shuffle index,
// rebuilding synchronously if missing or stale, and assembles the runtime
// ShuffleCatalog coordinator. A source whose index cannot be built is
// disabled; the other sources remain usable.
//
// The result is installed atomically so the render thread can keep using
// the old (nil or empty) catalog while the rebuild is in progress.
func (a *App) buildShuffleCatalog() {
	if a.trackCache != nil {
		if err := a.trackCache.ReconcileManifest(); err != nil {
			slog.Warn("track cache manifest reconcile", "error", err)
		}
	}
	var sources []catalog.SourceIndex

	if a.modlandShuffleSrc != nil {
		_ = a.modlandShuffleSrc.Close()
		a.modlandShuffleSrc = nil
	}
	if src := a.openModlandShuffleSource(); src != nil {
		a.modlandShuffleSrc = src
		sources = append(sources, src)
	}

	if a.modarchiveShuffleSrc != nil {
		_ = a.modarchiveShuffleSrc.Close()
		a.modarchiveShuffleSrc = nil
	}
	if src := a.openModArchiveShuffleSource(); src != nil {
		a.modarchiveShuffleSrc = src
		sources = append(sources, src)
	}

	if a.localShuffleSrc != nil {
		sources = append(sources, a.localShuffleSrc)
	}

	cat := catalog.NewShuffleCatalog(sources...)
	a.refreshOfflineProjection()
	a.shuffleCatalog.Store(cat)
	slog.Info("shuffle catalog ready", "sources", len(sources), "tracks", cat.TotalTrackCount())
}

// refreshOfflineProjection publishes a complete source-preserving view of
// local tracks and cached remote tracks. It never mutates the snapshot already
// used by playback, so selection cannot observe a half-updated cache index.
func (a *App) refreshOfflineProjection() {
	var indexes []catalog.SourceIndex
	if a.localShuffleSrc != nil {
		indexes = append(indexes, a.localShuffleSrc)
	}

	if a.trackCache != nil {
		bySource := make(map[catalog.SourceKind][]catalog.ShuffleTrack)
		for _, virtualPath := range a.trackCache.CachedVirtualPaths() {
			track := offlineShuffleTrack(virtualPath)
			bySource[track.Source] = append(bySource[track.Source], track)
		}
		for _, source := range []catalog.SourceKind{catalog.SourceModland, catalog.SourceModArchive} {
			if tracks := bySource[source]; len(tracks) > 0 {
				indexes = append(indexes, catalog.NewListSource(source, tracks))
			}
		}
	}

	a.offlineProjection.Store(catalog.NewOfflineProjection(indexes...))
}

func offlineShuffleTrack(virtualPath string) catalog.ShuffleTrack {
	if player.IsModland(virtualPath) {
		remote := player.RemotePath(virtualPath)
		directory := path.Dir(remote)
		if directory == "." {
			directory = ""
		}
		return catalog.ShuffleTrack{
			Path:         virtualPath,
			Source:       catalog.SourceModland,
			DirectoryKey: catalog.DirectoryKey{Source: catalog.SourceModland, Locator: directory},
			TrackKey:     path.Base(remote),
			AlbumName:    path.Base(directory),
		}
	}

	remote := player.RemotePath(virtualPath)
	parsed, err := url.Parse(remote)
	if err != nil {
		return catalog.ShuffleTrack{Path: virtualPath, Source: catalog.SourceModArchive}
	}
	locator := *parsed
	locator.Fragment = ""
	locator.RawFragment = ""
	albumName := path.Base(strings.TrimSuffix(parsed.Path, "/"))
	if parsed.Fragment == "" {
		directory := path.Dir(parsed.Path)
		locator.Path = directory + "/"
		albumName = path.Base(directory)
	}
	trackKey := path.Base(parsed.Fragment)
	if trackKey == "." || trackKey == "/" || trackKey == "" {
		trackKey = path.Base(parsed.Path)
	}
	return catalog.ShuffleTrack{
		Path:         virtualPath,
		Source:       catalog.SourceModArchive,
		DirectoryKey: catalog.DirectoryKey{Source: catalog.SourceModArchive, Locator: locator.String()},
		TrackKey:     trackKey,
		AlbumName:    albumName,
	}
}

// openModlandShuffleSource opens modland.idx, rebuilding it synchronously
// from the cached catalog if missing or stale. Returns nil (source
// disabled) if no valid index can be produced. Avoids double-reading the
// GSA on the fast path (index is fresh): open once, check fingerprint,
// only rebuild+reopen if stale.
func (a *App) openModlandShuffleSource() *modland.ShuffleSource {
	dir := baseDir()
	src := modland.OpenShuffleSource(dir)
	if src != nil {
		// Index opened — check fingerprint to detect staleness.
		if !modland.ShuffleIndexStale(dir, src.Fingerprint()) {
			return src // fast path: index is fresh
		}
		src.Close()
	}
	// Slow path: rebuild from catalog, then open.
	cat := modland.LoadCatalog(dir)
	if cat == nil {
		slog.Debug("modland: no cached catalog, shuffle source disabled")
		return nil
	}
	if err := modland.BuildShuffleIndex(dir, cat); err != nil {
		slog.Warn("modland: shuffle index rebuild failed, source disabled", "error", err)
		return nil
	}
	src = modland.OpenShuffleSource(dir)
	if src == nil {
		slog.Warn("modland: shuffle index unavailable after rebuild, source disabled")
	}
	return src
}

// openModArchiveShuffleSource opens modarchive.idx, rebuilding it
// synchronously from the snapshot/addendum GSA and cached catalog if
// missing or stale. Returns nil (source disabled) if no valid index can
// be produced. Avoids double-reading the GSA on the fast path.
func (a *App) openModArchiveShuffleSource() *modarchive.ShuffleSource {
	dir := baseDir()
	src := modarchive.OpenShuffleSource(dir)
	if src != nil {
		if !modarchive.ShuffleIndexStale(dir, src.Fingerprint()) {
			return src // fast path: index is fresh
		}
		src.Close()
	}
	// Slow path: rebuild from cache, then open.
	if err := modarchive.RebuildShuffleIndexFromCache(dir); err != nil {
		slog.Warn("modarchive: shuffle index rebuild failed, source disabled", "error", err)
		return nil
	}
	src = modarchive.OpenShuffleSource(dir)
	if src == nil {
		slog.Warn("modarchive: shuffle index unavailable after rebuild, source disabled")
	}
	return src
}
