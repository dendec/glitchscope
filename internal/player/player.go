// Package player provides audio playback via SoLoud.
package player

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/dendec/mdpp/internal/openmpt"
	"github.com/dendec/mdpp/internal/soloud"
)

// pendingLoad is the result of a background file load.
type pendingLoad struct {
	path     string
	wav      *soloud.Wav     // non-nil for WAV/MP3/FLAC etc.
	mod      *soloud.Openmpt // non-nil for tracker formats
	bpm      float64         // tracker metadata
	channels int
	duration float64
	err      error
}

// Player manages audio playback.
type Player struct {
	s               *soloud.Soloud
	voice           uint
	currentWav      *soloud.Wav     // non-nil when playing WAV/MP3/FLAC etc.
	currentMod      *soloud.Openmpt // non-nil when playing tracker module
	currentPath     string
	currentBPM      float64
	currentDuration float64
	currentBitrate  float64
	channels        int
	isTracker       bool

	Downloader func(path string, expectedSize int64) (string, error) // optional remote downloader (e.g. for modland)
	pendingCh  chan pendingLoad                  // background load results
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

// PlayFile loads and plays an audio file. Only one file at a time.
// Tracker formats (.mod/.xm/.it/.s3m/…) are streamed via SoLoud's built-in Openmpt.
func (p *Player) PlayFile(path string) error {
	ext := strings.ToLower(filepath.Ext(path))
	if openmpt.SupportedExts[ext] {
		return p.playTracker(path)
	}

	w, err := soloud.LoadWav(path)
	if err != nil {
		return fmt.Errorf("load %s: %w", path, err)
	}
	p.replaceSource(w, nil)
	p.currentPath = path
	p.currentDuration = w.GetLength()
	p.currentBPM = 0
	p.currentBitrate = fileBitrate(path, p.currentDuration)
	p.channels = w.GetChannels()
	p.isTracker = false
	slog.Info("playing", "path", path)
	return nil
}

// PlayFileAsync starts loading a file in a background goroutine.
// Call CheckPending() from the main loop to pick up the result.
// Stops current playback immediately so the UI feels responsive.
func (p *Player) PlayFileAsync(path string) {
	// Drain any pending result from a previous load.
	select {
	case <-p.pendingCh:
	default:
	}

	// Stop current playback immediately.
	p.s.StopAll()
	p.voice = 0
	p.currentPath = path // show path in UI while loading

	// Launch background load.
	go func() {
		localPath := path
		if p.Downloader != nil {
			dlPath, err := p.Downloader(path, 0) // size unknown at call site
			if err != nil {
				p.pendingCh <- pendingLoad{path: path, err: err}
				return
			}
			localPath = dlPath
		}

		ext := strings.ToLower(filepath.Ext(localPath))
		if openmpt.SupportedExts[ext] {
			fileBuf, err := os.ReadFile(localPath)
			if err != nil {
				p.pendingCh <- pendingLoad{path: path, err: err}
				return
			}
			mod, err := soloud.NewOpenmpt(fileBuf)
			if err != nil {
				p.pendingCh <- pendingLoad{path: path, err: err}
				return
			}
			bpm, ch, dur, _ := openmpt.GetTrackerMetaFromBytes(fileBuf)
			p.pendingCh <- pendingLoad{path: path, mod: mod, bpm: bpm, channels: ch, duration: dur}
			return
		}
		w, err := soloud.LoadWav(localPath)
		p.pendingCh <- pendingLoad{path: path, wav: w, err: err}
	}()
}

// CheckPending picks up a completed background load and starts playback.
// Returns true if a track was started. Call every frame from main loop.
func (p *Player) CheckPending() bool {
	select {
	case res := <-p.pendingCh:
		if res.path != p.currentPath {
			// User selected a different track while loading; clean up loaded sources.
			if res.wav != nil {
				res.wav.Destroy()
			}
			if res.mod != nil {
				res.mod.Destroy()
			}
			return false
		}
		if res.err != nil {
			slog.Error("async load", "path", res.path, "error", res.err)
			p.currentPath = "" // clear failed path so player state is consistent
			return false
		}
		if res.mod != nil {
			p.replaceSource(nil, res.mod)
			p.currentPath = res.path
			p.currentBPM = res.bpm
			p.currentBitrate = 0
			p.channels = res.channels
			p.isTracker = true
			p.currentDuration = res.duration
			slog.Info("tracker (async)", "path", res.path)
		} else if res.wav != nil {
			p.replaceSource(res.wav, nil)
			p.currentPath = res.path
			p.currentDuration = res.wav.GetLength()
			p.currentBPM = 0
			p.currentBitrate = fileBitrate(res.path, p.currentDuration)
			p.channels = res.wav.GetChannels()
			p.isTracker = false
			slog.Info("playing (async)", "path", res.path)
		}
		return true
	default:
		return false
	}
}

func (p *Player) playTracker(path string) error {
	fileBuf, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("tracker read %s: %w", path, err)
	}
	mod, err := soloud.NewOpenmpt(fileBuf)
	if err != nil {
		return fmt.Errorf("tracker %s: %w", path, err)
	}
	p.replaceSource(nil, mod)
	p.currentPath = path

	// Metadata via lightweight libopenmpt probe (no decode).
	bpm, ch, dur, err := openmpt.GetTrackerMetaFromBytes(fileBuf)
	if err != nil {
		slog.Warn("tracker meta", "path", path, "err", err)
	}
	p.currentBPM = bpm
	p.currentBitrate = 0
	p.channels = ch
	p.isTracker = true
	p.currentDuration = dur
	slog.Info("tracker", "path", path, "bpm", bpm, "channels", ch, "duration", dur)
	return nil
}

