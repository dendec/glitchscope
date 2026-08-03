// Package player provides audio playback via SoLoud.
package player

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/dendec/pmv/internal/openmpt"
	"github.com/dendec/pmv/internal/soloud"
	"github.com/dendec/pmv/internal/xmp"
)

// loadResult holds the outcome of loading a single audio source.
type loadResult struct {
	src        soloud.AudioSource
	path       string
	localPath  string
	requestID  uint64
	bpm        float64
	channels   int
	duration   float64
	isTracker  bool
	trackCount int // non-zero for multi-track sources (gme, sid, hvl)
	err        error
}

type Player struct {
	s               *soloud.Soloud
	voice           uint
	current         soloud.AudioSource // single field replaces 10 format-specific fields
	currentPath     string
	currentBPM      float64
	currentDuration float64
	currentBitrate  float64
	channels        int
	isTracker       bool

	// Downloader optionally fetches a remote file to a local path (e.g. for modland).
	Downloader  func(path string, expectedSize int64, onProgress func(read, total int64)) (string, error)
	loadFunc    func(localPath string) loadResult // test seam: override loadSource
	pendingCh   chan loadResult                    // background load results
	pendingMu   sync.Mutex
	requestID   atomic.Uint64
	loading     atomic.Bool
	loadPercent atomic.Int64 // download progress percent [0..100], -1 if unknown
	loadWG      sync.WaitGroup
	closed      bool
}

// New creates and initializes a Player.
func New() (*Player, error) {
	s := soloud.New()
	if s == nil {
		return nil, fmt.Errorf("soloud create failed")
	}
	if err := s.Init(); err != nil {
		s.Destroy()
		return nil, fmt.Errorf("soloud init: %w", err)
	}
	slog.Info("SoLoud initialized")
	return &Player{s: s, pendingCh: make(chan loadResult, 1)}, nil
}

func isTrackerExt(ext string) bool {
	return xmp.SupportedExts[ext] || openmpt.SupportedExts[ext]
}

func isGmeExt(ext string) bool {
	switch ext {
	case ".ay", ".nsf", ".nsfe", ".spc", ".gbs", ".hes", ".kss", ".sap", ".vgm", ".vgz":
		return true
	default:
		return false
	}
}

func isSidExt(ext string) bool { return ext == ".sid" || ext == ".rsid" }

func isAyumiExt(ext string) bool { return ext == ".vtx" }

func isPt3Ext(ext string) bool { return ext == ".pt3" }

func isYmExt(ext string) bool { return ext == ".ym" || ext == ".lh" || ext == ".lha" }

func isHvlExt(ext string) bool { return ext == ".ahx" || ext == ".hvl" }

func isFfmpegExt(ext string) bool {
	switch ext {
	case ".aac", ".ac3", ".eac3", ".mp1", ".mp2", ".mp3",
		".ogg", ".oga", ".opus", ".spx", ".flac",
		".wav", ".rf64", ".aiff", ".aif", ".aifc", ".caf",
		".m4a", ".m4b", ".mp4", ".mov", ".mka", ".mkv", ".webm",
		".wma", ".asf", ".amr", ".ape", ".tta", ".wv", ".mpc",
		".ts", ".m2ts", ".ra", ".rm":
		return true
	default:
		return false
	}
}

