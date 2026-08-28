package ui

import (
	"testing"
	"time"

	"github.com/dendec/glitchscope/internal/presets"
)

func TestBuildPresetTreeFlat(t *testing.T) {
	keys := []string{
		"Fractal/a.milk",
		"Fractal/b.milk",
		"Fractal/c.milk",
	}
	tree := buildPresetTree(keys)
	if len(tree) != 1 {
		t.Fatalf("root has %d nodes, want 1", len(tree))
	}
	if tree[0].name != "Fractal" || tree[0].isLeaf {
		t.Errorf("root[0] = %v, want Fractal (dir)", tree[0].name)
	}
	if len(tree[0].children) != 3 {
		t.Errorf("Fractal has %d children, want 3", len(tree[0].children))
	}
	for i, want := range []string{"a", "b", "c"} {
		if tree[0].children[i].name != want {
			t.Errorf("child[%d] = %q, want %q", i, tree[0].children[i].name, want)
		}
		if !tree[0].children[i].isLeaf {
			t.Errorf("child[%d] should be leaf", i)
		}
		if tree[0].children[i].key != "Fractal/"+want+".milk" {
			t.Errorf("child[%d].key = %q", i, tree[0].children[i].key)
		}
	}
}

func TestBuildPresetTreeNested(t *testing.T) {
	keys := []string{
		"Fractal/Loops/a.milk",
		"Fractal/Loops/b.milk",
		"Fractal/Nested/c.milk",
		"Dancer/Infect/d.milk",
	}
	tree := buildPresetTree(keys)
	if len(tree) != 2 {
		t.Fatalf("root has %d nodes, want 2", len(tree))
	}
	// Dancer comes before Fractal (dirs sorted alphabetically).
	if tree[0].name != "Dancer" {
		t.Errorf("root[0] = %q, want Dancer", tree[0].name)
	}
	if tree[1].name != "Fractal" {
		t.Errorf("root[1] = %q, want Fractal", tree[1].name)
	}
	// Fractal has 2 subcategories.
	if len(tree[1].children) != 2 {
		t.Errorf("Fractal has %d children, want 2", len(tree[1].children))
	}
	if tree[1].children[0].name != "Loops" {
		t.Errorf("Fractal child[0] = %q, want Loops", tree[1].children[0].name)
	}
	if tree[1].children[1].name != "Nested" {
		t.Errorf("Fractal child[1] = %q, want Nested", tree[1].children[1].name)
	}
}

func TestBuildPresetTreeRootFiles(t *testing.T) {
	keys := []string{
		"root_file.milk",
		"Fractal/a.milk",
	}
	tree := buildPresetTree(keys)
	// root_file should be a leaf at root level, Fractal is a dir.
	if len(tree) != 2 {
		t.Fatalf("root has %d nodes, want 2", len(tree))
	}
	// Dir comes first.
	if tree[0].name != "Fractal" || tree[0].isLeaf {
		t.Errorf("root[0] = %v, want Fractal (dir)", tree[0].name)
	}
	if tree[1].name != "root_file" || !tree[1].isLeaf {
		t.Errorf("root[1] = %v, want root_file (leaf)", tree[1].name)
	}
	if tree[1].key != "root_file.milk" {
		t.Errorf("root_file.key = %q, want root_file.milk", tree[1].key)
	}
}

func TestBuildPresetTreeEmpty(t *testing.T) {
	tree := buildPresetTree(nil)
	if tree != nil {
		t.Errorf("expected nil tree for empty input, got %v", tree)
	}
}

func TestNavigationExpandCollapse(t *testing.T) {
	keys := []string{
		"Fractal/Loops/a.milk",
		"Fractal/Nested/b.milk",
	}
	tree := buildPresetTree(keys)
	nav := presetNavigation{
		stack: []presetNavLevel{{nodes: tree}},
	}

	// Root level has Fractal.
	if nav.Selected() == nil || nav.Selected().name != "Fractal" {
		t.Fatalf("initial selection = %v, want Fractal", nav.Selected())
	}

	// Expand Fractal.
	nav.Expand(nav.Selected())
	if len(nav.stack) != 2 {
		t.Fatalf("stack depth = %d, want 2", len(nav.stack))
	}
	nav.current().cursor = 1 // Skip the ".." entry.
	if nav.Selected() == nil || nav.Selected().name != "Loops" {
		t.Errorf("after expand, selection = %v, want Loops", nav.Selected())
	}

	// Collapse back.
	if !nav.Collapse() {
		t.Error("Collapse returned false, want true")
	}
	if nav.Selected() == nil || nav.Selected().name != "Fractal" {
		t.Errorf("after collapse, selection = %v, want Fractal", nav.Selected())
	}

	// Cannot collapse past root.
	if nav.Collapse() {
		t.Error("Collapse at root returned true, want false")
	}
}