// replaceSource stops current playback and swaps in a new audio source.
func (p *Player) replaceSource(w *soloud.Wav, mod *soloud.Openmpt) {
	p.s.StopAll()
	if p.currentWav != nil {
		p.currentWav.Destroy()
		p.currentWav = nil
	}
	if p.currentMod != nil {
		p.currentMod.Destroy()
		p.currentMod = nil
	}
	if w != nil {
		p.currentWav = w
		p.voice = p.s.Play(w)
	} else if mod != nil {
		p.currentMod = mod
		p.voice = p.s.PlayOpenmpt(mod)
	}
}

// GetWave returns the current SoLoud waveform data (256 float32 samples).
// Returns nil if visualization is not enabled.
func (p *Player) GetWave() []float32 {
	return p.s.GetWave()
}

// Pause pauses playback. No-op if nothing playing.
func (p *Player) Pause() {
	if p.voice == 0 {
		return
	}
	p.s.SetPause(p.voice, true)
}

// Resume resumes playback. No-op if nothing playing.
func (p *Player) Resume() {
	if p.voice == 0 {
		return
	}
	p.s.SetPause(p.voice, false)
}

// TogglePause switches between paused and playing. No-op if nothing playing.
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

// Voice returns the current SoLoud voice handle. 0 = nothing playing.
func (p *Player) Voice() uint {
	return p.voice
}

// IsValidVoice returns true if the current voice is still playing.
func (p *Player) IsValidVoice() bool {
	if p.voice == 0 {
		return false
	}
	return p.s.IsValidVoiceHandle(p.voice)
}

// Stop stops all playback.
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
	p.currentPath = ""
}

// Close shuts down the player.
func (p *Player) Close() {
	p.Stop()
	p.s.Destroy()
	slog.Info("SoLoud shut down")
}

// Position returns the current playback position in seconds.
func (p *Player) Position() float64 {
	if p.voice == 0 {
		return 0
	}
	return p.s.GetStreamTime(p.voice)
}

// Duration returns the total duration of the current track in seconds.
func (p *Player) Duration() float64 {
	if p.currentWav != nil {
		return p.currentWav.GetLength()
	}
	return p.currentDuration
}

// SampleRate returns the sample rate of the current voice.
func (p *Player) SampleRate() float32 {
	if p.voice == 0 {
		return 0
	}
	return p.s.GetSamplerate(p.voice)
}

// Channels returns the source channel count or tracker channel count.
func (p *Player) Channels() int {
	return p.channels
}

// IsTracker reports whether the current source is a tracker module.
func (p *Player) IsTracker() bool {
	return p.isTracker
}

// BPM returns the current track's BPM (0 for non-tracker).
func (p *Player) BPM() float64 {
	return p.currentBPM
}

// IsPaused returns true if the current voice is paused.
func (p *Player) IsPaused() bool {
	if p.voice == 0 {
		return false
	}
	return p.s.GetPause(p.voice)
}

// TrackPath returns the path of the currently playing track.
func (p *Player) TrackPath() string {
	return p.currentPath
}

// Bitrate returns the approximate encoded bitrate in kilobits per second.
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
