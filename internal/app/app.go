// Package app wires the application — initialization, main loop, and benchmark.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dendec/glitchscope/internal/archive"
	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/filesystem"
	"github.com/dendec/glitchscope/internal/input"
	"github.com/dendec/glitchscope/internal/mic"
	"github.com/dendec/glitchscope/internal/modarchive"
	"github.com/dendec/glitchscope/internal/modland"
	"github.com/dendec/glitchscope/internal/player"
	"github.com/dendec/glitchscope/internal/presets"
	"github.com/dendec/glitchscope/internal/prof"
	"github.com/dendec/glitchscope/internal/projectm"
	"github.com/dendec/glitchscope/internal/ui"
	"github.com/dendec/glitchscope/internal/util"
	"github.com/veandco/go-sdl2/sdl"
)

type App struct {
	window  *sdl.Window
	glCtx   sdl.GLContext
	pm      *projectm.Handle
	rt      *projectm.RenderTarget
	preview *previewRenderer

	overlay *ui.Overlay
	inp     *input.Input
	playbackState
	presenter overlayPresenter

	mic *mic.Capture // active microphone capture, nil when off

	modlandSizes map[string]int64 // remote path → expected size for downloads

	// Provider shuffle-index handles, owned here for lifecycle (Close).
	// The coordinator built from them lives on playbackState (shuffleCatalog
	// field) since that's where lazy Source/All selection happens.
	modlandShuffleSrc    *modland.ShuffleSource
	modarchiveShuffleSrc *modarchive.ShuffleSource
	localShuffleSrc      *player.LocalShuffleSource
	localScanFingerprint string // last fingerprint from a successful (OK) scan

	prof              *prof.Collector
	settings          *config.Settings
	settingsPath      string
	textureDir        string
	texturesOnce      sync.Once // ensures textures are extracted at most once
	connectivityWg    sync.WaitGroup
	shuffleWg         sync.WaitGroup // tracks background shuffle build goroutine
	presetNames       []string
	presetIdx         int
	presetCats        []string // all preset keys for presets page tree
	transitionPresets []string // "!"-prefixed presets for smooth transitions

	startupFile string

	adaptive         resolutionState
	presetTuning     presetTuning
	presentRequested bool
	nextPresent      time.Time
	vizClock         visualizerClock // visualizer frame clock, promoted from runState

	// appCtx/appCancel govern background work tied to the app lifetime.
	// Cancelled in Close() so in-flight goroutines (e.g. connectivity check)
	// stop promptly instead of firing after teardown.
	appCtx    context.Context
	appCancel context.CancelFunc

	pending          pendingPreset // pending preset name + scheduled load time
	adaptiveResumeAt time.Time
	presetTicker     *time.Ticker
	resumePath       string
	resumeSeconds    float64
	resumeAttempted  bool
	online           atomic.Bool
	quit             atomic.Bool
	presetSwitch     atomic.Bool

	deleteSvc *deleteService

	favorites     *player.Favorites
	favoritesPath string

	seek           seekControl // continuous-seek drivetrain state
	onPresetsPage  bool        // true when UI is on Presets page (main viz stopped)
	selectedPreset string      // confirmed on Presets page, loaded when the page closes
	testSignalFreq float64     // phase accumulator for synthetic test signal
	testSignalBuf  []float32   // reusable buffer for test signal (avoids alloc per frame)
}

