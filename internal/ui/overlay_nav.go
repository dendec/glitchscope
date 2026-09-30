package ui

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/filesystem"
	"github.com/dendec/glitchscope/internal/i18n"
	"github.com/dendec/glitchscope/internal/modarchive"
	"github.com/dendec/glitchscope/internal/player"
	"github.com/dendec/glitchscope/internal/radio"
)

// This file owns the library navigation model: local-album / modland / modarchive
// format/album drill-down tree. State mutation in overlay_input.go;
// struct fields in overlay.go.

const modlandNamePrefix = "Modland: "

// currentAlbums returns the one-frame snapshot of the lib.Albums list (local
// + virtual + catalog). Never nil — returns nil when no callback is
// registered. The snapshot is refreshed once per frame in Update and after
// every album mutation, so all consumers within a frame see the same list.
func (o *Overlay) currentAlbums() []player.Album {
	return o.cachedAlbums
}

// refreshAlbumsCache re-reads the lib.Albums snapshot. Called once per frame
// (Update) and after every mutation (SetLibAlbums, catalog album addition).
func (o *Overlay) refreshAlbumsCache() {
	if o.libAlbums == nil {
		return
	}
	o.cachedAlbums = o.libAlbums()
}

// navEntryKind classifies a row in the library navigation panel.
type navEntryKind int

const (
	entrySource           navEntryKind = iota // virtual source root entry
	entryFormat                               // modland format bucket (e.g. "Protracker")
	entryModlandAlbum                         // modland author/album within a format — leaf, has tracks
	entryModArchiveDir                        // ModArchive HTTP directory folder
	entryCatalogTrack                         // track in a catalog album
	entryParent                               // parent navigation level
	entryNCDir                                // NC directory entry
	entryNCFile                               // NC file entry (leaf)
	entryFavoriteFolder                       // favorites playlist folder
	entryFavoriteTrack                        // track in a favorites playlist
	entryMicrophoneDevice                     // SDL capture device entry
	entryMicrophoneStop                       // stops active capture
	entryInfo                                 // non-selectable informational row
	entryRadioCategory
	entryRadioFilter
	entryRadioStation
	entryRadioHistoryClear
)

// navEntry is one row in the library's left (navigation) panel.
type navEntry struct {
	label        string
	kind         navEntryKind
	source       sourceKind // set when kind == entrySource
	albumIdx     int        // index into Overlay.allAlbums when kind is a leaf album, else -1
	format       string     // set when kind == entryFormat
	url          string     // set when kind == entryModArchiveDir
	dirPath      string     // set when kind == entryNCDir
	filePath     string     // set when kind == entryNCFile
	trackIdx     int        // set when kind == entryCatalogTrack
	device       string     // set when kind == entryMicrophoneDevice
	radioKind    radio.BrowseKind
	radioFilter  string
	radioStation radio.Station
}

// navLevel is one visible listing in the unified navigation stack.
// The stack bottom is the virtual source root; Back never pops it.
type navLevel struct {
	ctx         navCtx
	entries     []navEntry
	cursor      int
	scroll      int
	dirPath     string // ctxNC: the filesystem directory this level lists
	label       string // ctxCatalog: breadcrumb label for this level
	playlistID  string // ctxFavorites: PlaylistID ("star", "heart", "note")
	radioKind   radio.BrowseKind
	radioFilter string
}

// splitModlandName splits "Modland: Format/Author" into format and author.
func splitModlandName(name string) (format, author string) {
	n := strings.TrimPrefix(name, modlandNamePrefix)
	if idx := strings.IndexByte(n, '/'); idx >= 0 {
		return n[:idx], n[idx+1:]
	}
	return n, ""
}

func labelsOf(entries []navEntry) []string {
	labels := make([]string, len(entries))
	for i, e := range entries {
		labels[i] = e.label
	}
	return labels
}

func (e *navEntry) IsLeafAlbum() bool {
	if e == nil {
		return false
	}
	return e.kind == entryModlandAlbum
}

func (e *navEntry) IsNCDirectory() bool {
	return e != nil && e.kind == entryNCDir
}

func (e *navEntry) IsNCFile() bool {
	return e != nil && e.kind == entryNCFile
}

func (e *navEntry) IsCatalogTrack() bool {
	return e != nil && e.kind == entryCatalogTrack
}

func (e *navEntry) IsFavoriteTrack() bool {
	return e != nil && e.kind == entryFavoriteTrack
}

// relParts returns the path segments of path relative to base: nil when path
// isn't under base, empty for base itself.
func relParts(base, path string) []string {
	rel, err := filepath.Rel(base, path)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return nil
	}
	if rel == "." {
		return []string{}
	}
	return strings.Split(filepath.ToSlash(rel), "/")
}

// micLabel shows the microphone source entry.
func (o *Overlay) micLabel() string {
	if o.micActive {
		return o.catalog.Text(i18n.SourceMicrophoneCapturing) + "/"
	}
	return o.catalog.Text(i18n.SourceMicrophone) + "/"
}

func (o *Overlay) buildSourceEntries() []navEntry {
	entries := []navEntry{{label: o.catalog.Text(i18n.SourceLocalMusic) + "/", kind: entrySource, source: sourceMusic, albumIdx: -1}}
	if o.favoritesView != nil && o.favoritesView.TotalCount() > 0 {
		entries = append(entries, navEntry{label: o.catalog.Text(i18n.SourceFavorites) + "/", kind: entrySource, source: sourceFavorites, albumIdx: -1})
	}
	entries = append(entries, navEntry{label: o.catalog.Text(i18n.SourceDownloads) + "/", kind: entrySource, source: sourceDownloads, albumIdx: -1})
	if len(o.micDevices) > 0 || o.micActive {
		entries = append(entries, navEntry{label: o.micLabel(), kind: entrySource, source: sourceMicrophone, albumIdx: -1})
	}
	slog.Debug("buildSourceEntries", "online", o.online)
	entries = append(entries,
		navEntry{label: o.catalog.Text(i18n.SourceRadio) + "/", kind: entrySource, source: sourceRadio, albumIdx: -1},
		navEntry{label: "Modland/", kind: entrySource, source: sourceModland, albumIdx: -1},
		navEntry{label: "ModArchive/", kind: entrySource, source: sourceModArchive, albumIdx: -1},
	)
	return entries
}

