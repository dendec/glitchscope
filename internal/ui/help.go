package ui

import (
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/dendec/glitchscope/internal/i18n"
	"golang.org/x/image/font"
)

//go:embed assets/help.json
var helpData []byte

//go:embed assets/licenses/*
var licenseFS embed.FS

var licenseTexts = loadLicenseTexts()

func loadLicenseTexts() map[string][]string {
	entries, err := licenseFS.ReadDir("assets/licenses")
	if err != nil {
		return nil
	}
	texts := make(map[string][]string, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := licenseFS.ReadFile("assets/licenses/" + e.Name())
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		texts[e.Name()] = lines
	}
	return texts
}

var helpTopics = loadHelpTopics(helpData)

// CatalogInfo is the small runtime projection shown by Help. Counts come from
// persistent shuffle-index metadata; no catalog records are loaded for Help.
type CatalogInfo struct {
	ModlandTracks         uint64
	ModlandDirectories    uint64
	ModArchiveTracks      uint64
	ModArchiveDirectories uint64
	IndexLoading          bool
}

func loadHelpTopics(data []byte) []HelpTopic {
	topics, err := parseHelpTopics(data)
	if err != nil {
		// A malformed embedded asset would otherwise crash at startup. Degrade
		// gracefully: log it and leave the help page empty instead of panicking.
		slog.Error("invalid embedded Help asset; help page disabled", "error", err)
		return nil
	}
	return topics
}

func parseHelpTopics(data []byte) ([]HelpTopic, error) {
	var topics []HelpTopic
	if err := json.Unmarshal(data, &topics); err != nil {
		return nil, fmt.Errorf("decode Help asset: %w", err)
	}
	for i := range topics {
		for j := range topics[i].Children {
			expandLicenseEntry(&topics[i].Children[j])
		}
	}
	return topics, nil
}

func expandLicenseEntry(e *HelpEntry) {
	if len(e.Lines) == 0 {
		return
	}
	licenseType := e.Lines[0]
	if text, ok := licenseTexts[licenseType]; ok {
		e.Lines = append(e.Lines, "")
		e.Lines = append(e.Lines, text...)
	}
}

func helpTopic(id HelpTopicID) HelpTopic {
	if len(helpTopics) == 0 {
		return HelpTopic{}
	}
	if int(id) >= 0 && int(id) < len(helpTopics) {
		return helpTopics[id]
	}
	return helpTopics[HelpQuickStart]
}

func (o *Overlay) helpTopic(id HelpTopicID) HelpTopic {
	topics := o.effectiveHelpTopics()
	if len(topics) == 0 {
		return HelpTopic{}
	}
	if int(id) >= 0 && int(id) < len(topics) {
		return topics[id]
	}
	return topics[HelpQuickStart]
}

func (o *Overlay) effectiveHelpTopics() []HelpTopic {
	if o.helpTopics != nil {
		return o.helpTopics
	}
	return helpTopics
}

func (o *Overlay) helpTopicCount() int { return len(o.effectiveHelpTopics()) }

func helpMenuTitle(title string, hasSubmenu bool) string {
	if hasSubmenu {
		return title + "/"
	}
	return title
}

func wrapHelpLines(lines []string, face font.Face, maxTextW int) []string {
	if face == nil || maxTextW <= 0 {
		return append([]string(nil), lines...)
	}
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		clean := strings.ReplaceAll(line, "**", "")
		words := strings.Fields(clean)
		if len(words) == 0 {
			wrapped = append(wrapped, "")
			continue
		}
		current := ""
		for _, word := range words {
			candidate := word
			if current != "" {
				candidate = current + " " + word
			}
			if font.MeasureString(face, candidate).Ceil() <= maxTextW {
				current = candidate
				continue
			}
			if current != "" {
				wrapped = append(wrapped, current)
			}
			current = word
			for font.MeasureString(face, current).Ceil() > maxTextW {
				runes := []rune(current)
				keep := len(runes) - 1
				for keep > 1 && font.MeasureString(face, string(runes[:keep])).Ceil() > maxTextW {
					keep--
				}
				if keep <= 1 {
					break
				}
				wrapped = append(wrapped, string(runes[:keep]))
				current = string(runes[keep:])
			}
		}
		if current != "" {
			wrapped = append(wrapped, current)
		}
	}
	return wrapped
}

