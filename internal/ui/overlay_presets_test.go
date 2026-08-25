package ui

import (
	"testing"
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
	cur.cursor = 1
	if nav.Selected().name != "b" {
		t.Errorf("cursor=1, selection = %v, want b", nav.Selected())
	}

	cur.cursor = 2
	if nav.Selected().name != "c" {
		t.Errorf("cursor=2, selection = %v, want c", nav.Selected())
	}

	// Out of bounds returns nil.
	cur.cursor = 5
	if nav.Selected() != nil {
		t.Errorf("cursor=5, selection = %v, want nil", nav.Selected())
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