// refreshSourceRoot rebuilds the virtual source root while preserving its
// navigation state. When visible, the UI fields are the source of truth.
func (o *Overlay) refreshSourceRoot() {
	root := o.navStack[0]
	root.entries = o.buildSourceEntries()
	if o.topLevel().ctx == ctxSourceRoot {
		root.cursor = o.albumCursor
		root.scroll = o.albumsScroll
	}
	root.cursor = clampCursor(root.cursor, len(root.entries))
	o.navStack[0] = root

	if o.topLevel().ctx == ctxSourceRoot {
		o.albumCursor = root.cursor
		o.albumsScroll = root.scroll
		o.syncPanels()
	}
}

// relocalizeSourceLabels updates the source root and any open source-level
// breadcrumb without disturbing navigation or cursor state.
func (o *Overlay) relocalizeSourceLabels() {
	if len(o.navStack) == 0 {
		return
	}
	o.refreshSourceRoot()
	for i := 1; i < len(o.navStack); i++ {
		switch o.navStack[i].ctx {
		case ctxFavorites:
			if i == 1 {
				o.navStack[i].label = o.catalog.Text(i18n.SourceFavorites)
			}
		case ctxMicrophone:
			o.navStack[i].label = o.catalog.Text(i18n.SourceMicrophone)
		case ctxCatalog:
			if i == 1 && o.source == sourceDownloads {
				o.navStack[i].label = o.catalog.Text(i18n.SourceDownloads)
			} else if i == 1 && o.source == sourceModland {
				o.navStack[i].label = "Modland"
			} else if i == 1 && o.source == sourceModArchive {
				o.navStack[i].label = "ModArchive"
			}
		}
	}
}

// rebuildCurrentFavoritePlaylist refreshes the entries of an open favourite
// playlist level, preserving the cursor position when possible.
func (o *Overlay) rebuildCurrentFavoritePlaylist() {
	if o.favoritesView == nil {
		return
	}
	lvl := o.topLevel()
	if lvl.ctx != ctxFavorites || lvl.playlistID == "" {
		return
	}
	kind := player.PlaylistID(lvl.playlistID)
	tracks := o.favoritesView.Tracks(kind)
	entries := withParentEntry(o.favoriteTrackEntries(tracks, false))
	path := ""
	if o.albumCursor >= 0 && o.albumCursor < len(lvl.entries) {
		path = lvl.entries[o.albumCursor].filePath
	}
	o.navStack[len(o.navStack)-1] = navLevel{
		ctx:        ctxFavorites,
		entries:    entries,
		cursor:     0,
		scroll:     0,
		label:      lvl.label,
		playlistID: lvl.playlistID,
	}
	if path != "" {
		for i, e := range entries {
			if e.filePath == path {
				o.navStack[len(o.navStack)-1].cursor = i
				break
			}
		}
	}
	o.albumEntries = o.currentLevelEntries()
	o.albums = labelsOf(o.albumEntries)
	o.albumCursor = clampCursor(o.navStack[len(o.navStack)-1].cursor, len(o.albumEntries))
	o.albumsScroll = 0
	o.syncPanels()
}

func (o *Overlay) favoriteStation(path string) (radio.Station, bool) {
	if o.radioStationLookup == nil {
		return radio.Station{}, false
	}
	station, ok := o.radioStationLookup(path)
	return station, ok && strings.TrimSpace(station.Name) != "" && station.StreamURL() != ""
}

func (o *Overlay) favoriteTrackEntry(path string) (navEntry, bool) {
	if player.IsRadio(path) {
		station, ok := o.favoriteStation(path)
		if !ok {
			return navEntry{}, false
		}
		return navEntry{label: station.DisplayName(), kind: entryFavoriteTrack, filePath: path}, true
	}
	return navEntry{label: player.FavoriteTrackTitle(path), kind: entryFavoriteTrack, filePath: path}, true
}

func (o *Overlay) requestFavoriteStationMetadata(paths []string) {
	if o.radioStationResolveRequest == nil {
		return
	}
	stations := make([]string, 0, len(paths))
	for _, path := range paths {
		if !player.IsRadio(path) {
			continue
		}
		if _, ok := o.favoriteStation(path); !ok {
			stations = append(stations, path)
		}
	}
	if len(stations) > 0 {
		o.radioStationResolveRequest(stations)
	}
}

func (o *Overlay) favoriteTrackEntries(paths []string, skipMissingLocal bool) []navEntry {
	o.requestFavoriteStationMetadata(paths)
	entries := make([]navEntry, 0, len(paths))
	for _, path := range paths {
		if skipMissingLocal && player.IsLocalPath(path) {
			if _, err := os.Stat(path); err != nil {
				continue
			}
		}
		if entry, ok := o.favoriteTrackEntry(path); ok {
			entries = append(entries, entry)
		}
	}
	return o.sortNavEntries(entries)
}

// ShowMicrophoneDevices enters the capture-device selection list.
func (o *Overlay) ShowMicrophoneDevices(devices []string) {
	entries := make([]navEntry, 0, len(devices)+1)
	if o.micActive {
		entries = append(entries, navEntry{label: o.catalog.Text(i18n.SourceStopCapture), kind: entryMicrophoneStop, albumIdx: -1})
	}
	for _, device := range devices {
		entries = append(entries, navEntry{label: device, kind: entryMicrophoneDevice, device: device, albumIdx: -1})
	}
	if len(entries) == 0 {
		entries = append(entries, navEntry{label: o.catalog.Text(i18n.SourceNoInputDevices), kind: entryInfo, albumIdx: -1})
	}
	o.source = sourceMicrophone
	o.refreshSourceRoot()
	root := o.navStack[0]
	o.navStack = []navLevel{root}
	o.albumCursor = root.cursor
	o.albumsScroll = root.scroll
	o.pushLevel(navLevel{ctx: ctxMicrophone, label: o.catalog.Text(i18n.SourceMicrophone), entries: entries})
	o.focusPanel = 0
}

// buildFormatEntries lists distinct modland formats.
func (o *Overlay) buildFormatEntries() []navEntry {
	seen := map[string]bool{}
	var formats []string
	for _, a := range o.currentAlbums() {
		if !strings.HasPrefix(a.Path, player.ModlandPrefix) {
			continue
		}
		if !o.online && o.hasCachedUnder != nil && !o.hasCachedUnder(a.Path) {
			continue
		}
		format, _ := splitModlandName(a.Name)
		if !seen[format] {
			seen[format] = true
			formats = append(formats, format)
		}
	}
	sort.Strings(formats)
	slog.Debug("buildFormatEntries", "allAlbums", len(o.currentAlbums()), "modlandFormats", len(formats))
	entries := make([]navEntry, len(formats))
	for i, f := range formats {
		entries[i] = navEntry{label: f + "/", kind: entryFormat, format: f, albumIdx: -1}
	}
	return o.sortNavEntries(entries)
}

