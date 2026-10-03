package ui

import (
	"fmt"
	"testing"

	"github.com/dendec/glitchscope/internal/player"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

func TestLibraryActionLayoutKeepsIconsClearOfGlyphs(t *testing.T) {
	parsed, err := opentype.Parse(unifontData)
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{16, 24, 36} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: float64(size), DPI: 72})
			if err != nil {
				t.Fatal(err)
			}
			defer face.Close()
			measure := func(s string) int { return font.MeasureString(face, s).Ceil() }
			lh := face.Metrics().Height.Ceil()
			for _, label := range []string{"Delete download", "Удалить загрузку", "ダウンロードを削除"} {
				for _, confirm := range []bool{false, true} {
					actions := []libraryAction{{icon: iconPlay, label: "Play", selected: true}, {icon: iconDelete, label: label, confirm: confirm}}
					items := layoutLibraryActions(actions, lh, 4, measure(">"), measure)
					for i, item := range items {
						bounds, _ := font.BoundString(face, item.label)
						if left := item.labelX + bounds.Min.X.Floor(); left <= item.iconX+lh {
							t.Fatalf("%q glyphs begin at %d, overlap icon ending at %d", item.label, left, item.iconX+lh)
						}
						if item.iconX <= item.cursorX+measure(">") {
							t.Fatal("cursor overlaps icon")
						}
						if i > 0 && item.cursorX <= items[i-1].endX {
							t.Fatal("actions overlap")
						}
					}
					actions[0].selected = false
					actions[1].selected = true
					moved := layoutLibraryActions(actions, lh, 4, measure(">"), measure)
					for i := range items {
						if items[i].iconX != moved[i].iconX || items[i].labelX != moved[i].labelX {
							t.Fatal("focus moves action geometry")
						}
					}
				}
			}
		})
	}
}

func TestFavoriteBadgesReserveFullRowCell(t *testing.T) {
	o := &Overlay{screenH: 480, favoritesView: nil, navStack: []navLevel{{ctx: ctxCatalog}}}
	if o.favoriteIconTextInset(24) != 0 {
		t.Fatal("no favorites view should reserve no badge column")
	}
	o.favoritesView = player.NewReadOnlyFavorites()
	for _, lh := range []int{16, 24, 36} {
		if got := o.favoriteIconTextInset(lh); got <= lh {
			t.Fatalf("badge slot %d must fit icon %d plus gap", got, lh)
		}
	}
	o.navStack = []navLevel{{ctx: ctxSourceRoot}}
	if o.favoriteIconTextInset(24) != 0 {
		t.Fatal("source icons must not reserve a trailing badge")
	}
}

func TestActionPanelHeightsKeepButtonsInsideBackground(t *testing.T) {
	o := &Overlay{fontSize: 13}
	for _, panelH := range []int{0, 8, 390} {
		for _, hasActions := range []bool{false, true} {
			metadataH, actionH := o.actionPanelHeights(panelH, 16, hasActions)
			if metadataH < 0 || actionH < 0 || metadataH+actionH != panelH {
				t.Fatalf("panel %d, actions %t: metadata %d + buttons %d", panelH, hasActions, metadataH, actionH)
			}
			if !hasActions && actionH != 0 {
				t.Fatal("panel without actions reserved button space")
			}
			if hasActions && actionH != min(panelH, o.actionBarHeight(16)) {
				t.Fatal("button row does not match shared action height")
			}
		}
	}
}
