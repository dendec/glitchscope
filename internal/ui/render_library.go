package ui

import (
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/png"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dendec/glitchscope/internal/filesystem"
	"github.com/dendec/glitchscope/internal/i18n"
	"github.com/dendec/glitchscope/internal/player"
	"github.com/dendec/glitchscope/internal/radio"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
)

// This file owns rendering for the Library page: albums/tracks panels.
// Shared primitives in overlay_render.go.

// renderLibraryPanels draws the albums (left) and tracks (right) panels.
func (o *Overlay) renderLibraryPanels(winW, winH, viewW, viewH int, panelW, panelY, panelH, lh int) {
	// Text clip width must match truncateEnd/rebuildMarqueeLine.
	textW := o.availableRowTextWidth(panelW) - 2*o.borderWidthPx()
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
		textOffset := float32(textPadding(o.fontSize))
		textY := py - textOffset
		textX := px
		marqueeX := px
		marqueeW := float32(max(1, textW-o.favoriteIconTextInset(lh)))
		if o.topLevel().ctx == ctxSourceRoot || o.showsFavoriteFolderIcons() {
			iconOffset := o.sourceIconTextOffset(lh)
			textX += float32(iconOffset)
			marqueeX += float32(iconOffset)
			marqueeW -= float32(iconOffset)
			if marqueeW < 1 {
				marqueeW = 1
			}
		}
		drawPanelBg(o, px, py, float32(panelW), float32(panelH), winW, winH, viewW, viewH)
		if o.panelEntered && len(o.albums) > 0 {
			rowY := py + float32((o.albumCursor-o.albumsScroll)*lh) - textOffset
			o.drawCursorHighlight(px, rowY, float32(panelW), float32(lh), winW, winH, viewW, viewH)
		}
		glDrawOverlayTextClipped(o.programText, o.albumsTex, 1,
			textX, textY, float32(o.albumsTexW), float32(o.albumsTexH),
			px, py, float32(panelW), float32(panelH), winW, winH, viewW, viewH)
		am := panelH / lh
		if am < 1 {
			am = 1
		}
		drawScrollbar(o, px+float32(panelW)-sbW, py, float32(panelH), len(o.albums), am, o.albumsScroll, winW, winH, viewW, viewH)
		// Cursor highlight row — drawn as separate overlay, no texture rebuild needed.
		if o.panelEntered && len(o.albums) > 0 {
			rowY := py + float32((o.albumCursor-o.albumsScroll)*lh) - textOffset
			o.drawMarqueeCol(&o.marqueeL, marqueeX, py, marqueeW, float32(panelH), lh, rowY, winW, winH, viewW, viewH)
		}
		o.drawLibraryIcons(px, py, float32(panelW), float32(panelH), o.albumsScroll,
			min(o.albumsScroll+max(panelH/lh, 1), len(o.albumEntries)), lh, winW, winH, viewW, viewH)
		if o.focusPanel == 0 {
			drawPanelBorder(o, px, py, float32(panelW), float32(panelH), winW, winH, viewW, viewH)
		}
	}

	// --- Tracks panel ---
	tracksX := winW - panelW
	if o.tracksDirty {
		o.rebuildTracksTex(panelW, panelH)
	}
	if o.tracksTex != 0 || o.hasNCSelection() {
		tx, ty := float32(tracksX), float32(panelY)
		drawPanelBg(o, tx, ty, float32(panelW), float32(panelH), winW, winH, viewW, viewH)
		// NC, catalog-track, and local-album info panels are static info views.
		isNCInfo := o.hasNCSelection()
		isCatalogInfo := o.currentEntry() != nil && o.currentEntry().IsCatalogTrack()
		isRadioInfo := o.currentEntry() != nil && o.currentEntry().kind == entryRadioStation
		isLocalInfo := o.infoLines != nil && !isNCInfo && !isCatalogInfo

		// The info image and metadata share one vertical scroll offset. Clip both
		// to the complete panel so neither can escape or overlap outside it.
		actionH := float32(0)
		metadataH := float32(panelH)
		catalogDelete := isCatalogInfo && o.isTrackCached != nil && o.isTrackCached(o.currentEntry().filePath)
		if isNCInfo || catalogDelete {
			actionH = float32(o.actionBarHeight(lh))
			metadataH -= actionH
			if metadataH < float32(lh) {
				metadataH = float32(lh)
			}
		}
		var scrollPx float32
		if isNCInfo || isLocalInfo || isCatalogInfo || isRadioInfo {
			scrollPx = float32(o.ncInfoScroll * lh)
		}
		contentY := ty - scrollPx
		imageTex, imageW, imageH := o.coverArtTex, o.coverArtTexW, o.coverArtTexH
		if isRadioInfo {
			imageTex, imageW, imageH = o.radioFaviconTex, o.radioFaviconTexW, o.radioFaviconTexH
		}
		isInfoPanel := isNCInfo || isCatalogInfo || isLocalInfo || isRadioInfo
		if !isInfoPanel {
			if o.panelEntered && o.focusPanel == 1 && len(o.trackInfos) > 0 {
				rowY := ty + float32((o.trackCursor-o.tracksScroll)*lh)
				o.drawCursorHighlight(tx, rowY, float32(panelW), float32(lh), winW, winH, viewW, viewH)
			}
		}
		remainH := metadataH
		glDrawOverlayTextClipped(o.programText, o.tracksTex, 1,
			tx, contentY, float32(o.tracksTexW), float32(o.tracksTexH),
			tx, ty, float32(panelW), remainH, winW, winH, viewW, viewH)
		if imageTex != 0 && imageW > 0 {
			pad := float32(o.scalePx(8))
			imgW := float32(imageW)
			imgH := float32(imageH)
			imgX := tx + (float32(panelW)-imgW)/2
			imgY := contentY + float32(o.tracksTexH) + pad
			glDrawOverlayImageClipped(o.programImage, imageTex, 1,
				imgX, imgY, imgW, imgH, tx, ty, float32(panelW), metadataH,
				winW, winH, viewW, viewH)
		}
		if isLocalInfo || isNCInfo || isCatalogInfo {
			drawScrollbar(o, tx+float32(panelW)-sbW, ty, remainH, o.ncInfoLines, o.ncInfoVisible, o.ncInfoScroll, winW, winH, viewW, viewH)
			if isNCInfo || isCatalogInfo {
				o.drawInfoMarquee(tx, contentY, float32(textW), ty, remainH, winW, winH, viewW, viewH)
			}
		} else {
			tm := panelH / lh
			if tm < 1 {
				tm = 1
			}
			tracksCount := len(o.trackInfos)
			drawScrollbar(o, tx+float32(panelW)-sbW, contentY, remainH, tracksCount, tm, o.tracksScroll, winW, winH, viewW, viewH)
			if o.panelEntered && o.focusPanel == 1 && len(o.trackInfos) > 0 {
				rowY := contentY + float32((o.trackCursor-o.tracksScroll)*lh)
				o.drawMarqueeCol(&o.marqueeR, tx, contentY, float32(textW), remainH, lh, rowY, winW, winH, viewW, viewH)
			}
		}
		if o.focusPanel == 1 {
			drawPanelBorder(o, tx, ty, float32(panelW), float32(panelH), winW, winH, viewW, viewH)
		}
		if (isNCInfo || catalogDelete) && o.ncActionsTex != 0 {
			actionY := ty + metadataH
			glDrawOverlayTextClipped(o.programText, o.ncActionsTex, 1,
				tx, actionY, float32(o.ncActionsTexW), float32(o.ncActionsTexH),
				tx, actionY, float32(panelW), actionH, winW, winH, viewW, viewH)
			o.drawActionIcons(tx, actionY, float32(panelW), actionH, winW, winH, viewW, viewH)
		}
	}
}

