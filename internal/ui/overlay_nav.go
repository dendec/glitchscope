package ui

import (
	"context"
	"log/slog"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dendec/glitchscope/internal/filesystem"
	"github.com/dendec/glitchscope/internal/modarchive"
	"github.com/dendec/glitchscope/internal/player"
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
	entryMicrophoneDevice                     // SDL capture device entry
	entryMicrophoneStop                       // stops active capture
	entryInfo                                 // non-selectable informational row
)

// navEntry is one row in the library's left (navigation) panel.
type navEntry struct {
	label    string
	kind     navEntryKind
	source   sourceKind // set when kind == entrySource
	albumIdx int        // index into Overlay.allAlbums when kind is a leaf album, else -1
	format   string     // set when kind == entryFormat
	url      string     // set when kind == entryModArchiveDir
	dirPath  string     // set when kind == entryNCDir
	filePath string     // set when kind == entryNCFile
	trackIdx int        // set when kind == entryCatalogTrack
	device   string     // set when kind == entryMicrophoneDevice
}

// navLevel is one visible listing in the unified navigation stack.
// The stack bottom is the virtual source root; Back never pops it.
type navLevel struct {
	ctx     navCtx
	entries []navEntry
	cursor  int
	scroll  int
	dirPath string // ctxNC: the filesystem directory this level lists
	label   string // ctxCatalog: breadcrumb label for this level
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
		return "microphone [capturing]/"
	}
	return "microphone/"
}