// New creates an App with display initialised. Player/overlay/input/library
// are created later by Init().
func New(fullscreen bool, width, height int, startupFile string) (*App, error) {
	a := &App{
		prof:         prof.NewCollector(),
		settingsPath: config.SettingsPath(),
		startupFile:  startupFile,
		modlandSizes: make(map[string]int64),
		presenter:    newOverlayPresenter(nil),
	}
	a.playbackState.shuffle.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	a.appCtx, a.appCancel = context.WithCancel(context.Background())

	if err := sdl.Init(sdl.INIT_VIDEO | sdl.INIT_EVENTS | sdl.INIT_GAMECONTROLLER | sdl.INIT_JOYSTICK | sdl.INIT_AUDIO); err != nil {
		return nil, fmt.Errorf("sdl init: %w", err)
	}

	winFlags := uint32(sdl.WINDOW_OPENGL | sdl.WINDOW_SHOWN | sdl.WINDOW_RESIZABLE)
	if fullscreen {
		winFlags |= sdl.WINDOW_FULLSCREEN
	}
	win, err := sdl.CreateWindow("Portable Music Visualizer",
		sdl.WINDOWPOS_UNDEFINED, sdl.WINDOWPOS_UNDEFINED,
		int32(width), int32(height), winFlags)
	if err != nil {
		sdl.Quit()
		return nil, fmt.Errorf("window: %w", err)
	}
	a.window = win
	// On Linux, input methods (ibus/fcitx) consume letter-key KEYDOWN
	// events and only emit TEXTINPUT. Disable text input so all keypresses
	// arrive as KEYDOWN — needed for n/p/m/q/r/b bindings.
	sdl.StopTextInput()

	glCtx, err := win.GLCreateContext()
	if err != nil {
		_ = win.Destroy()
		sdl.Quit()
		return nil, fmt.Errorf("gl context: %w", err)
	}
	a.glCtx = glCtx
	_ = sdl.GLSetSwapInterval(0)

	t0 := time.Now()
	pm, err := projectm.Create()
	if err != nil {
		sdl.GLDeleteContext(glCtx)
		_ = win.Destroy()
		sdl.Quit()
		return nil, fmt.Errorf("projectm init: %w", err)
	}
	a.pm = pm
	a.pm.SetSoftCutDuration(softCutDuration.Seconds())
	slog.Info("projectM init", "ms", time.Since(t0).Milliseconds())

	// Texture extraction is deferred to the first render frame (ensureTextures)
	// to avoid blocking startup. projectM uses built-in textures until then.

	w, h := win.GLGetDrawableSize()

	gs, err := config.LoadSettings(a.settingsPath)
	if err != nil {
		slog.Warn("settings load", "error", err)
		gs = config.DefaultSettings()
	}

	resolutions := config.ComputeResolutions(int(w), int(h))
	var renderW, renderH int
	switch {
	case gs.Graphics.Adaptive && len(resolutions) > 0:
		a.adaptive.Reset(int(w), int(h), gs.Graphics.PerformanceMode.Params())
		renderW, renderH = a.adaptive.resolutions[a.adaptive.index].Width, a.adaptive.resolutions[a.adaptive.index].Height
	case gs.Graphics.Adaptive:
		slog.Warn("adaptive: empty resolution list at startup, using saved size",
			"window", fmt.Sprintf("%dx%d", int(w), int(h)))
		savedRes := config.RenderResolution{Width: gs.Graphics.RenderWidth, Height: gs.Graphics.RenderHeight}
		target := config.ClosestResolution(resolutions, savedRes)
		renderW, renderH = target.Width, target.Height
	default:
		savedRes := config.RenderResolution{Width: gs.Graphics.RenderWidth, Height: gs.Graphics.RenderHeight}
		target := config.ClosestResolution(resolutions, savedRes)
		renderW, renderH = target.Width, target.Height
	}
	gs.Graphics.RenderWidth, gs.Graphics.RenderHeight = renderW, renderH

	pm.SetWindowSize(renderW, renderH)
	rt := projectm.NewRenderTarget(renderW, renderH)
	rt.SetNearest(gs.Graphics.UpscaleFilter.IsNearest())
	a.rt = rt
	a.settings = &gs
	a.pm.SetBeatSensitivity(gs.Graphics.BeatSensitivity)
	a.pm.SetHardCutEnabled(!gs.Graphics.VisualizerOff && gs.PresetInterval == config.PresetAuto)
	a.pm.SetPresetSwitchRequestedHandler(func(bool) {
		if a.settings.PresetInterval == config.PresetAuto {
			a.presetSwitch.Store(true)
		}
	})

	slog.Info("render size", "window", fmt.Sprintf("%dx%d", int(w), int(h)),
		"internal", fmt.Sprintf("%dx%d", renderW, renderH))

	if err := presets.Open(presetDirPath()); err != nil {
		slog.Warn("presets dir not found", "error", err)
	}
	a.presetNames = presets.Names()

	return a, nil
}

