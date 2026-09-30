package ui

import (
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/dendec/glitchscope/internal/i18n"
	"github.com/dendec/glitchscope/internal/input"
	"golang.org/x/image/font"
)

// PointerResult is the small command surface between the overlay and app.
// Pointer selection is routed through the app's existing Select action so
// playback, settings, and deletion keep their current ownership boundaries.
type PointerResult uint8

const (
	PointerResultNone PointerResult = iota
	PointerResultSelect
	PointerResultClose
)

// PointerTapSlop returns the movement tolerance in drawable/UI pixels. It is
// scaled with the same height-based UI scale as the rest of the overlay.
func PointerTapSlop(screenH int) float32 {
	if screenH <= 0 {
		return 12
	}
	return float32(max(12, int(math.Round(float64(screenH)*12/480))))
}

type pointerTargetKind uint8

const (
	pointerTargetNone pointerTargetKind = iota
	pointerTargetClose
	pointerTargetPagePrev
	pointerTargetPageNext
	pointerTargetPage
	pointerTargetBreadcrumb
	pointerTargetPanel
	pointerTargetRow
	pointerTargetAction
)

type pointerTarget struct {
	kind          pointerTargetKind
	page          UIPage
	index         int
	panel         int
	presetDepth   int
	navStackDepth int
	dirPath       string
	helpBack      bool
}

type pointerRect struct {
	x, y, w, h float32
}

type pointerTargetRect struct {
	target pointerTarget
	rect   pointerRect
}

type pointerLayout struct {
	targets []pointerTargetRect
	panelY  float32
	panelH  float32
	panelW  float32
	lineH   float32
}

type pointerPress struct {
	active            bool
	device            input.PointerDevice
	pointer           int64
	target            pointerTarget
	downX             float32
	downY             float32
	lastY             float32
	dragged           bool
	scrollDY          float32
	scrollbarDrag     bool
	scrollbarOffset   float32
	keepHelpSelection bool
	downAt            time.Time
	text              *pointerTextRow
}

type pointerScrollbar struct {
	panel    int
	track    pointerRect
	thumb    pointerRect
	total    int
	visible  int
	hitWidth float32
}

type pointerTextRow struct {
	text     string
	link     string
	copyText string
	x        float32
	y        float32
	w        float32
	h        float32
}

type breadcrumbSegment struct {
	label  string
	icon   string
	target pointerTarget
}

type breadcrumbPlacement struct {
	label  string
	icon   string
	target pointerTarget
	x, w   float32
}

// HandlePointer consumes one neutral pointer event. It intentionally has no
// SDL knowledge; input translation and high-DPI conversion happen upstream.
func (o *Overlay) HandlePointer(event input.PointerEvent, winW, winH int) PointerResult {
	if !o.uiVisible {
		return PointerResultNone
	}

	if event.Phase == input.PointerCancel {
		o.pointerPress = pointerPress{}
		return PointerResultNone
	}
	o.markInteraction()
	if event.Device == input.PointerTouch && o.pointerPress.active && event.PointerID != o.pointerPress.pointer {
		return PointerResultNone
	}

	switch event.Phase {
	case input.PointerMove:
		return o.handlePointerMove(event, winW, winH)
	case input.PointerWheel:
		return o.handlePointerWheel(event, winW, winH)
	case input.PointerDown:
		return o.handlePointerDown(event, winW, winH)
	case input.PointerUp:
		return o.handlePointerUp(event, winW, winH)
	default:
		return PointerResultNone
	}
}

func (o *Overlay) handlePointerDown(event input.PointerEvent, winW, winH int) PointerResult {
	textRow := o.pointerTextAt(event.X, event.Y)
	if event.Device == input.PointerMouse && event.Button == input.PointerButtonSecondary {
		target := layoutTargetAt(o.pointerLayout(winW, winH).targets, event.X, event.Y)
		if o.pointerModalBlocks(target) {
			return PointerResultNone
		}
		if textRow != nil {
			o.pendingPointerCopy = pointerValue(*textRow)
		}
		return PointerResultNone
	}
	if event.Button != input.PointerButtonPrimary {
		return PointerResultNone
	}
	layout := o.pointerLayout(winW, winH)
	target := layoutTargetAt(layout.targets, event.X, event.Y)
	if target.kind == pointerTargetNone {
		return PointerResultNone
	}
	if scrollbar, ok := o.pointerScrollbarAt(event.X, event.Y, winW, winH); ok {
		modalTarget := target
		if o.uiPage == PageSettings && o.settingsEditing && scrollbar.panel == 1 {
			modalTarget = pointerTarget{kind: pointerTargetRow, panel: 1}
		}
		if o.pointerModalBlocks(modalTarget) {
			return PointerResultNone
		}
		o.applyPointerPanelFocus(pointerTarget{kind: pointerTargetPanel, panel: scrollbar.panel})
		grabOffset := scrollbar.thumb.h / 2
		if event.Y >= scrollbar.thumb.y && event.Y < scrollbar.thumb.y+scrollbar.thumb.h {
			grabOffset = event.Y - scrollbar.thumb.y
		}
		o.pointerPress = pointerPress{
			active:          true,
			device:          event.Device,
			pointer:         event.PointerID,
			target:          pointerTarget{kind: pointerTargetPanel, panel: scrollbar.panel},
			downX:           event.X,
			downY:           event.Y,
			lastY:           event.Y,
			dragged:         true,
			scrollbarDrag:   true,
			scrollbarOffset: grabOffset,
		}
		o.updatePointerScrollbar(event.Y, scrollbar.panel, grabOffset, winW, winH)
		return PointerResultNone
	}
	if o.pointerModalBlocks(target) {
		return PointerResultNone
	}
	keepHelpSelection := o.focusPanel == 1 && o.isCurrentHelpRow(target)
	var pressedText *pointerTextRow
	if textRow != nil {
		copy := *textRow
		pressedText = &copy
	}
	if event.Device == input.PointerTouch {
		o.applyPointerPanelFocus(target)
	} else {
		o.applyPointerFocus(target)
	}
	o.pointerPress = pointerPress{
		active:            true,
		device:            event.Device,
		pointer:           event.PointerID,
		target:            target,
		downX:             event.X,
		downY:             event.Y,
		lastY:             event.Y,
		keepHelpSelection: keepHelpSelection,
		downAt:            time.Now(),
		text:              pressedText,
	}
	return PointerResultNone
}