func (o *Overlay) helpLines(topic HelpTopic) []string {
	if topic.ID == HelpDevice {
		return o.deviceInfoLines()
	}
	lines := make([]string, 0, len(topic.Lines))
	inWrongSection := false
	for _, line := range topic.Lines {
		if line == "{keyboard_heading}" || line == "{gamepad_heading}" {
			isGamepadSection := line == "{gamepad_heading}"
			if o.controllerConnected != isGamepadSection {
				inWrongSection = true
				continue
			}
			inWrongSection = false
			lines = append(lines, o.expandHelpControls(line))
			continue
		}
		if inWrongSection {
			continue
		}
		lines = append(lines, readableHelpLines(o.expandHelpControls(line))...)
	}
	return lines
}

func readableHelpLines(line string) []string {
	if len(line) <= 240 || strings.HasPrefix(strings.TrimSpace(line), "**") {
		return []string{line}
	}
	parts := strings.Split(line, ". ")
	if len(parts) < 3 {
		return []string{line}
	}
	lines := make([]string, 0, len(parts)+len(parts)/2)
	for i := 0; i < len(parts); i += 2 {
		end := min(i+2, len(parts))
		paragraph := strings.Join(parts[i:end], ". ")
		if end < len(parts) && !strings.HasSuffix(paragraph, ".") {
			paragraph += "."
		}
		lines = append(lines, paragraph)
		if end < len(parts) {
			lines = append(lines, "")
		}
	}
	return lines
}

func (o *Overlay) deviceInfoLines() []string {
	if o.deviceInfo == nil && o.deviceInfoProvider != nil {
		o.deviceInfo = o.deviceInfoProvider()
		o.deviceInfoProvider = nil
	}
	if o.deviceInfo == nil {
		return []string{"Device information unavailable."}
	}
	return o.deviceInfo.DeviceInfoLines()
}

func (o *Overlay) helpMoveTopic(dir int) {
	next := o.helpView.TopicCursor + dir
	if next >= 0 && next < o.helpTopicCount() {
		o.helpView.TopicCursor = next
		o.helpView.EntryCursor = 0
		o.helpView.EntryTop = 0
		o.helpView.ContentTop = 0
		o.helpView.InChildren = false
		o.helpView.InGrandChildren = false
		o.helpView.GrandChildCursor = 0
		o.helpView.GrandChildTop = 0
		o.helpDirty = true
	}
}

func (o *Overlay) helpMoveEntry(dir int) {
	topic := o.helpTopic(HelpTopicID(o.helpView.TopicCursor))
	if o.helpView.InGrandChildren {
		o.helpMoveGrandChild(dir)
		return
	}
	next := o.helpView.EntryCursor + dir
	if next >= 0 && next < len(topic.Children) {
		o.helpView.EntryCursor = next
		o.helpView.EntryTop = 0
		o.helpView.ContentTop = 0
		o.helpDirty = true
	}
}

func (o *Overlay) helpMoveGrandChild(dir int) {
	topic := o.helpTopic(HelpTopicID(o.helpView.TopicCursor))
	if o.helpView.EntryCursor < 0 || o.helpView.EntryCursor >= len(topic.Children) {
		return
	}
	cat := topic.Children[o.helpView.EntryCursor]
	next := o.helpView.GrandChildCursor + dir
	if next >= 0 && next < len(cat.Children) {
		o.helpView.GrandChildCursor = next
		o.helpView.GrandChildTop = 0
		o.helpView.ContentTop = 0
		o.helpDirty = true
	}
}

func (o *Overlay) enterHelpChildren() {
	topic := o.helpTopic(HelpTopicID(o.helpView.TopicCursor))
	if len(topic.Children) == 0 {
		return
	}
	o.helpView.InChildren = true
	o.helpView.EntryCursor = 0
	o.helpView.EntryTop = 0
	o.helpView.ContentTop = 0
	o.helpView.InGrandChildren = false
	o.helpView.GrandChildCursor = 0
	o.helpView.GrandChildTop = 0
	o.helpDirty = true
}

