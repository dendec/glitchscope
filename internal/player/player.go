// Package player provides audio playback via SoLoud.
package player

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/dendec/pmv/internal/openmpt"
	"github.com/dendec/pmv/internal/soloud"
	"github.com/dendec/pmv/internal/xmp"
)

type pendingLoad struct {
	path     string
	wav      *soloud.Wav     // non-nil for WAV/MP3/FLAC etc.
	mod      *soloud.Xmp     // non-nil for libxmp-supported formats
	ompt     *soloud.Openmpt // non-nil for libopenmpt fallback formats
	gme      *soloud.Gme     // non-nil for Game Music Emu formats
	ayumi    *soloud.Ayumi   // non-nil for AY/YM VTX formats
	pt3      *soloud.Pt3     // non-nil for PT3 formats
	ym       *soloud.Ym      // non-nil for YM/LHA formats
	bpm      float64         // tracker metadata
	channels int
	duration float64
	err      error
}

type Player struct {
	s               *soloud.Soloud
	voice           uint
	currentWav      *soloud.Wav     // non-nil when playing WAV/MP3/FLAC etc.
	currentMod      *soloud.Xmp     // non-nil when playing via libxmp
	currentOmpt     *soloud.Openmpt // non-nil when playing via libopenmpt
	currentGme      *soloud.Gme     // non-nil when playing via Game Music Emu
	currentAyumi    *soloud.Ayumi   // non-nil when playing via AY/YM VTX
	currentPt3      *soloud.Pt3     // non-nil when playing via PT3
	currentYm       *soloud.Ym      // non-nil when playing via YM/LHA
	currentPath     string
	currentBPM      float64
	currentDuration float64
	currentBitrate  float64
	channels        int
	isTracker       bool

	// Downloader optionally fetches a remote file to a local path (e.g. for modland).
	// onProgress, if non-nil, should be called with (bytesRead, totalBytes) as the
	// download progresses; totalBytes may be 0 if unknown.
	Downloader  func(path string, expectedSize int64, onProgress func(read, total int64)) (string, error)
	pendingCh   chan pendingLoad // background load results
	loading     atomic.Bool      // true while an async load is in flight
	loadPercent atomic.Int64     // download progress percent [0..100], -1 if unknown
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
	return &Player{s: s, pendingCh: make(chan pendingLoad, 1)}, nil
}

func isTrackerExt(ext string) bool {
	return xmp.SupportedExts[ext] || openmpt.SupportedExts[ext]
}

func isGmeExt(ext string) bool {
	switch ext {
	case ".nsf", ".nsfe", ".spc", ".gbs", ".hes", ".kss", ".sgc", ".sap", ".vgm", ".vgz":
		return true
	default:
		return false
	}
}

func isAyumiExt(ext string) bool { return ext == ".vtx" }

func isPt3Ext(ext string) bool { return ext == ".pt3" }

func isYmExt(ext string) bool { return ext == ".ym" || ext == ".lh" || ext == ".lha" }

// PlayFile loads and plays an audio file. Only one file at a time.
func (p *Player) PlayFile(path string) error {
	ext := strings.ToLower(filepath.Ext(path))
	if isTrackerExt(ext) {
		return p.playTracker(path)
	}
	if isGmeExt(ext) {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("gme read %s: %w", path, err)
		}
		gme, err := soloud.NewGme(data)
		if err != nil {
			return fmt.Errorf("gme %s: %w", path, err)
		}
		p.replaceSource(nil, nil, nil, gme)
		p.currentPath = path
		p.currentDuration = gme.GetLength()
		p.currentBPM = 0
		p.currentBitrate = fileBitrate(path, p.currentDuration)
		p.channels = 2
		p.isTracker = true
		slog.Info("playing gme", "path", path, "tracks", gme.GetTrackCount())
		return nil
	}
	if isAyumiExt(ext) {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("vtx read %s: %w", path, err)
		}
		ayumi, err := soloud.NewAyumi(data)
		if err != nil {
			return fmt.Errorf("vtx %s: %w", path, err)
		}
		p.replaceSource(nil, nil, nil, nil, ayumi)
		p.currentPath = path
		p.currentDuration = ayumi.GetLength()
		p.channels = 2
		p.isTracker = true
		return nil
	}
	if isPt3Ext(ext) {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("pt3 read %s: %w", path, err)
		}
		pt3, err := soloud.NewPt3(data)
		if err != nil {
			return fmt.Errorf("pt3 %s: %w", path, err)
		}
		p.replacePt3Source(pt3)
		p.currentPath = path
		p.currentDuration = pt3.GetLength()
		p.channels = 2
		p.isTracker = true
		return nil
	}
	if isYmExt(ext) {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("ym read %s: %w", path, err)
		}
		ym, err := soloud.NewYm(data)
		if err != nil {
			return fmt.Errorf("ym %s: %w", path, err)
		}
		p.replaceYmSource(ym)
		p.currentPath = path
		p.currentDuration = ym.GetLength()
		p.channels = 2
		p.isTracker = true
		return nil
	}

	w, err := soloud.LoadWav(path)
	if err != nil {
		return fmt.Errorf("load %s: %w", path, err)
	}
	p.replaceSource(w, nil, nil, nil)
	p.currentPath = path
	p.currentDuration = w.GetLength()
	p.currentBPM = 0
	p.currentBitrate = fileBitrate(path, p.currentDuration)
	p.channels = w.GetChannels()
	p.isTracker = false
	slog.Info("playing", "path", path)
	return nil
}

