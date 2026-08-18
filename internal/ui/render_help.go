package ui

// renderHelpPanels draws a topic or child-entry list on the left and its text on the right.
func (o *Overlay) renderHelpPanels(winW, winH, viewW, viewH int, panelW, panelY, panelH, lh int) {
	maxTextW := availableRowTextWidth(panelW)
	topic := helpTopic(HelpTopicID(o.helpView.TopicCursor))
	maxRows := panelH / lh
	if maxRows < 1 {
		maxRows = 1
	}
	o.helpVisibleRows = maxRows

	if o.helpDirty || !glIsTexture(o.helpColL.tex) || !glIsTexture(o.helpColR.tex) {
		leftItems := helpTopics
		if o.helpView.InChildren {
			leftItems = nil
		}
		leftTotal := len(leftItems)
		leftCursor := o.helpView.TopicCursor
		if o.helpView.InChildren {
			leftTotal = len(topic.Children)
			leftCursor = o.helpView.EntryCursor
		}
		o.helpView.EntryTop = scrollOffset(o.helpView.EntryTop, leftCursor, leftTotal, maxRows)
		leftEnd := o.helpView.EntryTop + maxRows
		if leftEnd > leftTotal {
			leftEnd = leftTotal
		}
		leftRows := make([]listRow, 0, leftEnd-o.helpView.EntryTop)
		for i := o.helpView.EntryTop; i < leftEnd; i++ {
			if o.helpView.InChildren {
				leftRows = append(leftRows, listRow{text: topic.Children[i].Title})
			} else {
				leftRows = append(leftRows, listRow{text: leftItems[i].Title})
			}
		}
		o.rebuildListRows(&o.helpColL, leftRows, maxTextW, panelW)

		rightRows := []listRow{{text: topic.Title, bold: true}}
		if o.helpView.InChildren && len(topic.Children) > 0 {
			entry := topic.Children[o.helpView.EntryCursor]
			rightRows[0] = listRow{text: entry.Title, bold: true}
			rightRows = append(rightRows, listRow{text: ""})
			for _, line := range wrapHelpLines(o.helpLines(HelpTopic{Lines: entry.Lines}), o.face, maxTextW) {
				rightRows = append(rightRows, listRow{text: line})
			}
		} else {
			for _, line := range wrapHelpLines(o.helpLines(topic), o.face, maxTextW) {
				rightRows = append(rightRows, listRow{text: line})
			}
		}
		maxTop := len(rightRows) - maxRows
		if maxTop < 0 {
			maxTop = 0
		}
		if o.helpView.ContentTop > maxTop {
			o.helpView.ContentTop = maxTop
		}
		rightEnd := o.helpView.ContentTop + maxRows
		if rightEnd > len(rightRows) {
			rightEnd = len(rightRows)
		}
		o.rebuildListRows(&o.helpColR, rightRows[o.helpView.ContentTop:rightEnd], maxTextW, panelW)
		o.helpDirty = false
	}

	lx, rx := float32(0), float32(winW-panelW)
	py, ph, pw := float32(panelY), float32(panelH), float32(panelW)
	drawPanelBg(o, lx, py, pw, ph, winW, winH, viewW, viewH)
	drawPanelBg(o, rx, py, pw, ph, winW, winH, viewW, viewH)
	drawPanelBorder(o, lx, py, pw, ph, winW, winH, viewW, viewH)
	drawPanelBorder(o, rx, py, pw, ph, winW, winH, viewW, viewH)
	leftCursor := o.helpView.TopicCursor
	if o.helpView.InChildren {
		leftCursor = o.helpView.EntryCursor
	}
	leftScroll := o.helpView.EntryTop
	drawListColumn(o, lx, py, pw, ph, o.helpColL, o.panelEntered && o.focusPanel == 0, leftCursor, leftScroll, lh, winW, winH, viewW, viewH)
	drawListColumn(o, rx, py, pw, ph, o.helpColR, o.panelEntered && o.focusPanel == 1, -1, o.helpView.ContentTop, lh, winW, winH, viewW, viewH)
	sbW := float32(o.scrollbarWidthPx())
	leftTotal := len(helpTopics)
	if o.helpView.InChildren {
		leftTotal = len(topic.Children)
	}
	drawScrollbar(o, lx+pw-sbW, py, ph, leftTotal, maxRows, leftScroll, winW, winH, viewW, viewH)
	drawScrollbar(o, rx+pw-sbW, py, ph, o.helpContentRowCount(topic), maxRows, o.helpView.ContentTop, winW, winH, viewW, viewH)
}