func (a *App) Close() {
	// Wait for background shuffle build to finish before tearing down
	// resources it may be using (shuffle sources, baseDir, etc.).
	a.shuffleWg.Wait()
	// Abort background work (e.g. the connectivity probe) before tearing down
	// resources it may observe, so no in-flight goroutine touches a freed overlay.
	if a.appCancel != nil {
		a.appCancel()
	}
	a.connectivityWg.Wait()
	modarchive.CloseSnapshotCatalog()
	if a.modlandShuffleSrc != nil {
		_ = a.modlandShuffleSrc.Close()
	}
	if a.modarchiveShuffleSrc != nil {
		_ = a.modarchiveShuffleSrc.Close()
	}
	a.savePlaybackPosition()
	if a.pm != nil {
		a.pm.SetPresetSwitchRequestedHandler(nil)
	}
	if a.presetTicker != nil {
		a.presetTicker.Stop()
	}
	if a.preview != nil {
		a.preview.Destroy()
	}
	if a.rt != nil {
		a.rt.Destroy()
	}
	if a.overlay != nil {
		a.overlay.Close()
	}
	if a.pl != nil {
		a.pl.Close()
	}
	if a.mic != nil {
		a.mic.Close()
		a.mic = nil
	}
	if a.inp != nil {
		a.inp.Close()
	}
	if a.pm != nil {
		a.pm.Destroy()
	}
	if a.textureDir != "" {
		_ = os.RemoveAll(a.textureDir)
	}
	if a.glCtx != nil {
		sdl.GLDeleteContext(a.glCtx)
	}
	if a.window != nil {
		_ = a.window.Destroy()
	}
	sdl.Quit()
}

// previewFeedPCM forwards the current audio signal to the preview instance.
func (a *App) previewFeedPCM(wave []float32) {
	if a.preview != nil && a.preview.pm != nil {
		a.preview.pm.PCMAddFloat(wave, projectm.Mono)
	}
}

// testSignalSize is the number of samples per test signal buffer.
const testSignalSize = 512

// readAudio returns the current audio wave from mic or player, or nil.
func (a *App) readAudio() []float32 {
	if a.mic != nil {
		return a.mic.Read()
	}
	if a.pl != nil {
		return a.pl.GetWave()
	}
	return nil
}

// testSignal produces a harmonic signal for preview when audio is unavailable.
// Reuses a pre-allocated buffer to avoid per-frame allocation.
func (a *App) testSignal() []float32 {
	if cap(a.testSignalBuf) < testSignalSize {
		a.testSignalBuf = make([]float32, testSignalSize)
	}
	wave := a.testSignalBuf[:testSignalSize]
	const (
		base  = 110.0 // Hz
		amp   = 0.22
		twoPi = 2 * 3.141592653589793
		sr    = 44100.0
	)
	for i := range wave {
		t := a.testSignalFreq / sr
		wave[i] = float32(amp * (math.Sin(twoPi*base*t) +
			0.65*math.Sin(twoPi*base*2*t) +
			0.35*math.Sin(twoPi*base*4*t)))
		a.testSignalFreq++
	}
	return wave
}

func (a *App) Init() {
	a.initAudio()
	a.initLibrary()
	a.initFavorites()
	a.deleteSvc = newDeleteService(baseDir())
	if a.deleteSvc.baseErr != nil {
		slog.Error("delete service unavailable", "error", a.deleteSvc.baseErr)
	}
	a.checkConnectivity()
	a.initInput()
	a.initPreset()
	if a.startupFile != "" && a.pl != nil {
		a.playTrack(a.startupFile, "command line")
	} else if a.startupFile != "" {
		slog.Warn("startup file skipped, audio unavailable", "path", a.startupFile)
	} else {
		a.restoreSavedPosition(false)
	}
	a.startPresetTicker()
	if a.overlay != nil {
		a.overlay.SetTheme(a.settings.UI.Theme, int(a.settings.UI.Transparency))
	}
}

