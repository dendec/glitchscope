// Package ui injects text into the visualizer's frame feedback.
package ui

import (
	"context"
	"image"
	"image/color"
	"log/slog"
	"math"
	"reflect"
	"sync"
	"time"

	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/filesystem"
	"github.com/dendec/glitchscope/internal/i18n"
	"github.com/dendec/glitchscope/internal/modarchive"
	"github.com/dendec/glitchscope/internal/player"
	"github.com/dendec/glitchscope/internal/radio"
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
	HelpControls
	HelpPlayback
	HelpFrameRate
	HelpTroubleshooting
	HelpSources
	HelpFormats
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
	Header bool // non-selectable group separator, rendered bold
}

// navCtx selects the navigation model for a stack level.
type navCtx int

const (
	ctxSourceRoot navCtx = iota // virtual source root
	ctxNC                       // NC-local filesystem browser (dirPath set)
	ctxCatalog                  // remote catalog level: formats / albums / modarchive dirs
	ctxRadio                    // Radio Browser categories, filters, and stations
	ctxMicrophone               // SDL capture-device selection
	ctxFavorites                // favorites playlist level
)

type sourceKind int

const (
	sourceMusic sourceKind = iota
	sourceMicrophone
	sourceFavorites
	sourceDownloads
	sourceRadio
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
	catalog             i18n.Catalog
	menuHint            menuHint
	controllerConnected bool
	programText         uint32
	programImage        uint32
	programRect         uint32
	face                font.Face
	statsFace           font.Face

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
	isTrackCached   func(string) bool
	hasCachedUnder  func(string) bool
	cachedTracks    func() []string
	cachedAlbums    []player.Album // one-frame snapshot of libAlbums(); never mutated by the overlay
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

	// Local track info panel (non-nil when showing track info in right panel).
	infoLines []string

	settingsRows        []SettingRow
	settingsCursor      int
	settingsValueCursor int
	settingsEditing     bool
	settingsDirty       bool
	settingsColL        listTex
	settingsColR        listTex
	helpView            HelpViewState
	helpTopics          []HelpTopic
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
	previewBgTex     uint32 // thumbnail texture drawn full-screen as presets page background

	scrollUp    scrollHold
	scrollDown  scrollHold
	scrollLeft  scrollHold
	scrollRight scrollHold

	lastInteraction time.Time

	albumsScroll int
	tracksScroll int

	ncRight           ncRightPanel // which NC right-panel area is focused
	ncConfirm         bool         // delete confirm dialog active
	ncDeleteConfirmed bool         // delete was just confirmed (one-shot)
	fileMetadataPath  string
	fileMetadata      player.TrackInfo
	fileCover         *image.RGBA
	ncInfoFile        string // selected file path for right-panel info
	ncInfoDir         string // selected dir path for right-panel info
	ncInfoIsDir       bool   // selected entry is a directory
	ncInfoScroll      int    // vertical scroll offset for right-panel info
	ncInfoLines       int    // total rendered lines in right-panel info
	ncInfoVisible     int    // visible lines in right-panel info
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

	albumsTex                                uint32
	albumsTexW, albumsTexH                   int
	tracksTex                                uint32
	tracksTexW, tracksTexH                   int
	ncActionsTex                             uint32
	ncActionsTexW, ncActionsTexH             int
	coverArtTex                              uint32
	coverArtTexW, coverArtTexH               int
	coverArtPath                             string // file whose cover art is cached in coverArtTex
	radioFaviconTex                          uint32
	radioFaviconTexW, radioFaviconTexH       int
	radioFaviconTexPath                      string
	radioFaviconTexMaxW, radioFaviconTexMaxH int
	bottomTex                                uint32
	bottomTexW, bottomTexH                   int
	bottomPrefixTex                          uint32
	bottomPrefixTexW, bottomPrefixTexH       int
	bottomSuffixTex                          uint32
	bottomSuffixTexW, bottomSuffixTexH       int
	bottomTitleX, bottomTitleW               int
	statsTex                                 uint32
	statsTexW, statsTexH                     int
	presetNameTex                            uint32
	presetNameTexW, presetNameTexH           int

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
	actionPlacements   []actionPlacement
	iconTextures       map[string]uint32
	iconTextureSize    int
	iconTextureColor   color.RGBA

	albumsDirty          bool
	albumsContentDirty   bool
	tracksDirty          bool
	tracksContentDirty   bool
	statsDirty           bool
	bottomDirty          bool
	presetNameDirty      bool
	presetsDirty         bool
	presetCursorDirty    bool
	presetsDetailDirty   bool
	presetsRightRows     int // cached right panel row count for thumbnail draw
	presetPreviewFPSNow  int // rounded value currently baked into the detail texture
	online               bool
	micActive            bool // microphone capture is running
	micDevices           []string
	micMenuRequested     bool   // one-shot: microphone source selected
	micDeviceSelected    string // one-shot: selected SDL capture device name
	micStopRequested     bool   // one-shot: stop capture selected
	closeInjectPending   bool
	modArchiveItems      map[string][]modarchive.DirItem
	modArchiveCancel     context.CancelFunc
	modArchiveWG         sync.WaitGroup
	modArchiveResults    chan modArchiveResult
	modArchiveRequestID  uint64
	modArchivePendingURL string
	radioQueries         map[string][]radio.Station
	radioQueryMore       map[string]bool
	radioFavicons        map[string]*image.RGBA
	radioValues          map[radio.BrowseKind][]string
	radioValueCounts     map[radio.BrowseKind]map[string]int
	radioBrowseRequested bool
	radioPageRequested   bool
	radioPageKind        radio.BrowseKind
	radioPageFilter      string
	radioBrowseKind      radio.BrowseKind
	radioBrowseFilter    string
	radioSelected        *radio.Station
	radioNowPlayingPath  string
	radioNowPlayingTitle string
	favoritesView        FavoritesView
	deviceInfo           *DeviceInfo
	deviceInfoProvider   func() *DeviceInfo
	catalogInfoProvider  func() CatalogInfo
	catalogInfo          CatalogInfo
}