func (o *Overlay) handlePointerMove(event input.PointerEvent, winW, winH int) PointerResult {
	if !o.pointerPress.active {
		// Pointer motion is deliberately passive. Moving the mouse over a
		// row must not move the UI cursor or paint a selection highlight.
		// Focus/cursor changes happen on button/tap press only.
		return PointerResultNone
	}
	if event.PointerID != o.pointerPress.pointer || event.Device != o.pointerPress.device {
		return PointerResultNone
	}
	if o.pointerPress.scrollbarDrag {
		o.updatePointerScrollbar(event.Y, o.pointerPress.target.panel, o.pointerPress.scrollbarOffset, winW, winH)
		return PointerResultNone
	}
	previousY := o.pointerPress.lastY
	o.pointerPress.lastY = event.Y
	slop := PointerTapSlop(winH)
	dx := event.X - o.pointerPress.downX
	dy := event.Y - o.pointerPress.downY
	if event.Device == input.PointerTouch {
		// Keep the complete path, including the small motions before the
		// slop threshold is crossed. Otherwise a swipe made of several small
		// SDL events can end with less than one line of accumulated scroll.
		o.pointerPress.scrollDY += -(event.Y - previousY)
	}
	if !o.pointerPress.dragged && dx*dx+dy*dy > slop*slop {
		o.pointerPress.dragged = true
	}
	if o.pointerPress.dragged && event.Device == input.PointerTouch {
		// Negative finger movement means scrolling down through the content.
		// Use absolute positions rather than SDL's normalized DY: a few SDL
		// backends report zero/rounded deltas even while X/Y changes.
		if o.pointerPress.target.kind == pointerTargetPanel || o.pointerPress.target.kind == pointerTargetRow {
			o.consumePointerScroll(o.pointerPress.target.panel, &o.pointerPress.scrollDY, winW, winH)
		}
	}
	return PointerResultNone
}

func (o *Overlay) handlePointerUp(event input.PointerEvent, winW, winH int) PointerResult {
	if event.Button != input.PointerButtonPrimary {
		return PointerResultNone
	}
	press := o.pointerPress
	o.pointerPress = pointerPress{}
	if !press.active || event.PointerID != press.pointer || event.Device != press.device {
		return PointerResultNone
	}
	if press.scrollbarDrag {
		return PointerResultNone
	}
	slop := PointerTapSlop(winH)
	dx := event.X - press.downX
	dy := event.Y - press.downY
	if press.dragged || dx*dx+dy*dy > slop*slop {
		return PointerResultNone
	}
	if press.device == input.PointerTouch && press.text != nil && time.Since(press.downAt) >= 600*time.Millisecond {
		o.pendingPointerCopy = pointerValue(*press.text)
		return PointerResultNone
	}
	if press.text != nil && press.text.link != "" && o.pointerOverRowLink(*press.text, event.X) {
		o.pendingPointerURL = press.text.link
		return PointerResultNone
	}
	o.applyPointerFocus(press.target)
	if press.keepHelpSelection {
		return PointerResultNone
	}
	if press.target.kind == pointerTargetRow && press.target.panel == 1 &&
		o.uiPage == PageSettings && !o.settingsEditing {
		// Pointer selection is direct: keep the value under the tap as the
		// temporary value, then let the normal Settings Select action commit
		// it. Do not copy the old committed index here.
		row := o.settingsCursor
		if row >= 0 && row < len(o.settingsRows) && press.target.index >= 0 && press.target.index < len(o.settingsRows[row].Values) {
			o.settingsValueCursor = press.target.index
			o.settingsEditing = true
			o.settingsDirty = true
		}
	}
	if o.pointerModalBlocks(press.target) {
		return PointerResultNone
	}
	switch press.target.kind {
	case pointerTargetClose:
		return PointerResultClose
	case pointerTargetPagePrev:
		if !o.settingsEditing {
			o.PrevScreen()
		}
	case pointerTargetPageNext:
		if !o.settingsEditing {
			o.NextScreen()
		}
	case pointerTargetPage:
		if !o.settingsEditing {
			o.pointerSetPage(press.target.page)
		}
	case pointerTargetBreadcrumb:
		if !o.settingsEditing {
			o.pointerNavigateBreadcrumb(press.target)
		}
	case pointerTargetRow:
		if o.uiPage == PageSettings && press.target.panel == 1 {
			return PointerResultSelect
		}
		if o.uiPage == PageSettings && press.target.panel == 0 {
			return PointerResultNone
		}
		return PointerResultSelect
	case pointerTargetAction:
		return PointerResultSelect
	}
	return PointerResultNone
}

// ConsumePointerCopy returns text queued by a right click or long tap.
func (o *Overlay) ConsumePointerCopy() string {
	value := o.pendingPointerCopy
	o.pendingPointerCopy = ""
	return value
}

// ConsumePointerURL returns a URL queued by a primary click.
func (o *Overlay) ConsumePointerURL() string {
	value := o.pendingPointerURL
	o.pendingPointerURL = ""
	return value
}

// PointerOverLink reports whether a point is over a visible right-panel URL.
func (o *Overlay) PointerOverLink(x, y float32) bool {
	row := o.pointerTextAt(x, y)
	return row != nil && row.link != "" && o.pointerOverRowLink(*row, x)
}

func (o *Overlay) pointerTextAt(x, y float32) *pointerTextRow {
	for i := range o.pointerTextRows {
		row := &o.pointerTextRows[i]
		if x >= row.x && x < row.x+row.w && y >= row.y && y < row.y+row.h {
			return row
		}
	}
	return nil
}

func (o *Overlay) pointerOverRowLink(row pointerTextRow, x float32) bool {
	if row.link == "" || o.face == nil {
		return false
	}
	_, start, end := findTextURL(row.text)
	if start < 0 {
		return x >= row.x && x < row.x+row.w
	}
	left := row.x + float32(font.MeasureString(o.face, row.text[:start]).Ceil())
	right := row.x + float32(font.MeasureString(o.face, row.text[:end]).Ceil())
	return x >= left && x <= right
}

func pointerValue(row pointerTextRow) string {
	if row.copyText != "" {
		return row.copyText
	}
	text := strings.TrimSpace(row.text)
	if row.link != "" {
		return row.link
	}
	if separator := strings.Index(text, ": "); separator >= 0 {
		return strings.TrimSpace(text[separator+2:])
	}
	return text
}

