package ui

import (
	"net/url"
	"sort"
	"strings"

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
	entryModlandRoot                         // "Modland" pseudo-folder at the library root
	entryFormat                              // modland format bucket (e.g. "Protracker")
	entryModlandAlbum                        // modland author/album within a format — leaf, has tracks
	entryModArchiveRoot                      // "ModArchive" pseudo-folder at the library root
	entryModArchiveDir                       // ModArchive HTTP directory folder
	entryModArchiveAlbum                     // ModArchive leaf album containing tracks
)

// navEntry is one row in the library's left (navigation) panel.
type navEntry struct {
	label    string
	kind     navEntryKind
	albumIdx int    // index into Overlay.allAlbums when kind is a leaf album, else -1
	format   string // set when kind == entryFormat
	url      string // set when kind == entryModArchiveDir
}

// navLevel is a pushed navigation level.
type navLevel struct {
	entries []navEntry
	cursor  int
	scroll  int
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
	o.navStack = nil
	o.refreshAlbumLabels()
	o.trackCursor = trackIdx

	if albumIdx >= 0 && albumIdx < len(o.allAlbums) {
		path := o.allAlbums[albumIdx].Path
		if strings.HasPrefix(path, player.ModlandPrefix) {
			o.focusModlandAlbum(albumIdx)
		} else if strings.HasPrefix(path, player.ModArchivePrefix) {
			o.focusModArchiveAlbum(albumIdx)
		} else {
			o.albumCursor = o.rootIndexOf(albumIdx)
			o.refreshPreview()
		}
	} else {
		o.albumCursor = 0
		o.refreshPreview()
	}

	if e := o.currentEntry(); e.IsLeafAlbum() {
		o.focusPanel = 1
	} else {
		o.focusPanel = 0
	}
	o.panelEntered = true
	o.albumsDirty = true
	o.tracksDirty = true
}

func (e *navEntry) IsLeafAlbum() bool {
	if e == nil {
		return false
	}
	return e.kind == entryLocalAlbum || e.kind == entryModlandAlbum || e.kind == entryModArchiveAlbum
}

// focusModlandAlbum drills the nav stack to the format/album containing
// allAlbums[albumIdx], positioning the cursor on the playing album.
func (o *Overlay) focusModlandAlbum(albumIdx int) {
	o.rootCursor = o.rootModlandIndex()
	o.rootScroll = 0

	format, _ := splitModlandName(o.allAlbums[albumIdx].Name)

	formats := o.buildFormatEntries()
	o.navStack = append(o.navStack, navLevel{entries: formats, cursor: indexOfEntry(formats, func(e navEntry) bool { return e.format == format })})

	albums := o.buildAlbumsInFormatEntries(format)
	albumCursor := indexOfEntry(albums, func(e navEntry) bool { return e.albumIdx == albumIdx })
	o.navStack = append(o.navStack, navLevel{entries: albums, cursor: albumCursor})

	o.albumCursor = albumCursor
	o.albumsScroll = 0
	o.syncPanels()
}

func (o *Overlay) focusModArchiveAlbum(albumIdx int) {
	o.rootCursor = o.rootModArchiveIndex()
	o.rootScroll = 0

	o.albumCursor = albumIdx
	o.albumsScroll = 0
	o.syncPanels()
}

// rootModlandIndex returns the root-level index of the "Modland" entry.
func (o *Overlay) rootModlandIndex() int {
	return indexOfEntry(o.rootEntries, func(e navEntry) bool { return e.kind == entryModlandRoot })
}

