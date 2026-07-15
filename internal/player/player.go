// Package player provides audio playback via SoLoud.
package player

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/dendec/mdpp/internal/openmpt"
	"github.com/dendec/mdpp/internal/soloud"
)

// Player manages audio playback.
type Player struct {
	s           *soloud.Soloud
	voice       uint
	currentWav  *soloud.Wav // keep alive while SoLoud references it
	currentPath string
	currentBPM  float64
	channels    int
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
	return &Player{s: s}, nil
}

// PlayFile loads and plays an audio file. Only one file at a time.
// Tracker formats (.mod/.xm/.it/.s3m/…) are decoded via libopenmpt.
func (p *Player) PlayFile(path string) error {
	ext := strings.ToLower(filepath.Ext(path))
	if openmpt.SupportedExts[ext] {
		return p.playTracker(path)
	}

	w, err := soloud.LoadWav(path)
	if err != nil {
		return fmt.Errorf("load %s: %w", path, err)
	}
	p.replaceSource(w)
	p.currentPath = path
	slog.Info("playing", "path", path)
	return nil
}

func (p *Player) playTracker(path string) error {
	data, sr, bpm, ch, err := openmpt.DecodeToF32(path)
	if err != nil {
		return fmt.Errorf("tracker %s: %w", path, err)
	}
	w, err := soloud.NewWavFromF32(data, float32(sr), 2)
	if err != nil {
		return fmt.Errorf("tracker wav %s: %w", path, err)
	}
	p.replaceSource(w)
	p.currentPath = path
	p.currentBPM = bpm
	p.channels = ch
	slog.Info("tracker", "path", path, "samples", len(data), "sr", sr, "bpm", bpm, "channels", ch)
	return nil
}

// replaceSource stops current playback and swaps in a new Wav source.
func (p *Player) replaceSource(w *soloud.Wav) {
	p.s.StopAll()
	old := p.currentWav
	p.currentWav = nil
	// Mixer is free again. StopAll already cleared all voices,
	// so it's safe to destroy the old Wav — no voice references it.
	if old != nil {
		old.Destroy()
	}
	p.voice = p.s.Play(w)
	p.currentWav = w
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
	if p.currentWav == nil {
		return 0
	}
	return p.currentWav.GetLength()
}

// SampleRate returns the sample rate of the current voice.
func (p *Player) SampleRate() float32 {
	if p.voice == 0 {
		return 0
	}
	return p.s.GetSamplerate(p.voice)
}

// Channels returns the number of tracker channels (0 for non-tracker).
func (p *Player) Channels() int {
	return p.channels
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