// New creates an Overlay. The stack always has a virtual source root.
func New() *Overlay {
	o := &Overlay{
		catalog:          i18n.MustLoad(i18n.English),
		helpTopics:       helpTopics,
		programText:      glCreateTextProgram(),
		programImage:     glCreateImageProgram(),
		programRect:      glCreateRectProgram(),
		modArchiveItems:  make(map[string][]modarchive.DirItem),
		radioQueries:     make(map[string][]radio.Station),
		radioQueryMore:   make(map[string]bool),
		radioFavicons:    make(map[string]*image.RGBA),
		radioValues:      make(map[radio.BrowseKind][]string),
		radioValueCounts: make(map[radio.BrowseKind]map[string]int),
	}
	o.navStack = []navLevel{{ctx: ctxSourceRoot, entries: o.buildSourceEntries()}}
	o.albumEntries = o.navStack[0].entries
	o.albums = labelsOf(o.albumEntries)
	return o
}

// SetLanguage replaces the immutable catalog and invalidates every text cache.
func (o *Overlay) SetLanguage(language config.Language) error {
	i18nLanguage := i18n.Language(language)
	catalog, err := i18n.Load(i18nLanguage)
	if err != nil {
		return err
	}
	helpAsset := helpData
	if i18nLanguage != i18n.English {
		helpAsset, err = i18n.HelpData(i18nLanguage)
		if err != nil {
			return err
		}
	}
	topics, err := parseHelpTopics(helpAsset)
	if err != nil {
		return err
	}
	o.catalog = catalog
	o.helpTopics = topics
	o.relocalizeSourceLabels()
	o.clampHelpView()
	o.markAllDirty()
	if o.notif.TextKey() != "" {
		o.notif.Relocalize(catalog, o.fontSize, o.textColor())
	}
	return nil
}

