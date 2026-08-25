package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dendec/glitchscope/internal/filesystem"
	"github.com/dendec/glitchscope/internal/player"
	"golang.org/x/image/font"
)

// This file owns rendering for the Library page: albums/tracks panels.
// Shared primitives in overlay_render.go.

// renderLibraryPanels draws the albums (left) and tracks (right) panels.
func (o *Overlay) renderLibraryPanels(winW, winH, viewW, viewH int, panelW, panelY, panelH, lh int) {
	// Text clip width must match truncateEnd/rebuildMarqueeLine.
	textW := availableRowTextWidth(panelW) - 2*o.borderWidthPx()
	if textW < 1 {
		textW = 1
	}
	sbW := float32(o.scrollbarWidthPx())

	// --- Albums panel ---
	if o.albumsDirty {
		o.rebuildAlbumsTex(panelW, panelH)
	}
	if o.albumsTex != 0 {
		px, py := float32(0), float32(panelY)
		drawPanelBg(o, px, py, float32(panelW), float32(panelH), winW, winH, viewW, viewH)
		if o.panelEntered && o.focusPanel == 0 && len(o.albums) > 0 {
			rowY := py + float32((o.albumCursor-o.albumsScroll)*lh)
			o.drawCursorHighlight(px, rowY, float32(panelW), float32(lh), winW, winH, viewW, viewH)
		}
		glDrawOverlayTextClipped(o.programText, o.albumsTex, 1,
			px, py, float32(o.albumsTexW), float32(o.albumsTexH),
			px, py, float32(panelW), float32(panelH), winW, winH, viewW, viewH)
		am := panelH / lh
		if am < 1 {
			am = 1
		}
		drawScrollbar(o, px+float32(panelW)-sbW, py, float32(panelH), len(o.albums), am, o.albumsScroll, winW, winH, viewW, viewH)
		// Cursor highlight row — drawn as separate overlay, no texture rebuild needed.
		if o.panelEntered && o.focusPanel == 0 && len(o.albums) > 0 {
			rowY := py + float32((o.albumCursor-o.albumsScroll)*lh)
			o.drawMarqueeCol(&o.marqueeL, px, py, float32(textW), float32(panelH), lh, rowY, winW, winH, viewW, viewH)
		}
		if o.focusPanel == 0 {
			drawPanelBorder(o, px, py, float32(panelW), float32(panelH), winW, winH, viewW, viewH)
		}
	}

	// --- Tracks panel ---
	tracksX := winW - panelW
	if o.tracksDirty {
		o.rebuildTracksTex(panelW, panelH)
	}
	if o.tracksTex != 0 {
		tx, ty := float32(tracksX), float32(panelY)
		drawPanelBg(o, tx, ty, float32(panelW), float32(panelH), winW, winH, viewW, viewH)
		// NC and catalog-track info panels are static info views: no cursor
		// highlight, no marquee, and their scrollbar is driven by the shared
		// ncInfo* scroll state instead of the track list.
		isInfoPanel := o.isNC() || (o.currentEntry() != nil && o.currentEntry().IsCatalogTrack())
		if !isInfoPanel {
			if o.panelEntered && o.focusPanel == 1 && len(o.trackInfos) > 0 {
				rowY := ty + float32((o.trackCursor-o.tracksScroll)*lh)
				o.drawCursorHighlight(tx, rowY, float32(panelW), float32(lh), winW, winH, viewW, viewH)
			}
		}
		glDrawOverlayTextClipped(o.programText, o.tracksTex, 1,
			tx, ty, float32(o.tracksTexW), float32(o.tracksTexH),
			tx, ty, float32(panelW), float32(panelH), winW, winH, viewW, viewH)
		if !isInfoPanel {
			tm := panelH / lh
			if tm < 1 {
				tm = 1
			}
			tracksCount := len(o.trackInfos)
			drawScrollbar(o, tx+float32(panelW)-sbW, ty, float32(panelH), tracksCount, tm, o.tracksScroll, winW, winH, viewW, viewH)
			if o.panelEntered && o.focusPanel == 1 && len(o.trackInfos) > 0 {
				rowY := ty + float32((o.trackCursor-o.tracksScroll)*lh)
				o.drawMarqueeCol(&o.marqueeR, tx, ty, float32(textW), float32(panelH), lh, rowY, winW, winH, viewW, viewH)
			}
		} else {
			drawScrollbar(o, tx+float32(panelW)-sbW, ty, float32(panelH), o.ncInfoLines, o.ncInfoVisible, o.ncInfoScroll, winW, winH, viewW, viewH)
			o.drawInfoMarquee(tx, ty, float32(textW), float32(panelH), winW, winH, viewW, viewH)
		}
		if o.focusPanel == 1 {
			drawPanelBorder(o, tx, ty, float32(panelW), float32(panelH), winW, winH, viewW, viewH)
		}
	}
}