// rootModArchiveIndex returns the root-level index of the "ModArchive" entry.
func (o *Overlay) rootModArchiveIndex() int {
	return indexOfEntry(o.rootEntries, func(e navEntry) bool { return e.kind == entryModArchiveRoot })
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

// buildRootEntries builds the library-root rows from allAlbums.
func (o *Overlay) buildRootEntries() []navEntry {
	var entries []navEntry
	hasModland := false
	for i, a := range o.allAlbums {
		if strings.HasPrefix(a.Path, player.ModlandPrefix) {
			hasModland = true
			continue
		}
		if strings.HasPrefix(a.Path, player.ModArchivePrefix) {
			continue
		}
		entries = append(entries, navEntry{label: a.Name, kind: entryLocalAlbum, albumIdx: i})
	}
	if hasModland {
		entries = append(entries, navEntry{label: "Modland", kind: entryModlandRoot, albumIdx: -1})
	}
	entries = append(entries, navEntry{label: "ModArchive", kind: entryModArchiveRoot, albumIdx: -1})
	return entries
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
		entries[i] = navEntry{label: f, kind: entryFormat, format: f, albumIdx: -1}
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

// buildModArchiveEntries reads directory items from the catalog/cache, or
// fetches the listing over HTTP synchronously, and converts to navEntries.
func (o *Overlay) buildModArchiveEntries(targetURL string) []navEntry {
	items, err := modarchive.FetchDirectory(o.baseDir, targetURL)
	if err != nil || len(items) == 0 {
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
					label:    item.CleanName,
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

		label := modArchiveAlbumLabel(targetURL)
		if albumIdx < 0 {
			albumName := "ModArchive: " + label
			var tracks []string
			for _, item := range items {
				if item.Kind == modarchive.KindFile {
					tracks = append(tracks, player.ModArchivePrefix+item.URL)
				}
			}
			newAlbum := player.Album{
				Name:   albumName,
				Path:   albumPath,
				Tracks: tracks,
			}
			o.allAlbums = append(o.allAlbums, newAlbum)
			albumIdx = len(o.allAlbums) - 1
		}

		return []navEntry{
			{
				label:    label,
				kind:     entryModArchiveAlbum,
				albumIdx: albumIdx,
				url:      targetURL,
			},
		}
	}

	return nil
}

func modArchiveAlbumLabel(targetURL string) string {
	u, err := url.Parse(targetURL)
	if err != nil {
		return targetURL
	}
	p := strings.Trim(u.Path, "/")
	parts := strings.Split(p, "/")
	if len(parts) >= 3 {
		return modarchive.FormatDirName(parts[0]) + "/" + strings.Join(parts[1:], "/")
	}
	if len(parts) > 0 {
		return modarchive.FormatDirName(parts[0])
	}
	return "ModArchive"
}

// rootIndexOf finds the root-level row for a real allAlbums index.
func (o *Overlay) rootIndexOf(albumIdx int) int {
	for i, e := range o.rootEntries {
		if e.kind == entryLocalAlbum && e.albumIdx == albumIdx {
			return i
		}
	}
	return 0
}

// currentLevelEntries returns the rows for the currently displayed level.
func (o *Overlay) currentLevelEntries() []navEntry {
	if len(o.navStack) == 0 {
		return o.rootEntries
	}
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
	o.refreshAlbumLabels()
	o.refreshPreview()
	o.albumsDirty = true
	o.tracksDirty = true
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
	default:
		o.previewEntries = nil
		o.previewActive = false
	}
	o.tracksDirty = true
}

// pushLevel drills into a non-leaf entry, saving cursor/scroll for popLevel.
func (o *Overlay) pushLevel(entries []navEntry) {
	if len(o.navStack) == 0 {
		o.rootCursor, o.rootScroll = o.albumCursor, o.albumsScroll
	} else {
		top := &o.navStack[len(o.navStack)-1]
		top.cursor, top.scroll = o.albumCursor, o.albumsScroll
	}
	o.navStack = append(o.navStack, navLevel{entries: entries})
	o.albumCursor = 0
	o.albumsScroll = 0
	o.trackCursor = 0
	o.marqueeL.invalidate(o)
	o.syncPanels()
}

// popLevel goes up one navigation level. Returns false if already at root.
func (o *Overlay) popLevel() bool {
	if len(o.navStack) == 0 {
		return false
	}
	o.navStack = o.navStack[:len(o.navStack)-1]
	if len(o.navStack) == 0 {
		o.albumCursor, o.albumsScroll = o.rootCursor, o.rootScroll
	} else {
		top := o.navStack[len(o.navStack)-1]
		o.albumCursor, o.albumsScroll = top.cursor, top.scroll
	}
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
	}
}

// SelectedTrackPath returns the album name and track path for the current
// track selection, or ("", "") when none is selected.
func (o *Overlay) SelectedTrackPath() (string, string) {
	if o.focusPanel != 1 {
		return "", ""
	}
	e := o.currentEntry()
	if e == nil || !e.IsLeafAlbum() || e.albumIdx < 0 || e.albumIdx >= len(o.allAlbums) {
		return "", ""
	}
	album := o.allAlbums[e.albumIdx]
	if o.trackCursor < 0 || o.trackCursor >= len(album.Tracks) {
		return "", ""
	}
	return album.Name, album.Tracks[o.trackCursor]
}
