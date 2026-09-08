package ui

import (
	"strings"

	"github.com/dendec/glitchscope/internal/i18n"
	"golang.org/x/image/font"
)

// This file owns the context action-hint model: mapping the overlay's current
// UI state to a short list of compact `[Key] Verb` hints shown in the footer.
// Rendering lives in render_action_hints.go. The mapping here must stay pure —
// it reads state and returns hints, so it can be unit-tested without GL.

// UIHint is a single compact action hint, e.g. "[Enter] Open".
type UIHint struct {
	Key   string // concrete control label under the active mapping
	Label string // short verb: "Open", "Back", "Move", ...
}

// Abstract action keys that a hint can surface. They are mapped to the actual
// keyboard or gamepad control by the active mapping (see controlLabel).
const (
	hintSelect   = "select"
	hintBack     = "back"
	hintFocus    = "focus"
	hintMove     = "move"
	hintPages    = "pages"
	hintPlay     = "play"
	hintFavorite = "favourite"
	hintMenu     = "menu"
	hintSeek     = "seek"
	hintPresets  = "presets"
	hintRandom   = "random"
	hintTracks   = "tracks"
	hintOverlay  = "overlay"
)

// controlLabel returns the concrete control label for an abstract action under
// the currently active mapping. Only one mapping is ever shown at a time, per
// the UI-HELP-PLAN contract: gamepad when a controller is connected, otherwise
// keyboard. These mirrors must stay in sync with internal/input/input.go.
func (o *Overlay) controlLabel(action string) string {
	if o.controllerConnected {
		switch action {
		case hintMenu:
			return "START"
		case hintSeek:
			return "Right stick"
		case hintPresets:
			return "L1/R1"
		case hintRandom:
			return "Y"
		case hintTracks:
			return "D-pad Left/Right"
		case hintOverlay:
			return "SELECT"
		case hintSelect:
			return "A" // Nintendo A (right) = confirm/open
		case hintBack:
			return "B" // Nintendo B (bottom) = back
		case hintFocus, hintMove:
			return "D-pad"
		case hintPages:
			return "L1/R1"
		case hintPlay:
			return "X"
		case hintFavorite:
			return "X"
		}
		return "?"
	}
	switch action {
	case hintMenu:
		return "TAB"
	case hintSeek:
		return ", / ."
	case hintPresets:
		return "P/N"
	case hintRandom:
		return "R"
	case hintTracks:
		return "Left/Right"
	case hintOverlay:
		return "B"
	case hintSelect:
		return "Enter"
	case hintBack:
		return "Backspace"
	case hintFocus:
		return "Left/Right"
	case hintMove:
		return "Up/Down"
	case hintPages:
		return "P/N"
	case hintPlay:
		return "Space"
	case hintFavorite:
		return "F"
	}
	return "?"
}

// ActionHints returns the context action hints for the current overlay state,
// in priority order (primary action first, cancel/back second, then
// navigation, then page-cycle and play/pause). The footer renderer truncates
// trailing hints to fit the width, so primary + back must be the first two
// entries and always shown.
func (o *Overlay) ActionHints() []UIHint {
	// Delete confirmation is an exclusive state: only Confirm + Cancel.
	if o.uiPage == PageLibrary && o.ncConfirm {
		return []UIHint{
			{Key: o.controlLabel(hintSelect), Label: o.catalog.Text(i18n.ActionConfirm)},
			{Key: o.controlLabel(hintBack), Label: o.catalog.Text(i18n.ActionCancel)},
		}
	}

	var hints []UIHint
	switch o.uiPage {
	case PageHelp:
		hints = o.helpHints()
	case PageSettings:
		hints = o.settingsHints()
	case PagePresets:
		hints = o.presetsHints()
	default:
		hints = o.libraryHints()
	}

	// Common low-priority hints shared across pages. Drawn when space allows;
	// the renderer drops trailing hints first.
	if o.playingPath != "" && !o.loading {
		label := o.catalog.Text(i18n.ActionPause)
		if o.paused {
			label = o.catalog.Text(i18n.ActionPlay)
		}
		hints = append(hints, UIHint{Key: o.controlLabel(hintPlay), Label: label})
	}
	hints = append(hints, UIHint{Key: o.controlLabel(hintPages), Label: o.catalog.Text(i18n.ActionScreens)})
	return hints
}

