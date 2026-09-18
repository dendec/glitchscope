package ui

import (
	"image"
	"image/color"
	"testing"

	"github.com/dendec/glitchscope/internal/player"
	"golang.org/x/image/font/basicfont"
)

func TestNearestIconRasterSize(t *testing.T) {
	tests := []struct {
		size int
		want int
	}{
		{size: 10, want: 16},
		{size: 16, want: 16},
		{size: 20, want: 16},
		{size: 25, want: 24},
		{size: 32, want: 36},
		{size: 48, want: 36},
	}
	for _, test := range tests {
		if got := nearestIconRasterSize(test.size); got != test.want {
			t.Errorf("nearestIconRasterSize(%d) = %d, want %d", test.size, got, test.want)
		}
	}
	if got := nearestIconRasterSize(0); got != 0 {
		t.Fatalf("nearestIconRasterSize(0) = %d, want 0", got)
	}
}

func TestOutlinedIconRGBAAddsContrastingBorderAndPadding(t *testing.T) {
	mask := image.NewAlpha(image.Rect(0, 0, 1, 1))
	mask.SetAlpha(0, 0, color.Alpha{A: 255})

	got := outlinedIconRGBA(mask, color.RGBA{255, 255, 255, 255}, 1)
	if got.Bounds().Dx() != 3 || got.Bounds().Dy() != 3 {
		t.Fatalf("outlined icon size = %v, want 3x3", got.Bounds())
	}
	if center := got.RGBAAt(1, 1); center != (color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("icon center = %v, want white", center)
	}
	if border := got.RGBAAt(0, 1); border != shadowColorFor(color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("icon border = %v, want %v", border, shadowColorFor(color.RGBA{255, 255, 255, 255}))
	}
}

func TestSourceIconName(t *testing.T) {
	tests := []struct {
		source sourceKind
		want   string
	}{
		{source: sourceMusic, want: iconLocalMusic},
		{source: sourceFavorites, want: iconFavorite},
		{source: sourceMicrophone, want: iconMicrophone},
		{source: sourceRadio, want: iconRadio},
		{source: sourceModland, want: iconCatalogs},
		{source: sourceModArchive, want: iconCatalogs},
	}
	for _, test := range tests {
		if got := sourceIconName(test.source); got != test.want {
			t.Errorf("sourceIconName(%d) = %q, want %q", test.source, got, test.want)
		}
	}
	if got := sourceIconName(sourceKind(-1)); got != "" {
		t.Fatalf("sourceIconName(unknown) = %q, want empty", got)
	}
}

func TestPageIconName(t *testing.T) {
	want := []string{iconLibrary, iconPresets, iconSettings, iconHelp}
	for page, name := range want {
		if got := pageIconName(UIPage(page)); got != name {
			t.Errorf("pageIconName(%d) = %q, want %q", page, got, name)
		}
	}
	if got := pageIconName(UIPage(-1)); got != "" {
		t.Fatalf("pageIconName(unknown) = %q, want empty", got)
	}
}

func TestBreadcrumbRootUsesHomeIcon(t *testing.T) {
	o := &Overlay{
		face:     basicfont.Face7x13,
		fontSize: 13,
		screenH:  480,
		uiPage:   PageLibrary,
		navStack: []navLevel{{ctx: ctxSourceRoot}},
	}
	placements := o.breadcrumbPlacements(640)
	if len(placements) != 1 {
		t.Fatalf("root breadcrumb placements = %#v, want one placement", placements)
	}
	if placements[0].icon != iconHome || placements[0].label != "" {
		t.Fatalf("root breadcrumb placement = %#v, want home icon without text", placements[0])
	}
	wantWidth := o.sourceIconTextOffset(o.lineHeight())
	if int(placements[0].w) != wantWidth {
		t.Fatalf("home breadcrumb width = %.0f, want %d", placements[0].w, wantWidth)
	}
}

func TestFavoriteFolderIconName(t *testing.T) {
	tests := map[string]string{
		string(player.PlaylistStar):  iconStar,
		string(player.PlaylistHeart): iconFavorite,
		string(player.PlaylistNote):  iconLibrary,
	}
	for playlistID, want := range tests {
		if got := favoriteFolderIconName(playlistID); got != want {
			t.Errorf("favoriteFolderIconName(%q) = %q, want %q", playlistID, got, want)
		}
	}
}

func TestSourceIconTextOffset(t *testing.T) {
	o := &Overlay{screenH: 480}
	if got, want := o.sourceIconTextOffset(24), 28; got != want {
		t.Fatalf("sourceIconTextOffset(24) = %d, want %d", got, want)
	}
	if got := o.sourceIconTextOffset(0); got != 0 {
		t.Fatalf("sourceIconTextOffset(0) = %d, want 0", got)
	}
}
