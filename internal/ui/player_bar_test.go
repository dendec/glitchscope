package ui

import (
	"testing"

	"github.com/dendec/glitchscope/internal/input"
)

func TestPlayerBarScrub(t *testing.T) {
	for _, menu := range []bool{false, true} {
		for _, device := range []input.PointerDevice{input.PointerMouse, input.PointerTouch} {
			o := testPointerOverlay()
			o.uiVisible, o.showPlayerBar, o.playingPath, o.duration, o.paused = menu, true, "track.mp3", 200, true
			_, end, _ := o.playerBarBounds(640, 480)
			e := input.PointerEvent{Device: device, Phase: input.PointerDown, PointerID: 1, X: 160, Y: float32(end - 2), Button: input.PointerButtonPrimary}
			r := o.HandlePlayerBarPointer(e, 640, 480)
			if !r.Consumed || r.Seek || o.playerBarPosition() != 50 {
				t.Fatalf("press: %+v", r)
			}
			e.Phase, e.X, e.Y = input.PointerMove, 800, 0
			r = o.HandlePlayerBarPointer(e, 640, 480)
			if !r.Consumed || r.Seek || o.playerBarPosition() != 200 {
				t.Fatalf("move: %+v", r)
			}
			e.Phase = input.PointerUp
			r = o.HandlePlayerBarPointer(e, 640, 480)
			if !r.Seek || r.Seconds != 200 || r.Path != "track.mp3" || !o.paused || o.barPress.active {
				t.Fatalf("release: %+v", r)
			}
		}
	}
}

func TestPlayerBarCancelAndTrackReplacement(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		o := testPointerOverlay()
		o.showPlayerBar, o.playingPath, o.duration = true, "old.mp3", 100
		_, end, _ := o.playerBarBounds(640, 480)
		e := input.PointerEvent{Phase: input.PointerDown, X: 100, Y: float32(end - 1), Button: input.PointerButtonPrimary}
		o.HandlePlayerBarPointer(e, 640, 480)
		if cancel {
			e.Phase = input.PointerCancel
		} else {
			o.playingPath = "new.mp3"
			e.Phase = input.PointerUp
		}
		r := o.HandlePlayerBarPointer(e, 640, 480)
		if r.Seek || o.barPress.active {
			t.Fatalf("stale seek: %+v", r)
		}
	}
}

func TestPlayerBarButtonsAndStatus(t *testing.T) {
	o := testPointerOverlay()
	o.showPlayerBar, o.playingPath = true, "track.mp3"
	if o.playerBarPrefix() != "▶ " {
		t.Fatal("legacy playing status")
	}
	o.paused = true
	if o.playerBarPrefix() != "⏸ " {
		t.Fatal("legacy paused status")
	}
	o.SetPointerCapabilities(true, true, false)
	if o.playerBarPrefix() != " ⏮   ▶   ⏭ " {
		t.Fatal("pointer resume action")
	}
	o.paused = false
	if o.playerBarPrefix() != " ⏮   ⏸   ⏭ " {
		t.Fatal("pointer pause action")
	}
	_, _, y := o.playerBarBounds(640, 480)
	for i, action := range []input.Action{input.ActionPrevTrack, input.ActionPlayPause, input.ActionNextTrack} {
		x := float32(0)
		for x < 640 && o.playerBarTarget(x, float32(y+2), 640, 480) != i+1 {
			x++
		}
		if x == 640 {
			t.Fatalf("missing button %d", i)
		}
		e := input.PointerEvent{Phase: input.PointerDown, X: x, Y: float32(y + 2), Button: input.PointerButtonPrimary}
		o.HandlePlayerBarPointer(e, 640, 480)
		e.Phase = input.PointerUp
		r := o.HandlePlayerBarPointer(e, 640, 480)
		if !r.Consumed || r.Action != action {
			t.Fatalf("button %d: %+v", i, r)
		}
	}
}