// loadSource loads an audio source from a local path and returns a loadResult.
func loadSource(localPath string) loadResult {
	ext := strings.ToLower(filepath.Ext(localPath))

	if isHvlExt(ext) {
		return loadFromBytes(localPath, ext, func(data []byte) (soloud.AudioSource, error) {
			return soloud.NewHvl(data)
		}, true, 2)
	}
	if isTrackerExt(ext) {
		return loadTracker(localPath, ext)
	}
	if isGmeExt(ext) {
		return loadFromBytes(localPath, ext, func(data []byte) (soloud.AudioSource, error) {
			return soloud.NewGme(data)
		}, true, 2)
	}
	if isSidExt(ext) {
		return loadFromBytes(localPath, ext, func(data []byte) (soloud.AudioSource, error) {
			return soloud.NewSid(data)
		}, true, 1)
	}
	if isAyumiExt(ext) {
		return loadFromBytes(localPath, ext, func(data []byte) (soloud.AudioSource, error) {
			return soloud.NewAyumi(data)
		}, true, 2)
	}
	if isPt3Ext(ext) {
		return loadFromBytes(localPath, ext, func(data []byte) (soloud.AudioSource, error) {
			return soloud.NewPt3(data)
		}, true, 2)
	}
	if isYmExt(ext) {
		return loadFromBytes(localPath, ext, func(data []byte) (soloud.AudioSource, error) {
			return soloud.NewYm(data)
		}, true, 2)
	}
	if isFfmpegExt(ext) {
		return loadFromBytes(localPath, ext, func(data []byte) (soloud.AudioSource, error) {
			return soloud.NewFfmpeg(data)
		}, false, 0) // channels determined after load
	}

	// WAV and other formats loaded via SoLoud's file loader.
	w, err := soloud.LoadWav(localPath)
	if err != nil {
		return loadResult{path: localPath, err: fmt.Errorf("load %s: %w", localPath, err)}
	}
	return loadResult{
		src:       w,
		path:      localPath,
		duration:  w.GetLength(),
		channels:  w.GetChannels(),
		isTracker: false,
	}
}

// loadFromBytes is a helper that reads a file and creates an audio source.
func loadFromBytes(path, ext string, factory func([]byte) (soloud.AudioSource, error), isTracker bool, channels int) loadResult {
	data, err := os.ReadFile(path)
	if err != nil {
		return loadResult{path: path, err: fmt.Errorf("%s read %s: %w", ext, path, err)}
	}
	src, err := factory(data)
	if err != nil {
		return loadResult{path: path, err: fmt.Errorf("%s %s: %w", ext, path, err)}
	}
	r := loadResult{
		src:       src,
		path:      path,
		duration:  src.(interface{ GetLength() float64 }).GetLength(),
		channels:  channels,
		isTracker: isTracker,
	}
	// Extract track count for multi-track sources.
	if tc, ok := src.(interface{ GetTrackCount() int }); ok {
		r.trackCount = tc.GetTrackCount()
	}
	// Determine channels dynamically for ffmpeg.
	if ch, ok := src.(interface{ GetChannels() int }); ok && channels == 0 {
		r.channels = ch.GetChannels()
	}
	return r
}

// loadTracker loads a tracker file, trying openmpt first, then xmp.
func loadTracker(path, ext string) loadResult {
	data, err := os.ReadFile(path)
	if err != nil {
		return loadResult{path: path, err: fmt.Errorf("tracker read %s: %w", path, err)}
	}

	// Try libopenmpt first (broader format support), fallback to libxmp.
	if openmpt.HasExt(ext) {
		ompt, err := soloud.NewOpenmpt(data)
		if err == nil {
			bpm, ch, dur, _ := openmpt.GetTrackerMeta(data)
			return loadResult{src: ompt, path: path, bpm: bpm, channels: ch, duration: dur, isTracker: true}
		}
		slog.Debug("openmpt fallback to xmp", "ext", ext, "err", err)
	}

	mod, err := soloud.NewXmp(data)
	if err != nil {
		return loadResult{path: path, err: fmt.Errorf("tracker %s: %w", path, err)}
	}
	bpm, ch, dur, err := xmp.GetTrackerMetaFromBytes(data)
	if err != nil {
		slog.Warn("tracker meta", "path", path, "err", err)
	}
	return loadResult{src: mod, path: path, bpm: bpm, channels: ch, duration: dur, isTracker: true}
}

// PlayFile loads and plays an audio file. Only one file at a time.
func (p *Player) PlayFile(path string) error {
	r := loadSource(path)
	if r.err != nil {
		return r.err
	}
	p.applyResult(r, path)
	if r.trackCount > 0 {
		slog.Info("playing", "format", strings.ToLower(filepath.Ext(path)), "path", path, "tracks", r.trackCount)
	} else {
		slog.Info("playing", "path", path)
	}
	return nil
}