func TestNavigationCursorMove(t *testing.T) {
	keys := []string{
		"Fractal/a.milk",
		"Fractal/b.milk",
		"Fractal/c.milk",
	}
	tree := buildPresetTree(keys)
	nav := presetNavigation{
		stack: []presetNavLevel{{nodes: tree}},
	}

	// Expand Fractal.
	nav.Expand(nav.Selected())
	cur := nav.current()

	// Move down.
	cur.cursor = 2 // Cursor 0 is the ".." entry.
	if nav.Selected().name != "b" {
		t.Errorf("cursor=2, selection = %v, want b", nav.Selected())
	}

	cur.cursor = 3
	if nav.Selected().name != "c" {
		t.Errorf("cursor=3, selection = %v, want c", nav.Selected())
	}

	// Out of bounds returns nil.
	cur.cursor = 5
	if nav.Selected() != nil {
		t.Errorf("cursor=5, selection = %v, want nil", nav.Selected())
	}
}

func TestPresetPreviewWaitsForNavigationToSettle(t *testing.T) {
	now := time.Unix(100, 0)
	var requested []string
	var metadata []string
	o := &Overlay{
		presetPreviewReq: func(key string) {
			requested = append(requested, key)
		},
		presetMeta: func(key string) presets.PresetMeta {
			metadata = append(metadata, key)
			return presets.PresetMeta{}
		},
	}
	o.SetPresetTree([]string{
		"Fractal/a.milk",
		"Fractal/b.milk",
	})
	o.presetNav.Expand(o.presetNav.Selected())
	o.presetNav.current().cursor = 1

	o.schedulePreviewForSelected(now)
	o.presetNav.current().cursor = 2
	o.schedulePreviewForSelected(now.Add(presetPreviewDelay / 2))
	o.requestScheduledPreview(now.Add(presetPreviewDelay))
	o.buildPresetDetailRows()
	if len(requested) != 0 {
		t.Fatalf("preview requested while navigating: %v", requested)
	}
	if len(metadata) != 0 {
		t.Fatalf("metadata requested while navigating: %v", metadata)
	}

	o.requestScheduledPreview(now.Add(presetPreviewDelay + presetPreviewDelay/2))
	o.buildPresetDetailRows()
	if len(requested) != 1 || requested[0] != "Fractal/b.milk" {
		t.Fatalf("preview requests = %v, want final selection only", requested)
	}
	if len(metadata) != 1 || metadata[0] != "Fractal/b.milk" {
		t.Fatalf("metadata requests = %v, want final selection only", metadata)
	}
}

func TestScrollHoldMovesAtMostOncePerFrame(t *testing.T) {
	now := time.Unix(100, 0)
	o := &Overlay{uiPage: PagePresets}
	hold := scrollHold{
		holdStart:  now.Add(-3 * time.Second),
		lastStep:   now.Add(-time.Second),
		active:     true,
		uiPage:     PagePresets,
		focusPanel: 0,
	}
	steps := 0

	o.updateScrollHold(&hold, true, now, func() { steps++ })

	if steps != 1 {
		t.Fatalf("steps in one frame = %d, want 1", steps)
	}
}

func TestPresetTreeEqual(t *testing.T) {
	a := []presetNode{
		{name: "Fractal", isLeaf: false, children: []presetNode{
			{name: "a", key: "Fractal/a.milk", isLeaf: true},
		}},
	}
	b := []presetNode{
		{name: "Fractal", isLeaf: false, children: []presetNode{
			{name: "a", key: "Fractal/a.milk", isLeaf: true},
		}},
	}
	if !presetTreeEqual(a, b) {
		t.Error("identical trees should be equal")
	}

	c := []presetNode{
		{name: "Fractal", isLeaf: false, children: []presetNode{
			{name: "b", key: "Fractal/b.milk", isLeaf: true},
		}},
	}
	if presetTreeEqual(a, c) {
		t.Error("different trees should not be equal")
	}
}

func TestSetPresetTreeFollowsPlayingPreset(t *testing.T) {
	o := &Overlay{presetName: "Fractal/Loops/playing.milk"}
	o.SetPresetMetaProvider(func(key string) presets.PresetMeta {
		if key != o.presetName {
			t.Fatalf("detail requested for %q, want playing preset %q", key, o.presetName)
		}
		return presets.PresetMeta{Shapes: 2, Waves: 1}
	})

	o.SetPresetTree([]string{
		"Dancer/other.milk",
		"Fractal/Loops/other.milk",
		"Fractal/Loops/playing.milk",
	})

	if got := o.SelectedPresetKey(); got != o.presetName {
		t.Fatalf("selected preset = %q, want playing preset %q", got, o.presetName)
	}
	if got := o.presetNav.Depth(); got != 3 {
		t.Fatalf("navigation depth = %d, want 3", got)
	}
	if rows := o.buildPresetDetailRows(); len(rows) == 0 {
		t.Fatal("playing preset detail is empty")
	}
}

