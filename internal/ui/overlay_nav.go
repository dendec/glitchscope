package ui

import (
	"sort"
	"strings"

	"github.com/dendec/mdpp/internal/player"
)

// This file owns the library navigation model: the local-album / modland
// format / modland-album drill-down tree, its construction from the flat
// album list, and cursor-to-level bookkeeping (push/pop/refresh). State
// mutation for input events lives in overlay_input.go; struct fields live in
// overlay.go.

// modlandNamePrefix is prepended to modland album names by app.addModlandAlbums
// ("Modland: Format/Author"). Stripped when splitting into format/author for
// hierarchical browsing.
const modlandNamePrefix = "Modland: "

// navEntryKind classifies a row in the library navigation panel.
type navEntryKind int

const (
	entryLocalAlbum   navEntryKind = iota // real album/folder — leaf, has tracks
	entryModlandRoot                      // "Modland" pseudo-folder at the library root
	entryFormat                           // modland format bucket (e.g. "Protracker")
	entryModlandAlbum                     // modland author/album within a format — leaf, has tracks
)

// navEntry is one row shown in the library's left (navigation) panel.
type navEntry struct {
	label    string
	kind     navEntryKind
	albumIdx int    // index into Overlay.allAlbums when kind is a leaf album, else -1
	format   string // set when kind == entryFormat
}

// navLevel is a pushed navigation level (everything below the library root).
type navLevel struct {
	entries []navEntry
	cursor  int
	scroll  int
}

// splitModlandName splits a modland album's display name ("Modland:
// Format/Author") into its format and author parts.
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

// FocusPlayingTrack points the library page cursor at the given album/track
// and focuses the tracks (right) panel. Called when the playlist screen is
// opened so the cursor starts on the currently playing file — including
// deep-linking into the modland format/album drill-down when the playing
// track lives there, so reopening the UI always lands back on it.
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

// focusModlandAlbum drills the navigation stack down to the format and
// author/album containing allAlbums[albumIdx] (format level, then album
// level), positioning the cursor on the playing album at the bottom. The
// root level is left parked on the "Modland" entry, so popping back out
// lands on it rather than at row 0.
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

// rootModlandIndex returns the root-level row index of the "Modland" entry,
// or 0 if there isn't one.
func (o *Overlay) rootModlandIndex() int {
	return indexOfEntry(o.rootEntries, func(e navEntry) bool { return e.kind == entryModlandRoot })
}

// indexOfEntry returns the index of the first entry matching pred, or 0 if
// none matches (row 0 is a safe fallback cursor position in every level).
func indexOfEntry(entries []navEntry, pred func(navEntry) bool) int {
	for i, e := range entries {
		if pred(e) {
			return i
		}
	}
	return 0
}

// buildRootEntries builds the library-root navigation rows from allAlbums:
// local albums verbatim, plus a single "Modland" entry when any modland
// albums are present.
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

// buildFormatEntries lists the distinct modland formats (e.g. "Protracker").
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

// buildAlbumsInFormatEntries lists the modland albums (authors) within a
// single format. Each is a leaf with its own tracks.
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

// rootIndexOf finds the root-level row for a real allAlbums index (only
// local albums are addressable this way — modland leaves live behind the
// "Modland" entry and fall back to root position 0).
func (o *Overlay) rootIndexOf(albumIdx int) int {
	for i, e := range o.rootEntries {
		if e.kind == entryLocalAlbum && e.albumIdx == albumIdx {
			return i
		}
	}
	return 0
}

// currentLevelEntries returns the rows for whichever level is currently
// displayed in the left panel: the root, or the top of navStack.
func (o *Overlay) currentLevelEntries() []navEntry {
	if len(o.navStack) == 0 {
		return o.rootEntries
	}
	return o.navStack[len(o.navStack)-1].entries
}

// currentEntry returns the row under the left-panel cursor, or nil.
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

// syncPanels refreshes both left-panel labels and the right-panel preview
// for the current cursor position, then marks both panels dirty. Common
// tail shared by pushLevel, popLevel and focusModlandAlbum.
func (o *Overlay) syncPanels() {
	o.refreshAlbumLabels()
	o.refreshPreview()
	o.albumsDirty = true
	o.tracksDirty = true
}

// refreshPreview recomputes the right-panel preview for a non-leaf entry
// under the cursor (formats list, or albums-in-format list). For a leaf
// entry, previewActive is false and the right panel falls back to the
// externally supplied trackInfos, as before.
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

// pushLevel drills into a non-leaf entry, saving the current level's
// cursor/scroll so popLevel can restore it.
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

// popLevel goes up one navigation level, restoring the parent's cursor and
// scroll position. Returns false if already at the root (caller should
// treat this as "back"/exit instead).
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

// AlbumCursor returns the real allAlbums index of the leaf album currently
// previewed/selected, or -1 when the cursor is on a non-leaf row (the
// "Modland" entry or a format bucket) — callers must not fetch tracks or
// play in that case, since there is no single album yet.
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
