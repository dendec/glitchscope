package ui

import (
	_ "embed"
	"encoding/json"
	"strings"

	"golang.org/x/image/font"
)

//go:embed assets/help.json
var helpData []byte

var helpTopics = loadHelpTopics()

func loadHelpTopics() []HelpTopic {
	var topics []HelpTopic
	if err := json.Unmarshal(helpData, &topics); err != nil {
		panic("invalid embedded Help asset: " + err.Error())
	}
	return topics
}

func helpTopic(id HelpTopicID) HelpTopic {
	if int(id) >= 0 && int(id) < len(helpTopics) {
		return helpTopics[id]
	}
	return helpTopics[HelpGettingStarted]
}

func helpTopicCount() int { return len(helpTopics) }

func wrapHelpLines(lines []string, face font.Face, maxTextW int) []string {
	if face == nil || maxTextW <= 0 {
		return append([]string(nil), lines...)
	}
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		words := strings.Fields(line)
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
	selectLabel, backLabel, navigateLabel, focusLabel, pagesLabel := "Enter", "Backspace", "Arrows", "Left/Right", "P"
	if o.controllerConnected {
		selectLabel, backLabel, navigateLabel, focusLabel, pagesLabel = "B", "A", "D-pad", "D-pad", "L1/R1"
	}
	replacer := strings.NewReplacer(
		"{select}", selectLabel,
		"{back}", backLabel,
		"{navigate}", navigateLabel,
		"{focus}", focusLabel,
		"{pages}", pagesLabel,
	)
	lines := make([]string, len(topic.Lines))
	for i, line := range topic.Lines {
		lines[i] = replacer.Replace(line)
	}
	return lines
}

func (o *Overlay) helpMoveTopic(dir int) {
	next := o.helpView.TopicCursor + dir
	if next >= 0 && next < helpTopicCount() {
		o.helpView.TopicCursor = next
		o.helpView.EntryCursor = 0
		o.helpView.EntryTop = 0
		o.helpView.ContentTop = 0
		o.helpView.InChildren = false
		o.helpDirty = true
	}
}

func (o *Overlay) helpMoveEntry(dir int) {
	topic := helpTopic(HelpTopicID(o.helpView.TopicCursor))
	next := o.helpView.EntryCursor + dir
	if next >= 0 && next < len(topic.Children) {
		o.helpView.EntryCursor = next
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
	o.helpDirty = true
}

func (o *Overlay) helpContentRowCount(topic HelpTopic) int {
	lineCount := len(wrapHelpLines(o.helpLines(topic), o.face, o.helpTextWidth()))
	if o.helpView.InChildren && o.helpView.EntryCursor >= 0 && o.helpView.EntryCursor < len(topic.Children) {
		lineCount = len(wrapHelpLines(o.helpLines(HelpTopic{Lines: topic.Children[o.helpView.EntryCursor].Lines}), o.face, o.helpTextWidth())) + 1
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
