// Package app wires the application — initialization, main loop, and benchmark.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/dendec/pmv/internal/archive"
	"github.com/dendec/pmv/internal/config"
	"github.com/dendec/pmv/internal/input"
	"github.com/dendec/pmv/internal/modarchive"
	"github.com/dendec/pmv/internal/modland"
	"github.com/dendec/pmv/internal/player"
	"github.com/dendec/pmv/internal/presets"
	"github.com/dendec/pmv/internal/prof"
	"github.com/dendec/pmv/internal/projectm"
	"github.com/dendec/pmv/internal/ui"
	"github.com/veandco/go-sdl2/sdl"
)

type App struct {
	window *sdl.Window
	glCtx  sdl.GLContext
	pm     *projectm.Handle
	rt     *projectm.RenderTarget

	overlay *ui.Overlay
	inp     *input.Input
	playbackState

	modlandSizes map[string]int64 // remote path → expected size for downloads

	prof              *prof.Collector
	settings          *config.Settings
	settingsPath      string
	textureDir        string
	presetNames       []string
	presetIdx         int
	presetCats        []ui.PresetCat // categories for presets page
	transitionPresets []string       // "!"-prefixed presets for smooth transitions

	renderScale         float64
	renderScaleExplicit bool
	showFPS             bool
	startupFile         string

	adaptive resolutionState

	pending      pendingPreset // pending preset name + scheduled load time
	presetTicker *time.Ticker
}

