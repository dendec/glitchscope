package ui

import (
	"sort"
	"strings"

	"github.com/dendec/pmv/internal/player"
)

// This file owns the library navigation model: local-album / modland
// format/album drill-down tree. State mutation in overlay_input.go;
// struct fields in overlay.go.

// modlandNamePrefix is prepended to modland album names by app.addModlandAlbums.
// Stripped when splitting into format/author for hierarchical browsing.
const modlandNamePrefix = "Modland: "

// navEntryKind classifies a row in the library navigation panel.
type navEntryKind int

const (
	entryLocalAlbum   navEntryKind = iota // real album/folder — leaf, has tracks
	entryModlandRoot                      // "Modland" pseudo-folder at the library root
	entryFormat                           // modland format bucket (e.g. "Protracker")
	entryModlandAlbum                     // modland author/album within a format — leaf, has tracks
)

// navEntry is one row in the library's left (navigation) panel.
type navEntry struct {
	label    string
	kind     navEntryKind
	albumIdx int    // index into Overlay.allAlbums when kind is a leaf album, else -1
	format   string // set when kind == entryFormat
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
// including deep-linking into modland drill-down when needed.
func (o *Overlay) FocusPlayingTrack(albumIdx, trackIdx int) {
	o.navStack = nil
	o.refreshAlbumLabels()
	o.trackCursor = trackIdx

	if albumIdx >= 0 && albumIdx < len(o.allAlbums) && strings.HasPrefix(o.allAlbums[albumIdx].Path, player.ModlandPrefix) {
		o.focusModlandAlbum(albumIdx)
	} else {
		o.albumCursor = o.rootIndexOf(albumIdx)
		o.refreshPreview()
	}

	if e := o.currentEntry(); e != nil && (e.kind == entryLocalAlbum || e.kind == entryModlandAlbum) {
		o.focusPanel = 1
	} else {
		o.focusPanel = 0
	}
	o.panelEntered = true
	o.albumsDirty = true
	o.tracksDirty = true
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

// rootModlandIndex returns the root-level index of the "Modland" entry.
func (o *Overlay) rootModlandIndex() int {
	return indexOfEntry(o.rootEntries, func(e navEntry) bool { return e.kind == entryModlandRoot })
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
		entries = append(entries, navEntry{label: a.Name, kind: entryLocalAlbum, albumIdx: i})
	}
	if hasModland {
		entries = append(entries, navEntry{label: "Modland", kind: entryModlandRoot, albumIdx: -1})
	}
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
	if e == nil {
		return -1
	}
	switch e.kind {
	case entryLocalAlbum, entryModlandAlbum:
		return e.albumIdx
	default:
		return -1
	}
}