// buildAlbumsInFormatEntries lists modland albums within a single format.
func (o *Overlay) buildAlbumsInFormatEntries(format string) []navEntry {
	var entries []navEntry
	for i, a := range o.currentAlbums() {
		if !strings.HasPrefix(a.Path, player.ModlandPrefix) {
			continue
		}
		if !o.online && o.hasCachedUnder != nil && !o.hasCachedUnder(a.Path) {
			continue
		}
		f, author := splitModlandName(a.Name)
		if f != format {
			continue
		}
		label := author
		if label == "" {
			label = f
		}
		entries = append(entries, navEntry{label: label, kind: entryModlandAlbum, albumIdx: i})
	}
	return o.sortNavEntries(entries)
}

func (o *Overlay) buildCatalogTrackEntries(albumIdx int) []navEntry {
	all := o.currentAlbums()
	if albumIdx < 0 || albumIdx >= len(all) {
		return nil
	}
	tracks := all[albumIdx].Tracks
	entries := make([]navEntry, 0, len(tracks))
	for i, track := range tracks {
		if !o.online && o.isTrackCached != nil && !o.isTrackCached(track) {
			continue
		}
		label := player.TrackTitle(track)
		entries = append(entries, navEntry{
			label:    label,
			kind:     entryCatalogTrack,
			albumIdx: albumIdx,
			trackIdx: i,
			filePath: track,
		})
	}
	return o.sortNavEntries(entries)
}

// buildModArchiveEntries returns directory items from the overlay cache or
// the pre-crawled local index. A missing or empty index is refreshed on demand.
func (o *Overlay) buildModArchiveEntries(targetURL string) []navEntry {
	items, ok := o.modArchiveItems[targetURL]
	if !ok {
		items, ok = modarchive.FetchDirectoryCached(o.baseDir, targetURL)
		if !ok {
			if !o.online {
				return nil
			}
			o.beginModArchiveDirectory(targetURL)
			return []navEntry{{label: o.catalog.Text(i18n.ValueLoading), kind: entryInfo}}
		}
		o.modArchiveItems[targetURL] = items
	}
	return o.buildModArchiveEntriesFromItems(targetURL, items)
}

func (o *Overlay) buildModArchiveEntriesFromItems(targetURL string, items []modarchive.DirItem) []navEntry {
	if len(items) == 0 {
		return []navEntry{}
	}

	hasDirs := false
	hasFiles := false
	for _, item := range items {
		if item.Kind == modarchive.KindDir || item.Kind == modarchive.KindArchive {
			hasDirs = true
		} else if item.Kind == modarchive.KindFile {
			hasFiles = true
		}
	}

	if hasDirs {
		var entries []navEntry
		for _, item := range items {
			if item.Kind == modarchive.KindDir || item.Kind == modarchive.KindArchive {
				if !o.online && o.hasCachedUnder != nil && !o.hasCachedUnder(player.ModArchivePrefix+item.URL) {
					continue
				}
				entries = append(entries, navEntry{
					label:    item.CleanName + "/",
					kind:     entryModArchiveDir,
					url:      item.URL,
					albumIdx: -1,
				})
			}
		}
		return o.sortNavEntries(entries)
	}

	if hasFiles {
		albumPath := player.ModArchivePrefix + targetURL
		albumIdx := -1
		// Search in the library albums (which now includes catalog albums).
		for i, a := range o.currentAlbums() {
			if a.Path == albumPath {
				albumIdx = i
				break
			}
		}

		if albumIdx < 0 && o.addCatalogAlbum != nil {
			if album := modarchive.BuildAlbum(targetURL, items); album != nil {
				albumIdx = o.addCatalogAlbum(*album)
			}
		}

		if albumIdx >= 0 {
			return o.buildCatalogTrackEntries(albumIdx)
		}
	}

	return nil
}

// currentLevelEntries returns the rows for the currently displayed level.
func (o *Overlay) currentLevelEntries() []navEntry {
	return o.navStack[len(o.navStack)-1].entries
}

func (o *Overlay) currentEntry() *navEntry {
	if o.albumCursor < 0 || o.albumCursor >= len(o.albumEntries) {
		return nil
	}
	return &o.albumEntries[o.albumCursor]
}

// refreshAlbumLabels recomputes albumEntries/albums from the current level.
func (o *Overlay) refreshAlbumLabels() {
	if len(o.navStack) == 0 {
		o.albumEntries = nil
		o.albums = nil
		o.albumCursor = 0
		o.albumsScroll = 0
		return
	}
	o.albumEntries = o.currentLevelEntries()
	o.albums = o.displayNavEntryLabels(o.albumEntries)
}

func (o *Overlay) displayNavEntryLabels(entries []navEntry) []string {
	labels := make([]string, len(entries))
	for i, entry := range entries {
		labels[i] = entry.label
		if entry.kind == entryRadioFilter {
			labels[i] = o.radioFilterLabel(entry.radioKind, entry.radioFilter)
		}
	}
	for i, entry := range entries {
		if entry.kind != entryRadioFilter {
			continue
		}
		if count := o.radioValueCounts[entry.radioKind][entry.radioFilter]; count > 0 {
			labels[i] = fmt.Sprintf("%s (%d)", labels[i], count)
		}
	}
	return labels
}

// syncPanels refreshes the visible navigation panels.
func (o *Overlay) syncPanels() {
	o.pointerScroll = [2]bool{}
	o.refreshAlbumLabels()
	if o.isNC() {
		o.refreshNCPreview()
		o.albumsDirty = true
		o.albumsContentDirty = true
		o.tracksDirty = true
		o.tracksContentDirty = true
		return
	}
	o.albumsDirty = true
	o.albumsContentDirty = true
	o.tracksDirty = true
	o.tracksContentDirty = true
}

func (o *Overlay) isCatalog() bool {
	return len(o.navStack) > 0 && o.topLevel().ctx == ctxCatalog
}

// pushLevel drills into a non-leaf entry, saving cursor/scroll for popLevel.
// The stack bottom is a root level and is never popped by popLevel.
func (o *Overlay) pushLevel(lvl navLevel) {
	top := &o.navStack[len(o.navStack)-1]
	top.cursor, top.scroll = o.albumCursor, o.albumsScroll
	lvl.entries = withParentEntry(lvl.entries)
	o.navStack = append(o.navStack, lvl)
	o.albumCursor = lvl.cursor
	o.albumsScroll = lvl.scroll
	o.trackCursor = 0
	o.marqueeL.invalidate(o)
	o.syncPanels()
}

