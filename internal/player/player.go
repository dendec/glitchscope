// Package player provides audio playback via SoLoud.
package player

import (
	"fmt"
	"log/slog"

	"github.com/dendec/mdpp/internal/soloud"
)

// Player manages audio playback.
type Player struct {
	s     *soloud.Soloud
	voice uint
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
func (p *Player) PlayFile(path string) error {
	w, err := soloud.LoadWav(path)
	if err != nil {
		return fmt.Errorf("load %s: %w", path, err)
	}
	p.Stop()
	p.voice = p.s.Play(w)
	slog.Info("playing", "path", path)
	return nil
}

// GetWave returns the current SoLoud waveform data (256 float32 samples).
// Returns nil if visualization is not enabled.
func (p *Player) GetWave() []float32 {
	return p.s.GetWave()
}

// GetFFT returns the current SoLoud FFT data (256 float32 bins).
// Returns nil if visualization is not enabled.
func (p *Player) GetFFT() []float32 {
	return p.s.CalcFFT()
}

// Stop stops all playback.
func (p *Player) Stop() {
	p.s.StopAll()
	p.voice = 0
}

// Close shuts down the player.
func (p *Player) Close() {
	p.Stop()
	p.s.Deinit()
	p.s.Destroy()
	slog.Info("SoLoud shut down")
}
