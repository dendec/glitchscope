package ui

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dendec/pmv/internal/formats"
	"github.com/dendec/pmv/internal/modarchive"
	"github.com/dendec/pmv/internal/player"
)

// This file owns the library navigation model: local-album / modland / modarchive
// format/album drill-down tree. State mutation in overlay_input.go;
// struct fields in overlay.go.

const modlandNamePrefix = "Modland: "

// isVirtualAlbum reports provider-browsed albums (modland/modarchive) that
// live only in the overlay, not in the library scan.
func isVirtualAlbum(a player.Album) bool {
	return strings.HasPrefix(a.Path, player.ModlandPrefix) || strings.HasPrefix(a.Path, player.ModArchivePrefix)
}

// realAlbumsOnly filters virtual provider albums out of a list.
func realAlbumsOnly(list []player.Album) []player.Album {
	out := make([]player.Album, 0, len(list))
	for _, a := range list {
		if !isVirtualAlbum(a) {
			out = append(out, a)
		}
	}
	return out
}

// navEntryKind classifies a row in the library navigation panel.
type navEntryKind int

const (
	entryLocalAlbum      navEntryKind = iota // real album/folder — leaf, has tracks
	entryLocalDir                            // real folder — intermediate, drill down
	entryModlandRoot                         // "Modland" pseudo-folder at the library root
	entryFormat                              // modland format bucket (e.g. "Protracker")
	entryModlandAlbum                        // modland author/album within a format — leaf, has tracks
	entryModArchiveRoot                      // "ModArchive" pseudo-folder at the library root
	entryModArchiveDir                       // ModArchive HTTP directory folder
	entryModArchiveAlbum                     // ModArchive leaf album containing tracks
	entryNCDir                               // NC directory entry
	entryNCFile                              // NC file entry (leaf)
)

// navEntry is one row in the library's left (navigation) panel.
type navEntry struct {
	label    string
	kind     navEntryKind
	albumIdx int    // index into Overlay.allAlbums when kind is a leaf album, else -1
	format   string // set when kind == entryFormat
	url      string // set when kind == entryModArchiveDir
	dirPath  string // set when kind == entryLocalDir or entryNCDir
	filePath string // set when kind == entryNCFile
}