func withParentEntry(entries []navEntry) []navEntry {
	return append([]navEntry{{label: "..", kind: entryParent, albumIdx: -1}}, entries...)
}

// popLevel goes up one navigation level. Returns false when already at a
// root level (the stack bottom). Back then applies the root-level action.
func (o *Overlay) popLevel() bool {
	if len(o.navStack) <= 1 {
		return false
	}
	o.navStack = o.navStack[:len(o.navStack)-1]
	top := o.navStack[len(o.navStack)-1]
	o.albumCursor = clampCursor(top.cursor, len(top.entries))
	o.albumsScroll = min(max(0, top.scroll), o.albumCursor)
	o.trackCursor = 0
	o.marqueeL.invalidate(o)
	o.syncPanels()
	return true
}

// jumpToNavStackDepth restores an existing library navigation level for a
// breadcrumb click. The root depth is one; the visible level keeps its saved
// cursor/scroll, while pointer navigation always returns focus to the left
// panel.
func (o *Overlay) jumpToNavStackDepth(depth int) {
	if depth <= 1 {
		o.switchToSourceRoot()
		return
	}
	if depth > len(o.navStack) {
		depth = len(o.navStack)
	}
	o.navStack = o.navStack[:depth]
	top := o.topLevel()
	o.albumCursor = clampCursor(top.cursor, len(top.entries))
	o.albumsScroll = min(max(0, top.scroll), o.albumCursor)
	o.trackCursor = 0
	o.focusPanel = 0
	o.ncRight = ncRightInfo
	o.marqueeL.invalidate(o)
	o.syncPanels()
}

// AlbumCursor returns the real allAlbums index of the current leaf album
// or catalog track, or -1 when on a non-leaf row.
func (o *Overlay) AlbumCursor() int {
	e := o.currentEntry()
	if e.IsLeafAlbum() || e.IsCatalogTrack() {
		return e.albumIdx
	}
	return -1
}

// SelectedCatalogInfo returns the album name, the selected track path, the
// album's full track list, and the selected track index for the current
// catalog track. Returns zero values when the current entry is not a catalog
// track. One lookup serves both playback (path) and playlist setup (tracks,
// idx) — see CatalogAlbumTracks/SelectedCatalogTrack, merged here.
func (o *Overlay) SelectedCatalogInfo() (albumName, path string, tracks []string, idx int) {
	e := o.currentEntry()
	all := o.currentAlbums()
	if !e.IsCatalogTrack() || e.albumIdx < 0 || e.albumIdx >= len(all) {
		return "", "", nil, -1
	}
	album := all[e.albumIdx]
	if e.trackIdx < 0 || e.trackIdx >= len(album.Tracks) {
		return "", "", nil, -1
	}
	path = album.Tracks[e.trackIdx]
	for _, entry := range o.albumEntries {
		if !entry.IsCatalogTrack() || entry.albumIdx != e.albumIdx || entry.trackIdx < 0 || entry.trackIdx >= len(album.Tracks) {
			continue
		}
		if entry.trackIdx == e.trackIdx {
			idx = len(tracks)
		}
		tracks = append(tracks, album.Tracks[entry.trackIdx])
	}
	if len(tracks) == 0 {
		return album.Name, path, album.Tracks, e.trackIdx
	}
	return album.Name, path, tracks, idx
}

// --- NC-local filesystem navigation ---

// buildNCDirectoryEntries lists entries in a local directory for the NC panel.
// Shows: ".." (unless at baseDir), directories, supported audio files.
// Hides: dotfiles, .gsa_meta.json, artwork, symlinks, empty dirs.
func (o *Overlay) buildNCDirectoryEntries(dirPath string) []navEntry {
	var entries []navEntry
	var dirEntries []filesystem.Entry
	o.ncListingStatus = filesystem.StatusOK
	report := filesystem.Walk(context.Background(), dirPath, filesystem.Options{
		Descend: func(filesystem.Entry) bool { return false },
	}, func(entry filesystem.Entry) {
		dirEntries = append(dirEntries, entry)
	})
	o.ncListingStatus = report.Status
	if report.Status == filesystem.StatusFailed {
		return entries
	}

	var dirs, files []navEntry
	for _, entry := range dirEntries {
		name := entry.Name
		if strings.HasPrefix(name, ".") || name == ".gsa_meta.json" {
			continue
		}
		if entry.IsDir() {
			fullPath := filepath.Join(dirPath, name)
			if entry.IsSymlink() {
				continue // v1: skip all symlinks
			}
			// Skip empty directories (no supported audio + no subdirs).
			empty, status := ncDirIsEmpty(fullPath)
			if status != filesystem.StatusOK {
				if o.ncListingStatus == filesystem.StatusOK {
					o.ncListingStatus = filesystem.StatusPartial
				}
			}
			if empty && status == filesystem.StatusOK {
				continue
			}
			dirs = append(dirs, navEntry{
				label:    name + "/",
				kind:     entryNCDir,
				dirPath:  fullPath,
				albumIdx: -1,
			})
			continue
		}
		if !entry.IsRegular() {
			continue
		}
		if !filesystem.IsAudioFile(entry) && !radio.IsPlaylistPath(name) {
			continue
		}
		files = append(files, navEntry{
			label:    name,
			kind:     entryNCFile,
			filePath: filepath.Join(dirPath, name),
			albumIdx: -1,
		})
	}

	if o.sortOrder == config.SortSource {
		sort.Slice(dirs, func(i, j int) bool { return dirs[i].label < dirs[j].label })
		sort.Slice(files, func(i, j int) bool { return files[i].label < files[j].label })
	} else {
		sort.SliceStable(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].label) < strings.ToLower(dirs[j].label) })
		sort.SliceStable(files, func(i, j int) bool { return strings.ToLower(files[i].label) < strings.ToLower(files[j].label) })
	}
	if o.sortOrder == config.SortZA {
		slices.Reverse(dirs)
		slices.Reverse(files)
	}
	entries = append(entries, dirs...)
	entries = append(entries, files...)

	return entries
}

func (o *Overlay) ncDirectoryCounts(dirPath string) (files, dirs int) {
	for _, entry := range o.buildNCDirectoryEntries(dirPath) {
		switch entry.kind {
		case entryNCFile:
			files++
		case entryNCDir:
			dirs++
		}
	}
	return files, dirs
}