func (o *Overlay) rebuildAlbumsTex(maxW, maxH int) {
	o.albumsDirty = false

	if len(o.albums) == 0 {
		o.deleteTex(&o.albumsTex)
		o.marqueeL.invalidate(o)
		o.albumsContentDirty = false
		return
	}

	lh := o.face.Metrics().Height.Ceil()
	maxRows := maxH / lh
	if maxRows < 1 {
		maxRows = 1
	}

	// NC mode uses albumCursor/albumsScroll like provider mode.
	cursor := o.albumCursor
	scroll := &o.albumsScroll

	*scroll = scrollOffset(*scroll, cursor, len(o.albums), maxRows)

	o.deleteTex(&o.albumsTex)
	o.marqueeL.invalidate(o)

	start := *scroll
	end := start + maxRows
	if end > len(o.albums) {
		end = len(o.albums)
	}
	maxTextPx := availableRowTextWidth(maxW) - 2*o.borderWidthPx()
	var rows []listRow
	for i := start; i < end; i++ {
		name := o.albums[i]
		prefix := "  "
		if o.isNC() {
			// NC: highlight by file path match.
			e := o.albumEntries[i]
			if e.IsNCFile() && e.filePath == o.playingPath {
				prefix = "▸ "
			}
		} else if name == o.playingAlbum {
			prefix = "▸ "
		}
		line := prefix + name
		rows = append(rows, listRow{text: line, active: i == cursor && o.panelEntered && o.focusPanel == 0})
		if i == cursor && o.focusPanel == 0 {
			o.rebuildMarqueeLine(&o.marqueeL, line, maxTextPx, false)
		}
	}
	o.albumsTex, o.albumsTexW, o.albumsTexH = o.renderListRows(rows, maxTextPx, maxW)
	o.albumsContentDirty = false
}

func (o *Overlay) rebuildTracksTex(maxW, maxH int) {
	o.tracksDirty = false

	if o.isNC() {
		o.infoMarquee.invalidate(o)
		o.rebuildNCInfoTex(maxW, maxH)
		o.tracksContentDirty = false
		return
	}

	if e := o.currentEntry(); e != nil && e.kind == entryParent {
		o.deleteTex(&o.tracksTex)
		o.marqueeR.invalidate(o)
		o.infoMarquee.invalidate(o)
		o.tracksContentDirty = false
		return
	}

	// Catalog track: show right-panel info if track has cached metadata.
	if e := o.currentEntry(); e != nil && e.IsCatalogTrack() {
		o.rebuildCatalogTrackInfoTex(e, maxW, maxH)
		return
	}

	// Catalog directory/format level: no right panel.
	if o.isCatalog() {
		o.deleteTex(&o.tracksTex)
		o.marqueeR.invalidate(o)
		o.infoMarquee.invalidate(o)
		o.tracksContentDirty = false
		return
	}

	e := o.currentEntry()
	if e == nil || !e.IsLeafAlbum() {
		o.deleteTex(&o.tracksTex)
		o.marqueeR.invalidate(o)
		o.infoMarquee.invalidate(o)
		o.tracksContentDirty = false
		return
	}

	if len(o.trackInfos) == 0 {
		o.deleteTex(&o.tracksTex)
		o.marqueeR.invalidate(o)
		o.infoMarquee.invalidate(o)
		o.tracksContentDirty = false
		return
	}

	lh := o.face.Metrics().Height.Ceil()
	maxRows := maxH / lh
	if maxRows < 1 {
		maxRows = 1
	}
	o.tracksScroll = scrollOffset(o.tracksScroll, o.trackCursor, len(o.trackInfos), maxRows)

	o.deleteTex(&o.tracksTex)
	o.marqueeR.invalidate(o)

	start := o.tracksScroll
	end := start + maxRows
	if end > len(o.trackInfos) {
		end = len(o.trackInfos)
	}
	maxTextPx := availableRowTextWidth(maxW) - 2*o.borderWidthPx()
	var rows []listRow
	for i := start; i < end; i++ {
		info := o.trackInfos[i]
		title := player.TrackTitle(info.Path)
		suffix := ""
		if info.Duration > 0 {
			suffix = "  " + formatDuration(info.Duration)
		}
		prefix := "  "
		if info.Path == o.playingPath {
			prefix = "▸ "
		}
		line := prefix + title + suffix
		rows = append(rows, listRow{text: line, active: i == o.trackCursor && o.panelEntered && o.focusPanel == 1})
		if i == o.trackCursor && o.focusPanel == 1 {
			o.rebuildMarqueeLine(&o.marqueeR, line, maxTextPx, false)
		}
	}
	o.tracksTex, o.tracksTexW, o.tracksTexH = o.renderListRows(rows, maxTextPx, maxW)
	o.tracksContentDirty = false
}