func TestPresetBreadcrumbFollowsPlayingPreset(t *testing.T) {
	o := &Overlay{
		uiPage:     PagePresets,
		presetName: "Transition/Illusion/playing.milk",
	}

	o.SetPresetTree([]string{
		"Other/first.milk",
		"Transition/Illusion/playing.milk",
	})

	if got := o.breadcrumbText(); got != "/Transition/Illusion" {
		t.Fatalf("breadcrumb = %q, want /Transition/Illusion", got)
	}
}

func TestSetPresetNameFollowsChangedPreset(t *testing.T) {
	o := &Overlay{uiPage: PagePresets}
	o.SetPresetTree([]string{
		"Dancer/first.milk",
		"Fractal/Loops/second.milk",
	})

	o.SetPresetName("Dancer/first.milk")
	if got := o.SelectedPresetKey(); got != "Dancer/first.milk" {
		t.Fatalf("selected preset after first change = %q, want Dancer/first.milk", got)
	}

	o.SetPresetName("Fractal/Loops/second.milk")
	if got := o.SelectedPresetKey(); got != "Fractal/Loops/second.milk" {
		t.Fatalf("selected preset after second change = %q, want Fractal/Loops/second.milk", got)
	}
}

func TestPrevScreenToPresetsFollowsPlayingPreset(t *testing.T) {
	o := &Overlay{uiPage: PageSettings, presetName: "Fractal/playing.milk"}
	o.SetPresetTree([]string{
		"Dancer/other.milk",
		"Fractal/playing.milk",
	})
	o.presetNav = presetNavigation{stack: []presetNavLevel{{nodes: o.presetTreeRoot}}}

	o.PrevScreen()

	if o.uiPage != PagePresets {
		t.Fatalf("page = %v, want Presets", o.uiPage)
	}
	if got := o.SelectedPresetKey(); got != o.presetName {
		t.Fatalf("selected preset = %q, want playing preset %q", got, o.presetName)
	}
}

func TestUpdateRefreshesPresetPreviewFPS(t *testing.T) {
	fps := 12.4
	o := &Overlay{
		uiVisible: true,
		uiPage:    PagePresets,
		presetPreviewFPS: func() float64 {
			return fps
		},
	}

	o.Update(false, false)
	if o.presetPreviewFPSNow != 12 {
		t.Fatalf("preview FPS = %d, want 12", o.presetPreviewFPSNow)
	}
	if !o.presetsDetailDirty {
		t.Fatal("Presets detail was not invalidated for initial FPS")
	}

	o.presetsDetailDirty = false
	o.Update(false, false)
	if o.presetsDetailDirty {
		t.Fatal("unchanged preview FPS invalidated Presets detail")
	}

	fps = 13.6
	o.Update(false, false)
	if o.presetPreviewFPSNow != 14 || !o.presetsDetailDirty || o.presetsDirty {
		t.Fatalf("preview FPS = %d, detail dirty = %t, list dirty = %t; want 14, true, false",
			o.presetPreviewFPSNow, o.presetsDetailDirty, o.presetsDirty)
	}
}

func TestFitPresetPreviewUsesPanelWidth(t *testing.T) {
	w, h := fitPresetPreview(160, 90, 576, 446)
	if w != 576 || h != 324 {
		t.Fatalf("preview size = %gx%g, want 576x324", w, h)
	}
}

func TestFitPresetPreviewConstrainsHeight(t *testing.T) {
	w, h := fitPresetPreview(160, 90, 576, 180)
	if w != 320 || h != 180 {
		t.Fatalf("preview size = %gx%g, want 320x180", w, h)
	}
}

func TestPresetPreviewSizeMatchesPanelAt720p(t *testing.T) {
	w, h := PresetPreviewSize(1280, 720)
	if w != 320 || h != 180 {
		t.Fatalf("preview size = %dx%d, want 320x180", w, h)
	}
}

func TestPresetPreviewSizePreservesWindowRatio(t *testing.T) {
	w, h := PresetPreviewSize(1024, 768)
	if w != 256 || h != 192 {
		t.Fatalf("preview size = %dx%d, want 256x192", w, h)
	}
}
