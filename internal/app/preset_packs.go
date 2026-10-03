package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/dendec/glitchscope/internal/presets"
	"github.com/dendec/glitchscope/internal/ui"
)

type presetPackResult struct {
	id     string
	action ui.PresetPackAction
	err    error
}

func (a *App) handlePresetPackAction(id string, action ui.PresetPackAction) {
	if action == ui.PresetPackCancel {
		if a.presetPackTest != nil && a.presetPackTest.id == id {
			a.cancelPresetPackTest(id)
			return
		}
		if id == a.presetPackID && a.presetPackCancel != nil {
			a.presetPackCancel()
		}
		return
	}
	if a.presetPackCancel != nil || a.presetPackTest != nil {
		return
	}
	if action == ui.PresetPackTest {
		a.startPresetPackTest(id)
		return
	}
	if action != ui.PresetPackInstall && action != ui.PresetPackRemove {
		return
	}
	if !knownPresetPack(id) {
		return
	}

	parent := a.appCtx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	a.presetPackCancel = cancel
	a.presetPackID = id
	a.presetPackProgress.Store(nil)
	for i := range a.presetPackItems {
		if a.presetPackItems[i].ID != id {
			continue
		}
		a.presetPackItems[i].Error = ""
		a.presetPackItems[i].Installing = false
		a.presetPackItems[i].Downloading = action == ui.PresetPackInstall
		a.presetPackItems[i].Busy = action == ui.PresetPackRemove
	}
	a.publishPresetPackItems()

	if a.presetPackResults == nil {
		a.presetPackResults = make(chan presetPackResult, 1)
	}
	dir := presetDirPath()
	a.presetPackWg.Add(1)
	go func() {
		defer a.presetPackWg.Done()
		var err error
		switch action {
		case ui.PresetPackInstall:
			installErr := presets.InstallPack(ctx, id, dir, func(progress presets.InstallProgress) {
				a.presetPackProgress.Store(&progress)
			})
			reloadErr := presets.Open(dir)
			if reloadErr != nil {
				reloadErr = fmt.Errorf("reload preset store: %w", reloadErr)
			}
			err = errors.Join(installErr, reloadErr)
		case ui.PresetPackRemove:
			if ctx.Err() == nil {
				err = presets.RemovePackAndReload(id, dir)
			}
		}
		result := presetPackResult{id: id, action: action, err: err}
		a.presetPackResults <- result
	}()
}

func knownPresetPack(id string) bool {
	for _, pack := range presets.AvailablePacks() {
		if pack.ID == id {
			return true
		}
	}
	return false
}

func (a *App) refreshPresetPackItems() {
	statuses := presets.PackStatuses(presetDirPath())
	profileContext := a.presetProfileKey("")
	a.presetPackItems = make([]ui.PresetPackItem, 0, len(statuses))
	for _, status := range statuses {
		item := ui.PresetPackItem{
			ID:           status.Pack.ID,
			Name:         status.Pack.Name,
			Installed:    status.Installed,
			Error:        status.Error,
			ArchiveBytes: status.ArchiveBytes,
			PresetCount:  status.PresetCount,
		}
		if item.Installed {
			names := presetPackPresetNames(a.presetNames, status.Pack.Name)
			item.TestTotal = len(names)
			for _, name := range names {
				key := profileContext
				key.name = name
				profile, exists := a.presetTuning.profiles[key]
				if presetProfileFullyTested(profile, exists) {
					item.TestProgress++
				}
			}
			item.Tested = item.TestTotal > 0 && item.TestProgress == item.TestTotal
		}
		a.presetPackItems = append(a.presetPackItems, item)
	}
	a.publishPresetPackItems()
}

func (a *App) publishPresetPackItems() {
	if a.overlay != nil {
		a.overlay.SetPresetPacks(a.presetPackItems)
	}
}

func (a *App) pollPresetPackOperation(now time.Time) {
	if a.presetPackCancel == nil {
		return
	}
	select {
	case result := <-a.presetPackResults:
		a.presetPackCancel()
		a.presetPackCancel = nil
		a.presetPackID = ""
		a.presetPackProgress.Store(nil)
		if result.action == ui.PresetPackInstall || result.action == ui.PresetPackRemove {
			for _, pack := range presets.AvailablePacks() {
				if pack.ID == result.id {
					a.invalidatePresetProfiles(pack.Name + "/")
					break
				}
			}
		}
		catalogChanged := !slices.Equal(presets.Names(), a.presetNames)
		if catalogChanged {
			a.refreshPresetCatalog(true)
		}
		a.refreshPresetPackItems()
		refreshTextures := result.err == nil || result.action == ui.PresetPackRemove
		if !refreshTextures && result.action == ui.PresetPackInstall {
			for _, item := range a.presetPackItems {
				if item.ID == result.id && item.Installed {
					refreshTextures = true
					break
				}
			}
		}
		if result.err != nil && !errors.Is(result.err, context.Canceled) {
			for i := range a.presetPackItems {
				if a.presetPackItems[i].ID == result.id {
					a.presetPackItems[i].Error = result.err.Error()
					break
				}
			}
			slog.Warn("preset pack operation failed", "pack", result.id, "action", result.action, "error", result.err)
		}
		if refreshTextures && a.pm != nil {
			a.ensureTextures(a.pm)
		}
		a.publishPresetPackItems()
		return
	default:
	}

	if now.Sub(a.presetPackLastUI) < 100*time.Millisecond {
		return
	}
	a.presetPackLastUI = now
	progress := a.presetPackProgress.Load()
	if progress == nil {
		return
	}
	for i := range a.presetPackItems {
		if a.presetPackItems[i].ID == a.presetPackID {
			a.presetPackItems[i].ProgressRead = progress.Read
			a.presetPackItems[i].ProgressTotal = progress.Total
			a.presetPackItems[i].Installing = progress.Installing
			break
		}
	}
	a.publishPresetPackItems()
}