// navLevel is one visible listing in the unified navigation stack.
// The stack bottom is a root level (ctxNC at baseDir, or the implicit
// library root when the stack is empty); Back never pops a root.
type navLevel struct {
	ctx     navCtx
	entries []navEntry
	cursor  int
	scroll  int
	dirPath string // ctxNC: the filesystem directory this level lists
	label   string // ctxLibrary/ctxCatalog: breadcrumb label for this level
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

// FocusPlayingTrack points the cursor at the currently playing album/track,
// including deep-linking into modland/modarchive drill-down when needed.
func (o *Overlay) FocusPlayingTrack(albumIdx, trackIdx int) {
	if albumIdx >= 0 && albumIdx < len(o.allAlbums) {
		path := o.allAlbums[albumIdx].Path
		if !strings.HasPrefix(path, player.ModlandPrefix) && !strings.HasPrefix(path, player.ModArchivePrefix) {
			// Local track — NC filesystem view at the album's directory.
			albumPath := o.allAlbums[albumIdx].Path
			o.switchToNC(albumPath)
			// Find the track file in the entries.
			if trackIdx >= 0 && trackIdx < len(o.allAlbums[albumIdx].Tracks) {
				trackPath := o.allAlbums[albumIdx].Tracks[trackIdx]
				for i, e := range o.albumEntries {
					if e.IsNCFile() && e.filePath == trackPath {
						o.albumCursor = i
						break
					}
				}
			}
			o.trackCursor = trackIdx
			o.focusPanel = 0
			o.panelEntered = true
			o.albumsDirty = true
			o.albumsContentDirty = true
			o.tracksDirty = true
			o.tracksContentDirty = true
			return
		}
	}

	// Remote or unknown — library root with a drill-down to the album.
	o.switchToLibrary()
	o.trackCursor = trackIdx

	if albumIdx >= 0 && albumIdx < len(o.allAlbums) {
		path := o.allAlbums[albumIdx].Path
		if strings.HasPrefix(path, player.ModlandPrefix) {
			o.focusModlandAlbum(albumIdx)
		} else if strings.HasPrefix(path, player.ModArchivePrefix) {
			o.focusModArchiveAlbum(albumIdx)
		} else {
			o.focusLocalAlbum(albumIdx)
		}
	} else {
		// No playback — start NC mode at baseDir.
		o.switchToNC(o.baseDir)
	}

	if e := o.currentEntry(); e != nil && e.IsLeafAlbum() {
		o.focusPanel = 1
	} else {
		o.focusPanel = 0
	}
	o.panelEntered = true
	o.albumsDirty = true
	o.albumsContentDirty = true
	o.tracksDirty = true
	o.tracksContentDirty = true
}

func (e *navEntry) IsLeafAlbum() bool {
	if e == nil {
		return false
	}
	return e.kind == entryLocalAlbum || e.kind == entryModlandAlbum || e.kind == entryModArchiveAlbum
}

func (e *navEntry) IsNCDirectory() bool {
	return e != nil && e.kind == entryNCDir
}

func (e *navEntry) IsNCFile() bool {
	return e != nil && e.kind == entryNCFile
}

// focusModlandAlbum drills the nav stack to the format/album containing
// allAlbums[albumIdx], positioning the cursor on the playing album.
func (o *Overlay) focusModlandAlbum(albumIdx int) {
	o.navStack[0].cursor = 0
	o.navStack[0].scroll = 0

	format, _ := splitModlandName(o.allAlbums[albumIdx].Name)

	formats := o.buildFormatEntries()
	o.navStack = append(o.navStack, navLevel{ctx: ctxCatalog, label: "Modland", entries: formats, cursor: indexOfEntry(formats, func(e navEntry) bool { return e.format == format })})

	albums := o.buildAlbumsInFormatEntries(format)
	albumCursor := indexOfEntry(albums, func(e navEntry) bool { return e.albumIdx == albumIdx })
	o.navStack = append(o.navStack, navLevel{ctx: ctxCatalog, label: format, entries: albums, cursor: albumCursor})

	o.albumCursor = albumCursor
	o.albumsScroll = 0
	o.syncPanels()
}

func (o *Overlay) focusModArchiveAlbum(albumIdx int) {
	o.navStack[0].cursor = 0
	o.navStack[0].scroll = 0
	album := o.allAlbums[albumIdx]
	remote := player.RemotePath(album.Path)
	o.navStack = append(o.navStack, navLevel{ctx: ctxCatalog, label: "ModArchive", entries: []navEntry{
		{label: modarchive.AlbumLabel(remote), kind: entryModArchiveAlbum, albumIdx: albumIdx, url: remote},
	}})
	o.albumCursor = 0
	o.albumsScroll = 0
	o.syncPanels()
}

// focusLocalAlbum drills the nav stack down the folder chain holding
// allAlbums[albumIdx], positioning the cursor on the playing album.
func (o *Overlay) focusLocalAlbum(albumIdx int) {
	parts := relParts(o.baseDir, o.allAlbums[albumIdx].Path)
	if len(parts) <= 1 {
		// Album at (or is) the library root — no drill needed.
		o.albumCursor = o.rootIndexOf(albumIdx)
		o.albumsScroll = 0
		o.syncPanels()
		return
	}

	o.navStack[0].cursor = o.rootLocalDirIndex(filepath.Join(o.baseDir, parts[0]))
	o.navStack[0].scroll = 0

	dirPath := o.baseDir
	for i, seg := range parts[:len(parts)-1] {
		dirPath = filepath.Join(dirPath, seg)
		entries := o.buildLocalDirEntries(dirPath)
		cursor := 0
		if i == len(parts)-2 {
			cursor = indexOfEntry(entries, func(e navEntry) bool { return e.albumIdx == albumIdx })
			o.albumCursor = cursor
		} else {
			cursor = indexOfEntry(entries, func(e navEntry) bool {
				return e.kind == entryLocalDir && e.dirPath == filepath.Join(dirPath, parts[i+1])
			})
		}
		o.navStack = append(o.navStack, navLevel{ctx: ctxLibrary, label: seg, entries: entries, cursor: cursor})
	}

	o.albumsScroll = 0
	o.syncPanels()
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

// rootLocalDirIndex returns the root-level index of a top-level local folder.
func (o *Overlay) rootLocalDirIndex(dirPath string) int {
	return indexOfEntry(o.navStack[0].entries, func(e navEntry) bool { return e.kind == entryLocalDir && e.dirPath == dirPath })
}

// indexOfEntry returns the first entry matching pred, or 0.
func indexOfEntry(entries []navEntry, pred func(navEntry) bool) int {
	for i, e := range entries {
		if pred(e) {
			return i
		}
	}
	return 0
}

// buildRootEntries builds the library-root rows from allAlbums: local albums
// are grouped into top-level folders, with albums sitting directly in the
// music root shown as leaf rows. Remote catalog rows live only in the NC
// filesystem root — catalogs are entered from NC.
func (o *Overlay) buildRootEntries() []navEntry {
	var entries []navEntry
	seenDirs := map[string]bool{}
	for i, a := range o.allAlbums {
		if isVirtualAlbum(a) {
			continue // modland/modarchive virtual albums — catalog-browsed
		}
		parts := relParts(o.baseDir, a.Path)
		if len(parts) <= 1 {
			// Album at the library root (or the root itself is an album).
			entries = append(entries, navEntry{label: a.Name, kind: entryLocalAlbum, albumIdx: i})
			continue
		}
		dirPath := filepath.Join(o.baseDir, parts[0])
		if !seenDirs[dirPath] {
			seenDirs[dirPath] = true
			entries = append(entries, navEntry{label: parts[0] + "/", kind: entryLocalDir, albumIdx: -1, dirPath: dirPath})
		}
	}
	return entries
}

// buildLocalDirEntries lists the immediate children of a local folder:
// sub-folders first, then leaf albums. A folder that also holds tracks of
// its own appears as a leaf album row too, so nothing becomes unreachable.
func (o *Overlay) buildLocalDirEntries(dirPath string) []navEntry {
	var folders, albums []navEntry
	seenDirs := map[string]bool{}
	for i, a := range o.allAlbums {
		if isVirtualAlbum(a) {
			continue
		}
		parts := relParts(dirPath, a.Path)
		if parts == nil {
			continue
		}
		if len(parts) <= 1 {
			// Leaf album in this folder — or the folder itself when it holds
			// tracks directly (hybrid dir), kept reachable.
			albums = append(albums, navEntry{label: a.Name, kind: entryLocalAlbum, albumIdx: i})
			continue
		}
		child := filepath.Join(dirPath, parts[0])
		if !seenDirs[child] {
			seenDirs[child] = true
			folders = append(folders, navEntry{label: parts[0] + "/", kind: entryLocalDir, albumIdx: -1, dirPath: child})
		}
	}
	sort.Slice(folders, func(x, y int) bool { return folders[x].label < folders[y].label })
	sort.Slice(albums, func(x, y int) bool { return albums[x].label < albums[y].label })
	return append(folders, albums...)
}

// buildFormatEntries lists distinct modland formats.
func (o *Overlay) buildFormatEntries() []navEntry {
	seen := map[string]bool{}
	var formats []string
	for _, a := range o.allAlbums {
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
	entries := make([]navEntry, len(formats))
	for i, f := range formats {
		entries[i] = navEntry{label: f + "/", kind: entryFormat, format: f, albumIdx: -1}
	}
	return entries
}

// buildAlbumsInFormatEntries lists modland albums within a single format.
func (o *Overlay) buildAlbumsInFormatEntries(format string) []navEntry {
	var entries []navEntry
	for i, a := range o.allAlbums {
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

// buildModArchiveEntries returns directory items from the overlay cache or
// the pre-crawled local index. All listings are cached locally — no fetch.
func (o *Overlay) buildModArchiveEntries(targetURL string) []navEntry {
	items, ok := o.modArchiveItems[targetURL]
	if !ok {
		items, ok = modarchive.FetchDirectoryCached(o.baseDir, targetURL)
		if !ok {
			return nil
		}
		o.modArchiveItems[targetURL] = items
	}
	return o.buildModArchiveEntriesFromItems(targetURL, items)
}

func (o *Overlay) buildModArchiveEntriesFromItems(targetURL string, items []modarchive.DirItem) []navEntry {
	if len(items) == 0 {
		return nil
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
		for i, a := range o.allAlbums {
			if a.Path == albumPath {
				albumIdx = i
				break
			}
		}

		if albumIdx < 0 {
			if album := modarchive.BuildAlbum(targetURL, items); album != nil {
				o.allAlbums = append(o.allAlbums, *album)
				albumIdx = len(o.allAlbums) - 1
			}
		}

		if albumIdx >= 0 {
			return []navEntry{
				{
					label:    modarchive.AlbumLabel(targetURL),
					kind:     entryModArchiveAlbum,
					albumIdx: albumIdx,
					url:      targetURL,
				},
			}
		}
	}

	return nil
}

// rootIndexOf finds the root-level row for a real allAlbums index.
// The root level is the stack bottom (library root).
func (o *Overlay) rootIndexOf(albumIdx int) int {
	for i, e := range o.navStack[0].entries {
		if e.kind == entryLocalAlbum && e.albumIdx == albumIdx {
			return i
		}
	}
	return 0
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

// syncPanels refreshes labels and preview for the current cursor position.
func (o *Overlay) syncPanels() {
	if o.isNC() {
		o.refreshNCPreview()
		o.albumsDirty = true
		o.albumsContentDirty = true
		o.tracksDirty = true
		o.tracksContentDirty = true
		return
	}
	o.refreshAlbumLabels()
	o.refreshPreview()
	o.albumsDirty = true
	o.albumsContentDirty = true
	o.tracksDirty = true
	o.tracksContentDirty = true
}

// refreshPreview recomputes the right-panel preview for a non-leaf entry.
func (o *Overlay) refreshPreview() {
	e := o.currentEntry()
	if e == nil {
		o.previewEntries = nil
		o.previewActive = false
		return
	}
	switch e.kind {
	case entryModlandRoot:
		o.previewEntries = o.buildFormatEntries()
		o.previewActive = true
	case entryFormat:
		o.previewEntries = o.buildAlbumsInFormatEntries(e.format)
		o.previewActive = true
	case entryModArchiveRoot:
		o.previewEntries = o.buildModArchiveEntries(modarchive.BaseURL)
		o.previewActive = true
	case entryModArchiveDir:
		entries := o.buildModArchiveEntries(e.url)
		if len(entries) == 1 && entries[0].kind == entryModArchiveAlbum {
			albumIdx := entries[0].albumIdx
			if albumIdx >= 0 && albumIdx < len(o.allAlbums) {
				var trackEntries []navEntry
				for _, trackPath := range o.allAlbums[albumIdx].Tracks {
					filename := trackPath
					if idx := strings.LastIndexByte(trackPath, '/'); idx >= 0 {
						filename = trackPath[idx+1:]
					}
					filename = strings.TrimSuffix(filename, ".zip")
					trackEntries = append(trackEntries, navEntry{
						label: filename,
					})
				}
				o.previewEntries = trackEntries
				o.previewActive = true
				break
			}
		}
		o.previewEntries = entries
		o.previewActive = true
	case entryLocalDir:
		o.previewEntries = o.buildLocalDirEntries(e.dirPath)
		o.previewActive = true
	default:
		o.previewEntries = nil
		o.previewActive = false
	}
	o.tracksDirty = true
	o.tracksContentDirty = true
}

// pushLevel drills into a non-leaf entry, saving cursor/scroll for popLevel.
// The stack bottom is a root level and is never popped by popLevel.
func (o *Overlay) pushLevel(lvl navLevel) {
	top := &o.navStack[len(o.navStack)-1]
	top.cursor, top.scroll = o.albumCursor, o.albumsScroll
	o.navStack = append(o.navStack, lvl)
	o.albumCursor = lvl.cursor
	o.albumsScroll = lvl.scroll
	o.trackCursor = 0
	o.albumEntries = lvl.entries
	o.albums = labelsOf(lvl.entries)
	o.marqueeL.invalidate(o)
	o.syncPanels()
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
	o.albumEntries = top.entries
	o.albums = labelsOf(o.albumEntries)
	o.trackCursor = 0
	o.marqueeL.invalidate(o)
	o.syncPanels()
	return true
}

// AlbumCursor returns the real allAlbums index of the current leaf album,
// or -1 when on a non-leaf row.
func (o *Overlay) AlbumCursor() int {
	e := o.currentEntry()
	if e.IsLeafAlbum() {
		return e.albumIdx
	}
	return -1
}

// refreshVirtualTracks fills the tracks panel for overlay-owned ModArchive
// albums, which the library's GetAlbumTracks can't resolve (the app's
// SetTrackInfos overwrites trackInfos with nil for them). Runs each frame
// to stay ahead of that overwrite.
func (o *Overlay) refreshVirtualTracks() {
	e := o.currentEntry()
	if e == nil || !e.IsLeafAlbum() || e.albumIdx < 0 || e.albumIdx >= len(o.allAlbums) {
		return
	}
	album := o.allAlbums[e.albumIdx]
	if !strings.HasPrefix(album.Path, player.ModArchivePrefix) {
		return
	}
	infos := make([]player.TrackInfo, len(album.Tracks))
	for i, tp := range album.Tracks {
		infos[i] = player.TrackInfo{Path: tp}
	}
	if len(o.trackInfos) != len(infos) {
		o.trackInfos = infos
		if o.trackCursor >= len(infos) {
			o.trackCursor = 0
		}
		o.tracksDirty = true
		o.tracksContentDirty = true
	}
}

// SelectedTrackPlaylist returns the current virtual album and all its tracks.
func (o *Overlay) SelectedTrackPlaylist() (string, []string, int) {
	if o.focusPanel != 1 {
		return "", nil, -1
	}
	e := o.currentEntry()
	if e == nil || !e.IsLeafAlbum() || e.albumIdx < 0 || e.albumIdx >= len(o.allAlbums) {
		return "", nil, -1
	}
	album := o.allAlbums[e.albumIdx]
	if o.trackCursor < 0 || o.trackCursor >= len(album.Tracks) {
		return "", nil, -1
	}
	return album.Name, album.Tracks, o.trackCursor
}

// --- NC-local filesystem navigation ---

// buildNCDirectoryEntries lists entries in a local directory for the NC panel.
// Shows: ".." (unless at baseDir), directories, supported audio files.
// Hides: dotfiles, .pmv_meta.json, artwork, symlinks, empty dirs.
func (o *Overlay) buildNCDirectoryEntries(dirPath string) []navEntry {
	var entries []navEntry

	// Parent directory entry. Not shown at baseDir — nothing above it.
	if dirPath != o.baseDir {
		entries = append(entries, navEntry{
			label:    "..",
			kind:     entryNCDir,
			dirPath:  filepath.Dir(dirPath),
			albumIdx: -1,
		})
	}

	dirEntries, err := os.ReadDir(dirPath)
	if err != nil {
		slog.Warn("nc readdir", "path", dirPath, "error", err)
		return entries
	}

	var dirs, files []navEntry
	for _, de := range dirEntries {
		name := de.Name()
		if strings.HasPrefix(name, ".") || name == ".pmv_meta.json" {
			continue
		}
		if de.IsDir() {
			fullPath := filepath.Join(dirPath, name)
			if de.Type()&os.ModeSymlink != 0 {
				continue // v1: skip all symlinks
			}
			// Skip empty directories (no supported audio + no subdirs).
			if ncDirIsEmpty(fullPath) {
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
		if !de.Type().IsRegular() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if !formats.SupportedExts[ext] {
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

	// Remote catalog pseudo-entries appear only at the NC root, after connectivity succeeds.
	if dirPath == o.baseDir && o.online {
		entries = append(entries,
			navEntry{label: "Modland/", kind: entryModlandRoot, albumIdx: -1},
			navEntry{label: "ModArchive/", kind: entryModArchiveRoot, albumIdx: -1},
		)
	}

	return entries
}

// ncDirIsEmpty reports whether a directory has no supported audio files and
// no non-hidden subdirectories. Used to prune empty leaf dirs from the tree.
func ncDirIsEmpty(dirPath string) bool {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return true
	}
	for _, de := range entries {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if de.IsDir() {
			return false
		}
		if de.Type().IsRegular() {
			ext := strings.ToLower(filepath.Ext(name))
			if formats.SupportedExts[ext] {
				return false
			}
		}
	}
	return true
}

// topLevel returns the visible level — the top of the stack. The stack is
// never empty: its bottom is always a root level (ctxNC or ctxLibrary).
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

// breadcrumbParts returns the breadcrumb path components for the current
// navigation position. NC levels are derived from the current directory
// (which fully describes the position, even after a deep-link that collapses
// the stack); library/catalog levels use their stored label.
func (o *Overlay) breadcrumbParts() []string {
	parts := []string{"Music"}
	if o.isNC() {
		if dir := o.ncDir(); dir != "" {
			parts = append(parts, relParts(o.baseDir, dir)...)
		}
	} else {
		for i, lvl := range o.navStack {
			if i > 0 && lvl.label != "" {
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

// breadcrumbText renders the breadcrumb path: root always shown, middle
// components elided so at most the last three survive.
func (o *Overlay) breadcrumbText() string {
	parts := o.breadcrumbParts()
	if len(parts) > 4 {
		shown := []string{parts[0], "…"}
		shown = append(shown, parts[len(parts)-3:]...)
		parts = shown
	}
	return strings.Join(parts, " / ")
}

// switchToNC replaces the stack with a single NC root level at dirPath
// (baseDir when empty). Used for root switching and local deep-links.
func (o *Overlay) switchToNC(dirPath string) {
	if dirPath == "" {
		dirPath = o.baseDir
	}
	lvl := navLevel{ctx: ctxNC, dirPath: dirPath, entries: o.buildNCDirectoryEntries(dirPath)}
	o.navStack = []navLevel{lvl}
	o.albumEntries = lvl.entries
	o.albums = labelsOf(lvl.entries)
	o.albumCursor = 0
	o.albumsScroll = 0
	o.focusPanel = 0
	o.ncRight = ncRightInfo
	o.marqueeL.invalidate(o)
	o.syncPanels()
}

// switchToLibrary replaces the stack with a single library root level.
// Used for root switching and remote deep-links.
func (o *Overlay) switchToLibrary() {
	lvl := navLevel{ctx: ctxLibrary, entries: o.buildRootEntries()}
	o.navStack = []navLevel{lvl}
	o.albumEntries = lvl.entries
	o.albums = labelsOf(lvl.entries)
	o.albumCursor = 0
	o.albumsScroll = 0
	o.focusPanel = 0
	o.ncRight = ncRightInfo
	o.marqueeL.invalidate(o)
	o.syncPanels()
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
	o.previewActive = false
	o.previewEntries = nil
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