// ncDirIsEmpty reports whether a directory has no supported audio files and
// no non-hidden subdirectories. Used to prune empty leaf dirs from the tree.
func ncDirIsEmpty(dirPath string) (bool, filesystem.Status) {
	var entries []filesystem.Entry
	report := filesystem.Walk(context.Background(), dirPath, filesystem.Options{
		Descend: func(filesystem.Entry) bool { return false },
	}, func(entry filesystem.Entry) {
		entries = append(entries, entry)
	})
	if report.Status != filesystem.StatusOK {
		return false, report.Status
	}
	for _, entry := range entries {
		name := entry.Name
		if strings.HasPrefix(name, ".") {
			continue
		}
		if entry.IsSymlink() {
			continue
		}
		if entry.IsDir() {
			return false, filesystem.StatusOK
		}
		if filesystem.IsAudioFile(entry) {
			return false, filesystem.StatusOK
		}
	}
	return true, filesystem.StatusOK
}

// topLevel returns the visible level — the top of the stack. The stack is
// never empty: its bottom is always the virtual source root.
func (o *Overlay) topLevel() *navLevel {
	return &o.navStack[len(o.navStack)-1]
}

// isNC reports whether the top of the stack is an NC filesystem level.
func (o *Overlay) isNC() bool {
	return o.topLevel().ctx == ctxNC
}

// ncDir returns the directory of the top NC level, or "" when not in NC.
func (o *Overlay) ncDir() string {
	if !o.isNC() {
		return ""
	}
	return o.topLevel().dirPath
}

func (o *Overlay) NCListingStatus() filesystem.Status {
	if !o.isNC() {
		return filesystem.StatusOK
	}
	return o.ncListingStatus
}

// breadcrumbParts returns the breadcrumb path components for the current
// navigation position. NC levels are derived from the current directory
// (which fully describes the position, even after a deep-link that collapses
// the stack); library/catalog levels use their stored label; presets page
// shows the tree path.
func (o *Overlay) breadcrumbParts() []string {
	if o.uiPage == PagePresets {
		parts := []string{"/"}
		for i, level := range o.presetNav.stack {
			if i > 0 && level.label != "" {
				parts = append(parts, level.label)
			}
		}
		return parts
	}
	if o.topLevel().ctx == ctxSourceRoot {
		return []string{"/"}
	}
	parts := []string{"/"}
	if o.source == sourceMusic {
		parts = append(parts, o.catalog.Text(i18n.SourceLocalMusic))
	}
	if o.isNC() {
		if dir := o.ncDir(); dir != "" {
			musicRoot := filepath.Join(o.baseDir, "music")
			parts = append(parts, relParts(musicRoot, dir)...)
		}
	} else {
		for i, lvl := range o.navStack {
			if i > 0 && lvl.label != "" && lvl.label != ".." {
				parts = append(parts, lvl.label)
			}
		}
		if o.focusPanel == 1 {
			if e := o.currentEntry(); e != nil && e.IsLeafAlbum() {
				parts = append(parts, e.label)
			}
		}
	}
	return parts
}

// breadcrumbText renders the breadcrumb path. Width truncation is handled by
// rebuildBreadcrumbTex after the complete path has been assembled.
func (o *Overlay) breadcrumbText() string {
	parts := o.breadcrumbParts()
	if len(parts) > 0 && parts[0] == "/" {
		if len(parts) == 1 {
			return "/"
		}
		return "/" + strings.Join(parts[1:], "/")
	}
	return strings.Join(parts, "/")
}

type modArchiveNavigationTarget struct {
	url   string
	label string
}

// modArchiveNavigationTargets returns the directory levels needed to restore
// an album. The displayed path and parent navigation then match the hierarchy
// the user would traverse manually.
func modArchiveNavigationTargets(albumURL string) []modArchiveNavigationTarget {
	parsed, err := url.Parse(albumURL)
	if err != nil {
		return nil
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return nil
	}

	targets := make([]modArchiveNavigationTarget, 0, len(parts))
	for index, part := range parts {
		targetURL := modarchive.BaseURL + strings.Join(parts[:index+1], "/")
		label := part
		if index == 0 {
			label = modarchive.FormatDirName(part)
		}
		if index < len(parts)-1 || strings.HasSuffix(parsed.Path, "/") {
			targetURL += "/"
		} else if filepath.Ext(part) != "" {
			label = strings.TrimSuffix(part, filepath.Ext(part))
		}
		targets = append(targets, modArchiveNavigationTarget{url: targetURL, label: label})
	}
	return targets
}

// switchToNC replaces the source stack with a Music source and an NC level
// at dirPath (musicDir, falling back to baseDir, when empty). Used for source
// selection and local deep-links.
func (o *Overlay) switchToNC(dirPath string) {
	if dirPath == "" {
		dirPath = o.musicDir
	}
	if dirPath == "" {
		dirPath = o.baseDir
	}
	lvl := navLevel{ctx: ctxNC, dirPath: dirPath, entries: withParentEntry(o.buildNCDirectoryEntries(dirPath))}
	o.source = sourceMusic
	o.navStack = []navLevel{{ctx: ctxSourceRoot, entries: o.buildSourceEntries()}, lvl}
	o.albumEntries = lvl.entries
	o.albums = labelsOf(lvl.entries)
	o.albumCursor = 0
	o.albumsScroll = 0
	o.focusPanel = 0
	o.ncRight = ncRightInfo
	o.marqueeL.invalidate(o)
	o.syncPanels()
}

func (o *Overlay) switchToSourceRoot() {
	o.navStack = []navLevel{{ctx: ctxSourceRoot, entries: o.buildSourceEntries()}}
	o.source = sourceMusic
	o.albumEntries = o.navStack[0].entries
	o.albums = labelsOf(o.albumEntries)
	o.albumCursor = 0
	o.albumsScroll = 0
	o.focusPanel = 0
	o.marqueeL.invalidate(o)
	o.syncPanels()
}

