package ui

import (
	_ "embed"
	"encoding/json"
	"strings"

	"golang.org/x/image/font"
)

//go:embed assets/help.json
var helpData []byte

var licenseTexts = map[string][]string{
	"MIT": {
		"Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the Software), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software.",
		"The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.",
	},
	"BSD-2-Clause": {
		"Redistribution and use in source and binary forms, with or without modification, are permitted provided that the following conditions are met:",
		"1. Redistributions retain the copyright notice.",
		"2. Redistributions in binary form reproduce the copyright notice in documentation.",
	},
	"BSD-3-Clause": {
		"Redistribution and use in source and binary forms, with or without modification, are permitted provided that the following conditions are met:",
		"1. Redistributions retain the copyright notice.",
		"2. Redistributions in binary form reproduce the copyright notice in documentation.",
		"3. Neither the names of the contributors may be used to endorse products without prior written permission.",
	},
	"LGPL-2.1": {
		"This library is free software; you can redistribute it and/or modify it under the terms of the GNU Lesser General Public License as published by the Free Software Foundation; either version 2.1 of the License, or (at your option) any later version.",
	},
	"LGPL-2.1+": {
		"This library is free software; you can redistribute it and/or modify it under the terms of the GNU Lesser General Public License as published by the Free Software Foundation; either version 2.1 of the License, or (at your option) any later version.",
		"Some files may be under GPL v2+ or BSD/X11/MIT licenses.",
	},
	"zlib/libpng": {
		"Permission is granted to anyone to use this software for any purpose, including commercial applications, and to alter it and redistribute it freely, subject to the following restrictions:",
		"1. The origin of this software must not be misrepresented.",
		"2. Altered source versions must be plainly marked as such.",
		"3. This notice may not be removed or altered from any source distribution.",
	},
	"WTFPL": {
		"Do what the fuck you want with the code, but please mention the original author.",
	},
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
