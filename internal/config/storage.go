package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// SettingsPath returns the settings file location.
// On PortMaster, XDG_DATA_HOME points to /roms/ports/glitchscope/conf/.
func SettingsPath() string {
	if p := os.Getenv("XDG_DATA_HOME"); p != "" {
		return filepath.Join(p, "settings.json")
	}
	return "settings.json"
}

// FavoritesPath returns the favorites file location, adjacent to settings.json.
func FavoritesPath() string {
	if p := os.Getenv("XDG_DATA_HOME"); p != "" {
		return filepath.Join(p, "favorites.json")
	}
	return "favorites.json"
}

// LoadSettings reads and validates a settings file. Missing files return defaults.
func LoadSettings(path string) (Settings, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultSettings(), nil
		}
		return DefaultSettings(), fmt.Errorf("settings open: %w", err)
	}
	defer func() { _ = f.Close() }()

	var raw struct {
		Graphics *struct {
			RenderWidth     *int             `json:"render_width"`
			RenderHeight    *int             `json:"render_height"`
			UpscaleFilter   *UpscaleFilter   `json:"upscale_filter"`
			Adaptive        *bool            `json:"adaptive"`
			BeatSensitivity *float64         `json:"beat_sensitivity"`
			PerformanceMode *PerformanceMode `json:"performance_mode"`
		} `json:"graphics"`
		Playback *struct {
			ShuffleMode  *ShuffleMode `json:"shuffle_mode"`
			Repeat       *RepeatMode  `json:"repeat"`
			LastPosition *struct {
				Path    *string  `json:"path"`
				Seconds *float64 `json:"seconds"`
			} `json:"last_position"`
		} `json:"playback"`
		PresetInterval *PresetInterval `json:"preset_interval"`
		UI             *struct {
			Theme        *Theme        `json:"theme"`
			Transparency *Transparency `json:"transparency"`
			ShowStats    *bool         `json:"show_stats"`
		} `json:"ui"`
	}
	if err := json.NewDecoder(f).Decode(&raw); err != nil {
		slog.Warn("settings: malformed JSON", "path", path, "error", err)
		return DefaultSettings(), fmt.Errorf("settings decode: %w", err)
	}

	s := DefaultSettings()

	if raw.Graphics != nil {
		if raw.Graphics.RenderWidth != nil {
			s.Graphics.RenderWidth = *raw.Graphics.RenderWidth
		}
		if raw.Graphics.RenderHeight != nil {
			s.Graphics.RenderHeight = *raw.Graphics.RenderHeight
		}
		if raw.Graphics.UpscaleFilter != nil {
			s.Graphics.UpscaleFilter = *raw.Graphics.UpscaleFilter
		}
		if raw.Graphics.Adaptive != nil {
			s.Graphics.Adaptive = *raw.Graphics.Adaptive
		}
		if raw.Graphics.BeatSensitivity != nil {
			s.Graphics.BeatSensitivity = *raw.Graphics.BeatSensitivity
		}
		if raw.Graphics.PerformanceMode != nil {
			s.Graphics.PerformanceMode = *raw.Graphics.PerformanceMode
		}
	}
	if raw.Playback != nil {
		if raw.Playback.ShuffleMode != nil {
			s.Playback.ShuffleMode = *raw.Playback.ShuffleMode
		}
		if raw.Playback.Repeat != nil {
			s.Playback.Repeat = *raw.Playback.Repeat
		}
		if raw.Playback.LastPosition != nil {
			if raw.Playback.LastPosition.Path != nil {
				s.Playback.LastPosition.Path = *raw.Playback.LastPosition.Path
			}
			if raw.Playback.LastPosition.Seconds != nil {
				s.Playback.LastPosition.Seconds = *raw.Playback.LastPosition.Seconds
			}
		}
	}
	if raw.PresetInterval != nil {
		s.PresetInterval = *raw.PresetInterval
	}
	if raw.UI != nil {
		if raw.UI.Theme != nil {
			s.UI.Theme = *raw.UI.Theme
		}
		if raw.UI.Transparency != nil {
			s.UI.Transparency = *raw.UI.Transparency
		}
		if raw.UI.ShowStats != nil {
			s.UI.ShowStats = *raw.UI.ShowStats
		}
	}

	if err := s.Validate(); err != nil {
		slog.Warn("settings: invalid values", "path", path, "error", err)
		return DefaultSettings(), fmt.Errorf("settings validate: %w", err)
	}
	return s, nil
}

// SaveSettings writes settings atomically (temp + rename).
func SaveSettings(path string, s Settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("settings mkdir: %w", err)
	}

	if err := s.Validate(); err != nil {
		return fmt.Errorf("settings validate: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "settings*.tmp")
	if err != nil {
		return fmt.Errorf("settings temp: %w", err)
	}
	tmpPath := tmp.Name()

	if err := json.NewEncoder(tmp).Encode(s); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("settings encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("settings close: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("settings rename: %w", err)
	}
	return nil
}