// PlayFileAsync starts loading in a background goroutine.
func (p *Player) PlayFileAsync(path string) {
	p.pendingMu.Lock()
	if p.closed {
		p.pendingMu.Unlock()
		return
	}
	p.loadWG.Add(1)
	p.pendingMu.Unlock()

	requestID := p.requestID.Add(1)
	p.pendingMu.Lock()
	select {
	case old := <-p.pendingCh:
		if old.src != nil {
			old.src.Destroy()
		}
	default:
	}
	p.pendingMu.Unlock()

	if p.s != nil {
		p.s.StopAll()
	}
	p.voice = 0
	p.currentPath = path // show path in UI while loading
	p.loading.Store(true)
	p.loadPercent.Store(-1)

	go func() {
		defer p.loadWG.Done()

		localPath := path
		if p.Downloader != nil {
			onProgress := func(read, total int64) {
				if p.requestID.Load() != requestID {
					return // stale request — ignore progress
				}
				if total <= 0 {
					return
				}
				pct := read * 100 / total
				if pct < 0 {
					pct = 0
				} else if pct > 100 {
					pct = 100
				}
				p.loadPercent.Store(pct)
			}
			dlPath, err := p.Downloader(path, 0, onProgress)
			if err != nil {
				p.publishResult(loadResult{path: path, requestID: requestID, err: err})
				return
			}
			localPath = dlPath
		}
		if p.requestID.Load() != requestID {
			return // stale — skip decode, do not reset progress
		}
		p.loadPercent.Store(-1) // decode phase: unknown progress
		loader := loadSource
		if p.loadFunc != nil {
			loader = p.loadFunc
		}
		r := loader(localPath)
		r.path = path
		r.localPath = localPath
		r.requestID = requestID
		p.publishResult(r)
	}()
}

func (p *Player) publishResult(r loadResult) {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	if r.requestID != p.requestID.Load() {
		if r.src != nil {
			r.src.Destroy()
		}
		return
	}

	select {
	case old := <-p.pendingCh:
		if old.src != nil {
			old.src.Destroy()
		}
	default:
	}
	p.pendingCh <- r
}

func (p *Player) Loading() bool {
	return p.loading.Load()
}

// LoadProgress returns whether an async load is in flight and download
// progress percent [0..100], or -1 when unknown.
func (p *Player) LoadProgress() (active bool, percent int64) {
	return p.loading.Load(), p.loadPercent.Load()
}

// CheckPending picks up a completed background load.
// Returns (started, failed). Call every frame.
func (p *Player) CheckPending() (started bool, failed bool) {
	p.pendingMu.Lock()
	select {
	case r := <-p.pendingCh:
		p.pendingMu.Unlock()
		if r.requestID != p.requestID.Load() {
			// Stale result — discard.
			if r.src != nil {
				r.src.Destroy()
			}
			return false, false
		}
		p.loading.Store(false)
		if r.err != nil {
			slog.Error("async load", "path", r.path, "error", r.err)
			p.currentPath = ""
			return false, true
		}
		p.applyResult(r, r.path)
		if r.trackCount > 0 {
			slog.Info("playing (async)", "format", strings.ToLower(filepath.Ext(r.path)), "path", r.path, "tracks", r.trackCount)
		} else {
			slog.Info("playing (async)", "path", r.path)
		}
		return true, false
	default:
		p.pendingMu.Unlock()
		return false, false
	}
}

// applyResult applies a loadResult to the player state, replacing any current source.
func (p *Player) applyResult(r loadResult, displayPath string) {
	p.replaceSource(r.src)
	p.currentPath = displayPath
	p.currentBPM = r.bpm
	p.currentDuration = r.duration
	p.channels = r.channels
	p.isTracker = r.isTracker
	if r.isTracker {
		p.currentBitrate = 0
	} else {
		bitratePath := r.localPath
		if bitratePath == "" {
			bitratePath = displayPath
		}
		p.currentBitrate = fileBitrate(bitratePath, r.duration)
	}
}

