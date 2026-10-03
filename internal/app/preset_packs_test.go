package app

import (
	"testing"
	"time"

	"github.com/dendec/glitchscope/internal/presets"
	"github.com/dendec/glitchscope/internal/ui"
)

func TestPresetPackProgressPublishesInstallationPhase(t *testing.T) {
	a := &App{
		presetPackCancel: func() {},
		presetPackID:     "butterchurn",
		presetPackItems:  []ui.PresetPackItem{{ID: "butterchurn", Downloading: true}},
	}
	now := time.Now()
	for _, progress := range []presets.InstallProgress{
		{Read: 50, Total: 100},
		{Installing: true},
	} {
		a.presetPackProgress.Store(&progress)
		a.pollPresetPackOperation(now)
		item := a.presetPackItems[0]
		if item.Installing != progress.Installing || item.ProgressRead != progress.Read || item.ProgressTotal != progress.Total || !item.Downloading {
			t.Fatalf("item = %+v, progress = %+v", item, progress)
		}
		now = now.Add(time.Second)
	}
}
