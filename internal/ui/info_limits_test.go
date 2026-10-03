package ui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
)

func TestBoundedInfoLines(t *testing.T) {
	o := &Overlay{face: basicfont.Face7x13, fontSize: 13}
	original := []string{"Title: Song", strings.Repeat("Мяу", 2000)}
	got := o.boundedInfoLines(original)
	if got[0] != original[0] {
		t.Fatalf("short metadata changed: %q", got[0])
	}
	if !utf8.ValidString(got[1]) || !strings.HasSuffix(got[1], "…") {
		t.Fatalf("long Unicode metadata lacks valid truncation: %q", got[1])
	}
	if font.MeasureString(o.face, got[1]).Ceil()+2*textPadding(o.fontSize)+8 > 1024 {
		t.Fatal("metadata exceeds texture width budget")
	}
	if len(original[1]) != len(strings.Repeat("Мяу", 2000)) {
		t.Fatal("original metadata was mutated")
	}

	lines := make([]string, 5000)
	for i := range lines {
		lines[i] = "Comment line"
	}
	got = o.boundedInfoLines(lines)
	if got[len(got)-1] != "…" {
		t.Fatal("omitted metadata lines lack an ellipsis")
	}
	if len(got)*o.face.Metrics().Height.Ceil()+2*textPadding(o.fontSize)+8 > 1024 {
		t.Fatal("metadata exceeds texture height budget")
	}
	if got := o.boundedInfoLines(nil); len(got) != 0 {
		t.Fatalf("empty metadata = %v", got)
	}
}

func TestGeneratorMetadataHidden(t *testing.T) {
	for _, key := range []string{"prompt", "workflow", " PROMPT "} {
		if !shouldHideExtraTag(key, "{}") {
			t.Fatalf("generator tag %q should be hidden", key)
		}
	}
	if shouldHideExtraTag("lyrics", "Song lyrics") {
		t.Fatal("lyrics should remain visible")
	}
}

func TestInfoScrollIncludesLeadingCover(t *testing.T) {
	o := &Overlay{coverArtTex: 1, coverArtTexH: 300, fontSize: 16}
	rows, visible := o.infoScrollMetrics(10, 16, 384)
	if rows*16 < 300+o.scalePx(8)+10*16 || visible != 24 || rows <= visible {
		t.Fatalf("cover and text scroll metrics = %d, %d", rows, visible)
	}
}