// rebuildNCInfoTex renders the right-panel NC info (file/dir details + buttons).
func (o *Overlay) rebuildNCInfoTex(maxW, maxH int) {
	o.deleteTex(&o.tracksTex)
	o.marqueeR.invalidate(o)
	o.infoMarquee.invalidate(o)

	var lines []string
	if status := o.NCListingStatus(); status != filesystem.StatusOK {
		if status == filesystem.StatusPartial {
			lines = append(lines, "! Part of catalog unavailable", "")
		} else {
			lines = append(lines, "! Catalog unavailable", "")
		}
	}

	if o.ncInfoIsDir && o.ncInfoDir != "" {
		// Directory info.
		baseName := filepath.Base(o.ncInfoDir)
		if o.ncInfoDir == o.baseDir {
			baseName = "Music"
		}
		files, dirs := o.ncDirectoryCounts(o.ncInfoDir)
		lines = append(lines, baseName, "",
			fmt.Sprintf("  Folders: %d", dirs),
			fmt.Sprintf("  Playable files: %d", files))
	} else if o.ncInfoFile != "" {
		// File info.
		if info, err := os.Stat(o.ncInfoFile); err == nil {
			lines = append(lines, fmt.Sprintf("  %s", formatSize(info.Size())))
		}
	} else if len(lines) == 0 {
		o.tracksTex, o.tracksTexW, o.tracksTexH = 0, 0, 0
		return
	} else {
		// No selected file/dir (e.g. cursor on ".."), but still show the banner.
		maxTextPx := availableRowTextWidth(maxW)
		var rows []listRow
		for _, line := range lines {
			rows = append(rows, listRow{text: line})
		}
		o.tracksTex, o.tracksTexW, o.tracksTexH = o.renderListRows(rows, maxTextPx, maxW)
		return
	}

	// Buttons.
	lines = append(lines, "")
	playLine := "  [Play]"
	if o.focusPanel == 1 && o.ncRight == ncRightPlay {
		playLine = " >[Play]"
	}
	lines = append(lines, playLine)

	if o.ncConfirm {
		deleteLine := "  [Delete?]"
		if o.focusPanel == 1 && o.ncRight == ncRightDelete {
			deleteLine = " >[Delete?]"
		}
		lines = append(lines, deleteLine, "  press Enter to confirm", "  press Backspace to cancel")
	} else if o.ncInfoFile != "" || (o.ncInfoIsDir && o.ncInfoDir != o.baseDir) {
		deleteLine := "  [Delete]"
		if o.focusPanel == 1 && o.ncRight == ncRightDelete {
			deleteLine = " >[Delete]"
		}
		lines = append(lines, deleteLine)
	}

	if ti := o.ncTrackInfo(); ti != nil && ti.Comment != "" {
		lines = append(lines, "")
		for _, cl := range strings.Split(ti.Comment, "\n") {
			lines = append(lines, "  "+cl)
		}
	}

	maxTextPx := availableRowTextWidth(maxW)
	infoTextW := maxTextPx
	for _, line := range lines {
		if width := font.MeasureString(o.face, line).Ceil(); width > infoTextW {
			infoTextW = width
		}
	}
	lh := o.face.Metrics().Height.Ceil()
	maxRows := maxH / lh
	if maxRows < 1 {
		maxRows = 1
	}
	o.ncInfoVisible = maxRows
	o.ncInfoLines = len(lines)
	maxScroll := max(0, len(lines)-maxRows)
	o.ncInfoScroll = min(o.ncInfoScroll, maxScroll)
	start := o.ncInfoScroll
	end := min(start+maxRows, len(lines))
	var rows []listRow
	for _, line := range lines[start:end] {
		if infoTextW > maxTextPx {
			line = ""
		}
		rows = append(rows, listRow{text: line})
	}
	o.tracksTex, o.tracksTexW, o.tracksTexH = o.renderListRows(rows, infoTextW, maxW)
	o.rebuildInfoMarquee(lines[start:end], maxTextPx, infoTextW)
}

func (o *Overlay) ncTrackInfo() *player.TrackInfo {
	for i := range o.trackInfos {
		if o.trackInfos[i].Path == o.ncInfoFile {
			return &o.trackInfos[i]
		}
	}
	return nil
}

