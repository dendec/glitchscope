package ui

import (
	"embed"
	"encoding/json"
	"strings"

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

var helpTopics = loadHelpTopics()

func loadHelpTopics() []HelpTopic {
	var topics []HelpTopic
	if err := json.Unmarshal(helpData, &topics); err != nil {
		panic("invalid embedded Help asset: " + err.Error())
	}
	for i := range topics {
		for j := range topics[i].Children {
			expandLicenseEntry(&topics[i].Children[j])
		}
	}
	return topics
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
	if int(id) >= 0 && int(id) < len(helpTopics) {
		return helpTopics[id]
	}
	return helpTopics[HelpQuickStart]
}

func helpTopicCount() int { return len(helpTopics) }

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
		if line == "**Keyboard:**" || line == "**Gamepad:**" {
			isGamepadSection := line == "**Gamepad:**"
			if o.controllerConnected != isGamepadSection {
				inWrongSection = true
				continue
			}
			inWrongSection = false
			lines = append(lines, line)
			continue
		}
		if inWrongSection {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func (o *Overlay) deviceInfoLines() []string {
	if o.deviceInfo == nil {
		return []string{"Collecting device information..."}
	}
	return o.deviceInfo.DeviceInfoLines()
}

func (o *Overlay) helpMoveTopic(dir int) {
	next := o.helpView.TopicCursor + dir
	if next >= 0 && next < helpTopicCount() {
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
	topic := helpTopic(HelpTopicID(o.helpView.TopicCursor))
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
	topic := helpTopic(HelpTopicID(o.helpView.TopicCursor))
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
	topic := helpTopic(HelpTopicID(o.helpView.TopicCursor))
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
	topic := helpTopic(HelpTopicID(o.helpView.TopicCursor))
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
