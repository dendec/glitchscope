package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dendec/glitchscope/internal/i18n"
)

// SetPresetPackAction wires collection commands to the app layer.
func (o *Overlay) SetPresetPackAction(fn func(id string, action PresetPackAction)) {
	o.presetPackAction = fn
}

// SetPresetPacks updates collection state shown in the preset tree.
func (o *Overlay) SetPresetPacks(items []PresetPackItem) {
	if slices.Equal(o.presetPacks, items) {
		return
	}
	selectedPackID := o.selectedPresetPackID()
	treeChanged := presetPackTreeChanged(o.presetPacks, items)
	selected := o.presetPackByID(selectedPackID)
	previousTesting := selected != nil && selected.Testing
	o.presetPacks = slices.Clone(items)
	if treeChanged {
		o.presetPackRemoveConfirm = false
		o.replacePresetTree(o.buildPresetTree(), selectedPackID)
	}
	selected = o.presetPackByID(selectedPackID)
	if previousTesting != (selected != nil && selected.Testing) {
		o.presetPackRemoveConfirm = false
	}
	o.clampPresetPackActionCursor()
	o.presetsDirty = true
	o.presetsDetailDirty = true
}

func presetPackTreeChanged(a, b []PresetPackItem) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Name != b[i].Name || a[i].Installed != b[i].Installed {
			return true
		}
	}
	return false
}

func (o *Overlay) presetPackByID(id string) *PresetPackItem {
	for i := range o.presetPacks {
		if o.presetPacks[i].ID == id {
			return &o.presetPacks[i]
		}
	}
	return nil
}

func (o *Overlay) presetPackStatusByID(id string) string {
	if item := o.presetPackByID(id); item != nil {
		if item.Downloading && item.Installing {
			return o.catalog.Text(i18n.PresetPacksInstalling)
		}
		if item.Downloading && item.ProgressTotal > 0 {
			percent := min(100, int(item.ProgressRead*100/item.ProgressTotal))
			return o.catalog.Format(i18n.PresetPacksDownloading, percent)
		}
		if item.Downloading {
			return o.catalog.Format(i18n.PresetPacksDownloading, 0)
		}
	}
	return ""
}

func (o *Overlay) presetPackActionCount(item PresetPackItem) int {
	switch {
	case item.Busy:
		return 0
	case item.Downloading, item.Testing:
		return 1
	case item.Installed:
		if item.Tested {
			return 1
		}
		return 2
	default:
		return 1
	}
}

func (o *Overlay) presetPackTestRunning() bool {
	for _, item := range o.presetPacks {
		if item.Testing {
			return true
		}
	}
	return false
}

func (o *Overlay) cancelPresetPackTestOnBack() bool {
	if o.uiPage != PagePresets || o.presetPackAction == nil {
		return false
	}
	for _, item := range o.presetPacks {
		if item.Testing {
			o.presetPackAction(item.ID, PresetPackCancel)
			return true
		}
	}
	return false
}

func (o *Overlay) presetPackActionRows(item PresetPackItem) []listRow {
	var actions []string
	switch {
	case item.Busy:
		actions = []string{o.catalog.Text(i18n.ValueLoading)}
	case item.Downloading:
		actions = []string{o.catalog.Text(i18n.ActionCancel)}
	case item.Testing:
		actions = []string{o.catalog.Text(i18n.ActionCancel)}
	case item.Installed:
		remove := o.catalog.Text(i18n.ActionDelete)
		if o.presetPackRemoveConfirm {
			remove = o.catalog.Format(i18n.PresetPacksRemovePrompt, item.Name)
		}
		actions = []string{remove}
		if !item.Tested {
			actions = append(actions, o.catalog.Text(i18n.ActionTest))
		}
	default:
		actions = []string{o.catalog.Text(i18n.ActionDownload)}
	}
	rows := make([]listRow, 0, len(actions)+2)
	for i, action := range actions {
		rows = append(rows, listRow{text: action, active: !item.Busy && o.focusPanel == 1 && o.panelEntered && i == o.presetPackActionCursor})
	}
	size, count := "—", "—"
	if item.ArchiveBytes > 0 {
		size = formatSize(item.ArchiveBytes)
		if !item.Installed {
			size = "≈ " + size
		}
	}
	if item.Installed || item.PresetCount > 0 {
		count = fmt.Sprintf("%d", item.PresetCount)
		if !item.Installed {
			count = "≈ " + count
		}
	}
	rows = append(rows, listRow{},
		listRow{text: o.catalog.Format(i18n.PresetPacksArchiveSize, size)},
		listRow{text: o.catalog.Format(i18n.PresetPacksPresetCount, count)},
	)
	if description := o.helpDescription(presetPackActionHelpRef(item, o.presetPackActionCursor)); description != "" {
		rows = append(rows, listRow{})
		for _, line := range wrapHelpLines([]string{description}, o.face, o.helpTextWidth()) {
			rows = append(rows, listRow{text: line})
		}
	}
	if item.Testing {
		rows = append(rows,
			listRow{text: o.catalog.Format(i18n.PresetPacksTesting, item.TestProgress, item.TestTotal)},
			listRow{text: fmt.Sprintf("FPS: %d", item.TestFPS)},
		)
	} else if item.Tested {
		rows = append(rows, listRow{text: o.catalog.Format(i18n.PresetPacksTested, item.TestProgress, item.TestTotal)})
	}
	if item.Error != "" {
		rows = append(rows, listRow{text: strings.TrimSpace(item.Error)})
	}
	return rows
}

func presetPackActionHelpRef(item PresetPackItem, cursor int) string {
	switch {
	case item.Busy:
		return ""
	case item.Downloading:
		return "preset_pack.cancel"
	case item.Testing:
		return "preset_pack.cancel"
	case item.Installed && cursor == 0:
		return "preset_pack.remove"
	case item.Installed:
		return "preset_pack.test"
	default:
		return "preset_pack.download"
	}
}

func (o *Overlay) activatePresetPackAction() bool {
	node := o.presetNav.Selected()
	if node == nil || node.packID == "" || o.presetPackAction == nil {
		return false
	}
	item := o.presetPackByID(node.packID)
	if item == nil || item.Busy {
		return false
	}
	var action PresetPackAction
	switch {
	case item.Downloading, item.Testing:
		action = PresetPackCancel
	case item.Installed && o.presetPackActionCursor == 0:
		if !o.presetPackRemoveConfirm {
			o.presetPackRemoveConfirm = true
			o.presetsDetailDirty = true
			return true
		}
		o.presetPackRemoveConfirm = false
		action = PresetPackRemove
	case item.Installed:
		action = PresetPackTest
	case !item.Installed:
		action = PresetPackInstall
	default:
		return false
	}
	o.presetPackAction(item.ID, action)
	return true
}

func (o *Overlay) clampPresetPackActionCursor() {
	item := o.selectedPresetPack()
	count := 0
	if item != nil {
		count = o.presetPackActionCount(*item)
	}
	o.presetPackActionCursor = max(0, min(o.presetPackActionCursor, count-1))
}

func (o *Overlay) selectedPresetPack() *PresetPackItem {
	node := o.presetNav.Selected()
	if node == nil || node.packID == "" {
		return nil
	}
	return o.presetPackByID(node.packID)
}
