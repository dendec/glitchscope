package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// SettingsPath returns the location of the JSON settings file.
// On PortMaster XDG_DATA_HOME points to /roms/ports/mdpp/conf/,
// so settings.json lands next to the binary by default.
func SettingsPath() string {
	if p := os.Getenv("XDG_DATA_HOME"); p != "" {
		return filepath.Join(p, "settings.json")
	}
	// Fallback: next to the config package (unlikely to be right,
	// but callers override via CLI or env).
	return "settings.json"
}

// LoadSettings reads and validates a settings file. Missing or malformed
// files silently return defaults.
func LoadSettings(path string) (GraphicsSettings, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultGraphics(), nil
		}
		return DefaultGraphics(), fmt.Errorf("settings open: %w", err)
	}
	defer func() { _ = f.Close() }()

	var raw struct {
		Graphics *struct {
			RenderWidth   *int           `json:"render_width"`
			RenderHeight  *int           `json:"render_height"`
			UpscaleFilter *UpscaleFilter `json:"upscale_filter"`
		} `json:"graphics"`
	}
	if err := json.NewDecoder(f).Decode(&raw); err != nil {
		slog.Warn("settings: malformed JSON, using defaults", "path", path, "error", err)
		return DefaultGraphics(), nil
	}
	if raw.Graphics == nil {
		return DefaultGraphics(), nil
	}

	gs := DefaultGraphics()
	if raw.Graphics.RenderWidth != nil {
		gs.RenderWidth = *raw.Graphics.RenderWidth
	}
	if raw.Graphics.RenderHeight != nil {
		gs.RenderHeight = *raw.Graphics.RenderHeight
	}
	if raw.Graphics.UpscaleFilter != nil {
		gs.UpscaleFilter = *raw.Graphics.UpscaleFilter
	}
	if err := gs.Validate(); err != nil {
		return DefaultGraphics(), nil // fallback on invalid data
	}
	return gs, nil
}

// SaveSettings writes settings atomically (write to temp, rename).
func SaveSettings(path string, gs GraphicsSettings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("settings mkdir: %w", err)
	}

	if err := gs.Validate(); err != nil {
		return fmt.Errorf("settings validate: %w", err)
	}

	doc := struct {
		Graphics GraphicsSettings `json:"graphics"`
	}{Graphics: gs}

	tmp, err := os.CreateTemp(filepath.Dir(path), "settings*.tmp")
	if err != nil {
		return fmt.Errorf("settings temp: %w", err)
	}
	tmpPath := tmp.Name()

	if err := json.NewEncoder(tmp).Encode(doc); err != nil {
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