func (a *App) initAudio() {
	t := time.Now()
	pl, err := player.New()
	if err != nil {
		slog.Warn("audio unavailable, running without sound", "error", err)
		return
	}
	a.pl = pl
	a.pl.SetRenderBudget(a.settings.Playback.SeekMemory.BudgetBytes())
	a.pl.Downloader = func(ctx context.Context, path string, expectedSize int64, onProgress func(read, total int64)) (string, error) {
		if player.IsModland(path) {
			remotePath := player.RemotePath(path)
			if expectedSize == 0 {
				expectedSize = a.modlandSizes[remotePath]
			}
			return modland.DownloadFile(ctx, baseDir(), remotePath, expectedSize, onProgress)
		}
		if player.IsModArchive(path) {
			remoteURL := player.RemotePath(path)
			return modarchive.DownloadAndExtract(ctx, baseDir(), remoteURL, onProgress)
		}
		return path, nil
	}
	a.overlay = ui.New()
	a.presenter = newOverlayPresenter(a.overlay)
	a.overlay.SetBaseDir(baseDir())
	a.overlay.SetShowFPS(a.settings.UI.ShowStats)
	a.overlay.SetMicDevices(mic.InputDevices())
	w, h := a.window.GLGetDrawableSize()
	a.overlay.SetScreenSize(int(w), int(h))
	info := ui.CollectDeviceInfo(a.window)
	if a.pl != nil {
		backend, sr, ch, buf, ver := a.pl.BackendInfo()
		info.AudioBackend = backend
		info.AudioSamplerate = sr
		info.AudioChannels = ch
		info.AudioBuffer = buf
		info.SoloudVersion = ver
	}
	a.overlay.SetDeviceInfo(info)
	slog.Info("audio init", "ms", time.Since(t).Milliseconds())
}

func (a *App) initLibrary() {
	musicDir := a.findMusicDir()
	// Ensure the music directory exists so the scanner always has a root.
	if err := os.MkdirAll(musicDir, 0o755); err != nil {
		slog.Warn("cannot create music dir", "path", musicDir, "error", err)
	}
	if a.overlay != nil {
		a.overlay.SetMusicDir(musicDir)
	}
	t := time.Now()
	albums, scanStatus, err := player.ScanLibraryAlbums(musicDir)
	if err != nil {
		slog.Warn("music scan failed", "error", err)
	}
	lib := player.NewLibraryFromScan(albums)
	a.lib = lib
	a.lib.SetBaseDir(baseDir())
	slog.Info("music scan", "albums", lib.AlbumCount(), "ms", time.Since(t).Milliseconds())

	// Load catalogs from cache in parallel (no download on startup).
	// Each provider is independent; running them concurrently saves ~1-2s
	// on slow ARM CPUs where gzip/zstd decompression is CPU-bound.
	slog.Debug("initLibrary: loading catalogs from cache", "baseDir", baseDir())
	t = time.Now()
	var hasModland, hasModArchive bool
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		hasModland = a.loadModlandFromCache()
	}()
	go func() {
		defer wg.Done()
		hasModArchive = modarchive.InitCatalog(baseDir())
	}()
	wg.Wait()
	slog.Debug("initLibrary: catalogs loaded", "hasModland", hasModland, "hasModArchive", hasModArchive,
		"libAlbums", a.lib.AlbumCount(), "ms", time.Since(t).Milliseconds())
	if hasModland || hasModArchive {
		slog.Debug("initLibrary: cached catalogs available for navigation")
		a.online.Store(true)
		if a.overlay != nil {
			a.overlay.SetOnline(true)
		}
	}

	a.updateLocalShuffleSource(albums, scanStatus)
	// Build shuffle catalog only when shuffle is actually enabled.
	if a.settings != nil && a.settings.Playback.ShuffleMode != config.ShuffleOff {
		// Run the (potentially slow) shuffle build in the background so
		// the UI and first audio frame are not blocked. The render thread
		// reads shuffleCatalog via atomic.Pointer — it will be nil until
		// the goroutine finishes, causing advanceShuffleLazy to fall back
		// to legacy pool-based selection transparently.
		a.shuffleWg.Add(1)
		go func() {
			defer a.shuffleWg.Done()
			a.buildShuffleCatalog()
		}()
	} else {
		slog.Debug("initLibrary: shuffle off, skipping shuffle catalog build")
	}

	// Wire catalog album creation: when the overlay navigates into a
	// modarchive directory with files, it delegates album creation to the
	// library (single owner of albums).
	if a.overlay != nil && a.lib != nil {
		a.overlay.SetLibAlbums(func() []player.Album {
			return a.lib.Albums
		})
		a.overlay.SetAddCatalogAlbum(func(album player.Album) int {
			idx := a.lib.AddCatalogAlbum(album)
			slog.Debug("catalog album added", "name", album.Name, "idx", idx, "tracks", len(album.Tracks))
			return idx
		})
	}
}