func (o *Overlay) actionBarHeight(lh int) int {
	return lh + 2*textPadding(o.fontSize)
}

func (o *Overlay) hasNCSelection() bool {
	if !o.isNC() {
		return false
	}
	e := o.currentEntry()
	return e != nil && e.kind != entryParent
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

	if o.pointerScroll[0] {
		*scroll = clampPointerScroll(*scroll, len(o.albums), maxRows)
	} else {
		*scroll = scrollOffset(*scroll, cursor, len(o.albums), maxRows)
	}

	o.deleteTex(&o.albumsTex)
	o.marqueeL.invalidate(o)

	start := *scroll
	end := start + maxRows
	if end > len(o.albums) {
		end = len(o.albums)
	}
	maxTextPx := o.availableRowTextWidth(maxW) - 2*o.borderWidthPx() - o.favoriteIconTextInset(lh)
	minTextureW := maxW
	if o.topLevel().ctx == ctxSourceRoot || o.showsFavoriteFolderIcons() {
		iconOffset := o.sourceIconTextOffset(lh)
		maxTextPx -= iconOffset
		minTextureW -= iconOffset
	}
	if maxTextPx < 1 {
		maxTextPx = 1
	}
	if minTextureW < 1 {
		minTextureW = 1
	}
	var rows []listRow
	for i := start; i < end; i++ {
		name := o.albums[i]
		prefix := "  "
		if o.topLevel().ctx == ctxSourceRoot || o.showsFavoriteFolderIcons() {
			// The renderer reserves a real pixel slot for the source icon.
			prefix = ""
		}
		if o.isNC() {
			// NC: highlight by file path match.
			e := o.albumEntries[i]
			if e.IsNCFile() && e.filePath == o.playingPath {
				prefix = "▸ "
			}
		} else if o.topLevel().ctx == ctxFavorites {
			// Inside playlist: only now-playing marker.
			e := o.albumEntries[i]
			if e.IsFavoriteTrack() && e.filePath == o.playingPath {
				prefix = "▸ "
			}
		} else if o.topLevel().ctx == ctxRadio {
			e := o.albumEntries[i]
			if e.kind == entryRadioStation && e.radioStation.Path() == o.playingPath {
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
	o.albumsTex, o.albumsTexW, o.albumsTexH = o.renderListRows(rows, maxTextPx, minTextureW)
	o.albumsContentDirty = false
}

func (o *Overlay) rebuildTracksTex(maxW, maxH int) {
	o.tracksDirty = false
	o.clearInfoPanel()

	if o.isNC() {
		if !o.hasNCSelection() {
			o.tracksContentDirty = false
			return
		}
		o.rebuildNCInfoTex(maxW, maxH)
		o.tracksContentDirty = false
		return
	}

	if e := o.currentEntry(); e != nil && e.kind == entryRadioStation {
		nowPlaying := ""
		if e.radioStation.Path() == o.radioNowPlayingPath {
			nowPlaying = o.radioNowPlayingTitle
		}
		o.rebuildRadioInfoTex(e.radioStation, nowPlaying, maxW, maxH)
		return
	}

	if e := o.currentEntry(); e != nil && e.kind == entryParent {
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
		o.tracksContentDirty = false
		return
	}

	e := o.currentEntry()
	if e == nil || !e.IsLeafAlbum() {
		o.tracksContentDirty = false
		return
	}

	if len(o.trackInfos) == 0 {
		o.tracksContentDirty = false
		return
	}

	// Local album track: show right-panel info for the selected track.
	o.rebuildLocalTrackInfoTex(maxW, maxH)
}

func (o *Overlay) rebuildRadioInfoTex(station radio.Station, nowPlaying string, maxW, maxH int) {
	o.tracksContentDirty = false
	lines := radioInfoLines(o.catalog, station, nowPlaying)
	o.infoLines = lines
	o.infoMarquee.invalidate(o)
	lh := o.face.Metrics().Height.Ceil()
	logoMaxH := maxH * 32 / 100
	if logoMaxH < lh*3 {
		logoMaxH = lh * 3
	}
	if logoMaxH > maxH {
		logoMaxH = maxH
	}
	o.ensureRadioFaviconTexture(station.Path(), maxW, logoMaxH)
	contentRows, visibleRows := o.infoScrollMetrics(len(lines), lh, maxH)
	o.ncInfoVisible = visibleRows
	o.ncInfoLines = contentRows
	o.ncInfoScroll = min(o.ncInfoScroll, max(0, contentRows-visibleRows))
	rows := make([]listRow, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, listRow{text: line})
	}
	o.tracksTex, o.tracksTexW, o.tracksTexH = o.renderListRows(rows, o.availableRowTextWidth(maxW), maxW)
}

func radioInfoLines(catalog i18n.Catalog, station radio.Station, nowPlaying string) []string {
	lines := []string{station.DisplayName(), ""}
	if nowPlaying != "" {
		lines = append(lines, catalog.Format(i18n.RadioNow, nowPlaying))
	}
	if station.Codec != "" {
		lines = append(lines, catalog.Format(i18n.RadioCodec, station.Codec))
	}
	if station.Bitrate > 0 {
		lines = append(lines, catalog.Format(i18n.RadioBitrate, station.Bitrate))
	}
	if station.Tags != "" {
		lines = append(lines, catalog.Format(i18n.RadioTags, station.Tags))
	}
	if station.Language != "" {
		lines = append(lines, catalog.Format(i18n.RadioLanguage, station.Language))
	}
	if station.Country != "" {
		lines = append(lines, catalog.Format(i18n.RadioCountry, station.Country))
	}
	if station.Homepage != "" {
		lines = append(lines, catalog.Format(i18n.RadioHomepage, station.Homepage))
	}
	return lines
}

func (o *Overlay) ensureRadioFaviconTexture(path string, maxWidth, maxHeight int) {
	processed := o.radioFavicons[path]
	if path == o.radioFaviconTexPath && o.radioFaviconTexMaxW == maxWidth && o.radioFaviconTexMaxH == maxHeight && (o.radioFaviconTex != 0 || processed == nil) {
		return
	}
	o.deleteTex(&o.radioFaviconTex)
	o.radioFaviconTexW = 0
	o.radioFaviconTexH = 0
	o.radioFaviconTexPath = path
	o.radioFaviconTexMaxW = maxWidth
	o.radioFaviconTexMaxH = maxHeight
	if path == "" || processed == nil || maxWidth <= 0 || maxHeight <= 0 {
		return
	}

	contentMaxW := max(1, maxWidth-2*o.scalePx(8))
	contentMaxH := max(1, maxHeight)
	w, h := fitRadioFaviconSize(processed.Bounds().Dx(), processed.Bounds().Dy(), contentMaxW, contentMaxH)
	scaled := scaleRadioFavicon(processed, w, h)
	o.radioFaviconTex = glUploadTexture(scaled)
	o.radioFaviconTexW = w
	o.radioFaviconTexH = h
}

func scaleRadioFavicon(src image.Image, width, height int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	if width == src.Bounds().Dx() && height == src.Bounds().Dy() {
		draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
		return dst
	}
	if width > src.Bounds().Dx() {
		xdraw.NearestNeighbor.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
		return dst
	}
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

const radioFaviconMinSize = 128

func fitRadioFaviconSize(srcW, srcH, maxW, maxH int) (w, h int) {
	if srcW <= 0 || srcH <= 0 || maxW <= 0 || maxH <= 0 {
		return 0, 0
	}

	// Keep tiny logos readable by scaling their longest side up to the
	// minimum size. The scale is capped by the panel bounds below, so a small
	// logo is never allowed to overflow the right panel.
	scaleNum, scaleDen := int64(1), int64(1)
	if longest := max(srcW, srcH); longest < radioFaviconMinSize {
		scaleNum = radioFaviconMinSize
		scaleDen = int64(longest)
	}
	if int64(maxW)*scaleDen < int64(srcW)*scaleNum {
		scaleNum = int64(maxW)
		scaleDen = int64(srcW)
	}
	if int64(maxH)*scaleDen < int64(srcH)*scaleNum {
		scaleNum = int64(maxH)
		scaleDen = int64(srcH)
	}

	return max(1, int((int64(srcW)*scaleNum+scaleDen/2)/scaleDen)),
		max(1, int((int64(srcH)*scaleNum+scaleDen/2)/scaleDen))
}

// rebuildLocalTrackInfoTex renders the right-panel info for the currently
// selected track in a local library album.
func (o *Overlay) rebuildLocalTrackInfoTex(maxW, maxH int) {
	o.tracksContentDirty = false

	if o.trackCursor < 0 || o.trackCursor >= len(o.trackInfos) {
		return
	}
	ti := &o.trackInfos[o.trackCursor]
	if ti.Path == o.fileMetadataPath {
		ti = &o.fileMetadata
	}
	// Load cover art for the selected track.
	o.loadCoverArt(ti.Path, o.availableRowTextWidth(maxW))
	lines := trackInfoLines(o.catalog, player.TrackTitle(ti.Path), ti)
	if len(lines) == 0 {
		return
	}

	maxTextPx := o.availableRowTextWidth(maxW)
	o.infoMarquee.invalidate(o)
	o.infoLines = lines
	// Vertical scroll: keep the cursor line visible.
	lh := o.face.Metrics().Height.Ceil()
	if lh <= 0 {
		lh = 1
	}
	contentRows, visibleRows := o.infoScrollMetrics(len(lines), lh, maxH)
	o.ncInfoVisible = visibleRows
	o.ncInfoLines = contentRows
	o.ncInfoScroll = min(o.ncInfoScroll, max(0, contentRows-visibleRows))
	var rows []listRow
	for _, line := range lines {
		rows = append(rows, listRow{text: line})
	}
	o.tracksTex, o.tracksTexW, o.tracksTexH = o.renderListRows(rows, maxTextPx, maxW)
}

// infoScrollMetrics computes total content rows and visible rows for an
// info panel, accounting for its image height. contentRows includes the
// image (converted to row units) plus text lines.
func (o *Overlay) infoScrollMetrics(textLines, lh, panelH int) (contentRows, visibleRows int) {
	imageH := 0
	if o.coverArtTex != 0 && o.coverArtTexH > 0 {
		imageH = o.coverArtTexH
	}
	if e := o.currentEntry(); e != nil && e.kind == entryRadioStation && o.radioFaviconTex != 0 && o.radioFaviconTexH > 0 {
		imageH = o.radioFaviconTexH
	}
	if imageH > 0 {
		imageH += o.scalePx(8)
	}
	contentRows = (imageH + textLines*lh + lh - 1) / lh
	visibleRows = panelH / lh
	if visibleRows < 1 {
		visibleRows = 1
	}
	return
}

// loadCoverArt extracts and decodes embedded cover art from a file,
// scales it to maxWidth, and uploads as a GL texture. Returns the
// texture handle, width, and height. Returns 0 if no cover art.
func (o *Overlay) loadCoverArt(path string, maxWidth int) (uint32, int, int) {
	if path == "" || maxWidth <= 0 {
		return 0, 0, 0
	}
	// Don't re-extract if we already have this file's cover art.
	if o.coverArtPath == path {
		return o.coverArtTex, o.coverArtTexW, o.coverArtTexH
	}
	o.deleteTex(&o.coverArtTex)
	if o.fileMetadataPath != path {
		return 0, 0, 0
	}
	img := o.fileCover
	if img == nil {
		o.coverArtPath = path
		o.coverArtTexW, o.coverArtTexH = 0, 0
		return 0, 0, 0
	}
	// Scale to fit within maxWidth, preserving aspect ratio.
	origW := img.Bounds().Dx()
	origH := img.Bounds().Dy()
	w, h := origW, origH
	if w > maxWidth {
		h = origH * maxWidth / origW
		w = maxWidth
	}
	if w <= 0 || h <= 0 {
		return 0, 0, 0
	}
	scaled := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.NearestNeighbor.Scale(scaled, scaled.Bounds(), img, img.Bounds(), xdraw.Over, nil)
	tex := glUploadTexture(scaled)
	o.coverArtTex = tex
	o.coverArtTexW = w
	o.coverArtTexH = h
	o.coverArtPath = path
	return tex, w, h
}

func (o *Overlay) clearInfoPanel() {
	o.deleteTex(&o.tracksTex)
	o.tracksTexW = 0
	o.tracksTexH = 0
	o.infoLines = nil
	o.ncInfoLines = 0
	o.ncInfoVisible = 0
	o.marqueeR.invalidate(o)
	o.infoMarquee.invalidate(o)
	o.deleteTex(&o.coverArtTex)
	o.coverArtTexW = 0
	o.coverArtTexH = 0
	o.coverArtPath = ""
}

// rebuildNCInfoTex renders the right-panel NC info (file/dir details + buttons).
func (o *Overlay) rebuildNCInfoTex(maxW, maxH int) {
	o.rebuildNCActionsTex()

	var lines []string
	if status := o.NCListingStatus(); status != filesystem.StatusOK {
		if status == filesystem.StatusPartial {
			lines = append(lines, "! "+o.catalog.Text(i18n.InfoCatalogPartial), "")
		} else {
			lines = append(lines, "! "+o.catalog.Text(i18n.InfoCatalogUnavailable), "")
		}
	}

	if o.ncInfoIsDir && o.ncInfoDir != "" {
		// Directory info.
		baseName := filepath.Base(o.ncInfoDir)
		if o.ncInfoDir == o.baseDir {
			baseName = o.catalog.Text(i18n.InfoMusic)
		}
		files, dirs := o.ncDirectoryCounts(o.ncInfoDir)
		lines = append(lines, baseName, "",
			"  "+o.catalog.Format(i18n.InfoFolders, dirs),
			"  "+o.catalog.Format(i18n.InfoPlayableFiles, files))
	} else if o.ncInfoFile != "" {
		// Load cover art first (before text) so the texture is ready for rendering.
		o.loadCoverArt(o.ncInfoFile, o.availableRowTextWidth(maxW))
		// File info.
		if o.fileMetadataPath == o.ncInfoFile {
			lines = append(lines, "  "+formatInfoSize(o.catalog, o.fileMetadata.Size))
		}
		// Read audio metadata for the file.
		meta := player.TrackInfo{}
		if o.fileMetadataPath == o.ncInfoFile {
			meta = o.fileMetadata
		}
		if meta.Title != "" {
			lines = append(lines, "", "  "+o.catalog.Format(i18n.InfoTitle, meta.Title))
		}
		if meta.Artist != "" {
			lines = append(lines, "  "+o.catalog.Format(i18n.InfoArtist, meta.Artist))
		}
		if meta.Album != "" {
			lines = append(lines, "  "+o.catalog.Format(i18n.InfoAlbum, meta.Album))
		}
		if meta.AlbumArtist != "" {
			lines = append(lines, "  "+o.catalog.Format(i18n.InfoAlbumArtist, meta.AlbumArtist))
		}
		if meta.Genre != "" {
			lines = append(lines, "  "+o.catalog.Format(i18n.InfoGenre, meta.Genre))
		}
		if meta.Date != "" {
			lines = append(lines, "  "+o.catalog.Format(i18n.InfoDate, meta.Date))
		}
		if meta.Track != "" {
			lines = append(lines, "  "+o.catalog.Format(i18n.InfoTrack, meta.Track))
		}
		if meta.Composer != "" {
			lines = append(lines, "  "+o.catalog.Format(i18n.InfoComposer, meta.Composer))
		}
		if meta.Disc != "" {
			lines = append(lines, "  "+o.catalog.Format(i18n.InfoDisc, meta.Disc))
		}
		if meta.Duration > 0 {
			lines = append(lines, "  "+o.catalog.Format(i18n.InfoLength, formatDuration(meta.Duration)))
		}
		if meta.Channels > 0 {
			lines = append(lines, "  "+o.catalog.Format(i18n.InfoChannels, meta.Channels))
		}
		if meta.BPM > 0 {
			lines = append(lines, "  "+o.catalog.Format(i18n.InfoBPM, meta.BPM))
		}
		if meta.Comment != "" {
			lines = append(lines, "  "+o.catalog.Text(i18n.InfoComment))
			for _, c := range strings.Split(meta.Comment, "\n") {
				lines = append(lines, "  "+c)
			}
		}
		if len(meta.Extra) > 0 {
			// Sort extra tags alphabetically for stable display.
			keys := make([]string, 0, len(meta.Extra))
			for k := range meta.Extra {
				if shouldHideExtraTag(k, meta.Extra[k]) {
					continue
				}
				keys = append(keys, k)
			}
			slices.Sort(keys)
			for _, k := range keys {
				label := k
				if len(label) > 0 {
					label = strings.ToUpper(label[:1]) + label[1:]
				}
				v := normalizeMetadataLines(meta.Extra[k])
				if strings.Contains(v, "\n") {
					lines = append(lines, "", "  "+label+":")
					for _, ln := range strings.Split(v, "\n") {
						lines = append(lines, "    "+ln)
					}
				} else {
					lines = append(lines, fmt.Sprintf("  %s: %s", label, v))
				}
			}
		}
	} else if len(lines) == 0 {
		o.tracksTex, o.tracksTexW, o.tracksTexH = 0, 0, 0
		return
	} else {
		// No selected file/dir (e.g. cursor on ".."), but still show the banner.
		maxTextPx := o.availableRowTextWidth(maxW)
		var rows []listRow
		for _, line := range lines {
			rows = append(rows, listRow{text: line})
		}
		o.tracksTex, o.tracksTexW, o.tracksTexH = o.renderListRows(rows, maxTextPx, maxW)
		return
	}

	maxTextPx := o.availableRowTextWidth(maxW)
	infoTextW := maxTextPx
	for _, line := range lines {
		if width := font.MeasureString(o.face, line).Ceil(); width > infoTextW {
			infoTextW = width
		}
	}
	lh := o.face.Metrics().Height.Ceil()
	metadataH := maxH - o.actionBarHeight(lh)
	if metadataH < lh {
		metadataH = lh
	}
	contentRows, visibleRows := o.infoScrollMetrics(len(lines), lh, metadataH)
	o.ncInfoVisible = visibleRows
	o.ncInfoLines = contentRows
	maxScroll := max(0, contentRows-visibleRows)
	o.ncInfoScroll = min(o.ncInfoScroll, maxScroll)
	var rows []listRow
	for _, line := range lines {
		if infoTextW > maxTextPx {
			line = ""
		}
		rows = append(rows, listRow{text: line})
	}
	o.tracksTex, o.tracksTexW, o.tracksTexH = o.renderListRows(rows, infoTextW, maxW)
	o.rebuildInfoMarquee(lines, maxTextPx, infoTextW)
}

// trackInfoLines builds right-panel info lines for a track. Pure logic, no GL.
func trackInfoLines(catalog i18n.Catalog, title string, ti *player.TrackInfo) []string {
	var lines []string
	lines = append(lines, title, "")

	// Audio tags
	if ti.Title != "" {
		lines = append(lines, catalog.Format(i18n.InfoTitle, ti.Title))
	}
	if ti.Artist != "" {
		lines = append(lines, catalog.Format(i18n.InfoArtist, ti.Artist))
	}
	if ti.Album != "" {
		lines = append(lines, catalog.Format(i18n.InfoAlbum, ti.Album))
	}
	if ti.AlbumArtist != "" {
		lines = append(lines, catalog.Format(i18n.InfoAlbumArtist, ti.AlbumArtist))
	}
	if ti.Genre != "" {
		lines = append(lines, catalog.Format(i18n.InfoGenre, ti.Genre))
	}
	if ti.Date != "" {
		lines = append(lines, catalog.Format(i18n.InfoDate, ti.Date))
	}
	if ti.Track != "" {
		lines = append(lines, catalog.Format(i18n.InfoTrack, ti.Track))
	}
	if ti.Composer != "" {
		lines = append(lines, catalog.Format(i18n.InfoComposer, ti.Composer))
	}
	if ti.Disc != "" {
		lines = append(lines, catalog.Format(i18n.InfoDisc, ti.Disc))
	}
	for key, value := range ti.Extra {
		if shouldHideExtraTag(key, value) {
			continue
		}
		label := key
		if len(label) > 0 {
			label = strings.ToUpper(label[:1]) + label[1:]
		}
		value = normalizeMetadataLines(value)
		if strings.Contains(value, "\n") {
			lines = append(lines, "", label+":")
			for _, line := range strings.Split(value, "\n") {
				lines = append(lines, "    "+line)
			}
		} else {
			lines = append(lines, fmt.Sprintf("%s: %s", label, value))
		}
	}

	// Technical info
	var header string
	if ti.Duration > 0 {
		header = catalog.Format(i18n.InfoLength, formatDuration(ti.Duration))
	}
	if ti.Size > 0 {
		if header != "" {
			header += " "
		}
		header += formatInfoSize(catalog, ti.Size)
	}
	if header != "" {
		lines = append(lines, header)
	}
	var specs string
	if ti.Channels > 0 {
		specs = catalog.Format(i18n.InfoChannels, ti.Channels)
	}
	if ti.BPM > 0 {
		if specs != "" {
			specs += " "
		}
		specs += catalog.Format(i18n.InfoBPM, ti.BPM)
	}
	if specs != "" {
		lines = append(lines, specs)
	}
	if ti.Comment != "" {
		lines = append(lines, catalog.Text(i18n.InfoComment))
		lines = append(lines, strings.Split(ti.Comment, "\n")...)
	}
	return lines
}

func shouldHideExtraTag(key, value string) bool {
	normalizedKey := strings.ToLower(strings.TrimSpace(key))
	if normalizedKey == "handler_name" || normalizedKey == "handlername" {
		return true
	}
	return normalizedKey == "language" && isUnknownMetadataValue(value)
}

func isUnknownMetadataValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "und", "unknown", "undefined", "n/a", "na":
		return true
	default:
		return false
	}
}

func normalizeMetadataLines(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
}

func formatInfoSize(catalog i18n.Catalog, size int64) string {
	return catalog.Format(i18n.InfoSize, formatSize(size))
}

// catalogTrackInfoLines builds the right-panel lines for a cached catalog
// track. Returns nil when the track is not cached or metadata is not yet
// available — the caller hides the right panel in that case.
// Pure logic, no GL — unit-testable.
func catalogTrackInfoLines(catalog i18n.Catalog, e *navEntry, albums []player.Album, trackInfos []player.TrackInfo) []string {
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
		return trackInfoLines(catalog, player.TrackTitle(e.label), ti)
	}

	// Track info not yet available (metadata still loading).
	return nil
}