func (o *Overlay) clampHelpView() {
	if len(o.helpTopics) == 0 {
		o.helpView = HelpViewState{}
		return
	}
	o.helpView.TopicCursor = min(o.helpView.TopicCursor, len(o.helpTopics)-1)
	topic := o.helpTopics[o.helpView.TopicCursor]
	if len(topic.Children) == 0 {
		o.helpView.InChildren = false
		o.helpView.InGrandChildren = false
		o.helpView.EntryCursor = 0
		return
	}
	o.helpView.EntryCursor = min(o.helpView.EntryCursor, len(topic.Children)-1)
	entry := topic.Children[o.helpView.EntryCursor]
	if len(entry.Children) == 0 {
		o.helpView.InGrandChildren = false
		o.helpView.GrandChildCursor = 0
		return
	}
	o.helpView.GrandChildCursor = min(o.helpView.GrandChildCursor, len(entry.Children)-1)
}

// Catalog returns the current immutable translation catalog.
func (o *Overlay) Catalog() i18n.Catalog { return o.catalog }

func (o *Overlay) Close() {
	if o.modArchiveCancel != nil {
		o.modArchiveCancel()
	}
	o.modArchiveWG.Wait()
	o.deleteTex(&o.menuHint.texture.tex)
	o.notif.Hide()
	o.deleteTex(&o.albumsTex)
	o.deleteTex(&o.tracksTex)
	o.deleteTex(&o.ncActionsTex)
	o.deleteTex(&o.coverArtTex)
	o.deleteTex(&o.radioFaviconTex)
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
	o.deleteIconTextures()
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
	if o.statsFace != nil {
		_ = o.statsFace.Close()
	}
}

// Draw renders either the UI overlay or the notification.
// SetPreviewBackground stores the preview thumbnail texture to be drawn
// full-screen as background on the presets page. Pass 0 to clear.
func (o *Overlay) SetPreviewBackground(tex uint32) {
	o.previewBgTex = tex
}