func TestPlaybackProgressThemeColorsOpaque(t *testing.T) {
	for theme := range themePalettes {
		o := testPointerOverlay()
		o.theme = theme
		o.transparency = 1
		dark, light := o.playbackProgressColors()
		if dark.A != 255 || light.A != 255 || dark == light {
			t.Fatalf("theme %v: %v %v", theme, dark, light)
		}
		if 299*int(dark.R)+587*int(dark.G)+114*int(dark.B) > 299*int(light.R)+587*int(light.G)+114*int(light.B) {
			t.Fatalf("inverted theme %v", theme)
		}
	}
}

func TestPlaybackProgressState(t *testing.T) {
	tests := []struct {
		name               string
		showBar            bool
		path               string
		loading            bool
		presetNameTex      uint32
		duration, position float64
		visible, seekable  bool
		progress           float64
	}{
		{name: "hidden"},
		{name: "loading", showBar: true, path: "track.mp3", loading: true, duration: 100, visible: true},
		{name: "radio", showBar: true, path: "radio:station", duration: 100, visible: true},
		{name: "unknown duration", showBar: true, path: "track.mp3", visible: true},
		{name: "preset only", showBar: true, presetNameTex: 1, duration: 100, visible: true},
		{name: "seekable", showBar: true, path: "track.mp3", duration: 100, position: 50, visible: true, seekable: true, progress: 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := testPointerOverlay()
			o.showPlayerBar, o.playingPath, o.loading = tt.showBar, tt.path, tt.loading
			o.presetNameTex, o.duration, o.position = tt.presetNameTex, tt.duration, tt.position
			visible, seekable, progress := o.playbackProgressState()
			if visible != tt.visible || seekable != tt.seekable || progress != tt.progress {
				t.Fatalf("progress state = (%t, %t, %v), want (%t, %t, %v)", visible, seekable, progress, tt.visible, tt.seekable, tt.progress)
			}
		})
	}
}

func TestPlayerBarEmptyAndUnavailableTimeline(t *testing.T) {
	o := testPointerOverlay()
	o.uiVisible = false
	o.showPlayerBar, o.playingPath = true, "track.mp3"
	top, end, _ := o.playerBarBounds(640, 480)
	for _, y := range []int{top + 1, end - 1} {
		e := input.PointerEvent{Phase: input.PointerDown, X: 500, Y: float32(y), Button: input.PointerButtonPrimary}
		o.HandlePlayerBarPointer(e, 640, 480)
		e.Phase = input.PointerUp
		r := o.HandlePlayerBarPointer(e, 640, 480)
		if !r.Consumed || r.Seek || o.uiVisible {
			t.Fatalf("empty panel tap: %+v", r)
		}
	}
	o.showPlayerBar = false
	if o.HandlePlayerBarPointer(input.PointerEvent{X: 500, Y: 479}, 640, 480).Consumed {
		t.Fatal("hidden panel captured input")
	}
}

func TestPlayerBarCompactRows(t *testing.T) {
	for _, menu := range []bool{false, true} {
		for _, height := range []int{240, 480, 960} {
			o := testPointerOverlay()
			o.screenH = height
			o.uiVisible = menu
			o.showPlayerBar = true
			o.presetNameTex = 1
			o.bottomTexH = 100 // Texture padding must never inflate row geometry.
			top, end, statusY := o.playerBarBounds(640, height)
			lineH := o.lineHeight()
			outline := shadowRadius(o.fontSize)
			padding := textPadding(o.fontSize)
			presetInkBottom := o.playerBarTextY(float32(top)) + float32(padding+lineH+outline)
			if presetInkBottom > float32(statusY) {
				t.Fatal("preset ink overlaps button backdrop")
			}
			statusInkBottom := o.playerBarTextY(float32(statusY)) + float32(padding+lineH+outline)
			if statusInkBottom != float32(end-o.scalePx(4)) {
				t.Fatal("gap between status row and progress bar")
			}
			if got := o.playerBarTarget(500, float32(end-2), 640, height); got != 0 {
				t.Fatalf("unavailable timeline target: %d", got)
			}
		}
	}
}