func findTextURL(text string) (string, int, int) {
	start := strings.Index(text, "https://")
	httpStart := strings.Index(text, "http://")
	if start < 0 || (httpStart >= 0 && httpStart < start) {
		start = httpStart
	}
	if start < 0 {
		return "", -1, -1
	}
	end := start
	for end < len(text) && !strings.ContainsRune(" \t\n", rune(text[end])) {
		end++
	}
	urlEnd := end
	for urlEnd > start && strings.ContainsRune(".,;:!?)]}\"'", rune(text[urlEnd-1])) {
		urlEnd--
	}
	return text[start:urlEnd], start, urlEnd
}

func (o *Overlay) setPointerTextLines(lines []string, x, y, clipY, w, h float32, lineH int) {
	rows := make([]listRow, len(lines))
	commentStart := infoCommentLinesStart(lines, o.infoComment, strings.TrimSpace(o.catalog.Text(i18n.InfoComment)))
	commentEnd := commentStart + len(strings.Split(o.infoComment, "\n"))
	for i, line := range lines {
		copyText := ""
		if commentStart >= 0 && i >= commentStart && i < commentEnd {
			copyText = o.infoComment
		}
		rows[i] = listRow{text: line, copyText: copyText}
	}
	o.setPointerListRows(rows, x, y, clipY, w, h, lineH)
}

func infoCommentLinesStart(lines []string, comment, commentLabel string) int {
	if comment == "" || commentLabel == "" {
		return -1
	}
	commentLines := strings.Split(comment, "\n")
	for labelRow := len(lines) - len(commentLines) - 1; labelRow >= 0; labelRow-- {
		if strings.TrimSpace(lines[labelRow]) != commentLabel {
			continue
		}
		matches := true
		for i, commentLine := range commentLines {
			displayedLine := lines[labelRow+1+i]
			if displayedLine != commentLine && displayedLine != "  "+commentLine {
				matches = false
				break
			}
		}
		if matches {
			return labelRow + 1
		}
	}
	return -1
}

func (o *Overlay) setPointerListRows(rows []listRow, x, y, clipY, w, h float32, lineH int) {
	if (!o.pointerMouse && !o.pointerTouch) || lineH <= 0 {
		return
	}
	for i, row := range rows {
		rowY := y + float32(i*lineH)
		if rowY+float32(lineH) <= clipY || rowY >= clipY+h || row.text == "" {
			continue
		}
		link := row.link
		if link == "" {
			link, _, _ = findTextURL(row.text)
		}
		o.pointerTextRows = append(o.pointerTextRows, pointerTextRow{
			text: row.text, link: link, copyText: row.copyText,
			x: x, y: rowY, w: w, h: float32(lineH),
		})
	}
}

func (o *Overlay) drawPointerLinks(winW, winH, viewW, viewH int) {
	if (!o.pointerMouse && !o.pointerTouch) || o.face == nil {
		return
	}
	textColor := o.textColor()
	metrics := o.face.Metrics()
	underlineOffset := textPadding(o.fontSize) + metrics.Ascent.Ceil() + max(1, metrics.Descent.Ceil()/2)
	for _, row := range o.pointerTextRows {
		if row.link == "" {
			continue
		}
		_, start, end := findTextURL(row.text)
		left := row.x
		right := row.x + float32(font.MeasureString(o.face, row.text).Ceil())
		if start >= 0 {
			left += float32(font.MeasureString(o.face, row.text[:start]).Ceil())
			right = row.x + float32(font.MeasureString(o.face, row.text[:end]).Ceil())
		}
		right = min(right, row.x+row.w)
		if right > left {
			underlineY := row.y + float32(underlineOffset)
			glDrawFilledRect(o.programRect, left, underlineY, right-left, 1, float32(textColor.R)/255, float32(textColor.G)/255, float32(textColor.B)/255, o.uiAlpha(1), winW, winH, viewW, viewH)
		}
	}
}

func (o *Overlay) handlePointerWheel(event input.PointerEvent, winW, winH int) PointerResult {
	target := layoutTargetAt(o.pointerLayout(winW, winH).targets, event.X, event.Y)
	if target.kind == pointerTargetNone && event.Device == input.PointerMouse && !event.PositionValid {
		// SDL2 wheel events have no position. Before the first motion event,
		// use the panel that already owns keyboard/gamepad focus.
		target = pointerTarget{kind: pointerTargetPanel, panel: o.focusPanel}
	}
	if o.pointerModalBlocks(target) {
		return PointerResultNone
	}
	if target.kind != pointerTargetPanel && target.kind != pointerTargetRow && target.kind != pointerTargetAction {
		return PointerResultNone
	}
	if target.kind == pointerTargetAction {
		return PointerResultNone
	}
	if target.panel >= 0 {
		o.applyPointerPanelFocus(target)
	}
	delta := event.ScrollY
	if delta == 0 {
		return PointerResultNone
	}
	steps := max(1, int(math.Abs(float64(delta))))
	direction := 1
	if delta > 0 {
		direction = -1
	}
	for range steps {
		o.pointerScrollStep(target.panel, direction, winW, winH)
	}
	return PointerResultNone
}

func (o *Overlay) consumePointerScroll(panel int, remainder *float32, winW, winH int) {
	lineH := float32(max(1, o.lineHeight()))
	for *remainder >= lineH {
		o.pointerScrollStep(panel, 1, winW, winH)
		*remainder -= lineH
	}
	for *remainder <= -lineH {
		o.pointerScrollStep(panel, -1, winW, winH)
		*remainder += lineH
	}
}