func (o *Overlay) settingsHints() []UIHint {
	if o.settingsEditing {
		return []UIHint{
			{Key: o.controlLabel(hintSelect), Label: o.catalog.Text(i18n.ActionApply)},
			{Key: o.controlLabel(hintBack), Label: o.catalog.Text(i18n.ActionCancel)},
			{Key: o.controlLabel(hintMove), Label: o.catalog.Text(i18n.ActionValue)},
		}
	}
	return []UIHint{
		{Key: o.controlLabel(hintSelect), Label: o.catalog.Text(i18n.ActionEdit)},
		{Key: o.controlLabel(hintBack), Label: o.catalog.Text(i18n.ActionExit)},
		{Key: o.controlLabel(hintMove), Label: o.catalog.Text(i18n.ActionSetting)},
	}
}

func (o *Overlay) helpHints() []UIHint {
	if o.focusPanel == 1 {
		// Content panel: read/scroll only.
		return []UIHint{
			{Key: o.controlLabel(hintBack), Label: o.catalog.Text(i18n.ActionTopics)},
			{Key: o.controlLabel(hintMove), Label: o.catalog.Text(i18n.ActionScroll)},
		}
	}
	// Topic / entry list. Select is meaningful only for a row marked as a
	// submenu; leaf content is already visible in the right panel.
	hints := make([]UIHint, 0, 3)
	if o.helpSelectionHasSubmenu() {
		hints = append(hints, UIHint{Key: o.controlLabel(hintSelect), Label: o.catalog.Text(i18n.ActionOpen)})
	}
	hints = append(hints, UIHint{Key: o.controlLabel(hintBack), Label: o.catalog.Text(i18n.ActionBack)})
	if o.helpView.InChildren || o.helpView.InGrandChildren {
		hints = append(hints, UIHint{Key: o.controlLabel(hintMove), Label: o.catalog.Text(i18n.ActionEntry)})
	} else {
		hints = append(hints, UIHint{Key: o.controlLabel(hintMove), Label: o.catalog.Text(i18n.ActionTopic)})
	}
	return hints
}

func (o *Overlay) helpSelectionHasSubmenu() bool {
	topic := o.helpTopic(HelpTopicID(o.helpView.TopicCursor))
	if o.helpView.InGrandChildren {
		return false
	}
	if !o.helpView.InChildren {
		return len(topic.Children) > 0
	}
	if o.helpView.EntryCursor < 0 || o.helpView.EntryCursor >= len(topic.Children) {
		return false
	}
	return len(topic.Children[o.helpView.EntryCursor].Children) > 0
}

func (o *Overlay) presetsHints() []UIHint {
	hints := []UIHint{
		{Key: o.controlLabel(hintMove), Label: o.catalog.Text(i18n.ActionMove)},
		{Key: o.controlLabel(hintBack), Label: o.catalog.Text(i18n.ActionBack)},
	}
	node := o.presetNav.Selected()
	if node == nil {
		return hints
	}
	if !node.isLeaf {
		hints = append(hints, UIHint{Key: o.controlLabel(hintSelect), Label: o.catalog.Text(i18n.ActionOpen)})
	} else {
		hints = append(hints, UIHint{Key: o.controlLabel(hintSelect), Label: o.catalog.Text(i18n.ActionLoad)})
	}
	return hints
}