// PlayFileAsync starts loading in a background goroutine.
// Call CheckPending() from the main loop to pick up the result.
func (p *Player) PlayFileAsync(path string) {
	select {
	case <-p.pendingCh:
	default:
	}

	p.s.StopAll()
	p.voice = 0
	p.currentPath = path // show path in UI while loading
	p.loading.Store(true)
	p.loadPercent.Store(-1)

	// loading is cleared by CheckPending, NOT here — a newer goroutine
	// may already be running when this one ends.
	go func() {
		localPath := path
		if p.Downloader != nil {
			onProgress := func(read, total int64) {
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
			dlPath, err := p.Downloader(path, 0, onProgress) // size unknown at call site
			if err != nil {
				p.pendingCh <- pendingLoad{path: path, err: err}
				return
			}
			localPath = dlPath
		}
		p.loadPercent.Store(-1) // decode phase: unknown progress

		ext := strings.ToLower(filepath.Ext(localPath))
		if isTrackerExt(ext) {
			fileBuf, err := os.ReadFile(localPath)
			if err != nil {
				p.pendingCh <- pendingLoad{path: path, err: err}
				return
			}
			// Try libopenmpt first (broader format support), fallback to libxmp.
			if openmpt.HasExt(ext) {
				ompt, err := soloud.NewOpenmpt(fileBuf)
				if err == nil {
					bpm, ch, dur, _ := openmpt.GetTrackerMeta(fileBuf)
					p.pendingCh <- pendingLoad{path: path, ompt: ompt, bpm: bpm, channels: ch, duration: dur}
					return
				}
				slog.Debug("openmpt fallback to xmp", "ext", ext, "err", err)
			}
			mod, err := soloud.NewXmp(fileBuf)
			if err != nil {
				p.pendingCh <- pendingLoad{path: path, err: err}
				return
			}
			bpm, ch, dur, _ := xmp.GetTrackerMetaFromBytes(fileBuf)
			p.pendingCh <- pendingLoad{path: path, mod: mod, bpm: bpm, channels: ch, duration: dur}
			return
		}
		if isGmeExt(ext) {
			fileBuf, err := os.ReadFile(localPath)
			if err != nil {
				p.pendingCh <- pendingLoad{path: path, err: err}
				return
			}
			gme, err := soloud.NewGme(fileBuf)
			if err != nil {
				p.pendingCh <- pendingLoad{path: path, err: err}
				return
			}
			p.pendingCh <- pendingLoad{path: path, gme: gme}
			return
		}
		if isAyumiExt(ext) {
			fileBuf, err := os.ReadFile(localPath)
			if err != nil {
				p.pendingCh <- pendingLoad{path: path, err: err}
				return
			}
			ayumi, err := soloud.NewAyumi(fileBuf)
			if err != nil {
				p.pendingCh <- pendingLoad{path: path, err: err}
				return
			}
			p.pendingCh <- pendingLoad{path: path, ayumi: ayumi}
			return
		}
		if isPt3Ext(ext) {
			fileBuf, err := os.ReadFile(localPath)
			if err != nil {
				p.pendingCh <- pendingLoad{path: path, err: err}
				return
			}
			pt3, err := soloud.NewPt3(fileBuf)
			if err != nil {
				p.pendingCh <- pendingLoad{path: path, err: err}
				return
			}
			p.pendingCh <- pendingLoad{path: path, pt3: pt3}
			return
		}
		if isYmExt(ext) {
			fileBuf, err := os.ReadFile(localPath)
			if err != nil {
				p.pendingCh <- pendingLoad{path: path, err: err}
				return
			}
			ym, err := soloud.NewYm(fileBuf)
			if err != nil {
				p.pendingCh <- pendingLoad{path: path, err: err}
				return
			}
			p.pendingCh <- pendingLoad{path: path, ym: ym}
			return
		}
		w, err := soloud.LoadWav(localPath)
		p.pendingCh <- pendingLoad{path: path, wav: w, err: err}
	}()
}

func (p *Player) Loading() bool {
	return p.loading.Load()
}

// LoadProgress returns whether an async load is in flight and download
// progress percent [0..100], or -1 when unknown (decode phase or no download).
func (p *Player) LoadProgress() (active bool, percent int64) {
	return p.loading.Load(), p.loadPercent.Load()
}

// CheckPending picks up a completed background load.
// Returns (started, failed). Call every frame.
func (p *Player) CheckPending() (started bool, failed bool) {
	select {
	case res := <-p.pendingCh:
		if res.path != p.currentPath {
			if res.wav != nil {
				res.wav.Destroy()
			}
			if res.mod != nil {
				res.mod.Destroy()
			}
			if res.ompt != nil {
				res.ompt.Destroy()
			}
			if res.gme != nil {
				res.gme.Destroy()
			}
			if res.ayumi != nil {
				res.ayumi.Destroy()
			}
			if res.pt3 != nil {
				res.pt3.Destroy()
			}
			if res.ym != nil {
				res.ym.Destroy()
			}
			return false, false
		}
		p.loading.Store(false)
		if res.err != nil {
			slog.Error("async load", "path", res.path, "error", res.err)
			p.currentPath = "" // clear failed path so player state is consistent
			return false, true
		}
		if res.mod != nil {
			p.replaceSource(nil, res.mod, nil, nil)
			p.currentPath = res.path
			p.currentBPM = res.bpm
			p.currentBitrate = 0
			p.channels = res.channels
			p.isTracker = true
			p.currentDuration = res.duration
			slog.Info("tracker xmp (async)", "path", res.path)
		} else if res.ompt != nil {
			p.replaceSource(nil, nil, res.ompt, nil)
			p.currentPath = res.path
			p.currentBPM = res.bpm
			p.currentBitrate = 0
			p.channels = res.channels
			p.isTracker = true
			p.currentDuration = res.duration
			slog.Info("tracker openmpt (async)", "path", res.path)
		} else if res.wav != nil {
			p.replaceSource(res.wav, nil, nil, nil)
			p.currentPath = res.path
			p.currentDuration = res.wav.GetLength()
			p.currentBPM = 0
			p.currentBitrate = fileBitrate(res.path, p.currentDuration)
			p.channels = res.wav.GetChannels()
			p.isTracker = false
			slog.Info("playing (async)", "path", res.path)
		} else if res.gme != nil {
			p.replaceSource(nil, nil, nil, res.gme)
			p.currentPath = res.path
			p.currentDuration = res.gme.GetLength()
			p.currentBPM = 0
			p.currentBitrate = fileBitrate(res.path, p.currentDuration)
			p.channels = 2
			p.isTracker = true
			slog.Info("playing gme (async)", "path", res.path, "tracks", res.gme.GetTrackCount())
		} else if res.ayumi != nil {
			p.replaceSource(nil, nil, nil, nil, res.ayumi)
			p.currentPath = res.path
			p.currentDuration = res.ayumi.GetLength()
			p.currentBPM = 0
			p.currentBitrate = fileBitrate(res.path, p.currentDuration)
			p.channels = 2
			p.isTracker = true
			slog.Info("playing vtx (async)", "path", res.path)
		} else if res.pt3 != nil {
			p.replacePt3Source(res.pt3)
			p.currentPath = res.path
			p.currentDuration = res.pt3.GetLength()
			p.currentBPM = 0
			p.currentBitrate = fileBitrate(res.path, p.currentDuration)
			p.channels = 2
			p.isTracker = true
			slog.Info("playing pt3 (async)", "path", res.path)
		} else if res.ym != nil {
			p.replaceYmSource(res.ym)
			p.currentPath = res.path
			p.currentDuration = res.ym.GetLength()
			p.currentBPM = 0
			p.currentBitrate = fileBitrate(res.path, p.currentDuration)
			p.channels = 2
			p.isTracker = true
			slog.Info("playing ym (async)", "path", res.path)
		}
		return true, false
	default:
		return false, false
	}
}

func (p *Player) playTracker(path string) error {
	fileBuf, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("tracker read %s: %w", path, err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	// Try libopenmpt first (broader format support), fallback to libxmp.
	if openmpt.HasExt(ext) {
		ompt, err := soloud.NewOpenmpt(fileBuf)
		if err == nil {
			bpm, ch, dur, _ := openmpt.GetTrackerMeta(fileBuf)
			p.replaceSource(nil, nil, ompt, nil)
			p.currentPath = path
			p.currentBPM = bpm
			p.currentBitrate = 0
			p.channels = ch
			p.isTracker = true
			p.currentDuration = dur
			slog.Info("tracker openmpt", "path", path, "bpm", bpm, "channels", ch, "duration", dur)
			return nil
		}
		slog.Debug("openmpt fallback to xmp", "ext", ext, "err", err)
	}

	mod, err := soloud.NewXmp(fileBuf)
	if err != nil {
		return fmt.Errorf("tracker %s: %w", path, err)
	}
	p.replaceSource(nil, mod, nil, nil)
	p.currentPath = path

	// Metadata via lightweight libxmp probe (no decode).
	bpm, ch, dur, err := xmp.GetTrackerMetaFromBytes(fileBuf)
	if err != nil {
		slog.Warn("tracker meta", "path", path, "err", err)
	}
	p.currentBPM = bpm
	p.currentBitrate = 0
	p.channels = ch
	p.isTracker = true
	p.currentDuration = dur
	slog.Info("tracker xmp", "path", path, "bpm", bpm, "channels", ch, "duration", dur)
	return nil
}

// replaceSource stops current playback and swaps in a new audio source.
func (p *Player) replaceSource(w *soloud.Wav, mod *soloud.Xmp, ompt *soloud.Openmpt, gme *soloud.Gme, ayumi ...*soloud.Ayumi) {
	p.s.StopAll()
	if p.currentWav != nil {
		p.currentWav.Destroy()
		p.currentWav = nil
	}
	if p.currentMod != nil {
		p.currentMod.Destroy()
		p.currentMod = nil
	}
	if p.currentOmpt != nil {
		p.currentOmpt.Destroy()
		p.currentOmpt = nil
	}
	if p.currentGme != nil {
		p.currentGme.Destroy()
		p.currentGme = nil
	}
	if p.currentAyumi != nil {
		p.currentAyumi.Destroy()
		p.currentAyumi = nil
	}
	if p.currentPt3 != nil {
		p.currentPt3.Destroy()
		p.currentPt3 = nil
	}
	if p.currentYm != nil {
		p.currentYm.Destroy()
		p.currentYm = nil
	}
	if w != nil {
		p.currentWav = w
		p.voice = p.s.Play(w)
	} else if mod != nil {
		p.currentMod = mod
		p.voice = p.s.PlayXmp(mod)
	} else if ompt != nil {
		p.currentOmpt = ompt
		p.voice = p.s.PlayOpenmpt(ompt)
	} else if gme != nil {
		p.currentGme = gme
		p.voice = p.s.PlayGme(gme)
	} else if len(ayumi) > 0 && ayumi[0] != nil {
		p.currentAyumi = ayumi[0]
		p.voice = p.s.PlayAyumi(ayumi[0])
	}
}

func (p *Player) replacePt3Source(pt3 *soloud.Pt3) {
	p.replaceSource(nil, nil, nil, nil)
	p.currentPt3 = pt3
	p.voice = p.s.PlayPt3(pt3)
}

func (p *Player) replaceYmSource(ym *soloud.Ym) {
	p.replaceSource(nil, nil, nil, nil)
	p.currentYm = ym
	p.voice = p.s.PlayYm(ym)
}

// GetWave returns the current SoLoud waveform data (256 float32 samples).
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
// For trackers, also checks estimated duration since many contain internal
// loops that never signal end-of-stream.
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
	p.s.StopAll()
	p.voice = 0
	if p.currentWav != nil {
		p.currentWav.Destroy()
		p.currentWav = nil
	}
	if p.currentMod != nil {
		p.currentMod.Destroy()
		p.currentMod = nil
	}
	if p.currentOmpt != nil {
		p.currentOmpt.Destroy()
		p.currentOmpt = nil
	}
	if p.currentGme != nil {
		p.currentGme.Destroy()
		p.currentGme = nil
	}
	p.currentPath = ""
}

func (p *Player) Close() {
	p.Stop()
	p.s.Destroy()
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
	if p.currentWav != nil {
		return p.currentWav.GetLength()
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