func (o *Overlay) enterHelpGrandChildren() {
	topic := o.helpTopic(HelpTopicID(o.helpView.TopicCursor))
	if o.helpView.EntryCursor < 0 || o.helpView.EntryCursor >= len(topic.Children) {
		return
	}
	cat := topic.Children[o.helpView.EntryCursor]
	if len(cat.Children) == 0 {
		return
	}
	o.helpView.InGrandChildren = true
	o.helpView.GrandChildCursor = 0
	o.helpView.GrandChildTop = 0
	o.helpView.ContentTop = 0
	o.helpDirty = true
}

func (o *Overlay) helpContentRowCount(topic HelpTopic) int {
	lineCount := len(wrapHelpLines(o.helpLines(topic), o.face, o.helpTextWidth()))
	if o.helpView.InGrandChildren {
		if o.helpView.EntryCursor >= 0 && o.helpView.EntryCursor < len(topic.Children) {
			cat := topic.Children[o.helpView.EntryCursor]
			if o.helpView.GrandChildCursor >= 0 && o.helpView.GrandChildCursor < len(cat.Children) {
				entry := cat.Children[o.helpView.GrandChildCursor]
				lineCount = len(wrapHelpLines(o.helpLines(HelpTopic{Lines: entry.Lines}), o.face, o.helpTextWidth())) + 1
			}
		}
	} else if o.helpView.InChildren && o.helpView.EntryCursor >= 0 && o.helpView.EntryCursor < len(topic.Children) {
		entry := topic.Children[o.helpView.EntryCursor]
		if len(entry.Children) > 0 {
			lineCount = len(entry.Children)
		} else {
			lineCount = len(wrapHelpLines(o.helpLines(HelpTopic{Lines: entry.Lines}), o.face, o.helpTextWidth())) + 1
		}
	}
	return 1 + lineCount
}

func (o *Overlay) helpMaxContentTop(topic HelpTopic) int {
	visibleRows := o.helpVisibleRows
	if visibleRows < 1 {
		visibleRows = 1
	}
	maxTop := o.helpContentRowCount(topic) - visibleRows
	if maxTop < 0 {
		return 0
	}
	return maxTop
}

// Help and context hints share physical button labels (Nintendo layout on handhelds).
func (o *Overlay) expandHelpControls(line string) string {
	for _, action := range []string{hintMenu, hintSelect, hintBack, hintPlay, hintPages, hintMove, hintFocus, hintSeek, hintPresets, hintRandom, hintTracks, hintOverlay, hintFavorite} {
		line = strings.ReplaceAll(line, "{"+action+"}", o.controlLabel(action))
	}
	info := o.catalogInfo
	replacements := map[string]string{
		"{gamepad_heading}":        "**" + o.catalog.Text(i18n.LabelGamepad) + ":**",
		"{keyboard_heading}":       "**" + o.catalog.Text(i18n.LabelKeyboard) + ":**",
		"{modland_tracks}":         o.formatCatalogCount(info.ModlandTracks, info.IndexLoading),
		"{modland_directories}":    o.formatCatalogCount(info.ModlandDirectories, info.IndexLoading),
		"{modarchive_tracks}":      o.formatCatalogCount(info.ModArchiveTracks, info.IndexLoading),
		"{modarchive_directories}": o.formatCatalogCount(info.ModArchiveDirectories, info.IndexLoading),
		"{network_status}":         map[bool]string{true: o.catalog.Text(i18n.ValueOnline), false: o.catalog.Text(i18n.ValueOffline)}[o.online],
	}
	for placeholder, value := range replacements {
		line = strings.ReplaceAll(line, placeholder, value)
	}
	return line
}

func (o *Overlay) formatCatalogCount(count uint64, loading bool) string {
	if count == 0 {
		if loading {
			return o.catalog.Text(i18n.ValueLoading)
		}
		return o.catalog.Text(i18n.ValueUnavailable)
	}
	text := fmt.Sprintf("%d", count)
	for i := len(text) - 3; i > 0; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return text
}
