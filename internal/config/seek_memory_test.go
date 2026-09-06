package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSeekMemorySettingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	for _, mode := range AllSeekMemoryModes() {
		settings := DefaultSettings()
		settings.Playback.SeekMemory = mode
		settings.Graphics.VisualizerOff = true
		if err := SaveSettings(path, settings); err != nil {
			t.Fatal(err)
		}
		got, err := LoadSettings(path)
		if err != nil || got.Playback.SeekMemory != mode || !got.Graphics.VisualizerOff {
			t.Fatalf("round trip = %+v, %v", got, err)
		}
	}
	for _, value := range []string{`"unknown"`, `""`, `42`, `null`} {
		var mode SeekMemory
		if err := json.Unmarshal([]byte(value), &mode); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
	if err := os.WriteFile(path, []byte(`{"playback":{"shuffle_mode":"off"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(path)
	if err != nil || got.Playback.SeekMemory != SeekMemoryFull || got.Graphics.VisualizerOff {
		t.Fatalf("legacy defaults = %+v, %v", got, err)
	}
}
