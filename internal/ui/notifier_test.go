package ui

import (
	"testing"
	"time"
)

// newState constructs a Notifier in a known state for testing.
// tex=0 avoids GL dependency; GL paths (ShowTrack, Render) are untested.
func newState(visible, injected, hidden bool, alpha float64, started time.Time) *Notifier {
	return &Notifier{
		visible:  visible,
		injected: injected,
		hidden:   hidden,
		alpha:    alpha,
		started:  started,
	}
}

// --- Toggle ---

func TestToggle_hide_shows_notification(t *testing.T) {
	n := newState(false, false, true, 0, time.Time{})
	n.text = "song"
	n.Toggle()

	if !n.visible {
		t.Fatal("expected visible after unhide")
	}
	if n.hidden {
		t.Fatal("expected not hidden after unhide")
	}
	if n.alpha != 0 {
		t.Fatalf("expected alpha=0, got %f", n.alpha)
	}
	if n.injected {
		t.Fatal("expected injected=false after re-show")
	}
}

func TestToggle_show_hides_notification(t *testing.T) {
	n := newState(true, false, false, 1.0, time.Now())
	n.text = "song"
	n.Toggle()

	if !n.hidden {
		t.Fatal("expected hidden after hide toggle")
	}
	// visible stays true — Draw() checks Hidden() to suppress rendering.
	if !n.visible {
		t.Fatal("visible should remain true (hidden flag controls rendering)")
	}
}

func TestToggle_double_toggle_restores(t *testing.T) {
	n := newState(false, false, false, 0, time.Time{})
	n.text = "track"
	n.Toggle()
	n.Toggle()

	if n.hidden {
		t.Fatal("expected not hidden after double toggle")
	}
}

// --- Hide ---

func TestHide_resets_all_state(t *testing.T) {
	n := newState(true, true, false, 1.0, time.Now())
	n.tex = 42
	n.text = "song"

	n.Hide()

	if n.visible {
		t.Fatal("expected not visible")
	}
	if n.tex != 0 {
		t.Fatal("expected tex=0")
	}
	if n.text != "" {
		t.Fatal("expected empty text")
	}
	if n.alpha != 0 {
		t.Fatal("expected alpha=0")
	}
}

// --- Update ---

func TestUpdate_fade_in_progress(t *testing.T) {
	started := time.Now()
	n := newState(true, false, false, 0, started)

	n.Update(false)

	if n.alpha <= 0 || n.alpha >= 1.0 {
		t.Fatalf("expected partial alpha during fade-in, got %f", n.alpha)
	}
}

func TestUpdate_full_alpha_after_fade(t *testing.T) {
	started := time.Now().Add(-1 * time.Second) // past fade-in (400ms)
	n := newState(true, false, false, 0, started)

	n.Update(false)

	if n.alpha != 1.0 {
		t.Fatalf("expected alpha=1.0 after fade-in, got %f", n.alpha)
	}
}

func TestUpdate_hides_when_injected_and_ui_hidden(t *testing.T) {
	n := newState(true, true, false, 1.0, time.Now())

	n.Update(false)

	if n.visible {
		t.Fatal("expected hidden after injected + uiHidden")
	}
}

func TestUpdate_stays_when_injected_and_ui_visible(t *testing.T) {
	n := newState(true, true, false, 1.0, time.Now())

	n.Update(true)

	if !n.visible {
		t.Fatal("expected still visible when uiVisible and injected")
	}
}

func TestUpdate_hides_after_hold_when_ui_visible(t *testing.T) {
	// Past fade-in + hold: 400ms + 3s = 3.4s
	started := time.Now().Add(-4 * time.Second)
	n := newState(true, false, false, 1.0, started)

	n.Update(true)

	if n.visible {
		t.Fatal("expected hidden after hold period with ui visible")
	}
}

func TestUpdate_stays_after_hold_when_ui_hidden(t *testing.T) {
	started := time.Now().Add(-4 * time.Second)
	n := newState(true, false, false, 1.0, started)

	n.Update(false)

	if !n.visible {
		t.Fatal("expected still visible after hold period with ui hidden")
	}
}

func TestUpdate_noop_when_not_visible(t *testing.T) {
	n := newState(false, false, false, 0.5, time.Now())

	n.Update(false)

	// alpha should remain unchanged
	if n.alpha != 0.5 {
		t.Fatalf("expected alpha unchanged, got %f", n.alpha)
	}
}

// --- Layout ---

func TestLayout_centered(t *testing.T) {
	n := &Notifier{texW: 100, texH: 50}
	x, y, dw, dh := n.Layout(1920, 1080)

	// drawHeight = 1080 * 0.12 = 129.6
	if dh < 129 || dh > 130 {
		t.Fatalf("expected drawHeight ~129.6, got %f", dh)
	}
	// scale = 129.6/50 = 2.592, drawWidth = 100*2.592 = 259.2
	if dw < 258 || dw > 260 {
		t.Fatalf("expected drawWidth ~259.2, got %f", dw)
	}
	// x = (1920 - 259.2) / 2 = 830.4
	if x < 829 || x > 832 {
		t.Fatalf("expected x ~830.4, got %f", x)
	}
	// y = (1080 - 129.6) / 2 = 475.2
	if y < 474 || y > 477 {
		t.Fatalf("expected y ~475.2, got %f", y)
	}
}

func TestLayout_clamps_width(t *testing.T) {
	n := &Notifier{texW: 2000, texH: 100}
	_, _, dw, _ := n.Layout(1920, 1080)

	maxWidth := float32(1920) * 0.85
	if dw > maxWidth+1 {
		t.Fatalf("expected drawWidth <= %f, got %f", maxWidth, dw)
	}
}