func (o *Overlay) libraryHints() []UIHint {
	// NC right panel: Play / Delete button focus.
	if o.isNC() && o.focusPanel == 1 {
		switch o.ncRight {
		case ncRightInfo:
			return []UIHint{
				{Key: o.controlLabel(hintMove), Label: o.catalog.Text(i18n.ActionScroll)},
				{Key: o.controlLabel(hintBack), Label: o.catalog.Text(i18n.ActionLeftPanel)},
			}
		case ncRightPlay:
			return []UIHint{
				{Key: o.controlLabel(hintSelect), Label: o.catalog.Text(i18n.ActionPlay)},
				{Key: o.controlLabel(hintBack), Label: o.catalog.Text(i18n.ActionLeftPanel)},
			}
		case ncRightDelete:
			return []UIHint{
				{Key: o.controlLabel(hintSelect), Label: o.catalog.Text(i18n.ActionDelete)},
				{Key: o.controlLabel(hintBack), Label: o.catalog.Text(i18n.ActionLeftPanel)},
			}
		}
	}

	// Non-NC right panel (track / catalog track list).
	if o.focusPanel == 1 {
		if e := o.currentEntry(); e != nil && e.IsCatalogTrack() && o.isTrackCached != nil && o.isTrackCached(e.filePath) {
			return []UIHint{
				{Key: o.controlLabel(hintSelect), Label: o.catalog.Text(i18n.ActionDeleteCache)},
				{Key: o.controlLabel(hintBack), Label: o.catalog.Text(i18n.ActionLeft)},
			}
		}
		return []UIHint{
			{Key: o.controlLabel(hintSelect), Label: o.catalog.Text(i18n.ActionPlay)},
			{Key: o.controlLabel(hintBack), Label: o.catalog.Text(i18n.ActionLeft)},
			{Key: o.controlLabel(hintMove), Label: o.catalog.Text(i18n.ActionTrack)},
		}
	}

	// Left panel.
	backVerb := o.catalog.Text(i18n.ActionUp)
	if !o.isNC() && o.topLevel().ctx == ctxSourceRoot {
		backVerb = o.catalog.Text(i18n.ActionClose)
	}
	hints := []UIHint{
		{Key: o.controlLabel(hintSelect), Label: o.catalog.Text(i18n.ActionOpen)},
		{Key: o.controlLabel(hintBack), Label: backVerb},
		{Key: o.controlLabel(hintMove), Label: o.catalog.Text(i18n.ActionItem)},
	}
	if o.HasPlayableTrack() && o.favoritesView != nil {
		e := o.currentEntry()
		if e != nil {
			sym := o.favoritesView.Symbol(e.filePath)
			if sym != "" {
				hints = append(hints, UIHint{Key: o.controlLabel(hintFavorite), Label: o.catalog.Text(i18n.ActionRemove)})
			} else {
				hints = append(hints, UIHint{Key: o.controlLabel(hintFavorite), Label: o.catalog.Text(i18n.ActionFavorite)})
			}
		}
	}
	return hints
}

// buildHintsText joins hints into one footer line, dropping trailing hints
// (never the leading ones) so the line fits maxPx. Rendering must prefer
// dropping low-priority hints over splitting a single hint across lines.
func (o *Overlay) buildHintsText(hints []UIHint, maxW int) string {
	if o.face == nil || maxW <= 0 || len(hints) == 0 {
		return ""
	}
	const sep = "   "
	longestFit := 0
	for i := 1; i <= len(hints); i++ {
		line := joinHints(hints[:i], sep)
		if font.MeasureString(o.face, line).Ceil() <= maxW {
			longestFit = i
			continue
		}
		break
	}
	if longestFit == 0 {
		// Even the primary hint alone overflows: force it.
		return "[" + hints[0].Key + "] " + hints[0].Label
	}
	return joinHints(hints[:longestFit], sep)
}

func joinHints(hints []UIHint, sep string) string {
	parts := make([]string, 0, len(hints))
	for _, h := range hints {
		parts = append(parts, "["+h.Key+"] "+h.Label)
	}
	return strings.Join(parts, sep)
}