// replaceSource stops current playback and swaps in a new audio source.
func (p *Player) replaceSource(src soloud.AudioSource) {
	if p.s != nil {
		p.s.StopAll()
	}
	if p.current != nil {
		p.current.Destroy()
		p.current = nil
	}
	if src != nil {
		p.current = src
		if p.s != nil {
			p.voice = p.s.PlaySource(src)
		}
	}
}

func (p *Player) GetWave() []float32 {
	return p.s.GetWave()
}

func (p *Player) Pause() {
	if p.voice == 0 {
		return
	}
	p.s.SetPause(p.voice, true)
}

func (p *Player) Resume() {
	if p.voice == 0 {
		return
	}
	p.s.SetPause(p.voice, false)
}

func (p *Player) TogglePause() {
	if p.voice == 0 {
		return
	}
	if p.s.GetPause(p.voice) {
		p.Resume()
		slog.Debug("player resumed")
	} else {
		p.Pause()
		slog.Debug("player paused")
	}
}

func (p *Player) Voice() uint {
	return p.voice
}

func (p *Player) IsValidVoice() bool {
	if p.voice == 0 {
		return false
	}
	return p.s.IsValidVoiceHandle(p.voice)
}

// TrackFinished reports whether playback should be considered over.
func (p *Player) TrackFinished() bool {
	if !p.IsValidVoice() {
		return true
	}
	if p.isTracker && p.currentDuration > 0 && p.Position() >= p.currentDuration {
		return true
	}
	return false
}

func (p *Player) Stop() {
	if p.s != nil {
		p.s.StopAll()
	}
	p.voice = 0
	if p.current != nil {
		p.current.Destroy()
		p.current = nil
	}
	p.currentPath = ""
	p.loading.Store(false)
	p.loadPercent.Store(-1)
	p.requestID.Add(1) // invalidate any in-flight async load
}

func (p *Player) Close() {
	p.pendingMu.Lock()
	if p.closed {
		p.pendingMu.Unlock()
		return
	}
	p.closed = true
	p.pendingMu.Unlock()

	p.Stop()
	p.loadWG.Wait() // wait for all in-flight workers

	// Discard any remaining pending source.
	p.pendingMu.Lock()
	select {
	case r := <-p.pendingCh:
		if r.src != nil {
			r.src.Destroy()
		}
	default:
	}
	p.pendingMu.Unlock()

	if p.s != nil {
		p.s.Destroy()
	}
	slog.Info("SoLoud shut down")
}

func (p *Player) Position() float64 {
	if p.voice == 0 {
		return 0
	}
	return p.s.GetStreamTime(p.voice)
}

// Seek moves the current track position in seconds.
func (p *Player) Seek(seconds float64) error {
	if !p.IsValidVoice() {
		return fmt.Errorf("no active voice")
	}
	return p.s.Seek(p.voice, seconds)
}

func (p *Player) Duration() float64 {
	if w, ok := p.current.(*soloud.Wav); ok {
		return w.GetLength()
	}
	return p.currentDuration
}

func (p *Player) SampleRate() float32 {
	if p.voice == 0 {
		return 0
	}
	return p.s.GetSamplerate(p.voice)
}

func (p *Player) Channels() int {
	return p.channels
}

func (p *Player) IsTracker() bool {
	return p.isTracker
}

func (p *Player) BPM() float64 {
	return p.currentBPM
}

func (p *Player) IsPaused() bool {
	if p.voice == 0 {
		return false
	}
	return p.s.GetPause(p.voice)
}

func (p *Player) TrackPath() string {
	return p.currentPath
}

func (p *Player) Bitrate() float64 {
	return p.currentBitrate
}

func fileBitrate(path string, duration float64) float64 {
	if duration <= 0 {
		return 0
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return float64(info.Size()) * 8 / duration / 1000
}