func (o *Overlay) switchToProvider(source sourceKind) {
	slog.Debug("switchToProvider", "source", source, "allAlbums", len(o.currentAlbums()), "online", o.online)
	o.switchToSourceRoot()
	o.source = source
	for i, e := range o.albumEntries {
		if e.kind == entrySource && e.source == source {
			o.albumCursor = i
			break
		}
	}
	switch source {
	case sourceFavorites:
		o.switchToFavoritesRoot()
	case sourceDownloads:
		o.switchToDownloads()
	case sourceRadio:
		o.pushLevel(navLevel{ctx: ctxRadio, label: o.catalog.Text(i18n.SourceRadio), entries: o.buildRadioCategoryEntries()})
	case sourceModland:
		entries := o.buildFormatEntries()
		slog.Debug("switchToProvider modland", "formats", len(entries))
		o.pushLevel(navLevel{ctx: ctxCatalog, label: "Modland", entries: entries})
	case sourceModArchive:
		o.pushLevel(navLevel{ctx: ctxCatalog, dirPath: modarchive.BaseURL, label: "ModArchive", entries: o.buildModArchiveEntries(modarchive.BaseURL)})
	}
}

func (o *Overlay) buildRadioCategoryEntries() []navEntry {
	return []navEntry{
		{label: o.catalog.Text(i18n.RadioPopular), kind: entryRadioCategory, radioKind: radio.BrowsePopular},
		{label: o.catalog.Text(i18n.RadioRandom), kind: entryRadioCategory, radioKind: radio.BrowseRandom},
		{label: o.catalog.Text(i18n.RadioHistory), kind: entryRadioCategory, radioKind: radio.BrowseHistory},
		{label: o.catalog.Text(i18n.RadioByTag), kind: entryRadioCategory, radioKind: radio.BrowseTag},
		{label: o.catalog.Text(i18n.RadioByCountry), kind: entryRadioCategory, radioKind: radio.BrowseCountry},
	}
}

func (o *Overlay) buildRadioFilterEntries(kind radio.BrowseKind) []navEntry {
	values, loaded := o.radioValues[kind]
	counts := o.radioValueCounts[kind]
	entries := make([]navEntry, 0, len(values)+1)
	for _, value := range values {
		label := o.radioFilterLabel(kind, value)
		if count := counts[value]; count > 0 {
			label = fmt.Sprintf("%s (%d)", label, count)
		}
		entries = append(entries, navEntry{label: label, kind: entryRadioFilter, radioKind: kind, radioFilter: value})
	}
	if len(entries) == 0 {
		key := i18n.ValueLoading
		if loaded {
			key = i18n.RadioEmpty
		}
		entries = append(entries, navEntry{label: o.catalog.Text(key), kind: entryInfo})
	}
	return o.sortNavEntries(entries)
}

func (o *Overlay) radioFilterLabel(kind radio.BrowseKind, value string) string {
	if kind == radio.BrowseCountry {
		if label := o.catalog.DisplayRegionName(value); label != "" {
			return label
		}
		if label := o.catalog.DisplayRegionNameFromEnglish(value); label != "" {
			return label
		}
	}
	return value
}

func (o *Overlay) buildRadioStationEntries(kind radio.BrowseKind, filter string) []navEntry {
	stations, loaded := o.radioQueries[radioQueryKey(kind, filter)]
	entries := make([]navEntry, 0, len(stations)+1)
	for _, station := range stations {
		entries = append(entries, navEntry{label: station.DisplayName(), kind: entryRadioStation, radioKind: kind, radioFilter: filter, radioStation: station})
	}
	if len(entries) == 0 {
		key := i18n.ValueLoading
		if loaded {
			key = i18n.RadioEmpty
		}
		entries = append(entries, navEntry{label: o.catalog.Text(key), kind: entryInfo})
	}
	if kind != radio.BrowseHistory {
		entries = o.sortNavEntries(entries)
	}
	if kind == radio.BrowseHistory && len(stations) > 0 {
		clear := navEntry{label: o.catalog.Text(i18n.RadioClearHistory), kind: entryRadioHistoryClear}
		entries = append([]navEntry{clear}, entries...)
	}
	return entries
}

func radioQueryKey(kind radio.BrowseKind, filter string) string {
	return string(kind) + "\x00" + strings.ToLower(strings.TrimSpace(filter))
}

func (o *Overlay) switchToDownloads() {
	if o.cachedTracks == nil || o.addCatalogAlbum == nil {
		return
	}
	tracks := o.cachedTracks()
	albumName := o.catalog.Text(i18n.SourceDownloads)
	albumIdx := o.addCatalogAlbum(player.Album{Name: albumName, Path: player.DownloadsPrefix, Tracks: tracks})
	o.pushLevel(navLevel{ctx: ctxCatalog, label: albumName, entries: o.buildCatalogTrackEntries(albumIdx)})
}

// switchToFavoritesRoot opens the Favorites folder showing non-empty playlists.
func (o *Overlay) switchToFavoritesRoot() {
	if o.favoritesView == nil {
		return
	}
	entries := make([]navEntry, 0, 3)
	for _, spec := range player.PlaylistSpecs() {
		count := o.favoritesView.Count(spec.ID)
		if count == 0 {
			continue
		}
		entries = append(entries, navEntry{
			label:  fmt.Sprintf("%s (%d)", spec.Label, count),
			kind:   entryFavoriteFolder,
			source: sourceFavorites,
			format: string(spec.ID),
		})
	}
	o.pushLevel(navLevel{ctx: ctxFavorites, entries: entries, label: o.catalog.Text(i18n.SourceFavorites)})
}

// switchToFavoritesPlaylist opens a specific playlist showing its tracks.
func (o *Overlay) switchToFavoritesPlaylist(kind player.PlaylistID) {
	if o.favoritesView == nil {
		return
	}
	tracks := o.favoritesView.Tracks(kind)
	entries := o.favoriteTrackEntries(tracks, true)
	label := player.PlaylistLabel(kind)
	if label == "" {
		label = string(kind)
	}
	o.pushLevel(navLevel{
		ctx:        ctxFavorites,
		entries:    entries,
		label:      label,
		playlistID: string(kind),
	})
}

func (o *Overlay) sortNavEntries(entries []navEntry) []navEntry {
	if o.sortOrder == config.SortSource || len(entries) < 2 {
		return entries
	}
	sort.SliceStable(entries, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSuffix(entries[i].label, "/"))
		right := strings.ToLower(strings.TrimSuffix(entries[j].label, "/"))
		if o.sortOrder == config.SortZA {
			return left > right
		}
		return left < right
	})
	return entries
}

// ncEnterDir enters a subdirectory in NC mode.
func (o *Overlay) ncEnterDir(dirPath string) {
	o.pushLevel(navLevel{ctx: ctxNC, dirPath: dirPath, entries: o.buildNCDirectoryEntries(dirPath)})
}