func (o *Overlay) pointerScrollStep(panel, direction, winW, winH int) {
	if panel < 0 || panel > 1 {
		return
	}
	if o.focusPanel != panel {
		o.focusPanel = panel
		o.markPointerFocusDirty()
	}
	if o.uiPage == PagePresets && panel != 0 {
		return
	}
	visible := o.pointerVisibleRows(winW, winH)
	var changed bool
	switch o.uiPage {
	case PageLibrary:
		if panel == 0 {
			changed = stepPointerScroll(&o.albumsScroll, len(o.albums), visible, direction)
			o.pointerScroll[0] = true
			o.maybeRequestRadioPageAt(o.albumsScroll + visible - 1)
		} else if o.infoPanelFocused() {
			changed = stepPointerScroll(&o.ncInfoScroll, o.ncInfoLines, o.ncInfoVisible, direction)
		} else if o.isNC() && o.ncRight != ncRightInfo {
			return
		} else {
			changed = stepPointerScroll(&o.tracksScroll, len(o.trackInfos), visible, direction)
			o.pointerScroll[1] = true
		}
	case PageSettings:
		if panel == 0 {
			changed = stepPointerScroll(&o.albumsScroll, len(o.settingsRows), visible, direction)
			o.pointerScroll[0] = true
		} else {
			row := o.settingsCursor
			if row < 0 || row >= len(o.settingsRows) || o.settingsRows[row].Header {
				return
			}
			if !o.settingsEditing {
				o.settingsEditing = true
				o.settingsValueCursor = o.settingsRows[row].Index
			}
			changed = stepPointerScroll(&o.tracksScroll, len(o.settingsRows[row].Values), visible, direction)
			o.pointerScroll[1] = true
			o.settingsDirty = true
		}
	case PageHelp:
		if panel == 0 {
			if o.helpView.InGrandChildren {
				changed = stepPointerScroll(&o.helpView.GrandChildTop, o.helpLeftCount(), visible, direction)
			} else {
				changed = stepPointerScroll(&o.helpView.EntryTop, o.helpLeftCount(), visible, direction)
			}
			o.pointerScroll[0] = true
		} else {
			topic := o.helpTopic(HelpTopicID(o.helpView.TopicCursor))
			changed = stepPointerScroll(&o.helpView.ContentTop, o.helpContentRowCount(topic), visible, direction)
			o.pointerScroll[1] = true
		}
	case PagePresets:
		if cur := o.presetNav.current(); cur != nil {
			total := len(cur.nodes) + boolToInt(len(o.presetNav.stack) > 1)
			changed = stepPointerScroll(&cur.scroll, total, visible, direction)
			o.pointerScroll[0] = true
		}
	}
	if changed {
		o.markPointerScrollDirty(panel)
	}
}

func stepPointerScroll(current *int, total, visible, direction int) bool {
	if current == nil || total <= 0 || visible <= 0 {
		return false
	}
	maxScroll := max(0, total-visible)
	next := max(0, min(*current+direction, maxScroll))
	if next == *current {
		return false
	}
	*current = next
	return true
}

func clampPointerScroll(current, total, visible int) int {
	return max(0, min(current, max(0, total-visible)))
}

func (o *Overlay) pointerScrollbars(winW, winH int) []pointerScrollbar {
	l := o.overlayLayout(winW, winH)
	visible := max(1, l.panelH/l.lineH)
	barW := float32(o.scrollbarWidthPx())
	leftX := float32(l.panelW) - barW
	rightX := float32(winW) - barW
	result := make([]pointerScrollbar, 0, 2)
	add := func(panel int, x, y, h float32, total, pageVisible, scroll int) {
		thumbY, thumbH, ok := scrollbarThumbGeometry(y, h, total, pageVisible, scroll)
		if !ok {
			return
		}
		hitWidth := max(barW, float32(o.scalePx(18)))
		result = append(result, pointerScrollbar{
			panel:    panel,
			track:    pointerRect{x: x, y: y, w: barW, h: h},
			thumb:    pointerRect{x: x, y: thumbY, w: barW, h: thumbH},
			total:    total,
			visible:  pageVisible,
			hitWidth: hitWidth,
		})
	}

	switch o.uiPage {
	case PageLibrary:
		add(0, leftX, float32(l.panelY), float32(l.panelH), len(o.albums), visible, o.albumsScroll)
		isCatalogInfo := o.currentEntry() != nil && o.currentEntry().IsCatalogTrack()
		catalogDelete := isCatalogInfo && o.isTrackCached != nil && o.isTrackCached(o.currentEntry().filePath)
		metadataH := float32(l.panelH)
		if o.hasNCSelection() || catalogDelete {
			metadataH -= float32(o.actionBarHeight(l.lineH))
			if metadataH < float32(l.lineH) {
				metadataH = float32(l.lineH)
			}
		}
		if o.libraryRightInfoScrollbar() {
			add(1, rightX, float32(l.panelY), metadataH, o.ncInfoLines, o.ncInfoVisible, o.ncInfoScroll)
		} else {
			add(1, rightX, float32(l.panelY), float32(l.panelH), len(o.trackInfos), visible, o.tracksScroll)
		}
	case PageSettings:
		add(0, leftX, float32(l.panelY), float32(l.panelH), len(o.settingsRows), visible, o.albumsScroll)
		rightTotal := 0
		if o.panelEntered && o.settingsEditing && o.settingsCursor >= 0 && o.settingsCursor < len(o.settingsRows) {
			rightTotal = len(o.settingsRows[o.settingsCursor].Values)
		}
		add(1, rightX, float32(l.panelY), float32(l.panelH), rightTotal, visible, o.tracksScroll)
	case PagePresets:
		if cur := o.presetNav.current(); cur != nil {
			total := len(cur.nodes) + boolToInt(len(o.presetNav.stack) > 1)
			add(0, leftX, float32(l.panelY), float32(l.panelH), total, visible, cur.scroll)
		}
	case PageHelp:
		leftScroll := o.helpView.EntryTop
		if o.helpView.InGrandChildren {
			leftScroll = o.helpView.GrandChildTop
		}
		leftTotal := o.helpLeftCount()
		add(0, leftX, float32(l.panelY), float32(l.panelH), leftTotal, visible, leftScroll)
		topic := o.helpTopic(HelpTopicID(o.helpView.TopicCursor))
		add(1, rightX, float32(l.panelY), float32(l.panelH), o.helpContentRowCount(topic), visible, o.helpView.ContentTop)
	}
	return result
}

func (o *Overlay) pointerScrollbarAt(x, y float32, winW, winH int) (pointerScrollbar, bool) {
	for _, scrollbar := range o.pointerScrollbars(winW, winH) {
		hitX := scrollbar.track.x - (scrollbar.hitWidth-scrollbar.track.w)/2
		if x >= hitX && x < hitX+scrollbar.hitWidth && y >= scrollbar.track.y && y < scrollbar.track.y+scrollbar.track.h {
			return scrollbar, true
		}
	}
	return pointerScrollbar{}, false
}

func (o *Overlay) updatePointerScrollbar(y float32, panel int, grabOffset float32, winW, winH int) {
	for _, scrollbar := range o.pointerScrollbars(winW, winH) {
		if scrollbar.panel != panel {
			continue
		}
		travel := scrollbar.track.h - scrollbar.thumb.h
		if travel <= 0 {
			return
		}
		thumbTop := max(0, min(y-grabOffset-scrollbar.track.y, travel))
		position := int(math.Round(float64(thumbTop / travel * float32(scrollbar.total-scrollbar.visible))))
		o.setPointerScrollPosition(panel, position, winW, winH)
		return
	}
}

