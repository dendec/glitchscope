package app

import (
	"path/filepath"
	"testing"

	"github.com/dendec/glitchscope/internal/config"
)

func TestMenuAcknowledgementPersistsOnlyOnOpening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	settings, err := config.LoadSettings(path)
	if err != nil || settings.UI.MenuOpened {
		t.Fatalf("new user defaults = %+v, %v", settings.UI, err)
	}
	a := &App{settings: &settings, settingsPath: path}
	a.rememberMenuOpened()
	got, err := config.LoadSettings(path)
	if err != nil || !got.UI.MenuOpened {
		t.Fatalf("acknowledgement not persisted: %+v, %v", got.UI, err)
	}
	// Already acknowledged: no subsequent settings write is needed.
	a.settingsPath = filepath.Join(t.TempDir(), "absent", "settings.json")
	a.rememberMenuOpened()
	if !a.settings.UI.MenuOpened {
		t.Fatal("acknowledgement lost")
	}
}
