// Package ui injects text into the visualizer's frame feedback.
package ui

import (
	"log/slog"
	"math"
	"time"

	"github.com/dendec/pmv/internal/config"
	"github.com/dendec/pmv/internal/modarchive"
	"github.com/dendec/pmv/internal/player"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// This file owns the Overlay struct, its lifecycle (New/Close/Draw/Update)
// and the frame-data setters (Set*) that the app loop feeds each frame.
// Navigation-tree state lives in overlay_nav.go, cursor/input handling and
// page switching in overlay_input.go, theme/color derivation in
// overlay_theme.go, presets model + marquee animation in
// overlay_presets.go. Rendering lives in overlay_render.go; GL/cgo calls are
// isolated in gl.go.

const (
	fadeInDuration = 400 * time.Millisecond
	holdDuration   = 3 * time.Second
)

// UIPage selects which screen the overlay shows.
type UIPage int

const (
	PageLibrary UIPage = iota
	PageSettings
	PagePresets
)

// SettingRow describes one line in the settings page.
type SettingRow struct {
	Label  string
	Values []string
	Index  int
}

// PresetCat holds a category name and its preset keys.
type PresetCat struct {
	Name    string
	Presets []string
}

// Overlay manages UI and notification rendering.
type Overlay struct {
	programText uint32
	programRect uint32
	face        font.Face

	notif Notifier

	uiVisible    bool
	panelEntered bool
	showFPS      bool
	uiPage       UIPage

	screenW, screenH int
	fontSize         float64
	baseDir          string

	allAlbums      []player.Album
	rootEntries    []navEntry
	rootCursor     int
	rootScroll     int
	navStack       []navLevel
	albumEntries   []navEntry
	albums         []string
	albumCursor    int
	trackInfos     []player.TrackInfo
	previewEntries []navEntry
	previewActive  bool
	trackCursor    int
	statsLine      string
	position       float64
	duration       float64
	sampleRate     float32
	bitrate        float64
	bpm            float64
	channels       int
	paused         bool
	isTracker      bool
	presetName     string
	playingAlbum   string
	playingTrack   string
	loading        bool
	loadPercent    int64
	focusPanel     int // 0=albums, 1=tracks

	settingsRows        []SettingRow
	settingsCursor      int
	settingsValueCursor int
	settingsEditing     bool
	settingsDirty       bool
	settingsColL        listTex
	settingsColR        listTex

	presetCategories     []PresetCat
	presetCategoryCursor int
	presetCursor         int
	presetsColL          listTex
	presetsColR          listTex
	presetsScrollL       int
	presetsScrollR       int

	scrollUp   scrollHold
	scrollDown scrollHold

	albumsScroll int
	tracksScroll int

	theme        config.Theme
	transparency float32

	marqueeL marqueeState
	marqueeR marqueeState

	albumsTex                      uint32
	albumsTexW, albumsTexH         int
	tracksTex                      uint32
	tracksTexW, tracksTexH         int
	bottomTex                      uint32
	bottomTexW, bottomTexH         int
	statsTex                       uint32
	statsTexW, statsTexH           int
	presetNameTex                  uint32
	presetNameTexW, presetNameTexH int

	pageIndicatorTex   [3]uint32
	pageIndicatorTexW  [3]int
	pageIndicatorTexH  [3]int
	pageIndicatorDirty bool
	textureCacheReady  bool

	albumsDirty        bool
	albumsContentDirty bool
	tracksDirty        bool
	tracksContentDirty bool
	statsDirty         bool
	bottomDirty        bool
	presetNameDirty    bool
	presetsDirty       bool
	online             bool
	closeInjectPending bool
	modArchiveItems    map[string][]modarchive.DirItem
	modArchiveErrors   map[string]error
	modArchivePending  map[string]bool
	modArchiveResults  chan modArchiveResult
	closeCh            chan struct{} // closed by Close() to unblock goroutines
}

// New creates an Overlay.
func New() *Overlay {
	return &Overlay{
		programText:       glCreateTextProgram(),
		programRect:       glCreateRectProgram(),
		modArchiveItems:   make(map[string][]modarchive.DirItem),
		modArchiveErrors:  make(map[string]error),
		modArchivePending: make(map[string]bool),
		modArchiveResults: make(chan modArchiveResult, 8),
		closeCh:           make(chan struct{}),
	}
}

func (o *Overlay) Close() {
	close(o.closeCh)
	// Drain pending results so goroutines don't leak.
	for {
		select {
		case <-o.modArchiveResults:
		default:
			goto drained
		}
	}
drained:
	o.notif.Hide()
	o.deleteTex(&o.albumsTex)
	o.deleteTex(&o.tracksTex)
	o.deleteTex(&o.bottomTex)
	o.deleteTex(&o.statsTex)
	o.deleteTex(&o.presetNameTex)
	o.deleteTex(&o.settingsColL.tex)
	o.deleteTex(&o.settingsColR.tex)
	o.deleteTex(&o.presetsColL.tex)
	o.deleteTex(&o.presetsColR.tex)
	o.marqueeL.invalidate(o)
	o.marqueeR.invalidate(o)
	for i := range o.pageIndicatorTex {
		o.deleteTex(&o.pageIndicatorTex[i])
	}
	glDeleteProgram(o.programText)
	o.programText = 0
	glDeleteProgram(o.programRect)
	o.programRect = 0
	if o.face != nil {
		_ = o.face.Close()
	}
}

// Draw renders either the UI overlay or the notification.
func (o *Overlay) Draw(width, height int) {
	if o.uiVisible {
		o.renderUI(width, height, width, height)
		if o.notif.Visible() && o.notif.Tex() != 0 && !o.notif.Hidden() {
			o.notif.Render(o.programText, width, height)
		}
	} else {
		if o.showFPS {
			o.renderStatsOnly(width, height)
		}
		if o.notif.Visible() && !o.notif.Hidden() && !o.notif.Injected() {
			o.notif.Render(o.programText, width, height)
		}
	}
}

// --- Notification (delegates to Notifier) ---

// ShowTrack begins the fade-in animation for the given track path.
func (o *Overlay) ShowTrack(path string) {
	o.notif.ShowTrack(path, o.fontSize, o.textColor())
}

// Inject stamps the notification text into projectM's feedback framebuffer.
func (o *Overlay) Inject(width, height int) {
	if o.closeInjectPending {
		o.closeInjectPending = false
		o.renderUI(o.screenW, o.screenH, width, height)
	}
	if o.uiVisible {
		return
	}
	o.notif.Inject(o.programText, o.screenW, o.screenH, width, height)
}

func (o *Overlay) ToggleVisibility() {
	o.notif.Toggle()
}

// --- UI mode ---

func (o *Overlay) SetBaseDir(dir string) {
	o.baseDir = dir
}

func (o *Overlay) SetSettingsRows(rows []SettingRow, cursor int) {
	o.settingsRows = rows
	o.settingsCursor = cursor
	o.settingsDirty = true
}

func (o *Overlay) UIVisible() bool {
	return o.uiVisible
}

func (o *Overlay) SetShowFPS(v bool) {
	o.showFPS = v
}

func (o *Overlay) SettingsCursor() int { return o.settingsCursor }

func (o *Overlay) IsSettingsPage() bool { return o.uiPage == PageSettings }

func (o *Overlay) IsPresetsPage() bool { return o.uiPage == PagePresets }

func (o *Overlay) IsSettingsEditing() bool { return o.settingsEditing }

func (o *Overlay) markAllDirty() {
	o.albumsDirty = true
	o.albumsContentDirty = true
	o.tracksDirty = true
	o.tracksContentDirty = true
	o.bottomDirty = true
	o.statsDirty = true
	o.presetNameDirty = true
	o.settingsDirty = true
	o.presetsDirty = true
	o.pageIndicatorDirty = true
	o.marqueeL.invalidate(o)
	o.marqueeR.invalidate(o)
}

// SetScreenSize updates screen dimensions and recomputes font size.
func (o *Overlay) SetScreenSize(w, h int) {
	if o.screenW == w && o.screenH == h {
		return
	}
	// The panel width and row clipping width depend on w even when the font
	// size (which depends only on h) is unchanged.
	o.screenW = w
	o.screenH = h
	newSize := math.Round(float64(h) / 30)
	if newSize < 10 {
		newSize = 10
	}
	if o.fontSize != newSize {
		o.fontSize = newSize
		o.rebuildFace()
	}
	o.markAllDirty()
}

func (o *Overlay) rebuildFace() {
	f, err := opentype.Parse(unifontData)
	if err != nil {
		slog.Error("font parse", "error", err)
		return
	}
	if o.face != nil {
		_ = o.face.Close()
		o.face = nil
	}
	o.face, err = opentype.NewFace(f, &opentype.FaceOptions{
		Size:    o.fontSize,
		DPI:     72,
		Hinting: font.HintingNone,
	})
	if err != nil {
		slog.Error("font face", "error", err)
	}
}

// --- Data setters ---

// SetAlbums updates the album list and rebuilds library-root navigation rows.
func (o *Overlay) SetAlbums(albums []player.Album, cursor int) {
	previousCursor := o.albumCursor
	listChanged := false
	// Virtual provider albums (modland/modarchive) are overlay-owned and
	// don't exist in the library — ignore them when detecting a rescan,
	// otherwise creating a ModArchive album resets the nav stack to root.
	prev := realAlbumsOnly(o.allAlbums)
	next := realAlbumsOnly(albums)
	listChanged = len(prev) != len(next)
	if !listChanged {
		for i := range next {
			if prev[i].Name != next[i].Name || prev[i].Path != next[i].Path {
				listChanged = true
				break
			}
		}
	}
	if listChanged {
		o.allAlbums = albums
		o.rootEntries = o.buildRootEntries()
		// Library rescanned — any drill-down position is now stale.
		o.navStack = nil
	}
	// While the UI is open, the overlay cursor is independent of the
	// library's currently playing album. When hidden, keep it synchronized.
	// Only meaningful at the root level; a cursor deep in a modland
	// drill-down is left untouched.
	if (listChanged || !o.uiVisible) && len(o.navStack) == 0 {
		o.albumCursor = o.rootIndexOf(cursor)
	}
	if listChanged {
		o.refreshAlbumLabels()
	}
	cursorChanged := previousCursor != o.albumCursor
	if listChanged || cursorChanged {
		o.refreshPreview()
		o.albumsDirty = true
		if listChanged {
			o.albumsContentDirty = true
		}
	}
}

func (o *Overlay) SetTrackInfos(infos []player.TrackInfo, cursor int) {
	dataChanged := len(o.trackInfos) != len(infos)
	if !dataChanged {
		for i := range infos {
			if o.trackInfos[i] != infos[i] {
				dataChanged = true
				break
			}
		}
	}
	o.trackInfos = infos
	cursorChanged := o.trackCursor != cursor
	o.trackCursor = cursor
	if dataChanged || cursorChanged {
		o.tracksDirty = true
		if dataChanged {
			o.tracksContentDirty = true
		}
	}
}

// SetPlayback updates playback state.
func (o *Overlay) SetPlayback(pos, dur float64, sr float32, bitrate, bpm float64, ch int, paused, isTracker bool) {
	if o.position != pos || o.duration != dur || o.paused != paused ||
		o.sampleRate != sr || o.bitrate != bitrate || o.bpm != bpm || o.channels != ch || o.isTracker != isTracker {
		o.bottomDirty = true
	}
	o.position = pos
	o.duration = dur
	o.sampleRate = sr
	o.bitrate = bitrate
	o.bpm = bpm
	o.channels = ch
	o.paused = paused
	o.isTracker = isTracker
}

// SetPlaying identifies the currently playing album and track.
func (o *Overlay) SetPlaying(album, track string) {
	if o.playingAlbum != album || o.playingTrack != track {
		o.playingAlbum = album
		o.playingTrack = track
		o.albumsDirty = true
		o.albumsContentDirty = true
		o.tracksDirty = true
		o.tracksContentDirty = true
		o.bottomDirty = true
	}
}

// SetLoading updates the async-load indicator.
func (o *Overlay) SetLoading(active bool, percent int64) {
	if o.loading != active || o.loadPercent != percent {
		o.loading = active
		o.loadPercent = percent
		o.bottomDirty = true
	}
}

func (o *Overlay) SetStats(line string) {
	if o.statsLine == line {
		return
	}
	o.statsLine = line
	o.statsDirty = true
}

func (o *Overlay) SetPresetName(name string) {
	if o.presetName == name {
		return
	}
	o.presetName = name
	o.presetNameDirty = true
}

// SetOnline updates the connectivity flag. When transitioning true, rebuilds
// root entries so remote catalogs (Modland/ModArchive) appear in the nav list.
func (o *Overlay) SetOnline(v bool) {
	if o.online == v {
		return
	}
	o.online = v
	if v && len(o.navStack) == 0 {
		o.rootEntries = o.buildRootEntries()
		o.syncPanels()
	}
}

func (o *Overlay) SettingsRows() []SettingRow { return o.settingsRows }
