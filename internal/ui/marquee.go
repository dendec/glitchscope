package ui

import (
	"math"
	"time"

	"golang.org/x/image/font"
)

// marqueeState owns the cached full-width text and time-based animation for
// one clipped text element.
type marqueeState struct {
	tex    uint32
	texW   int
	texH   int
	text   string
	maxPx  int
	bold   bool
	offset float32
	start  time.Time
}

func (m *marqueeState) reset() {
	m.offset = 0
	m.start = time.Time{}
}

func (m *marqueeState) invalidate(o *Overlay) {
	o.deleteTex(&m.tex)
	*m = marqueeState{}
}

const (
	marqueeDelay   = 1 * time.Second
	marqueeSpeed   = 60.0
	marqueePauseAt = 1 * time.Second
)

func (o *Overlay) updateMarquee(now time.Time) {
	for _, m := range []*marqueeState{
		&o.marqueeL, &o.marqueeR,
		&o.statsMarquee, &o.breadcrumbMarquee,
		&o.presetNameMarquee, &o.bottomMarquee,
	} {
		o.updateMarqueeCol(m, now)
	}
}

func (o *Overlay) updateMarqueeCol(m *marqueeState, now time.Time) {
	if m.tex == 0 || m.texW <= 0 {
		return
	}
	if m.start.IsZero() {
		m.start = now
		return
	}
	elapsed := now.Sub(m.start)
	if elapsed < marqueeDelay {
		return
	}
	m.offset = float32((elapsed - marqueeDelay).Seconds()) * marqueeSpeed
}

// invalidateActiveMarquee resets the marquee for the focused list column.
func (o *Overlay) invalidateActiveMarquee() {
	if o.focusPanel == 0 {
		o.marqueeL.reset()
	} else {
		o.marqueeR.reset()
	}
}

// rebuildMarqueeLine caches a full-width line only when it is actually wider
// than the available viewport or when its rendering inputs changed.
func (o *Overlay) rebuildMarqueeLine(m *marqueeState, fullText string, maxPx int, bold bool) bool {
	if o.face == nil || fullText == "" || maxPx <= 0 || font.MeasureString(o.face, fullText).Ceil() <= maxPx {
		if m.tex != 0 || m.text != "" {
			o.invalidateMarquee(m)
		}
		return false
	}
	if m.tex != 0 && m.text == fullText && m.maxPx == maxPx && m.bold == bold {
		return true
	}
	o.invalidateMarquee(m)
	boldLines := []bool(nil)
	if bold {
		boldLines = []bool{true}
	}
	m.tex, m.texW, m.texH = o.renderTextToTexBold(fullText, 0, boldLines, o.textColor())
	m.text = fullText
	m.maxPx = maxPx
	m.bold = bold
	return m.tex != 0
}

func (o *Overlay) invalidateMarquee(m *marqueeState) {
	o.deleteTex(&m.tex)
	*m = marqueeState{}
}

// marqueeOffset returns the clamped position for a complete animation cycle.
func marqueeOffset(offset float32, contentW, viewportW int) float32 {
	maxOffset := float32(contentW - viewportW)
	if maxOffset <= 0 {
		return 0
	}
	cycle := maxOffset + float32(marqueePauseAt.Seconds())*marqueeSpeed
	if cycle > 0 {
		offset = float32(math.Mod(float64(offset), float64(cycle)))
	}
	if offset > maxOffset {
		return maxOffset
	}
	return offset
}