func (o *Overlay) buildSourceEntries() []navEntry {
	entries := []navEntry{{label: "music/", kind: entrySource, source: sourceMusic, albumIdx: -1}}
	if len(o.micDevices) > 0 || o.micActive {
		entries = append(entries, navEntry{label: o.micLabel(), kind: entrySource, source: sourceMicrophone, albumIdx: -1})
	}
	slog.Debug("buildSourceEntries", "online", o.online)
	if o.online {
		entries = append(entries,
			navEntry{label: "modland/", kind: entrySource, source: sourceModland, albumIdx: -1},
			navEntry{label: "modarchive/", kind: entrySource, source: sourceModArchive, albumIdx: -1},
		)
	}
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

// ShowMicrophoneDevices enters the capture-device selection list.
func (o *Overlay) ShowMicrophoneDevices(devices []string) {
	entries := make([]navEntry, 0, len(devices)+1)
	if o.micActive {
		entries = append(entries, navEntry{label: "stop capture", kind: entryMicrophoneStop, albumIdx: -1})
	}
	for _, device := range devices {
		entries = append(entries, navEntry{label: device, kind: entryMicrophoneDevice, device: device, albumIdx: -1})
	}
	if len(entries) == 0 {
		entries = append(entries, navEntry{label: "no input devices", kind: entryInfo, albumIdx: -1})
	}
	o.source = sourceMicrophone
	o.refreshSourceRoot()
	root := o.navStack[0]
	o.navStack = []navLevel{root}
	o.albumCursor = root.cursor
	o.albumsScroll = root.scroll
	o.pushLevel(navLevel{ctx: ctxMicrophone, label: "microphone", entries: entries})
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
	return entries
}

// buildAlbumsInFormatEntries lists modland albums within a single format.
func (o *Overlay) buildAlbumsInFormatEntries(format string) []navEntry {
	var entries []navEntry
	for i, a := range o.currentAlbums() {
		if !strings.HasPrefix(a.Path, player.ModlandPrefix) {
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
	return entries
}

func (o *Overlay) buildCatalogTrackEntries(albumIdx int) []navEntry {
	all := o.currentAlbums()
	if albumIdx < 0 || albumIdx >= len(all) {
		return nil
	}
	tracks := all[albumIdx].Tracks
	entries := make([]navEntry, 0, len(tracks))
	for i, track := range tracks {
		entries = append(entries, navEntry{
			label:    player.TrackTitle(track),
			kind:     entryCatalogTrack,
			albumIdx: albumIdx,
			trackIdx: i,
		})
	}
	return entries
}

// buildModArchiveEntries returns directory items from the overlay cache or
// the pre-crawled local index. A missing or empty index is refreshed on demand.
func (o *Overlay) buildModArchiveEntries(targetURL string) []navEntry {
	items, ok := o.modArchiveItems[targetURL]
	if !ok {
		items, ok = modarchive.FetchDirectoryCached(o.baseDir, targetURL)
		if !ok {
			var err error
			items, err = modarchive.FetchDirectory(o.baseDir, targetURL)
			if err != nil {
				return nil
			}
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
		if item.Kind == modarchive.KindDir {
			hasDirs = true
		} else if item.Kind == modarchive.KindFile {
			hasFiles = true
		}
	}

	if hasDirs {
		var entries []navEntry
		for _, item := range items {
			if item.Kind == modarchive.KindDir {
				entries = append(entries, navEntry{
					label:    item.CleanName + "/",
					kind:     entryModArchiveDir,
					url:      item.URL,
					albumIdx: -1,
				})
			}
		}
		return entries
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
	o.albumEntries = o.currentLevelEntries()
	o.albums = labelsOf(o.albumEntries)
}

// syncPanels refreshes the visible navigation panels.
func (o *Overlay) syncPanels() {
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
	o.albumCursor, o.albumsScroll = top.cursor, top.scroll
	o.trackCursor = 0
	o.marqueeL.invalidate(o)
	o.syncPanels()
	return true
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
	return album.Name, album.Tracks[e.trackIdx], album.Tracks, e.trackIdx
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
		if !filesystem.IsAudioFile(entry) {
			continue
		}
		files = append(files, navEntry{
			label:    name,
			kind:     entryNCFile,
			filePath: filepath.Join(dirPath, name),
			albumIdx: -1,
		})
	}

	sort.Slice(dirs, func(i, j int) bool { return dirs[i].label < dirs[j].label })
	sort.Slice(files, func(i, j int) bool { return files[i].label < files[j].label })
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
		// Each stack level beyond root represents an expanded directory.
		// The directory name is at stack[i-1].nodes[stack[i-1].cursor].
		for i := 0; i+1 < len(o.presetNav.stack); i++ {
			parentLevel := &o.presetNav.stack[i]
			nodesIdx := parentLevel.cursor
			if nodesIdx >= 0 && nodesIdx < len(parentLevel.nodes) {
				parts = append(parts, parentLevel.nodes[nodesIdx].name)
			}
		}
		return parts
	}
	if o.topLevel().ctx == ctxSourceRoot {
		return []string{"/"}
	}
	parts := []string{"/"}
	if o.source == sourceMusic {
		parts = append(parts, "music")
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
	o.source = source
	o.navStack = []navLevel{{ctx: ctxSourceRoot, entries: o.buildSourceEntries()}}
	switch source {
	case sourceModland:
		entries := o.buildFormatEntries()
		slog.Debug("switchToProvider modland", "formats", len(entries))
		o.pushLevel(navLevel{ctx: ctxCatalog, label: "modland", entries: entries})
	case sourceModArchive:
		o.pushLevel(navLevel{ctx: ctxCatalog, label: "modarchive", entries: o.buildModArchiveEntries(modarchive.BaseURL)})
	}
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
	if player.IsModland(path) || player.IsModArchive(path) {
		o.navigateToCatalogTrack(path)
		return
	}
	o.navigateToLocalTrack(path)
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

	// Build navigation tree: source root → modland → format → album tracks.
	// Temporarily enable online so the source root shows the provider entry;
	// restore the original state afterward so offline users aren't left with
	// phantom remote entries after closing the UI.
	savedOnline := o.online
	o.online = true
	o.source = sourceModland
	o.navStack = []navLevel{{ctx: ctxSourceRoot, entries: o.buildSourceEntries()}}
	o.online = savedOnline

	// Level 1: modland formats.
	formatEntries := o.buildFormatEntries()
	o.pushLevel(navLevel{ctx: ctxCatalog, label: "modland", entries: formatEntries})

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
	trackURL := strings.TrimRight(remote, "/")
	lastSlash := strings.LastIndexByte(trackURL, '/')
	if lastSlash < 0 {
		return
	}
	albumURL := trackURL[:lastSlash+1]

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

	album := all[albumIdx]

	// Build navigation tree: source root → modarchive → directory.
	// Temporarily enable online so the source root shows the provider entry;
	// restore the original state afterward.
	savedOnline := o.online
	o.online = true
	o.source = sourceModArchive
	o.navStack = []navLevel{{ctx: ctxSourceRoot, entries: o.buildSourceEntries()}}
	o.online = savedOnline
	o.pushLevel(navLevel{ctx: ctxCatalog, label: "modarchive", entries: o.buildModArchiveEntries(modarchive.BaseURL)})

	// Try to enter the directory if we can resolve it from the URL.
	parsed, err := url.Parse(albumURL)
	if err == nil {
		dirURL := modarchive.BaseURL + strings.TrimPrefix(parsed.Path, "/")
		dirEntries := o.buildModArchiveEntries(dirURL)
		if dirEntries != nil {
			o.pushLevel(navLevel{ctx: ctxCatalog, label: filepath.Base(parsed.Path), entries: dirEntries})
		}
	}

	// If the album has tracks, build the track list.
	if len(album.Tracks) > 0 {
		trackEntries := o.buildCatalogTrackEntries(albumIdx)
		if len(trackEntries) > 0 {
			o.pushLevel(navLevel{ctx: ctxCatalog, label: album.Name, entries: trackEntries})
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
