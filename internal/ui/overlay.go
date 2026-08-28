// Package ui injects text into the visualizer's frame feedback.
package ui

import (
	"log/slog"
	"math"
	"time"

	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/filesystem"
	"github.com/dendec/glitchscope/internal/modarchive"
	"github.com/dendec/glitchscope/internal/player"
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
	PagePresets
	PageSettings
	PageHelp
)

type HelpTopicID int

const (
	HelpQuickStart HelpTopicID = iota
	HelpFormats
	HelpCatalogs
	HelpDevice
	HelpAbout
	HelpLicenses
)

type HelpTopic struct {
	ID       HelpTopicID
	Title    string
	Lines    []string
	Children []HelpEntry
}

type HelpEntry struct {
	Title    string
	Lines    []string
	Children []HelpEntry `json:"children,omitempty"`
}

type HelpViewState struct {
	TopicCursor      int
	EntryCursor      int
	EntryTop         int
	ContentTop       int
	InChildren       bool
	InGrandChildren  bool
	GrandChildCursor int
	GrandChildTop    int
}

// SettingRow describes one line in the settings page.
type SettingRow struct {
	Label  string
	Values []string
	Index  int
}

// navCtx selects the navigation model for a stack level.
type navCtx int

const (
	ctxSourceRoot navCtx = iota // virtual source root
	ctxNC                       // NC-local filesystem browser (dirPath set)
	ctxCatalog                  // remote catalog level: formats / albums / modarchive dirs
	ctxMicrophone               // SDL capture-device selection
)

type sourceKind int

const (
	sourceMusic sourceKind = iota
	sourceMicrophone
	sourceModland
	sourceModArchive
)

// ncRightPanel tracks focus within the NC right panel.
type ncRightPanel int

const (
	ncRightInfo   ncRightPanel = iota // info area focused
	ncRightPlay                       // Play button focused
	ncRightDelete                     // Delete button focused
)

// Overlay manages UI and notification rendering.
type Overlay struct {
	controllerConnected bool
	programText         uint32
	programImage        uint32
	programRect         uint32
	face                font.Face

	notif Notifier

	uiVisible    bool
	panelEntered bool
	showFPS      bool
	uiPage       UIPage

	screenW, screenH int
	fontSize         float64
	baseDir          string
	musicDir         string // resolved local music root, may differ from baseDir/"music"
	source           sourceKind

	libAlbums       func() []player.Album  // reads lib.Albums — snapshot cached once per frame
	addCatalogAlbum func(player.Album) int // adds on-the-fly catalog album to lib, returns stable index
	cachedAlbums    []player.Album         // one-frame snapshot of libAlbums(); never mutated by the overlay
	navStack        []navLevel
	albumEntries    []navEntry
	albums          []string
	albumCursor     int
	trackInfos      []player.TrackInfo
	trackCursor     int
	statsLine       string
	position        float64
	duration        float64
	sampleRate      float32
	bitrate         float64
	bpm             float64
	channels        int
	paused          bool
	isTracker       bool
	presetName      string
	playingAlbum    string // display album name (from Library) for left-panel highlight
	playingPath     string // full path of the currently playing track
	loading         bool
	loadPercent     int64
	focusPanel      int // 0=albums, 1=tracks

	settingsRows        []SettingRow
	settingsCursor      int
	settingsValueCursor int
	settingsEditing     bool
	settingsDirty       bool
	settingsColL        listTex
	settingsColR        listTex
	helpView            HelpViewState
	helpColL            listTex
	helpColR            listTex
	helpDirty           bool
	helpVisibleRows     int

	presetTreeRoot   []presetNode
	presetNav        presetNavigation
	presetMeta       presetMetaProvider
	presetPreviewReq func(key string)
	presetPreviewTex func(key string) (tex uint32, w, h int, ok bool)
	presetPreviewFPS func() float64
	presetPreviewDue time.Time
	presetDetailKey  string
	presetsColL      listTex
	presetsColR      listTex

	scrollUp    scrollHold
	scrollDown  scrollHold
	scrollLeft  scrollHold
	scrollRight scrollHold

	albumsScroll int
	tracksScroll int

	ncRight           ncRightPanel // which NC right-panel area is focused
	ncConfirm         bool         // delete confirm dialog active
	ncDeleteConfirmed bool         // delete was just confirmed (one-shot)
	ncInfoFile        string       // selected file path for right-panel info
	ncInfoDir         string       // selected dir path for right-panel info
	ncInfoIsDir       bool         // selected entry is a directory
	ncInfoScroll      int          // vertical scroll offset for right-panel info
	ncInfoLines       int          // total rendered lines in right-panel info
	ncInfoVisible     int          // visible lines in right-panel info
	ncListingStatus   filesystem.Status

	theme        config.Theme
	transparency float32

	marqueeL          marqueeState
	marqueeR          marqueeState
	infoMarquee       marqueeState
	statsMarquee      marqueeState
	breadcrumbMarquee marqueeState
	presetNameMarquee marqueeState
	bottomMarquee     marqueeState

	albumsTex                          uint32
	albumsTexW, albumsTexH             int
	tracksTex                          uint32
	tracksTexW, tracksTexH             int
	bottomTex                          uint32
	bottomTexW, bottomTexH             int
	bottomPrefixTex                    uint32
	bottomPrefixTexW, bottomPrefixTexH int
	bottomSuffixTex                    uint32
	bottomSuffixTexW, bottomSuffixTexH int
	bottomTitleX, bottomTitleW         int
	statsTex                           uint32
	statsTexW, statsTexH               int
	presetNameTex                      uint32
	presetNameTexW, presetNameTexH     int

	breadcrumbTex                  uint32
	breadcrumbTexW, breadcrumbTexH int
	breadcrumbTextCache            string
	breadcrumbDirty                bool

	hintTex            uint32
	hintTexW, hintTexH int
	hintTextCache      string

	pageIndicatorTex   [4]uint32
	pageIndicatorTexW  [4]int
	pageIndicatorTexH  [4]int
	pageIndicatorDirty bool
	textureCacheReady  bool

	albumsDirty         bool
	albumsContentDirty  bool
	tracksDirty         bool
	tracksContentDirty  bool
	statsDirty          bool
	bottomDirty         bool
	presetNameDirty     bool
	presetsDirty        bool
	presetCursorDirty   bool
	presetsDetailDirty  bool
	presetsRightRows    int // cached right panel row count for thumbnail draw
	presetPreviewFPSNow int // rounded value currently baked into the detail texture
	online              bool
	micActive           bool // microphone capture is running
	micDevices          []string
	micMenuRequested    bool   // one-shot: microphone source selected
	micDeviceSelected   string // one-shot: selected SDL capture device name
	micStopRequested    bool   // one-shot: stop capture selected
	closeInjectPending  bool
	modArchiveItems     map[string][]modarchive.DirItem
	deviceInfo          *DeviceInfo
}