// checkConnectivity probes the network in the background. On success it
// enables the remote catalogs (Modland/ModArchive) in the navigation tree.
// The probe is tied to the app context so it is aborted promptly on Close
// and never writes to the overlay after it has been torn down.
func (a *App) checkConnectivity() {
	a.offline.Store(true)
	a.connectivityWg.Add(1)
	go func() {
		defer a.connectivityWg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			ctx, cancel := context.WithTimeout(a.appCtx, 5*time.Second)
			resp, err := util.Get(ctx, "https://modland.antarctica.no/", nil)
			if err != nil {
				slog.Debug("connectivity probe failed", "error", err)
			}
			reachable := err == nil
			if resp != nil {
				reachable = reachable && resp.StatusCode >= 200 && resp.StatusCode < 400
				if closeErr := resp.Body.Close(); closeErr != nil {
					slog.Debug("connectivity response close", "error", closeErr)
				}
			}
			cancel()
			a.offline.Store(!reachable)
			if reachable {
				a.online.Store(true)
			}
			select {
			case <-a.appCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (a *App) restoreSavedPosition(allowRemote bool) {
	if a.pl == nil || a.startupFile != "" || a.resumeAttempted {
		return
	}
	position := a.settings.Playback.LastPosition
	if !canRestorePosition(position, allowRemote) {
		if position.Path != "" && !player.IsModland(position.Path) && !player.IsModArchive(position.Path) {
			a.resumeAttempted = true
		}
		return
	}
	a.resumeAttempted = true
	a.resumePath = position.Path
	a.resumeSeconds = position.Seconds
	a.prepareSavedCatalogTrack(position.Path)
	a.playTrack(position.Path, "")
}

// prepareSavedCatalogTrack restores the catalog context needed for sequential
// playback. Catalog albums are normally created when the user browses into a
// directory, but a saved remote track can be loaded before that happens.
func (a *App) prepareSavedCatalogTrack(path string) {
	if a.lib == nil || !player.IsModArchive(path) {
		return
	}
	remotePath := player.RemotePath(path)
	targetURL := modarchive.AlbumURL(remotePath)
	if targetURL == "" {
		return
	}
	items, ok := modarchive.FetchDirectoryCached(baseDir(), targetURL)
	if !ok {
		return
	}
	album := modarchive.BuildAlbum(targetURL, items)
	if album == nil {
		return
	}
	trackIdx := slices.Index(album.Tracks, path)
	if trackIdx < 0 {
		return
	}
	a.lib.AddCatalogAlbum(*album)
	a.playbackState.setPlaylist(album.Tracks, trackIdx, album.Name)
}

func canRestorePosition(position config.PlaybackPosition, allowRemote bool) bool {
	if position.Path == "" {
		return false
	}
	if player.IsModland(position.Path) || player.IsModArchive(position.Path) {
		return allowRemote
	}
	info, err := os.Stat(position.Path)
	return err == nil && !info.IsDir()
}

func (a *App) savePlaybackPosition() {
	if a.settings == nil {
		return
	}
	position := config.PlaybackPosition{}
	if a.pl != nil && (a.pl.IsValidVoice() || a.pl.Loading()) {
		position.Path = a.pl.TrackPath()
		position.Seconds = a.pl.Position()
	}
	a.settings.Playback.LastPosition = position
	if err := config.SaveSettings(a.settingsPath, *a.settings); err != nil {
		slog.Warn("settings save on exit", "error", err)
	}
}

// RequestQuit makes the main loop return so its deferred cleanup can run.
func (a *App) RequestQuit() {
	a.quit.Store(true)
}

func (a *App) loadModlandFromCache() bool {
	slog.Debug("loadModlandFromCache: loading", "baseDir", baseDir())
	cat := modland.LoadCatalog(baseDir())
	if cat != nil && len(cat.Albums) > 0 {
		slog.Debug("modland: catalog loaded from cache", "albums", len(cat.Albums))
		a.addModlandAlbums(cat.Albums)
		return true
	}
	slog.Debug("modland: catalog not found", "catNil", cat == nil)
	return false
}

func (a *App) addModlandAlbums(catalogAlbums []modland.Album) {
	albums := make([]player.Album, len(catalogAlbums))
	for i, ma := range catalogAlbums {
		tracks := make([]string, len(ma.Tracks))
		for j := range ma.Tracks {
			remotePath := ma.TrackPath(j)
			tracks[j] = player.ModlandPrefix + remotePath
			if ma.Tracks[j].Size > 0 {
				a.modlandSizes[remotePath] = ma.Tracks[j].Size
			}
		}
		albums[i] = player.Album{
			Name:   "Modland: " + ma.Name,
			Path:   "modland:" + ma.Name,
			Tracks: tracks,
		}
	}
	a.lib.AddVirtualAlbums(albums)
}

// ensureShuffleCatalog builds the shuffle catalog lazily if it was skipped
// at startup (shuffle was off). Called when the user switches to a shuffle
// mode at runtime. Runs in the background to avoid blocking the UI.
func (a *App) ensureShuffleCatalog() {
	if a.shuffleCatalog.Load() != nil {
		return
	}
	a.updateLocalShuffleSource(a.lib.Albums, filesystem.StatusOK)
	go a.buildShuffleCatalog()
}

func (a *App) initFavorites() {
	a.favoritesPath = config.FavoritesPath()
	f, err := player.LoadFavorites(a.favoritesPath)
	if err != nil {
		slog.Warn("favorites: load failed, using read-only", "error", err)
		a.favorites = player.NewReadOnlyFavorites()
		if a.overlay != nil {
			a.overlay.SetFavorites(a.favorites)
			a.overlay.ShowTrack("favorites: load error, read-only")
		}
		return
	}
	a.favorites = f
	if a.overlay != nil {
		a.overlay.SetFavorites(f)
	}
	slog.Info("favorites: loaded", "path", a.favoritesPath, "total", a.favorites.TotalCount())
}

// favoriteMode reports whether the X button should trigger favourite actions.
func (a *App) favoriteMode() bool {
	if a.overlay == nil || !a.overlay.UIVisible() {
		return false
	}
	if !a.overlay.IsLibraryPage() {
		return false
	}
	return a.overlay.HasPlayableTrack()
}

func (a *App) initInput() {
	a.inp = input.New()
}

func (a *App) initPreset() {
	if len(a.presetNames) == 0 {
		a.pm.LoadPresetData(string(presets.DefaultPreset()), false)
		slog.Warn("no external presets, using minimal built-in")
		return
	}

	// Cache transition presets ("!" prefix).
	for _, n := range a.presetNames {
		if n[0] == '!' {
			a.transitionPresets = append(a.transitionPresets, n)
		}
	}

	// Pick a random non-transition preset for startup.
	var normals []string
	for _, n := range a.presetNames {
		if n[0] != '!' {
			normals = append(normals, n)
		}
	}
	if len(normals) == 0 {
		normals = a.presetNames
	}
	normIdx := rand.Intn(len(normals))
	for i, n := range a.presetNames {
		if n == normals[normIdx] {
			a.presetIdx = i
			break
		}
	}
	d, err := presets.Read(a.presetNames[a.presetIdx])
	if err != nil {
		slog.Warn("preset read error", "name", a.presetNames[a.presetIdx], "error", err)
		a.pm.LoadPresetData(string(presets.DefaultPreset()), false)
		return
	}
	a.pm.LoadPresetData(string(d), false)
	a.activatePresetProfile(a.presetNames[a.presetIdx], d)
	if a.overlay != nil {
		a.overlay.SetPresetName(a.presetNames[a.presetIdx])
	}
	slog.Info("preset loaded", "name", a.presetNames[a.presetIdx], "count", len(a.presetNames))

	// Build tree for presets page. Static for the process lifetime,
	// so push once here rather than every frame.
	a.presetCats = presets.Names()
	if a.overlay != nil {
		a.overlay.SetPresetTree(a.presetCats)
		a.overlay.SetPresetMetaProvider(presets.ReadMeta)
		a.overlay.SetPresetPreviewRequest(func(key string) {
			if a.preview == nil {
				a.preview = newPreviewRenderer()
				a.preview.SetFPS(a.settings.Graphics.PerformanceMode.Params().VisualizerFPS)
			}
			data, err := presets.Read(key)
			if err != nil {
				slog.Debug("preview read preset", "key", key, "error", err)
				return
			}
			slog.Debug("preview: read data", "key", key, "bytes", len(data))
			a.preview.Enqueue(key, string(data))
		})
		a.overlay.SetPresetPreviewTex(func(key string) (uint32, int, int, bool) {
			if a.preview == nil {
				return 0, 0, 0, false
			}
			tex, ok := a.preview.HasResult(key)
			if !ok {
				return 0, 0, 0, false
			}
			return tex, a.preview.w, a.preview.h, true
		})
		a.overlay.SetPresetPreviewFPS(func() float64 {
			if a.preview == nil {
				return 0
			}
			return a.preview.RenderFPS()
		})
	}
}

// ensureTextures extracts the texture archive exactly once, on first call.
// Subsequent calls are no-ops. This is safe to call from any goroutine
// thanks to sync.Once, but the caller must ensure the GL context is current
// if pm.SetTextureSearchPaths needs it (it doesn't — it just stores paths).
func (a *App) ensureTextures(pm *projectm.Handle) {
	a.texturesOnce.Do(func() {
		t := time.Now()
		extractedDir, extractErr := os.MkdirTemp("", "glitchscope-textures-")
		if extractErr != nil {
			slog.Warn("texture temporary directory creation failed", "error", extractErr)
			return
		}
		a.textureDir = extractedDir
		textureArchive, archiveErr := archive.Open(filepath.Join(presetDirPath(), "textures.gsa"), 10000)
		if archiveErr == nil {
			_, extractErr = textureArchive.Extract(extractedDir)
			_ = textureArchive.Close()
			if extractErr != nil {
				slog.Warn("texture archive extract failed", "error", extractErr)
			}
		} else if !os.IsNotExist(archiveErr) {
			slog.Warn("texture archive open failed", "error", archiveErr)
		}
		if copyErr := copyPresetTextures(presetDirPath(), extractedDir); copyErr != nil {
			slog.Warn("preset texture copy failed", "error", copyErr)
		}
		pm.SetTextureSearchPaths([]string{extractedDir})
		slog.Info("textures extracted", "ms", time.Since(t).Milliseconds())
	})
}

func baseDir() string {
	if binDir, err := os.Executable(); err == nil && binDir != "" {
		return filepath.Dir(binDir)
	}
	return "."
}

func (a *App) findMusicDir() string {
	for _, candidate := range []string{
		filepath.Join(baseDir(), "music"),
		"/userdata/music",
		"/userdata/roms/music",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return filepath.Join(baseDir(), "music") // default even if missing
}

func presetDirPath() string {
	return baseDir() + "/presets"
}

func copyPresetTextures(sourceDir, targetDir string) error {
	return filepath.WalkDir(sourceDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		ext := filepath.Ext(entry.Name())
		switch ext {
		case ".jpg", ".jpeg", ".png", ".dds", ".tga", ".bmp", ".dib":
		default:
			return nil
		}
		relative, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(targetDir, relative)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