// catalogTrackInfoLines builds the right-panel lines for a cached catalog
// track. Returns nil when the track is not cached or metadata is not yet
// available — the caller hides the right panel in that case.
// Pure logic, no GL — unit-testable.
func catalogTrackInfoLines(e *navEntry, albums []player.Album, trackInfos []player.TrackInfo) []string {
	if e.albumIdx < 0 || e.trackIdx < 0 {
		return nil
	}
	if e.albumIdx >= len(albums) {
		return nil
	}
	album := albums[e.albumIdx]
	if e.trackIdx >= len(album.Tracks) {
		return nil
	}
	trackPath := album.Tracks[e.trackIdx]

	// Find the track info from the presenter-provided trackInfos.
	for i := range trackInfos {
		if trackInfos[i].Path != trackPath {
			continue
		}
		ti := &trackInfos[i]
		if !ti.Cached {
			return nil
		}
		// Cached — show metadata. Even with zero metadata the panel is
		// visible, signalling that the file is locally available.
		var lines []string
		lines = append(lines, player.TrackTitle(e.label), "")
		// Line 1: Length + Size (aligned columns)
		var header string
		if ti.Duration > 0 {
			header = fmt.Sprintf("Length: %-5s", formatDuration(ti.Duration))
		}
		if ti.Size > 0 {
			if header != "" {
				header += " "
			}
			header += fmt.Sprintf("Size: %s", formatSize(ti.Size))
		}
		if header != "" {
			lines = append(lines, header)
		}
		// Line 2: Channels + BPM (aligned columns)
		var specs string
		if ti.Channels > 0 {
			specs = fmt.Sprintf("Channels: %-3d", ti.Channels)
		}
		if ti.BPM > 0 {
			if specs != "" {
				specs += " "
			}
			specs += fmt.Sprintf("BPM: %-3.0f", ti.BPM)
		}
		if specs != "" {
			lines = append(lines, specs)
		}
		// Comment block
		if ti.Comment != "" {
			lines = append(lines, "Comment:")
			lines = append(lines, strings.Split(ti.Comment, "\n")...)
		}
		return lines
	}

	// Track info not yet available (metadata still loading).
	return nil
}

// rebuildCatalogTrackInfoTex renders the right-panel info for a cached catalog
// track. Hides the panel (no texture) when the track is not cached or
// metadata is unavailable. Long comments scroll via ncInfo* scroll state.
func (o *Overlay) rebuildCatalogTrackInfoTex(e *navEntry, maxW, maxH int) {
	o.deleteTex(&o.tracksTex)
	o.marqueeR.invalidate(o)
	o.infoMarquee.invalidate(o)
	o.tracksContentDirty = false

	lines := catalogTrackInfoLines(e, o.currentAlbums(), o.trackInfos)
	if len(lines) == 0 {
		return
	}

	maxTextPx := availableRowTextWidth(maxW)
	infoTextW := maxTextPx
	for _, line := range lines {
		if width := font.MeasureString(o.face, line).Ceil(); width > infoTextW {
			infoTextW = width
		}
	}
	lh := o.face.Metrics().Height.Ceil()
	maxRows := maxH / lh
	if maxRows < 1 {
		maxRows = 1
	}
	o.ncInfoVisible = maxRows
	o.ncInfoLines = len(lines)
	maxScroll := max(0, len(lines)-maxRows)
	o.ncInfoScroll = min(o.ncInfoScroll, maxScroll)
	start := o.ncInfoScroll
	end := min(start+maxRows, len(lines))
	var rows []listRow
	for _, line := range lines[start:end] {
		if infoTextW > maxTextPx {
			line = ""
		}
		rows = append(rows, listRow{text: line})
	}
	o.tracksTex, o.tracksTexW, o.tracksTexH = o.renderListRows(rows, infoTextW, maxW)
	o.rebuildInfoMarquee(lines[start:end], maxTextPx, infoTextW)
}

func (o *Overlay) rebuildInfoMarquee(lines []string, viewportW, contentW int) {
	if contentW <= viewportW {
		o.infoMarquee.invalidate(o)
		return
	}
	marqueeLines := make([]string, len(lines))
	spaceW := font.MeasureString(o.face, " ").Ceil()
	if spaceW < 1 {
		spaceW = 1
	}
	for i, line := range lines {
		width := font.MeasureString(o.face, line).Ceil()
		padding := max(0, (contentW-width+spaceW-1)/spaceW)
		marqueeLines[i] = line + strings.Repeat(" ", padding)
	}
	o.rebuildMarqueeLine(&o.infoMarquee, strings.Join(marqueeLines, "\n"), viewportW, false)
}

func (o *Overlay) drawInfoMarquee(x, y, w, h float32, winW, winH, viewW, viewH int) {
	if o.infoMarquee.tex == 0 || float32(o.infoMarquee.texW) <= w {
		return
	}
	offset := max(0, min(int(o.infoMarquee.offset), o.infoMarquee.texW-int(w)))
	glDrawOverlayTextClipped(o.programText, o.infoMarquee.tex, 1,
		x-float32(offset), y, float32(o.infoMarquee.texW), float32(o.infoMarquee.texH),
		x, y, w, h, winW, winH, viewW, viewH)
}
