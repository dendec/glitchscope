// Package app wires the application — initialization, main loop, and benchmark.
package app

import (
	"fmt"
	"log/slog"
	"math/rand"
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

const (
	fpsWindow       = 30
	lowFPSThresh    = 30.0
	softCutDuration = 2.5 // seconds, for smooth preset transitions
)

type pendingPreset struct {
	name string
	at   time.Time
}

type shuffleState struct {
	order    []trackRef
	idx      int
	albumIdx int // tracks which album the order covers (ShuffleAlbum only)
}

type App struct {
	window *sdl.Window
	glCtx  sdl.GLContext
	pm     *projectm.Handle
	rt     *projectm.RenderTarget

	pl      *player.Player
	overlay *ui.Overlay
	inp     *input.Input
	lib     *player.Library

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
	startupFile         string

	pending      pendingPreset // pending preset name + scheduled load time
	presetTicker *time.Ticker

	shuffle shuffleState
}

// New creates an App with display initialised. Player/overlay/input/library
// are created later by Init().
func New(fullscreen bool, width, height int, renderScale float64, renderNearest bool, renderScaleExplicit, renderNearestSet bool, startupFile string) (*App, error) {
	a := &App{
		prof:                prof.NewCollector(),
		settingsPath:        config.SettingsPath(),
		renderScale:         renderScale,
		renderScaleExplicit: renderScaleExplicit,
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
	savedRes := config.RenderResolution{Width: gs.Graphics.RenderWidth, Height: gs.Graphics.RenderHeight}
	target := config.ClosestResolution(resolutions, savedRes)
	renderW, renderH := target.Width, target.Height
	if renderScaleExplicit {
		renderW, renderH = scaledDim(int(w), renderScale), scaledDim(int(h), renderScale)
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
	a.pl.Downloader = func(path string, expectedSize int64, onProgress func(read, total int64)) (string, error) {
		if player.IsModland(path) {
			remotePath := player.RemotePath(path)
			if expectedSize == 0 {
				expectedSize = a.modlandSizes[remotePath]
			}
			return modland.DownloadFile(baseDir(), remotePath, expectedSize, onProgress)
		}
		if player.IsModArchive(path) {
			remoteURL := player.RemotePath(path)
			return modarchive.DownloadAndExtract(baseDir(), remoteURL, onProgress)
		}
		return path, nil
	}
	a.overlay = ui.New()
	a.overlay.SetBaseDir(baseDir())
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
	if first := a.lib.PlayCurrent(); first != "" {
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

// Run enters the main loop. Must be called after Init().
func (a *App) Run() {
	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()

	lastFrame := time.Now()
	fpsBuf := make([]float64, 0, fpsWindow)
	lowFPSWarned := false

	var lastAlbumIdx = -1
	var prevW, prevH int

	for range ticker.C {
		now := time.Now()
		dt := now.Sub(lastFrame).Seconds()
		lastFrame = now

		if dt > 0 {
			fpsBuf = append(fpsBuf, 1.0/dt)
			if len(fpsBuf) > fpsWindow {
				fpsBuf = fpsBuf[1:]
			}
		}
		var fpsAvg float64
		for _, v := range fpsBuf {
			fpsAvg += v
		}
		fpsAvg /= float64(len(fpsBuf))

		if len(fpsBuf) == fpsWindow {
			if fpsAvg < lowFPSThresh && !lowFPSWarned {
				presetName := ""
				if a.presetIdx >= 0 && a.presetIdx < len(a.presetNames) {
					presetName = a.presetNames[a.presetIdx]
				}
				slog.Warn("low fps", "fps", fpsAvg, "threshold", lowFPSThresh, "preset", presetName)
				lowFPSWarned = true
			} else if fpsAvg >= lowFPSThresh {
				lowFPSWarned = false
			}
		}

		w32, h32 := a.window.GLGetDrawableSize()
		w, h := int(w32), int(h32)
		winChanged := w != prevW || h != prevH

		if winChanged && prevW > 0 && prevH > 0 {
			if a.renderScaleExplicit {
				a.settings.Graphics.RenderWidth = scaledDim(w, a.renderScale)
				a.settings.Graphics.RenderHeight = scaledDim(h, a.renderScale)
			} else {
				resolutions := config.ComputeResolutions(w, h)
				saved := config.RenderResolution{Width: a.settings.Graphics.RenderWidth, Height: a.settings.Graphics.RenderHeight}
				target := config.ClosestResolution(resolutions, saved)
				a.settings.Graphics.RenderWidth, a.settings.Graphics.RenderHeight = target.Width, target.Height
			}

			if a.overlay != nil && a.overlay.IsSettingsPage() {
				rows := ui.BuildSettingsRows(*a.settings, w, h)
				a.overlay.SetSettingsRows(rows, a.overlay.SettingsCursor())
			}
		}
		prevW, prevH = w, h

		renderW, renderH := a.settings.Graphics.RenderWidth, a.settings.Graphics.RenderHeight
		if rw, rh := a.rt.Size(); rw != renderW || rh != renderH {
			a.rt.Resize(renderW, renderH)
			a.pm.SetWindowSize(renderW, renderH)
		}

		if a.overlay != nil {
			a.overlay.SetScreenSize(w, h)
		}

		if a.overlay != nil {
			s := a.prof.ReadStats()
			line := fmt.Sprintf("FPS:%.0f MEM:%.0fM CPU:%.0f%%", fpsAvg, s.MemKB/1024, s.CPUPct)
			if s.GPUOK {
				line += fmt.Sprintf(" GPU:%.0fM %.0f%%", s.GPUMemKB/1024, s.GPUUtilPct)
			}
			a.overlay.SetStats(line)
			if a.pl != nil {
				a.overlay.SetPlayback(a.pl.Position(), a.pl.Duration(), a.pl.SampleRate(), a.pl.Bitrate(), a.pl.BPM(), a.pl.Channels(), a.pl.IsPaused(), a.pl.IsTracker())
			}
			if a.lib != nil {
				a.overlay.SetAlbums(a.lib.Albums, a.lib.CurrentAlbumIndex())

				trackAlbumIdx := a.lib.CurrentAlbumIndex()
				if a.overlay.UIVisible() {
					trackAlbumIdx = a.overlay.AlbumCursor()
				}
				if trackAlbumIdx != lastAlbumIdx {
					trackCursor := 0
					if trackAlbumIdx == a.lib.CurrentAlbumIndex() {
						trackCursor = a.lib.CurrentTrackIndex()
					}
					a.overlay.SetTrackInfos(a.lib.GetAlbumTracks(trackAlbumIdx), trackCursor)
					lastAlbumIdx = trackAlbumIdx
				}

				if a.pl != nil {
					curTrack := a.pl.TrackPath()
					if !a.pl.IsValidVoice() && !a.pl.Loading() {
						curTrack = ""
					}
					curAlbum := a.lib.CurrentAlbum().Name
					a.overlay.SetPlaying(curAlbum, curTrack)
					a.overlay.SetLoading(a.pl.LoadProgress())
				}
			}
		}

		for e := sdl.PollEvent(); e != nil; e = sdl.PollEvent() {
			act := a.inp.ProcessEvent(e)
			if act == input.ActionQuit {
				return
			}
			if act == input.ActionRandomPreset {
				a.randPreset()
			} else {
				a.handleAction(act, w, h)
			}
		}

		if a.pending.name != "" && now.After(a.pending.at) {
			if d, err := presets.Read(a.pending.name); err == nil {
				a.pm.LoadPresetData(string(d), true)
				a.applyPresetName(a.pending.name)
			}
			a.pending = pendingPreset{}
		}

		if a.presetTicker != nil {
			select {
			case <-a.presetTicker.C:
				a.randPreset()
			default:
			}
		}

		if a.pl != nil {
			if wave := a.pl.GetWave(); wave != nil {
				a.pm.PCMAddFloat(wave, projectm.Mono)
			}
		}

		if a.pl != nil {
			if _, failed := a.pl.CheckPending(); failed {
				if a.overlay != nil {
					a.overlay.ShowTrack(" playback error")
				}
			}
		}

		if a.pl != nil && a.lib != nil && a.pl.Voice() != 0 && a.pl.TrackFinished() {
			a.autoAdvance()
		}

		a.pm.SetFPS(int32(fpsAvg))

		a.pm.RenderFrame()
		a.rt.Capture()
		a.rt.BlitToScreen(w, h)

		if a.overlay != nil {
			rw, rh := a.rt.Size()
			a.overlay.Update(a.inp.DPadUpHeld(), a.inp.DPadDownHeld())
			a.overlay.Draw(w, h)
			a.pm.BindFeedbackFramebuffer()
			a.overlay.Inject(rw, rh)
		}

		a.window.GLSwap()
	}
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

// autoAdvance picks the next track based on shuffle/repeat settings.
func (a *App) autoAdvance() {
	if a.lib == nil || a.pl == nil {
		return
	}

	ps := a.settings.Playback

	if ps.Repeat == config.RepeatOne {
		path := a.lib.CurrentTrack()
		if path != "" {
			a.playTrack(path, a.lib.CurrentAlbum().Name)
		}
		return
	}

	if ps.ShuffleMode != config.ShuffleOff {
		// Regenerate when album changed, order empty, or exhausted with RepeatAll.
		needsRegen := len(a.shuffle.order) == 0
		if ps.ShuffleMode == config.ShuffleAlbum && a.shuffle.albumIdx != a.lib.CurrentAlbumIndex() {
			needsRegen = true
		}
		if a.shuffle.idx >= len(a.shuffle.order) && ps.Repeat == config.RepeatAll {
			needsRegen = true
		}
		if needsRegen {
			a.regenerateShuffleOrder()
		}
		if len(a.shuffle.order) == 0 || a.shuffle.idx >= len(a.shuffle.order) {
			return
		}
		t := a.shuffle.order[a.shuffle.idx]
		a.shuffle.idx++
		a.lib.SelectAlbum(t.albumIdx)
		a.lib.SelectTrack(t.trackIdx)
		a.playTrack(t.path, t.album)
		return
	}

	// Sequential: detect "last track" before TrackNext() (it wraps).
	album := a.lib.CurrentAlbum()
	if a.lib.CurrentTrackIndex() < len(album.Tracks)-1 {
		path := a.lib.TrackNext()
		if path != "" {
			a.playTrack(path, a.lib.CurrentAlbum().Name)
		}
		return
	}

	if ps.Repeat == config.RepeatAll {
		path := a.lib.AlbumNext()
		if path != "" {
			a.playTrack(path, a.lib.CurrentAlbum().Name)
		}
	}
	// RepeatOff + end of album: stop (no next track played).
}

type trackRef struct {
	path     string
	album    string
	albumIdx int
	trackIdx int
}

func (a *App) allTracks() []trackRef {
	var all []trackRef
	for ai, album := range a.lib.Albums {
		for ti, path := range album.Tracks {
			all = append(all, trackRef{path: path, album: album.Name, albumIdx: ai, trackIdx: ti})
		}
	}
	return all
}

func (a *App) localTracks() []trackRef {
	var all []trackRef
	for ai, album := range a.lib.Albums {
		if player.IsModland(album.Path) || player.IsModArchive(album.Path) {
			continue
		}
		for ti, path := range album.Tracks {
			all = append(all, trackRef{path: path, album: album.Name, albumIdx: ai, trackIdx: ti})
		}
	}
	return all
}

func (a *App) currentAlbumTracks() []trackRef {
	ai := a.lib.CurrentAlbumIndex()
	if ai < 0 || ai >= len(a.lib.Albums) {
		return nil
	}
	album := a.lib.Albums[ai]
	tracks := make([]trackRef, len(album.Tracks))
	for ti, path := range album.Tracks {
		tracks[ti] = trackRef{path: path, album: album.Name, albumIdx: ai, trackIdx: ti}
	}
	return tracks
}

// regenerateShuffleOrder builds a new shuffled order. Current track is
// excluded so it doesn't replay immediately (unless single-track pool).
func (a *App) regenerateShuffleOrder() {
	if a.lib == nil {
		a.shuffle = shuffleState{}
		return
	}

	var pool []trackRef
	switch a.settings.Playback.ShuffleMode {
	case config.ShuffleAlbum:
		pool = a.currentAlbumTracks()
		a.shuffle.albumIdx = a.lib.CurrentAlbumIndex()
	case config.ShuffleLocal:
		pool = a.localTracks()
	case config.ShuffleAll:
		pool = a.allTracks()
	default:
		a.shuffle = shuffleState{}
		return
	}

	cur := ""
	if a.pl != nil {
		cur = a.pl.TrackPath()
	}
	if cur != "" && len(pool) > 1 {
		filtered := pool[:0]
		for _, t := range pool {
			if t.path != cur {
				filtered = append(filtered, t)
			}
		}
		pool = filtered
	}

	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	a.shuffle.order = pool
	a.shuffle.idx = 0
}

func (a *App) startPresetTicker() {
	interval := a.settings.PresetInterval
	if interval == config.PresetOff {
		return
	}
	a.presetTicker = time.NewTicker(time.Duration(interval) * time.Second)
}

func (a *App) resetPresetTicker() {
	if a.presetTicker != nil {
		a.presetTicker.Stop()
		a.presetTicker = nil
	}
	a.startPresetTicker()
}