// New creates an App with display initialised. Player/overlay/input/library
// are created later by Init().
func New(fullscreen bool, width, height int, renderScale float64, renderNearest bool, renderScaleExplicit, renderNearestSet bool, startupFile string, showFPS bool) (*App, error) {
	a := &App{
		prof:                prof.NewCollector(),
		settingsPath:        config.SettingsPath(),
		renderScale:         renderScale,
		renderScaleExplicit: renderScaleExplicit,
		showFPS:             showFPS,
		startupFile:         startupFile,
		modlandSizes:        make(map[string]int64),
	}

	if err := sdl.Init(sdl.INIT_VIDEO | sdl.INIT_EVENTS | sdl.INIT_GAMECONTROLLER | sdl.INIT_JOYSTICK); err != nil {
		return nil, fmt.Errorf("sdl init: %w", err)
	}

	winFlags := uint32(sdl.WINDOW_OPENGL | sdl.WINDOW_SHOWN | sdl.WINDOW_RESIZABLE)
	if fullscreen {
		winFlags |= sdl.WINDOW_FULLSCREEN
	}
	win, err := sdl.CreateWindow("PMV — Portable Music Visualizer",
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
	a.pm.SetSoftCutDuration(softCutDuration)
	slog.Info("projectM init", "ms", time.Since(t0).Milliseconds())

	// Keep bundled and user textures in one temporary search directory. User
	// files are copied last so they override matching bundled textures.
	extractedDir, extractErr := os.MkdirTemp("", "pmv-textures-")
	if extractErr == nil {
		a.textureDir = extractedDir
		textureArchive, archiveErr := archive.Open(filepath.Join(presetDirPath(), "textures.pmv"), 10000)
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
	} else {
		slog.Warn("texture temporary directory creation failed", "error", extractErr)
	}

	w, h := win.GLGetDrawableSize()

	gs, err := config.LoadSettings(a.settingsPath)
	if err != nil {
		slog.Warn("settings load", "error", err)
		gs = config.DefaultSettings()
	}
	if renderNearestSet && renderNearest {
		gs.Graphics.UpscaleFilter = config.FilterPixel
	}

	resolutions := config.ComputeResolutions(int(w), int(h))
	var renderW, renderH int
	switch {
	case renderScaleExplicit:
		renderW, renderH = scaledDim(int(w), renderScale), scaledDim(int(h), renderScale)
	case gs.Graphics.Adaptive && len(resolutions) > 0:
		a.adaptive.Reset(int(w), int(h))
		renderW, renderH = a.adaptive.resolutions[0].Width, a.adaptive.resolutions[0].Height
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

	slog.Info("render size", "window", fmt.Sprintf("%dx%d", int(w), int(h)),
		"internal", fmt.Sprintf("%dx%d", renderW, renderH), "scale", renderScale)

	if err := presets.Open(presetDirPath()); err != nil {
		slog.Warn("presets dir not found", "error", err)
	}
	a.presetNames = presets.Names()

	return a, nil
}

func (a *App) Close() {
	if a.presetTicker != nil {
		a.presetTicker.Stop()
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

func (a *App) Init() {
	a.initAudio()
	a.initLibrary()
	a.checkConnectivity()
	a.initInput()
	a.initPreset()
	if a.startupFile != "" && a.pl != nil {
		a.playTrack(a.startupFile, "command line")
	} else if a.startupFile != "" {
		slog.Warn("startup file skipped, audio unavailable", "path", a.startupFile)
	} else {
		a.playFirst()
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
	a.overlay.SetBaseDir(baseDir())
	a.overlay.SetShowFPS(a.showFPS)
	w, h := a.window.GLGetDrawableSize()
	a.overlay.SetScreenSize(int(w), int(h))
	slog.Info("audio init", "ms", time.Since(t).Milliseconds())
}

func (a *App) initLibrary() {
	musicDir := a.findMusicDir()
	t := time.Now()
	lib, err := player.NewLibrary(musicDir)
	if err != nil {
		slog.Warn("music scan failed", "error", err)
		return
	}
	a.lib = lib
	slog.Info("music scan", "albums", lib.AlbumCount(), "ms", time.Since(t).Milliseconds())

	// Load modland from cache only (no download on startup).
	a.loadModlandFromCache()

	// Preload modarchive catalog if available on disk.
	modarchive.InitCatalog(baseDir())
}

// checkConnectivity probes the network in the background. On success it
// enables the remote catalogs (Modland/ModArchive) in the navigation tree.
func (a *App) checkConnectivity() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://modland.antarctica.no/", nil)
		if err != nil {
			slog.Info("connectivity check: request create failed", "error", err)
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			slog.Info("connectivity check: offline", "error", err)
			return
		}
		resp.Body.Close()
		slog.Info("connectivity check: online", "status", resp.StatusCode)
		if a.overlay != nil {
			a.overlay.SetOnline(true)
		}
	}()
}

func (a *App) loadModlandFromCache() {
	cat := modland.LoadCatalog(baseDir())
	if cat != nil && len(cat.Albums) > 0 {
		a.addModlandAlbums(cat.Albums)
		return
	}
	slog.Info("modland: catalog not found")
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
	if a.overlay != nil {
		a.overlay.SetPresetName(a.presetNames[a.presetIdx])
	}
	slog.Info("preset loaded", "name", a.presetNames[a.presetIdx], "count", len(a.presetNames))

	// Build categories for presets page. Static for the process lifetime,
	// so push once here rather than every frame.
	cats := presets.Categories()
	a.presetCats = make([]ui.PresetCat, len(cats))
	for i, c := range cats {
		a.presetCats[i] = ui.PresetCat{Name: c, Presets: presets.PresetsInCategory(c)}
	}
	if a.overlay != nil {
		a.overlay.SetPresetCategories(a.presetCats)
	}
}

func (a *App) playFirst() {
	if a.pl == nil || a.lib == nil {
		return
	}
	if first := a.lib.CurrentTrack(); first != "" {
		a.playTrack(first, a.lib.CurrentAlbum().Name)
	}
}

func baseDir() string {
	if binDir, err := os.Executable(); err == nil && binDir != "" {
		return filepath.Dir(binDir)
	}
	return "."
}

func (a *App) findMusicDir() string {
	for _, candidate := range []string{baseDir() + "/music", baseDir() + "/test_data/music", baseDir()} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return baseDir()
}

func scaledDim(v int, scale float64) int {
	if scale >= 1.0 {
		return v
	}
	if scale < 0.1 {
		scale = 0.1
	}
	d := int(float64(v) * scale)
	if d > v {
		d = v
	}
	return d
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
