// Package player provides audio playback via SoLoud.
package player

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dendec/glitchscope/internal/openmpt"
	"github.com/dendec/glitchscope/internal/soloud"
	"github.com/dendec/glitchscope/internal/xmp"
)

// Pre-render (decode-to-buffer) playback for tracker/chip formats whose native
// decoders have no reliable bidirectional seek (openmpt/xmp only seek forward
// coarsely, SID has none, and libstsound's YM seek is sub-format dependent).
// The track is decoded once, up front in the async load goroutine, into a
// memory-backed Wav so seeking is exact in both directions.
const (
	// renderSampleRate is the rate at which tracker/chip tracks are rendered.
	renderSampleRate = 44100
	// maxRenderSeconds bounds how much of a track is pre-rendered. Longer
	// tracks fall back to native streaming (backward seek restarts) rather
	// than consuming an unbounded amount of RAM on low-power handhelds.
	// 360s covers essentially all chip tracks (YM/SID etc.) at a worst case of
	// ~124 MiB for stereo float32 PCM (~0.17 MiB/s per channel-pair).
	maxRenderSeconds = 360.0
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

	// Authoritative playback clock. SoLoud's getStreamTime does not jump to the
	// seek target (it only advances by wall clock each mix), so we track our own
	// position: set on Seek, advanced lazily on Position() while playing.
	posMu    sync.Mutex
	pos      float64
	posLast  time.Time
	posValid bool
	seekBoth bool // true when the current source can seek both directions

	// Downloader optionally fetches a remote file to a local path (e.g. for modland).
	// The context is cancelled when a new track is requested or playback is stopped.
	Downloader  func(ctx context.Context, path string, expectedSize int64, onProgress func(read, total int64)) (string, error)
	loadFunc    func(localPath string) loadResult // test seam: override loadSource
	loadCancel  context.CancelFunc                // cancels the current in-flight load
	pendingCh   chan loadResult                   // background load results
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
		return loadChip(localPath, ext, 1, func(data []byte) (soloud.AudioSource, error) {
			return soloud.NewSid(data)
		})
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
		return loadChip(localPath, ext, 2, func(data []byte) (soloud.AudioSource, error) {
			return soloud.NewYm(data)
		})
	}
	if isFfmpegExt(ext) {
		// Stream large audio files (MP3/FLAC/WAV/Ogg/…) from disk instead of
		// reading them fully into memory — important on low-RAM handhelds.
		src, err := soloud.NewFfmpegFile(localPath)
		if err != nil {
			return loadResult{path: localPath, err: err}
		}
		return loadResult{
			src:       src,
			path:      localPath,
			duration:  src.GetLength(),
			channels:  src.GetChannels(),
			isTracker: false,
		}
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

// loadChip loads a single-chip format (SID, YM) whose native decoder has no
// reliable bidirectional seek: it renders the track into a seekable Wav when it
// fits within the pre-render cap, else keeps native streaming. defChannels is
// the native output channel count used when the source reports none.
func loadChip(path, ext string, defChannels int, factory func([]byte) (soloud.AudioSource, error)) loadResult {
	data, err := os.ReadFile(path)
	if err != nil {
		return loadResult{path: path, err: fmt.Errorf("%s read %s: %w", ext, path, err)}
	}
	src, err := factory(data)
	if err != nil {
		return loadResult{path: path, err: fmt.Errorf("%s %s: %w", ext, path, err)}
	}
	dur := src.(interface{ GetLength() float64 }).GetLength()
	trackCount := 0
	if tc, ok := src.(interface{ GetTrackCount() int }); ok {
		trackCount = tc.GetTrackCount()
	}
	channels := defChannels
	if ch, ok := src.(interface{ GetChannels() int }); ok {
		channels = ch.GetChannels()
	}
	repl, newDur, newCh, replaced := renderToSeekable(src, data, dur, channels)
	if replaced {
		src.Destroy()
		src = repl
		dur = newDur
		channels = newCh
	}

	return loadResult{
		src:        src,
		path:       path,
		duration:   dur,
		channels:   channels,
		isTracker:  true,
		trackCount: trackCount,
	}
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
			return finishTrackerLoad(ompt, data, path, bpm, ch, dur)
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
	return finishTrackerLoad(mod, data, path, bpm, ch, dur)
}

// finishTrackerLoad renders a tracker source into a seekable Wav when possible,
// else keeps native streaming, and assembles the loadResult.
func finishTrackerLoad(src soloud.AudioSource, data []byte, path string, bpm float64, channels int, duration float64) loadResult {
	repl, newDur, newCh, replaced := renderToSeekable(src, data, duration, channels)
	if replaced {
		src.Destroy()
		src = repl
		channels = newCh
		duration = newDur
	}
	return loadResult{src: src, path: path, bpm: bpm, channels: channels, duration: duration, isTracker: true}
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

	// Cancel any in-flight load from a previous track.
	if p.loadCancel != nil {
		p.loadCancel()
	}

	requestID := p.requestID.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	p.loadCancel = cancel

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
		defer cancel()

		if ctx.Err() != nil {
			return
		}

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
			dlPath, err := p.Downloader(ctx, path, 0, onProgress)
			if err != nil {
				if ctx.Err() != nil {
					return // cancelled — discard silently
				}
				p.publishResult(loadResult{path: path, requestID: requestID, err: err})
				return
			}
			localPath = dlPath
		}
		if ctx.Err() != nil {
			return
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
	p.posMu.Lock()
	p.pos = 0
	p.posLast = time.Now()
	p.posValid = src != nil
	p.seekBoth = src != nil && sourceSeeksBoth(src)
	p.posMu.Unlock()
	if src != nil {
		p.current = src
		if p.s != nil {
			p.voice = p.s.PlaySource(src)
		}
	}
}

// sourceSeeksBoth reports whether a source's decoder can seek in both
// directions. FFmpeg, Wav, GME, HVL, PT3, YM and Ayumi re-seek exactly; SID
// returns NOT_IMPLEMENTED for seek entirely, and the openmpt/xmp tracker
// sources only expose SoLoud's generic forward-only sample-discard seek.
func sourceSeeksBoth(src soloud.AudioSource) bool {
	switch src.(type) {
	case *soloud.Sid, *soloud.Openmpt, *soloud.Xmp:
		return false
	case nil:
		return false
	default:
		return true
	}
}

// maxRenderFrames returns how many frames to pre-render for a track of the
// given duration (seconds). Returns 0 when the track is too long to pre-render
// (caller falls back to native streaming). An unknown (<=0) duration defaults
// to the cap so we never over-allocate, since tracker files are typically
// short.
func maxRenderFrames(duration float64) int {
	if duration > maxRenderSeconds {
		return 0
	}
	if duration < 0 {
		duration = 0
	}
	frames := int(duration * renderSampleRate)
	if frames <= 0 {
		frames = int(maxRenderSeconds * renderSampleRate)
	}
	if cap := int(maxRenderSeconds * renderSampleRate); frames > cap {
		frames = cap
	}
	return frames
}

// interleavedToPlanar converts interleaved float32 PCM (samples[c*n…] per
// channel c is produced by decoders as LRLRLR…) into the planar layout SoLoud's
// in-memory Wav expects (channel c spans planar[c*n … (c+1)*n)). Pure function.
func interleavedToPlanar(interleaved []float32, channels int) []float32 {
	nframes := len(interleaved) / channels
	planar := make([]float32, nframes*channels)
	for i := 0; i < nframes; i++ {
		for c := 0; c < channels; c++ {
			planar[c*nframes+i] = interleaved[i*channels+c]
		}
	}
	return planar
}

// renderer is a decoder callback that writes up to maxFrames frames into an
// interleaved (or mono, when channels==1) float32 slice and reports the actual
// frame count. One per supported non-bidirectional format.
type renderer func(maxFrames int) (samples []float32, channels, frames int, err error)

// rendererFor returns a renderer for the given source type, or nil if the
// source already seeks natively in both directions and needs no pre-render.
func rendererFor(src soloud.AudioSource, data []byte) renderer {
	switch src.(type) {
	case *soloud.Openmpt:
		return func(mf int) ([]float32, int, int, error) {
			return openmpt.Render(data, renderSampleRate, mf)
		}
	case *soloud.Xmp:
		return func(mf int) ([]float32, int, int, error) {
			return xmp.Render(data, renderSampleRate, mf)
		}
	case *soloud.Ym:
		return func(mf int) ([]float32, int, int, error) {
			return soloud.RenderYm(data, mf)
		}
	case *soloud.Sid:
		return func(mf int) ([]float32, int, int, error) {
			return soloud.RenderSid(data, mf)
		}
	default:
		return nil
	}
}

// renderToSeekable pre-renders a non-bidirectional source (openmpt/xmp/YM/SID)
// into a memory-backed Wav so the player can seek exactly in both directions.
// If the source already seeks natively, or the track is longer than
// maxRenderSeconds, it returns src unchanged. On success the returned Wav
// replaces src and the caller must destroy the original src.
func renderToSeekable(src soloud.AudioSource, data []byte, duration float64, channels int) (replacement soloud.AudioSource, newDuration float64, newChannels int, replaced bool) {
	render := rendererFor(src, data)
	if render == nil {
		return src, duration, channels, false
	}
	mf := maxRenderFrames(duration)
	if mf <= 0 {
		// Over the cap: keep native streaming (backward seek restarts).
		return src, duration, channels, false
	}
	samples, rendCh, frames, err := render(mf)
	if err != nil || frames <= 0 {
		slog.Warn("render-to-buffer failed, using native playback", "err", err, "frames", frames)
		return src, duration, channels, false
	}
	if rendCh < 1 {
		slog.Warn("render-to-buffer bad channels", "channels", rendCh)
		return src, duration, channels, false
	}
	planar := interleavedToPlanar(samples, rendCh)
	wav, err := soloud.NewWavFromSamples(planar, renderSampleRate, rendCh)
	if err != nil {
		slog.Warn("render-to-buffer wav build failed, using native playback", "err", err)
		return src, duration, channels, false
	}
	slog.Info("pre-rendered track for exact seeking",
		"seconds", float64(frames)/renderSampleRate, "frames", frames, "channels", rendCh)
	newDuration = wav.GetLength()
	newChannels = channels
	return wav, newDuration, newChannels, true
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
	if p.loadCancel != nil {
		p.loadCancel()
		p.loadCancel = nil
	}
	if p.s != nil {
		p.s.StopAll()
	}
	p.voice = 0
	if p.current != nil {
		p.current.Destroy()
		p.current = nil
	}
	p.posMu.Lock()
	p.posValid = false
	p.pos = 0
	p.seekBoth = false
	p.posMu.Unlock()
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
	p.posMu.Lock()
	defer p.posMu.Unlock()

	// Once a source is gone (or we're loading), position is meaningless.
	if !p.posValid {
		return 0
	}
	// Refresh posLast on every read so a resume after pause (or a long stall)
	// doesn't jump the clock by the whole idle span. The clock only advances
	// while playing a live, unpaused voice.
	now := time.Now()
	p.pos = advancePositionClock(p.pos, p.voice != 0 && p.s != nil && !p.s.GetPause(p.voice), now.Sub(p.posLast).Seconds())
	p.posLast = now
	return p.pos
}

// advancePositionClock advances the authoritative position by dt seconds when
// the voice is playing; an idle (paused/stalled) span never advances the
// clock, and a single dt is capped to avoid a giant wall-clock jump.
func advancePositionClock(pos float64, playing bool, dt float64) float64 {
	if !playing {
		return pos
	}
	if dt < 0 {
		dt = 0
	}
	if dt > 10 {
		dt = 10
	}
	return pos + dt
}

// Seek moves the current track position in seconds and updates the
// authoritative clock so Position() reflects the jump immediately.
func (p *Player) Seek(seconds float64) error {
	if !p.IsValidVoice() {
		return fmt.Errorf("no active voice")
	}
	if seconds < 0 {
		seconds = 0
	}
	if dur := p.Duration(); dur > 0 && seconds > dur {
		seconds = dur
	}

	p.posMu.Lock()
	defer p.posMu.Unlock()

	// Formats that cannot seek backward (SID, openmpt/xmp trackers) restart
	// from the beginning instead of failing silently: their seek()/rewind()
	// return NOT_IMPLEMENTED, so we replay the source to get a fresh decoder.
	if !p.seekBoth && seconds < p.pos {
		if p.current != nil && p.s != nil {
			p.s.StopAll()
			p.voice = p.s.PlaySource(p.current)
			p.pos = 0
			p.posLast = time.Now()
			p.posValid = true
			return nil
		}
		return fmt.Errorf("cannot seek backward on this source")
	}

	if err := p.s.Seek(p.voice, seconds); err != nil {
		return err
	}
	p.pos = seconds
	p.posLast = time.Now()
	p.posValid = true
	return nil
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

// BackendInfo returns SoLoud audio backend details.
func (p *Player) BackendInfo() (backend string, samplerate, channels, bufferSize uint, soloudVersion uint) {
	if p.s == nil {
		return
	}
	backend = p.s.GetBackendString()
	samplerate = p.s.GetBackendSamplerate()
	channels = p.s.GetBackendChannels()
	bufferSize = p.s.GetBackendBufferSize()
	soloudVersion = p.s.GetVersion()
	return
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