func (o *Overlay) setPointerScrollPosition(panel, position, winW, winH int) {
	visible := o.pointerVisibleRows(winW, winH)
	var current *int
	switch o.uiPage {
	case PageLibrary:
		if panel == 0 {
			current = &o.albumsScroll
			o.pointerScroll[0] = true
		} else if o.libraryRightInfoScrollbar() {
			current = &o.ncInfoScroll
			visible = o.ncInfoVisible
		} else {
			current = &o.tracksScroll
			o.pointerScroll[1] = true
		}
	case PageSettings:
		if panel == 0 {
			current = &o.albumsScroll
			o.pointerScroll[0] = true
		} else {
			current = &o.tracksScroll
			o.pointerScroll[1] = true
		}
	case PagePresets:
		if panel == 0 {
			if cur := o.presetNav.current(); cur != nil {
				current = &cur.scroll
				o.pointerScroll[0] = true
			}
		}
	case PageHelp:
		if panel == 0 {
			o.pointerScroll[0] = true
			if o.helpView.InGrandChildren {
				current = &o.helpView.GrandChildTop
			} else {
				current = &o.helpView.EntryTop
			}
		} else {
			current = &o.helpView.ContentTop
		}
	}
	if current == nil {
		return
	}
	next := clampPointerScroll(position, pointerScrollbarTotal(o, panel), visible)
	if *current == next {
		return
	}
	*current = next
	o.markPointerScrollDirty(panel)
}

func pointerScrollbarTotal(o *Overlay, panel int) int {
	switch o.uiPage {
	case PageLibrary:
		if panel == 0 {
			return len(o.albums)
		}
		if o.libraryRightInfoScrollbar() {
			return o.ncInfoLines
		}
		return len(o.trackInfos)
	case PageSettings:
		if panel == 0 {
			return len(o.settingsRows)
		}
		if o.settingsCursor >= 0 && o.settingsCursor < len(o.settingsRows) {
			return len(o.settingsRows[o.settingsCursor].Values)
		}
	case PagePresets:
		if panel == 0 {
			if cur := o.presetNav.current(); cur != nil {
				return len(cur.nodes) + boolToInt(len(o.presetNav.stack) > 1)
			}
		}
	case PageHelp:
		if panel == 0 {
			return o.helpLeftCount()
		}
		return o.helpContentRowCount(o.helpTopic(HelpTopicID(o.helpView.TopicCursor)))
	}
	return 0
}

func (o *Overlay) libraryRightInfoScrollbar() bool {
	if o.hasNCSelection() {
		return true
	}
	entry := o.currentEntry()
	return (entry != nil && entry.IsCatalogTrack()) || o.infoLines != nil
}

func (o *Overlay) pointerVisibleRows(winW, winH int) int {
	l := o.overlayLayout(winW, winH)
	return max(1, l.panelH/l.lineH)
}

func (o *Overlay) helpLeftCount() int {
	topic := o.helpTopic(HelpTopicID(o.helpView.TopicCursor))
	count := o.helpTopicCount()
	if o.helpView.InGrandChildren {
		if o.helpView.EntryCursor >= 0 && o.helpView.EntryCursor < len(topic.Children) {
			count = len(topic.Children[o.helpView.EntryCursor].Children)
		}
	} else if o.helpView.InChildren {
		count = len(topic.Children)
	}
	if o.helpHasParent() {
		count++
	}
	return count
}

func (o *Overlay) helpHasParent() bool {
	return o.helpView.InChildren || o.helpView.InGrandChildren
}

func (o *Overlay) markPointerScrollDirty(panel int) {
	switch o.uiPage {
	case PageLibrary:
		if panel == 0 {
			o.albumsDirty = true
		} else {
			o.tracksDirty = true
		}
	case PageSettings:
		o.settingsDirty = true
	case PagePresets:
		o.presetsDirty = true
	case PageHelp:
		o.helpDirty = true
	}
}

func (o *Overlay) applyPointerFocus(target pointerTarget) {
	switch target.kind {
	case pointerTargetRow, pointerTargetAction, pointerTargetPanel:
		if target.panel >= 0 {
			focusChanged := o.focusPanel != target.panel
			o.focusPanel = target.panel
			if focusChanged {
				o.markPointerFocusDirty()
			}
			if target.kind == pointerTargetAction {
				o.applyPointerActionFocus(target.index)
			}
		}
		if target.kind == pointerTargetRow {
			o.setPointerCursor(target)
		}
	case pointerTargetPage, pointerTargetPagePrev, pointerTargetPageNext:
		// Page controls do not disturb the list cursor.
	}
}

func (o *Overlay) applyPointerPanelFocus(target pointerTarget) {
	if target.panel < 0 || target.panel > 1 {
		return
	}
	if o.focusPanel == target.panel {
		return
	}
	o.focusPanel = target.panel
	o.markPointerFocusDirty()
}

func (o *Overlay) markPointerFocusDirty() {
	switch o.uiPage {
	case PageLibrary:
		o.albumsDirty = true
		o.tracksDirty = true
	case PageSettings:
		o.settingsDirty = true
	case PagePresets:
		o.presetsDirty = true
	case PageHelp:
		o.helpDirty = true
	}
}

func (o *Overlay) applyPointerActionFocus(index int) {
	if o.uiPage != PageLibrary || !o.isNC() {
		return
	}
	if o.pointerActionIsDelete(index) {
		o.ncRight = ncRightDelete
	} else {
		o.ncRight = ncRightPlay
	}
	o.tracksDirty = true
	o.tracksContentDirty = true
}