// rebuildCatalogTrackInfoTex renders the right-panel info for a cached catalog
// track. Hides the panel (no texture) when the track is not cached or
// metadata is unavailable. Long comments scroll via ncInfo* scroll state.
func (o *Overlay) rebuildCatalogTrackInfoTex(e *navEntry, maxW, maxH int) {
	o.tracksContentDirty = false

	infos := o.trackInfos
	if o.fileMetadataPath != "" && o.fileMetadataPath == o.MetadataFilePath() {
		infos = []player.TrackInfo{o.fileMetadata}
	}
	lines := catalogTrackInfoLines(o.catalog, e, o.currentAlbums(), infos)
	if len(lines) == 0 {
		return
	}
	o.rebuildCatalogActionsTex()

	maxTextPx := o.availableRowTextWidth(maxW)
	infoTextW := maxTextPx
	for _, line := range lines {
		if width := font.MeasureString(o.face, line).Ceil(); width > infoTextW {
			infoTextW = width
		}
	}
	lh := o.face.Metrics().Height.Ceil()
	contentRows, visibleRows := o.infoScrollMetrics(len(lines), lh, maxH)
	o.ncInfoVisible = visibleRows
	o.ncInfoLines = contentRows
	maxScroll := max(0, contentRows-visibleRows)
	o.ncInfoScroll = min(o.ncInfoScroll, maxScroll)
	var rows []listRow
	for _, line := range lines {
		if infoTextW > maxTextPx {
			line = ""
		}
		rows = append(rows, listRow{text: line})
	}
	o.tracksTex, o.tracksTexW, o.tracksTexH = o.renderListRows(rows, infoTextW, maxW)
	o.rebuildInfoMarquee(lines, maxTextPx, infoTextW)
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

func (o *Overlay) drawInfoMarquee(x, y, w, clipY, clipH float32, winW, winH, viewW, viewH int) {
	if o.infoMarquee.tex == 0 || float32(o.infoMarquee.texW) <= w {
		return
	}
	offset := max(0, min(int(o.infoMarquee.offset), o.infoMarquee.texW-int(w)))
	glDrawOverlayTextClipped(o.programText, o.infoMarquee.tex, 1,
		x-float32(offset), y, float32(o.infoMarquee.texW), float32(o.infoMarquee.texH),
		x, clipY, w, clipH, winW, winH, viewW, viewH)
}