// New creates an Overlay. The stack always has a virtual source root.
func New() *Overlay {
	o := &Overlay{
		programText:     glCreateTextProgram(),
		programImage:    glCreateImageProgram(),
		programRect:     glCreateRectProgram(),
		modArchiveItems: make(map[string][]modarchive.DirItem),
	}
	o.navStack = []navLevel{{ctx: ctxSourceRoot, entries: o.buildSourceEntries()}}
	o.albumEntries = o.navStack[0].entries
	o.albums = labelsOf(o.albumEntries)
	return o
}

func (o *Overlay) Close() {
	o.notif.Hide()
	o.deleteTex(&o.albumsTex)
	o.deleteTex(&o.tracksTex)
	o.deleteTex(&o.bottomTex)
	o.deleteTex(&o.bottomPrefixTex)
	o.deleteTex(&o.bottomSuffixTex)
	o.deleteTex(&o.statsTex)
	o.deleteTex(&o.presetNameTex)
	o.deleteTex(&o.breadcrumbTex)
	o.deleteTex(&o.hintTex)
	o.deleteTex(&o.settingsColL.tex)
	o.deleteTex(&o.settingsColR.tex)
	o.deleteTex(&o.presetsColL.tex)
	o.deleteTex(&o.presetsColR.tex)
	o.deleteTex(&o.helpColL.tex)
	o.deleteTex(&o.helpColR.tex)
	o.marqueeL.invalidate(o)
	o.marqueeR.invalidate(o)
	o.statsMarquee.invalidate(o)
	o.breadcrumbMarquee.invalidate(o)
	o.presetNameMarquee.invalidate(o)
	o.bottomMarquee.invalidate(o)
	for i := range o.pageIndicatorTex {
		o.deleteTex(&o.pageIndicatorTex[i])
	}
	glDeleteProgram(o.programText)
	o.programText = 0
	glDeleteProgram(o.programImage)
	o.programImage = 0
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

// SetControllerConnected updates the active Help control mapping.
func (o *Overlay) SetControllerConnected(connected bool) {
	if o.controllerConnected == connected {
		return
	}
	o.controllerConnected = connected
	o.helpDirty = true
}

// SetMusicDir records the resolved local music root (see App.findMusicDir),
// used by NC navigation instead of assuming baseDir/"music" exists.
func (o *Overlay) SetMusicDir(dir string) {
	o.musicDir = dir
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
	o.presetsDetailDirty = true
	o.helpDirty = true
	o.pageIndicatorDirty = true
	o.breadcrumbDirty = true
	// The hint-footer texture must be forced to rebuild on font size / theme
	// / resize changes, even if its text string is unchanged.
	o.hintTextCache = ""
	o.marqueeL.invalidate(o)
	o.marqueeR.invalidate(o)
	o.statsMarquee.invalidate(o)
	o.breadcrumbMarquee.invalidate(o)
	o.presetNameMarquee.invalidate(o)
	o.bottomMarquee.invalidate(o)
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

// SetDeviceInfo stores device info for the Device help page.
func (o *Overlay) SetDeviceInfo(info *DeviceInfo) {
	o.deviceInfo = info
	o.helpDirty = true
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

// HandleRescan detects local album changes and triggers the appropriate
// navigation reset. Call from the presenter each frame with the current
// and previous local album lists.
func (o *Overlay) HandleRescan(prevLocal, nextLocal []player.Album) {
	if o.isNC() {
		return
	}
	listChanged := len(prevLocal) != len(nextLocal)
	if !listChanged {
		for i := range nextLocal {
			if prevLocal[i].Name != nextLocal[i].Name || prevLocal[i].Path != nextLocal[i].Path {
				listChanged = true
				break
			}
		}
	}
	if listChanged {
		atSourceRoot := o.topLevel().ctx == ctxSourceRoot
		if atSourceRoot {
			o.navStack[0].entries = o.buildSourceEntries()
			o.albumEntries = o.navStack[0].entries
			o.albums = labelsOf(o.albumEntries)
		} else {
			// A local rescan invalidates the current navigation data. Restart
			// the filesystem browser at the configured music root.
			o.switchToNC(o.musicDir)
		}
		o.refreshAlbumLabels()
		o.albumsDirty = true
		o.albumsContentDirty = true
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

// SetPlayingInfo updates the currently playing album name (for left-panel
// highlight) and track path (for navigation, right-panel highlight, bottom
// bar). Single setter — keeps both in sync.
func (o *Overlay) SetPlayingInfo(album, path string) {
	changed := o.playingAlbum != album || o.playingPath != path
	o.playingAlbum = album
	o.playingPath = path
	if changed {
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
	// Auto-follow: when preset changes on the Presets page, sync cursor.
	if o.uiPage == PagePresets {
		o.syncPresetTree()
	}
}

// SetOnline updates the connectivity flag and refreshes the virtual source root.
func (o *Overlay) SetOnline(v bool) {
	if o.online == v {
		return
	}
	o.online = v
	slog.Info("overlay connectivity changed", "online", v, "nc_dir", o.ncDir(), "base_dir", o.baseDir)
	o.refreshSourceRoot()
}

// SetLibAlbums registers a callback that returns the current lib.Albums list.
// Must be called before any catalog navigation. The result is snapshotted
// into cachedAlbums (refreshed once per frame in Update and on every
// mutation) so all consumers within a frame see the same list.
func (o *Overlay) SetLibAlbums(fn func() []player.Album) {
	o.libAlbums = fn
	o.refreshAlbumsCache()
}

// SetAddCatalogAlbum registers the callback that delegates on-the-fly catalog
// album creation to the library. Must be called before any catalog navigation.
// The callback is wrapped so the per-frame album snapshot is refreshed as soon
// as the album is added — the same navigation step reads it back.
func (o *Overlay) SetAddCatalogAlbum(fn func(player.Album) int) {
	o.addCatalogAlbum = func(album player.Album) int {
		idx := fn(album)
		o.refreshAlbumsCache()
		return idx
	}
}

func (o *Overlay) SettingsRows() []SettingRow { return o.settingsRows }

// SetMicActive records microphone capture state and refreshes the source
// root label, mirroring SetOnline.
func (o *Overlay) SetMicActive(active bool) {
	if o.micActive == active {
		return
	}
	o.micActive = active
	o.refreshSourceRoot()
}

// SetMicDevices updates the available capture devices and refreshes the source root.
func (o *Overlay) SetMicDevices(devices []string) {
	o.micDevices = append(o.micDevices[:0], devices...)
	o.refreshSourceRoot()
}

// ConsumeMicMenuRequest reports and clears a request to list input devices.
func (o *Overlay) ConsumeMicMenuRequest() bool {
	if !o.micMenuRequested {
		return false
	}
	o.micMenuRequested = false
	return true
}

// ConsumeMicDeviceSelection reports and clears the selected SDL capture name.
func (o *Overlay) ConsumeMicDeviceSelection() (string, bool) {
	if o.micDeviceSelected == "" {
		return "", false
	}
	device := o.micDeviceSelected
	o.micDeviceSelected = ""
	return device, true
}

// ConsumeMicStopRequest reports and clears a request to stop capture.
func (o *Overlay) ConsumeMicStopRequest() bool {
	if !o.micStopRequested {
		return false
	}
	o.micStopRequested = false
	return true
}

// NCSync rebuilds NC entries for the current path after external changes
// (e.g. file deletion), preserving navigation stack and cursor position.
func (o *Overlay) NCSync() {
	if !o.isNC() {
		return
	}
	lvl := &o.navStack[len(o.navStack)-1]
	lvl.entries = withParentEntry(o.buildNCDirectoryEntries(lvl.dirPath))
	lvl.cursor = clampCursor(lvl.cursor, len(lvl.entries))
	o.albumEntries = lvl.entries
	o.albums = labelsOf(lvl.entries)
	o.albumCursor = lvl.cursor
	o.albumsDirty = true
	o.albumsContentDirty = true
	o.tracksDirty = true
	o.tracksContentDirty = true
	o.refreshNCPreview()
}