// refreshNCPreview updates the right-panel NC info for the selected entry.
func (o *Overlay) refreshNCPreview() {
	e := o.currentEntry()
	o.ncInfoFile = ""
	o.ncInfoDir = ""
	o.ncInfoIsDir = false
	o.ncInfoScroll = 0
	if e == nil {
		o.tracksDirty = true
		o.tracksContentDirty = true
		return
	}
	if e.IsNCDirectory() {
		o.ncInfoDir = e.dirPath
		o.ncInfoIsDir = true
	} else if e.IsNCFile() {
		o.ncInfoFile = e.filePath
		o.ncInfoIsDir = false
	}
	o.tracksDirty = true
	o.tracksContentDirty = true
}

// NavigateToTrack opens the Library at the location of the given track path.
// For local files: enters the parent directory in NC mode and highlights the
// file. For catalog tracks (modland/modarchive): walks the provider tree to
// the album and highlights the track. Right-panel info is shown for catalog
// tracks; NC info panel is shown for local files.
func (o *Overlay) NavigateToTrack(path string) {
	if path == "" {
		return
	}
	if player.IsRadio(path) {
		// Radio stations need their full Station metadata, so the app should use
		// NavigateToRadioStation. Never interpret a virtual radio path as local
		// filesystem input.
		return
	}
	if player.IsModland(path) || player.IsModArchive(path) {
		o.navigateToCatalogTrack(path)
		return
	}
	o.navigateToLocalTrack(path)
}

// NavigateToRadioStation opens the Library on the currently playing station.
// It creates a small station view even when the station came from a playlist
// and is not present in a cached Radio Browser query.
func (o *Overlay) NavigateToRadioStation(station radio.Station) {
	if strings.TrimSpace(station.StationUUID) == "" {
		return
	}
	// Prefer the station listing that is already open. This keeps the user's
	// browsing context and moves its cursor to the station selected by
	// automatic recovery or normal auto-advance.
	if len(o.navStack) > 0 && o.topLevel().ctx == ctxRadio {
		for i, entry := range o.topLevel().entries {
			if entry.kind == entryRadioStation && entry.radioStation.Path() == station.Path() {
				o.albumCursor = i
				o.albumsScroll = 0
				o.focusPanel = 0
				o.syncPanels()
				return
			}
		}
	}
	o.switchToProvider(sourceRadio)
	o.pushLevel(navLevel{
		ctx:       ctxRadio,
		label:     station.DisplayName(),
		radioKind: radio.BrowseKind("now-playing"),
		entries:   []navEntry{{label: station.DisplayName(), kind: entryRadioStation, radioStation: station}},
	})
	o.albumCursor = min(1, len(o.albumEntries)-1)
	o.albumsScroll = 0
	o.focusPanel = 0
	o.syncPanels()
}

// NavigateToRadioStationInBrowse restores the category/filter path used to
// select a station. Cached rows are shown immediately; the app may refresh a
// stale query in the background through the normal browse request path.
func (o *Overlay) NavigateToRadioStationInBrowse(station radio.Station, kind radio.BrowseKind, filter string) {
	if strings.TrimSpace(station.StationUUID) == "" || !validRadioBrowseContext(kind, filter) {
		o.NavigateToRadioStation(station)
		return
	}
	o.radioBrowseKind, o.radioBrowseFilter = kind, filter
	o.radioBrowseRequested = true
	if len(o.navStack) > 0 && o.topLevel().ctx == ctxRadio && o.topLevel().radioKind == kind && o.topLevel().radioFilter == filter {
		if o.selectRadioStationCursor(station) {
			return
		}
	}

	o.switchToProvider(sourceRadio)
	categoryLabel := o.radioCategoryLabel(kind)
	if kind == radio.BrowsePopular {
		o.pushLevel(navLevel{
			ctx:         ctxRadio,
			label:       categoryLabel,
			entries:     o.radioEntriesWithStation(kind, filter, station),
			radioKind:   kind,
			radioFilter: filter,
		})
		o.selectRadioStationCursor(station)
		return
	}

	filterEntries := o.buildRadioFilterEntries(kind)
	filterIndex := -1
	for i, entry := range filterEntries {
		if entry.kind == entryRadioFilter && entry.radioFilter == filter {
			filterIndex = i
			break
		}
	}
	if filterIndex < 0 {
		filterEntries = append(filterEntries, navEntry{label: filter, kind: entryRadioFilter, radioKind: kind, radioFilter: filter})
		filterIndex = len(filterEntries) - 1
	}
	o.pushLevel(navLevel{
		ctx:       ctxRadio,
		label:     categoryLabel,
		entries:   filterEntries,
		radioKind: kind,
		cursor:    filterIndex + 1, // account for the parent entry
	})
	o.pushLevel(navLevel{
		ctx:         ctxRadio,
		label:       filter,
		entries:     o.radioEntriesWithStation(kind, filter, station),
		radioKind:   kind,
		radioFilter: filter,
	})
	o.selectRadioStationCursor(station)
}

func validRadioBrowseContext(kind radio.BrowseKind, filter string) bool {
	switch kind {
	case radio.BrowsePopular:
		return filter == ""
	case radio.BrowseTag, radio.BrowseCountry:
		return filter != ""
	default:
		return false
	}
}

func (o *Overlay) radioCategoryLabel(kind radio.BrowseKind) string {
	for _, entry := range o.buildRadioCategoryEntries() {
		if entry.radioKind == kind {
			return entry.label
		}
	}
	return string(kind)
}

func (o *Overlay) radioEntriesWithStation(kind radio.BrowseKind, filter string, station radio.Station) []navEntry {
	key := radioQueryKey(kind, filter)
	stations := o.radioQueries[key]
	if !slices.ContainsFunc(stations, func(candidate radio.Station) bool { return candidate.Path() == station.Path() }) {
		stations = append(append([]radio.Station(nil), stations...), station)
		o.radioQueries[key] = stations
	}
	return o.buildRadioStationEntries(kind, filter)
}

func (o *Overlay) selectRadioStationCursor(station radio.Station) bool {
	for index, entry := range o.topLevel().entries {
		if entry.kind == entryRadioStation && entry.radioStation.Path() == station.Path() {
			o.albumCursor = index
			o.albumsScroll = 0
			o.focusPanel = 0
			o.syncPanels()
			return true
		}
	}
	return false
}