func (o *Overlay) Draw(width, height int) {
	defer o.drawMenuHint(width, height)
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

// ShowMessage displays a localized notification.
func (o *Overlay) ShowMessage(key i18n.Key) {
	o.notif.ShowMessage(o.catalog, key, o.fontSize, o.textColor())
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
	oldCursor := o.settingsCursor
	oldValueCursor := o.settingsValueCursor
	oldEditing := o.settingsEditing
	var oldValue string
	oldValuesLen := 0
	if oldCursor >= 0 && oldCursor < len(o.settingsRows) {
		oldRow := o.settingsRows[oldCursor]
		oldValuesLen = len(oldRow.Values)
		valueCursor := oldRow.Index
		if oldEditing {
			valueCursor = oldValueCursor
		}
		if valueCursor >= 0 && valueCursor < len(oldRow.Values) {
			oldValue = oldRow.Values[valueCursor]
		}
	}
	o.settingsRows = rows
	// Skip header rows to land on the first selectable setting.
	for cursor < len(rows) && rows[cursor].Header {
		cursor++
	}
	if cursor >= len(rows) {
		cursor = 0
		for cursor < len(rows) && rows[cursor].Header {
			cursor++
		}
	}
	o.settingsCursor = cursor
	if oldEditing && oldCursor == cursor && cursor >= 0 && cursor < len(rows) && oldValue != "" {
		valueCursor := -1
		for i, value := range rows[cursor].Values {
			if value == oldValue {
				valueCursor = i
				break
			}
		}
		// Max carries the active refresh in its label, so its text changes
		// when the window moves between displays. It remains the last choice.
		if cursor == SettingFrameRate && oldValueCursor == oldValuesLen-1 {
			valueCursor = len(rows[cursor].Values) - 1
		}
		if cursor == SettingFrameRate && valueCursor < 0 && oldValueCursor >= len(rows[cursor].Values) {
			// A fixed cap that disappeared below a slower refresh now maps to
			// the display-bound Max choice.
			valueCursor = len(rows[cursor].Values) - 1
		}
		if valueCursor >= 0 && valueCursor < len(rows[cursor].Values) {
			o.settingsValueCursor = valueCursor
		}
	}
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

func (o *Overlay) IsLibraryPage() bool { return o.uiPage == PageLibrary }

func (o *Overlay) IsPresetsPage() bool { return o.uiPage == PagePresets }

// HasPlayableTrack reports whether the focused entry is a playable file.
func (o *Overlay) HasPlayableTrack() bool {
	e := o.currentEntry()
	if e == nil {
		return false
	}
	return e.IsNCFile() || e.IsCatalogTrack() || e.IsLeafAlbum() || e.IsFavoriteTrack() || e.kind == entryRadioStation
}

// SelectedTrackPath returns the file path of the focused entry, or "".
func (o *Overlay) SelectedTrackPath() string {
	e := o.currentEntry()
	if e == nil {
		return ""
	}
	if e.IsNCFile() || e.IsFavoriteTrack() || e.IsCatalogTrack() {
		return e.filePath
	}
	if e.kind == entryRadioStation {
		return e.radioStation.Path()
	}
	return ""
}

// FavoritesView is a read-only interface for displaying favorites.
type FavoritesView interface {
	GetPlaylist(path string) player.PlaylistID
	Tracks(id player.PlaylistID) []string
	Count(id player.PlaylistID) int
	TotalCount() int
}

// SetFavorites stores a read-only favorites view for display.
func (o *Overlay) SetFavorites(view FavoritesView) {
	o.favoritesView = view
	o.refreshSourceRoot()
}

// RefreshFavorites rebuilds the source root and open favourite playlist,
// then marks content dirty so the symbol appears immediately.
// RefreshFavorites rebuilds the source root and open favourite playlist,
// then marks content dirty so the symbol appears immediately.
func (o *Overlay) RefreshFavorites() {
	o.refreshSourceRoot()
	if o.topLevel().ctx == ctxFavorites && o.topLevel().playlistID != "" {
		o.rebuildCurrentFavoritePlaylist()
	}
	o.albumsDirty = true
	o.tracksDirty = true
}

func (o *Overlay) IsSettingsEditing() bool { return o.settingsEditing }

func (o *Overlay) markAllDirty() {
	o.menuHint.text = ""
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

// SetDeviceInfoProvider installs a lazy collector for the Device help topic.
// It is called at most once, on the render/input thread, and its result is cached.
func (o *Overlay) SetDeviceInfoProvider(provider func() *DeviceInfo) {
	o.deviceInfoProvider = provider
	o.deviceInfo = nil
	o.helpDirty = true
}

// SetCatalogInfoProvider installs a cheap snapshot provider for Help.
func (o *Overlay) SetCatalogInfoProvider(provider func() CatalogInfo) {
	o.catalogInfoProvider = provider
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
	if o.statsFace != nil {
		_ = o.statsFace.Close()
		o.statsFace = nil
	}
	o.face, err = opentype.NewFace(f, &opentype.FaceOptions{
		Size:    o.fontSize,
		DPI:     72,
		Hinting: font.HintingNone,
	})
	if err != nil {
		slog.Error("font face", "error", err)
		return
	}
	o.statsFace, err = opentype.NewFace(f, &opentype.FaceOptions{
		Size:    statsFontSize(o.fontSize),
		DPI:     72,
		Hinting: font.HintingNone,
	})
	if err != nil {
		slog.Error("stats font face", "error", err)
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
			if !reflect.DeepEqual(o.trackInfos[i], infos[i]) {
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
	if o.source == sourceModland || o.source == sourceModArchive {
		o.switchToProvider(o.source)
		return
	}
	o.refreshSourceRoot()
}

// SetRadioValues installs cached filter values and refreshes the open Radio
// filter level. It is called by the app after a background directory request.
func (o *Overlay) SetRadioValues(kind radio.BrowseKind, values []string, counts map[string]int) {
	if o.radioValues == nil {
		o.radioValues = make(map[radio.BrowseKind][]string)
	}
	if o.radioValueCounts == nil {
		o.radioValueCounts = make(map[radio.BrowseKind]map[string]int)
	}
	o.radioValues[kind] = append([]string(nil), values...)
	o.radioValueCounts[kind] = copyRadioCounts(counts)
	if o.topLevel().ctx == ctxRadio && o.topLevel().radioKind == kind && o.topLevel().radioFilter == "" {
		o.topLevel().entries = withParentEntry(o.buildRadioFilterEntries(kind))
		o.refreshAlbumLabels()
		o.albumCursor = clampCursor(o.albumCursor, len(o.albumEntries))
		o.syncPanels()
	}
}

// SetRadioStations installs a cached station listing and refreshes the open
// station level when it corresponds to this query.
func (o *Overlay) SetRadioStations(kind radio.BrowseKind, filter string, stations []radio.Station, hasMore bool) {
	if o.radioQueries == nil {
		o.radioQueries = make(map[string][]radio.Station)
	}
	if o.radioQueryMore == nil {
		o.radioQueryMore = make(map[string]bool)
	}
	key := radioQueryKey(kind, filter)
	o.radioQueries[key] = append([]radio.Station(nil), stations...)
	o.radioQueryMore[key] = hasMore
	if o.topLevel().ctx == ctxRadio && o.topLevel().radioKind == kind && o.topLevel().radioFilter == filter {
		o.topLevel().entries = withParentEntry(o.buildRadioStationEntries(kind, filter))
		o.refreshAlbumLabels()
		o.albumCursor = clampCursor(o.albumCursor, len(o.albumEntries))
		o.syncPanels()
	}
}

// SetRadioError replaces an empty loading view with the same unavailable
// state used elsewhere in the UI. Existing cached rows remain usable.
func (o *Overlay) SetRadioError(kind radio.BrowseKind, filter string) {
	if o.topLevel().ctx != ctxRadio || o.topLevel().radioKind != kind || o.topLevel().radioFilter != filter {
		return
	}
	key := radioQueryKey(kind, filter)
	if filter == "" && (kind == radio.BrowseTag || kind == radio.BrowseLanguage || kind == radio.BrowseCountry) {
		if len(o.radioValues[kind]) > 0 {
			return
		}
	} else if len(o.radioQueries[key]) > 0 {
		return
	}
	o.topLevel().entries = withParentEntry([]navEntry{{label: o.catalog.Text(i18n.ValueUnavailable), kind: entryInfo}})
	o.refreshAlbumLabels()
	o.albumCursor = clampCursor(o.albumCursor, len(o.albumEntries))
	o.syncPanels()
}

// SetRadioNowPlaying updates the live ICY title for one station. Keying the
// title by station prevents it appearing on another selected station.
func (o *Overlay) SetRadioNowPlaying(path, title string) {
	if o.radioNowPlayingPath == path && o.radioNowPlayingTitle == title {
		return
	}
	o.radioNowPlayingPath = path
	o.radioNowPlayingTitle = title
	if o.playingPath == path {
		o.bottomDirty = true
	}
	if o.source == sourceRadio {
		o.tracksDirty = true
		o.tracksContentDirty = true
	}
}

// SetRadioFavicon takes ownership of a prepared bitmap; upload stays on the GL thread.
func (o *Overlay) SetRadioFavicon(path string, data *image.RGBA) {
	if path == "" || data == nil {
		return
	}
	if o.radioFavicons == nil {
		o.radioFavicons = make(map[string]*image.RGBA)
	}
	if len(o.radioFavicons) >= 32 {
		for key := range o.radioFavicons {
			delete(o.radioFavicons, key)
			break
		}
	}
	o.radioFavicons[path] = data
	if path == o.radioFaviconTexPath {
		o.radioFaviconTexPath = ""
	}
	o.tracksDirty = true
	o.tracksContentDirty = true
}

// ConsumeRadioBrowseRequest returns a one-shot request for a background
// Radio Browser query.
func (o *Overlay) ConsumeRadioBrowseRequest() (radio.BrowseKind, string, bool) {
	if !o.radioBrowseRequested {
		return "", "", false
	}
	kind, filter := o.radioBrowseKind, o.radioBrowseFilter
	o.radioBrowseRequested = false
	return kind, filter, true
}

// ConsumeRadioPageRequest returns a one-shot request for the next station page.
func (o *Overlay) ConsumeRadioPageRequest() (radio.BrowseKind, string, bool) {
	if !o.radioPageRequested {
		return "", "", false
	}
	kind, filter := o.radioPageKind, o.radioPageFilter
	o.radioPageRequested = false
	o.radioPageKind, o.radioPageFilter = "", ""
	if len(o.navStack) == 0 || o.topLevel().ctx != ctxRadio || o.topLevel().radioKind != kind || o.topLevel().radioFilter != filter {
		return "", "", false
	}
	return kind, filter, true
}

func copyRadioCounts(counts map[string]int) map[string]int {
	if len(counts) == 0 {
		return nil
	}
	copy := make(map[string]int, len(counts))
	for key, value := range counts {
		copy[key] = value
	}
	return copy
}

// ConsumeRadioStationSelection returns a one-shot station chosen by the user,
// its visible listing, and the browse context that owns that listing.
func (o *Overlay) ConsumeRadioStationSelection() (radio.Station, []radio.Station, radio.BrowseKind, string, bool) {
	if o.radioSelected == nil {
		return radio.Station{}, nil, "", "", false
	}
	station := *o.radioSelected
	o.radioSelected = nil
	level := o.topLevel()
	stations := append([]radio.Station(nil), o.radioQueries[radioQueryKey(level.radioKind, level.radioFilter)]...)
	if len(stations) == 0 {
		stations = []radio.Station{station}
	}
	return station, stations, level.radioKind, level.radioFilter, true
}

// SetTrackCacheLookup supplies the read-only cache projection used by offline navigation.
func (o *Overlay) SetTrackCacheLookup(isCached, hasDescendant func(string) bool, cachedTracks func() []string) {
	o.isTrackCached = isCached
	o.hasCachedUnder = hasDescendant
	o.cachedTracks = cachedTracks
	o.refreshSourceRoot()
}

// RefreshTrackCache redraws cache-dependent actions and rebuilds an offline
// provider tree so newly empty folders disappear immediately.
func (o *Overlay) RefreshTrackCache() {
	if o.source == sourceDownloads || (!o.online && (o.source == sourceModland || o.source == sourceModArchive)) {
		o.switchToProvider(o.source)
	}
	o.markAllDirty()
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

// MetadataFilePath identifies the visible file whose details need background I/O.
func (o *Overlay) MetadataFilePath() string {
	if !o.UIVisible() {
		return ""
	}
	if len(o.navStack) > 0 {
		if o.isNC() {
			return o.ncInfoFile
		}
		e := o.currentEntry()
		if e == nil {
			return ""
		}
		if e.kind == entryFavoriteTrack {
			return e.filePath
		}
		albums := o.currentAlbums()
		if e.IsCatalogTrack() && e.albumIdx >= 0 && e.albumIdx < len(albums) && e.trackIdx >= 0 && e.trackIdx < len(albums[e.albumIdx].Tracks) {
			return albums[e.albumIdx].Tracks[e.trackIdx]
		}
		return ""
	}
	if o.trackCursor >= 0 && o.trackCursor < len(o.trackInfos) {
		return o.trackInfos[o.trackCursor].Path
	}
	return ""
}

// SetFileMetadata accepts only the current selection. GL upload remains in Draw.
func (o *Overlay) SetFileMetadata(path string, info player.TrackInfo, cover *image.RGBA) {
	if path != o.MetadataFilePath() {
		return
	}
	o.fileMetadataPath, o.fileMetadata, o.fileCover = path, info, cover
	o.coverArtPath = ""
	o.tracksDirty, o.tracksContentDirty = true, true
}