func (o *Overlay) setPointerCursor(target pointerTarget) {
	if target.panel >= 0 && target.panel < len(o.pointerScroll) {
		o.pointerScroll[target.panel] = false
	}
	switch o.uiPage {
	case PageLibrary:
		if target.panel == 0 {
			if target.index >= 0 && target.index < len(o.albums) {
				o.albumCursor = target.index
				o.trackCursor = 0
				if o.isNC() {
					o.refreshNCPreview()
				}
				o.albumsDirty = true
				o.tracksDirty = true
				o.tracksContentDirty = true
				o.maybeRequestRadioPage()
			}
		} else if target.index >= 0 && target.index < len(o.trackInfos) {
			o.trackCursor = target.index
			o.tracksDirty = true
		}
	case PageSettings:
		if target.panel == 0 && target.index >= 0 && target.index < len(o.settingsRows) && !o.settingsRows[target.index].Header {
			o.settingsCursor = target.index
			o.settingsValueCursor = o.settingsRows[target.index].Index
			o.settingsDirty = true
		} else if target.panel == 1 && o.settingsCursor >= 0 && o.settingsCursor < len(o.settingsRows) {
			values := o.settingsRows[o.settingsCursor].Values
			if target.index >= 0 && target.index < len(values) {
				o.settingsValueCursor = target.index
				o.settingsDirty = true
			}
		}
	case PagePresets:
		cur := o.presetNav.current()
		if cur != nil && target.index >= 0 && target.index < len(cur.nodes)+boolToInt(len(o.presetNav.stack) > 1) {
			cur.cursor = target.index
			o.presetCursorDirty = true
			o.schedulePreviewForSelected(time.Now())
		}
	case PageHelp:
		if target.panel == 0 {
			o.helpView.ParentSelected = false
			if o.helpView.InGrandChildren {
				o.helpView.GrandChildCursor = target.index
			} else if o.helpView.InChildren {
				o.helpView.EntryCursor = target.index
			} else {
				o.helpView.TopicCursor = target.index
			}
			o.helpDirty = true
		}
	}
	o.markInteraction()
}