// navigateToLocalTrack enters the parent directory of a local file and
// positions the cursor on the file. If the file is already visible in the
// current NC view, just moves the cursor.
func (o *Overlay) navigateToLocalTrack(path string) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return
	}
	dir := filepath.Dir(absPath)
	name := filepath.Base(absPath)

	// If already browsing this directory in NC, just move the cursor.
	if o.isNC() && o.ncDir() == dir {
		for i, e := range o.albumEntries {
			if e.IsNCFile() && e.filePath == absPath {
				o.albumCursor = i
				o.focusPanel = 0
				o.refreshNCPreview()
				o.albumsDirty = true
				return
			}
		}
	}

	// Enter the directory via NC.
	o.switchToNC(dir)

	// Find and highlight the file.
	for i, e := range o.albumEntries {
		if e.IsNCFile() && (e.filePath == absPath || filepath.Base(e.filePath) == name) {
			o.albumCursor = i
			o.focusPanel = 0
			o.refreshNCPreview()
			o.albumsDirty = true
			return
		}
	}
}

// navigateToCatalogTrack walks the provider tree to locate a catalog track
// and positions the cursor on it. The right panel shows track info.
// Navigation works as long as the album data exists in the library —
// regardless of the online flag (catalogs may be loaded from cache).
func (o *Overlay) navigateToCatalogTrack(path string) {
	if player.IsModland(path) {
		o.navigateToModlandTrack(path)
		return
	}
	if player.IsModArchive(path) {
		o.navigateToModArchiveTrack(path)
		return
	}
}

// navigateToModlandTrack builds: source root → modland → format → album → track.
func (o *Overlay) navigateToModlandTrack(path string) {
	remote := player.RemotePath(path)
	parts := strings.Split(remote, "/")
	if len(parts) < 3 {
		return
	}
	format := parts[0]
	albumName := modlandNamePrefix + parts[0] + "/" + parts[1]
	trackTitle := player.TrackTitle(path)

	// Find the album index in the library.
	all := o.currentAlbums()
	albumIdx := -1
	for i, a := range all {
		if a.Path == path[:len(path)-len(trackTitle)-1] || a.Name == albumName {
			albumIdx = i
			break
		}
	}
	if albumIdx < 0 {
		// Album not in library yet — try to find by prefix match.
		for i, a := range all {
			if strings.HasPrefix(a.Path, player.ModlandPrefix+format+"/") && strings.Contains(a.Name, parts[1]) {
				albumIdx = i
				break
			}
		}
	}
	if albumIdx < 0 {
		return
	}

	// Build the same parent selections as manual navigation.
	o.switchToProvider(sourceModland)
	for i, e := range o.albumEntries {
		if e.kind == entryFormat && e.format == format {
			o.albumCursor = i
			break
		}
	}

	// Level 2: albums within format.
	albumEntries := o.buildAlbumsInFormatEntries(format)
	o.pushLevel(navLevel{ctx: ctxCatalog, label: format, entries: albumEntries})

	// Find the cursor position for the album in the format list.
	for i, e := range o.albumEntries {
		if e.IsLeafAlbum() && e.albumIdx == albumIdx {
			o.albumCursor = i
			break
		}
	}

	// Level 3: tracks within album.
	trackEntries := o.buildCatalogTrackEntries(albumIdx)
	if len(trackEntries) == 0 {
		return
	}
	o.pushLevel(navLevel{ctx: ctxCatalog, label: parts[1], entries: trackEntries})

	// Find and highlight the track.
	for i, e := range o.albumEntries {
		if e.IsCatalogTrack() && e.label == trackTitle {
			o.albumCursor = i
			// Keep focus on the left panel — the right panel is a static
			// info view for catalog tracks and does not draw a cursor.
			o.albumsDirty = true
			o.tracksDirty = true
			o.tracksContentDirty = true
			return
		}
	}
	// Fallback: select the first track.
	if len(o.albumEntries) > 1 {
		o.albumCursor = 1
		o.albumsDirty = true
		o.tracksDirty = true
		o.tracksContentDirty = true
	}
}

// navigateToModArchiveTrack builds: source root → modarchive → directory tree → track.
// ModArchive tracks are identified by URL; we search the library for the
// matching album and navigate to its track list.
func (o *Overlay) navigateToModArchiveTrack(path string) {
	remote := player.RemotePath(path)
	trackTitle := player.TrackTitle(path)
	albumURL := modarchive.AlbumURL(remote)
	if albumURL == "" {
		return
	}

	// Album paths are directory URLs, while track paths append the archive file
	// name. Entering the directory also creates its lazy catalog album when the
	// track was restored before the user browsed ModArchive.
	all := o.currentAlbums()
	albumIdx := -1
	for i, a := range all {
		if strings.TrimRight(strings.TrimPrefix(a.Path, player.ModArchivePrefix), "/") == strings.TrimRight(albumURL, "/") {
			albumIdx = i
			break
		}
	}
	if albumIdx < 0 {
		o.buildModArchiveEntries(albumURL)
		all = o.currentAlbums()
		for i, a := range all {
			if strings.TrimRight(strings.TrimPrefix(a.Path, player.ModArchivePrefix), "/") == strings.TrimRight(albumURL, "/") {
				albumIdx = i
				break
			}
		}
	}
	if albumIdx < 0 {
		return
	}

	o.switchToProvider(sourceModArchive)

	// Restore each virtual snapshot directory so parent navigation mirrors the
	// tree the user would traverse manually.
	for _, target := range modArchiveNavigationTargets(albumURL) {
		dirEntries := o.buildModArchiveEntries(target.url)
		if dirEntries != nil {
			for i, e := range o.albumEntries {
				if e.kind == entryModArchiveDir && e.url == target.url {
					o.albumCursor = i
					break
				}
			}
			o.pushLevel(navLevel{ctx: ctxCatalog, dirPath: target.url, label: target.label, entries: dirEntries})
		}
	}

	// Find and highlight the track.
	for i, e := range o.albumEntries {
		if e.IsCatalogTrack() && e.label == trackTitle {
			o.albumCursor = i
			// Keep focus on the left panel — the right panel is a static
			// info view for catalog tracks and does not draw a cursor.
			o.albumsDirty = true
			o.tracksDirty = true
			o.tracksContentDirty = true
			return
		}
	}
	if len(o.albumEntries) > 1 {
		o.albumCursor = 1
		o.albumsDirty = true
		o.tracksDirty = true
		o.tracksContentDirty = true
	}
}

func clampCursor(cur, max int) int {
	if max == 0 {
		return 0
	}
	if cur < 0 {
		return 0
	}
	if cur >= max {
		return max - 1
	}
	return cur
}