func (o *Overlay) isCurrentHelpRow(target pointerTarget) bool {
	if o.uiPage != PageHelp || target.kind != pointerTargetRow || target.panel != 0 || o.helpView.ParentSelected {
		return false
	}
	if o.helpView.InGrandChildren {
		return target.index == o.helpView.GrandChildCursor
	}
	if o.helpView.InChildren {
		return target.index == o.helpView.EntryCursor
	}
	return target.index == o.helpView.TopicCursor
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (o *Overlay) pointerModalBlocks(target pointerTarget) bool {
	if target.kind == pointerTargetClose {
		return false
	}
	if o.uiPage == PageLibrary && o.ncConfirm {
		return target.kind != pointerTargetAction || !o.pointerActionIsDelete(target.index)
	}
	if o.uiPage == PageSettings && o.settingsEditing {
		return target.kind != pointerTargetRow || target.panel != 1
	}
	return false
}

func (o *Overlay) lineHeight() int {
	if o.face == nil {
		return max(1, int(math.Round(o.fontSize)))
	}
	return max(1, o.face.Metrics().Height.Ceil())
}

func layoutTargetAt(targets []pointerTargetRect, x, y float32) pointerTarget {
	for _, item := range targets {
		if x >= item.rect.x && x < item.rect.x+item.rect.w && y >= item.rect.y && y < item.rect.y+item.rect.h {
			return item.target
		}
	}
	return pointerTarget{}
}

func (o *Overlay) pointerLayout(winW, winH int) pointerLayout {
	l := o.overlayLayout(winW, winH)
	result := pointerLayout{panelY: float32(l.panelY), panelH: float32(l.panelH), panelW: float32(l.panelW), lineH: float32(l.lineH)}
	if o.pointerCloseVisible() {
		closeSize := float32(max(l.lineH, o.scalePx(24)))
		result.targets = append(result.targets, pointerTargetRect{
			target: pointerTarget{kind: pointerTargetClose, panel: -1},
			rect:   pointerRect{x: float32(winW) - float32(o.headerMarginX()) - closeSize, y: 0, w: closeSize, h: float32(l.headerHeight)},
		})
	}
	result.targets = append(result.targets, o.pagePointerTargets(winW, l.headerHeight)...)
	result.targets = append(result.targets, o.breadcrumbPointerTargets(winW, l.headerHeight)...)

	result.targets = append(result.targets, o.rowPointerTargets(l)...)
	// Panel rectangles are fallbacks for blank areas and must come after rows
	// and action buttons in the hit-test order.
	for panel := 0; panel < 2; panel++ {
		x := float32(panel * l.panelW)
		if panel == 1 {
			x = float32(winW - l.panelW)
		}
		result.targets = append(result.targets, pointerTargetRect{
			target: pointerTarget{kind: pointerTargetPanel, panel: panel},
			rect:   pointerRect{x: x, y: float32(l.panelY), w: float32(l.panelW), h: float32(l.panelH)},
		})
	}
	return result
}

func (o *Overlay) pagePointerTargets(winW, headerH int) []pointerTargetRect {
	if o.face == nil {
		return nil
	}
	result := make([]pointerTargetRect, 0, 6)
	lh := o.lineHeight()
	gap := int(o.fontSize * pageIndicatorGapFactor)
	iconGap := o.scalePx(2)
	x := o.pageIndicatorX(winW)
	result = append(result, pointerTargetRect{target: pointerTarget{kind: pointerTargetPagePrev, panel: -1}, rect: pointerRect{x: float32(x), y: 0, w: float32(lh), h: float32(headerH)}})
	x += lh + iconGap
	for i := range o.pageIndicatorTexW {
		w := lh + iconGap + o.pageIndicatorTexW[i]
		result = append(result, pointerTargetRect{target: pointerTarget{kind: pointerTargetPage, page: UIPage(i), panel: -1}, rect: pointerRect{x: float32(x), y: 0, w: float32(w), h: float32(headerH)}})
		x += w + gap
	}
	result = append(result, pointerTargetRect{target: pointerTarget{kind: pointerTargetPageNext, panel: -1}, rect: pointerRect{x: float32(x - gap), y: 0, w: float32(lh), h: float32(headerH)}})
	return result
}

func (o *Overlay) rowPointerTargets(l overlayLayout) []pointerTargetRect {
	result := make([]pointerTargetRect, 0)
	addRows := func(panel, first, count, scroll int) {
		if count <= 0 {
			return
		}
		visible := l.panelH / l.lineH
		if visible < 1 {
			visible = 1
		}
		count = min(count, visible)
		x := 0
		if panel == 1 {
			x = l.windowW - l.panelW
		}
		for row := 0; row < count; row++ {
			index := first + scroll + row
			result = append(result, pointerTargetRect{
				target: pointerTarget{kind: pointerTargetRow, panel: panel, index: index},
				rect:   pointerRect{x: float32(x), y: float32(l.panelY + row*l.lineH), w: float32(l.panelW), h: float32(l.lineH)},
			})
		}
	}

	switch o.uiPage {
	case PageLibrary:
		addRows(0, 0, len(o.albums), o.albumsScroll)
		if o.pointerActionBarVisible() {
			result = append(result, o.pointerActionTargets(l)...)
		} else if !o.infoPanelFocused() {
			addRows(1, 0, len(o.trackInfos), o.tracksScroll)
		}
	case PageSettings:
		for row := o.albumsScroll; row < len(o.settingsRows) && row < o.albumsScroll+l.panelH/l.lineH; row++ {
			if o.settingsRows[row].Header {
				continue
			}
			result = append(result, pointerTargetRect{target: pointerTarget{kind: pointerTargetRow, panel: 0, index: row}, rect: pointerRect{x: 0, y: float32(l.panelY + (row-o.albumsScroll)*l.lineH), w: float32(l.panelW), h: float32(l.lineH)}})
		}
		if o.settingsCursor >= 0 && o.settingsCursor < len(o.settingsRows) {
			values := o.settingsRows[o.settingsCursor].Values
			for row := o.tracksScroll; row < len(values) && row < o.tracksScroll+l.panelH/l.lineH; row++ {
				result = append(result, pointerTargetRect{target: pointerTarget{kind: pointerTargetRow, panel: 1, index: row}, rect: pointerRect{x: float32(l.windowW - l.panelW), y: float32(l.panelY + (row-o.tracksScroll)*l.lineH), w: float32(l.panelW), h: float32(l.lineH)}})
			}
		}
	case PagePresets:
		if cur := o.presetNav.current(); cur != nil {
			addRows(0, 0, len(cur.nodes)+boolToInt(len(o.presetNav.stack) > 1), cur.scroll)
		}
	case PageHelp:
		count := o.helpLeftCount()
		scroll := o.helpView.EntryTop
		if o.helpView.InGrandChildren {
			scroll = o.helpView.GrandChildTop
		}
		if o.helpHasParent() {
			addHelpRows := func() {
				visible := l.panelH / l.lineH
				if visible < 1 {
					visible = 1
				}
				end := min(scroll+visible, count)
				for displayIndex := scroll; displayIndex < end; displayIndex++ {
					target := pointerTarget{kind: pointerTargetRow, panel: 0, index: displayIndex - 1}
					if displayIndex == 0 {
						target = pointerTarget{kind: pointerTargetBreadcrumb, helpBack: true}
					}
					result = append(result, pointerTargetRect{
						target: target,
						rect: pointerRect{
							x: 0, y: float32(l.panelY + (displayIndex-scroll)*l.lineH),
							w: float32(l.panelW), h: float32(l.lineH),
						},
					})
				}
			}
			addHelpRows()
		} else {
			addRows(0, 0, count, scroll)
		}
	}
	return result
}

func (o *Overlay) pointerActionPlacements() []actionPlacement {
	if o.face == nil {
		return nil
	}
	measure := func(s string) int { return font.MeasureString(o.face, s).Ceil() }
	actions := []libraryAction{{icon: iconPlay, label: o.catalog.Text(i18n.ActionPlay)}}
	if o.isNC() {
		if o.ncInfoFile != "" || (o.ncInfoIsDir && o.ncInfoDir != o.baseDir) {
			actions = append(actions, libraryAction{icon: iconDelete, label: o.catalog.Text(i18n.ActionDelete), confirm: o.ncConfirm})
		}
	} else if e := o.currentEntry(); e != nil && e.IsCatalogTrack() && o.isTrackCached != nil && o.isTrackCached(e.filePath) {
		actions = []libraryAction{{icon: iconDelete, label: o.catalog.Text(i18n.ActionDeleteDownload), confirm: o.ncConfirm}}
	}
	return layoutLibraryActions(actions, o.lineHeight(), o.scalePx(4), measure(">"), measure)
}

func (o *Overlay) pointerActionBarVisible() bool {
	if o.isNC() {
		return o.hasNCSelection()
	}
	e := o.currentEntry()
	return e != nil && e.IsCatalogTrack() && o.isTrackCached != nil && o.isTrackCached(e.filePath)
}

func (o *Overlay) pointerActionTargets(l overlayLayout) []pointerTargetRect {
	actionY := l.panelY + l.panelH - o.actionBarHeight(l.lineH)
	actionH := o.actionBarHeight(l.lineH)
	placements := o.pointerActionPlacements()
	result := make([]pointerTargetRect, 0, len(placements))
	for i, item := range placements {
		result = append(result, pointerTargetRect{
			target: pointerTarget{kind: pointerTargetAction, panel: 1, index: i},
			rect: pointerRect{
				x: float32(l.windowW - l.panelW + item.cursorX),
				y: float32(actionY),
				w: float32(item.endX - item.cursorX + 2*o.scalePx(4)),
				h: float32(actionH),
			},
		})
	}
	return result
}

func (o *Overlay) pointerSetPage(page UIPage) {
	if page == o.uiPage {
		return
	}
	o.focusPanel = 0
	o.pointerScroll = [2]bool{}
	o.marqueeL.invalidate(o)
	o.marqueeR.invalidate(o)
	o.pageIndicatorDirty = true
	o.uiPage = page
	if page == PagePresets {
		o.syncPresetTree()
	}
	o.panelEntered = true
	o.settingsEditing = false
	o.markAllDirty()
}

func (o *Overlay) pointerNavigateBreadcrumb(target pointerTarget) {
	switch {
	case target.helpBack:
		o.Back()
	case target.dirPath != "":
		o.switchToNC(target.dirPath)
	case target.presetDepth > 0:
		for len(o.presetNav.stack) > target.presetDepth {
			o.presetNav.Collapse()
		}
		o.focusPanel = 0
		o.presetsDirty = true
		o.breadcrumbDirty = true
	case target.navStackDepth >= 0:
		o.jumpToNavStackDepth(target.navStackDepth)
	}
	o.ncConfirm = false
	o.markAllDirty()
}

func (o *Overlay) breadcrumbPointerTargets(winW, headerH int) []pointerTargetRect {
	placements := o.breadcrumbPlacements(winW)
	result := make([]pointerTargetRect, 0, len(placements))
	for _, placement := range placements {
		if placement.target.kind != pointerTargetBreadcrumb {
			continue
		}
		result = append(result, pointerTargetRect{
			target: placement.target,
			rect:   pointerRect{x: placement.x, y: float32(o.headerHeight(o.lineHeight())), w: placement.w, h: float32(max(1, headerH-o.headerHeight(o.lineHeight())))},
		})
	}
	return result
}

func (o *Overlay) breadcrumbSegments() []breadcrumbSegment {
	parts := o.breadcrumbParts()
	if len(parts) == 0 {
		return nil
	}
	segments := make([]breadcrumbSegment, 0, len(parts))
	rootTarget := pointerTarget{kind: pointerTargetBreadcrumb, navStackDepth: 1}
	if o.uiPage == PagePresets {
		rootTarget.navStackDepth = -1
		rootTarget.presetDepth = 1
	}
	if len(parts) == 1 {
		rootTarget.kind = pointerTargetNone
	}
	segments = append(segments, breadcrumbSegment{label: parts[0], target: rootTarget})
	if o.uiPage == PagePresets {
		for i := 1; i < len(parts); i++ {
			depth := i + 1
			target := pointerTarget{kind: pointerTargetBreadcrumb, presetDepth: depth}
			if depth >= len(o.presetNav.stack) {
				target.kind = pointerTargetNone
			}
			segments = append(segments, breadcrumbSegment{label: parts[i], target: target})
		}
		return segments
	}
	if o.isNC() {
		root := o.musicDir
		if root == "" {
			root = o.baseDir
		}
		rootTarget := pointerTarget{kind: pointerTargetBreadcrumb, dirPath: root}
		if root == o.ncDir() {
			rootTarget.kind = pointerTargetNone
		}
		segments = append(segments, breadcrumbSegment{label: parts[1], target: rootTarget})
		for i := 2; i < len(parts); i++ {
			dir := root
			for _, part := range parts[2 : i+1] {
				dir = filepath.Join(dir, part)
			}
			target := pointerTarget{kind: pointerTargetBreadcrumb, dirPath: dir}
			if dir == o.ncDir() {
				target.kind = pointerTargetNone
			}
			segments = append(segments, breadcrumbSegment{label: parts[i], target: target})
		}
		return segments
	}
	for i := 1; i < len(parts); i++ {
		depth := i + 1
		target := pointerTarget{kind: pointerTargetBreadcrumb, navStackDepth: depth}
		if depth >= len(o.navStack) || (o.focusPanel == 1 && i == len(parts)-1) {
			target.kind = pointerTargetNone
		}
		icon := ""
		if i < len(o.navStack) && o.navStack[i].ctx == ctxFavorites && o.navStack[i].playlistID != "" {
			icon = favoriteFolderIconName(o.navStack[i].playlistID)
		}
		segments = append(segments, breadcrumbSegment{label: parts[i], icon: icon, target: target})
	}
	return segments
}

func (o *Overlay) breadcrumbPlacements(winW int) []breadcrumbPlacement {
	if !o.showsBreadcrumb() || o.face == nil {
		return nil
	}
	segments := o.breadcrumbSegments()
	maxW := max(1, winW-2*o.headerMarginX())
	chosen := segments
	fullWidth := func(items []breadcrumbSegment) int {
		width := 0
		for i, item := range items {
			width += o.breadcrumbItemWidth(i, item)
		}
		return width
	}
	if fullWidth(chosen) > maxW && len(segments) > 3 {
		for tail := min(3, len(segments)-2); tail >= 1; tail-- {
			candidate := append([]breadcrumbSegment{}, segments[:2]...)
			candidate = append(candidate, breadcrumbSegment{label: "…", target: pointerTarget{kind: pointerTargetNone}})
			candidate = append(candidate, segments[len(segments)-tail:]...)
			if fullWidth(candidate) <= maxW {
				chosen = candidate
				break
			}
		}
		if fullWidth(chosen) > maxW {
			chosen = append([]breadcrumbSegment{}, segments[:2]...)
			chosen = append(chosen, breadcrumbSegment{label: "…", target: pointerTarget{kind: pointerTargetNone}})
			chosen = append(chosen, segments[len(segments)-1:]...)
		}
	}
	placements := make([]breadcrumbPlacement, 0, len(chosen))
	x := float32(o.headerMarginX())
	for i, item := range chosen {
		label, icon := o.breadcrumbItemLabel(i, item)
		w := float32(o.breadcrumbItemWidth(i, item))
		placements = append(placements, breadcrumbPlacement{label: label, icon: icon, target: item.target, x: x, w: w})
		x += w
	}
	return placements
}

func (o *Overlay) breadcrumbItemLabel(index int, item breadcrumbSegment) (string, string) {
	if index == 0 && item.label == "/" {
		return "", iconHome
	}
	if index > 0 {
		if item.icon != "" {
			return "/" + strings.Repeat(" ", o.breadcrumbIconSpacerCount()), item.icon
		}
		return "/" + item.label, ""
	}
	return item.label, ""
}

func (o *Overlay) breadcrumbIconSpacerCount() int {
	if o.face == nil {
		return 0
	}
	targetWidth := o.sourceIconTextOffset(o.lineHeight())
	spaces := 0
	for spaces < 64 && font.MeasureString(o.face, strings.Repeat(" ", spaces)).Ceil() < targetWidth {
		spaces++
	}
	return spaces
}

func (o *Overlay) breadcrumbItemWidth(index int, item breadcrumbSegment) int {
	label, icon := o.breadcrumbItemLabel(index, item)
	width := font.MeasureString(o.face, label).Ceil()
	if icon != "" && label == "" {
		width += o.sourceIconTextOffset(o.lineHeight())
	}
	return width
}

func (o *Overlay) breadcrumbDisplayText(winW int) string {
	placements := o.breadcrumbPlacements(winW)
	if len(placements) == 0 {
		return ""
	}
	var text strings.Builder
	for _, placement := range placements {
		text.WriteString(placement.label)
	}
	return text.String()
}

func (o *Overlay) pointerCloseVisible() bool {
	return o.pointerCapabilitiesSet && (o.pointerMouse || o.pointerTouch)
}

func (o *Overlay) keyMappingAvailable() bool {
	if !o.pointerCapabilitiesSet {
		// Preserve the pre-pointer behavior for model-only tests and for the
		// short interval before the app publishes its first capability snapshot.
		return true
	}
	return o.keyboardAvailable || o.controllerConnected
}

func (o *Overlay) pointerActionIsDelete(index int) bool {
	placements := o.pointerActionPlacements()
	return index >= 0 && index < len(placements) && placements[index].icon == iconDelete
}
