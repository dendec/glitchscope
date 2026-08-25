package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dendec/glitchscope/internal/presets"
)

// presetNode is a single node in the hierarchical preset tree.
// Directories have children; leaves (milk files) have a key.
type presetNode struct {
	name     string       // display name (category dir or .milk basename without ext)
	key      string       // full preset key (empty for directories)
	children []presetNode // subcategories + presets (sorted; immutable after build)
	isLeaf   bool         // true = .milk file, false = directory
}

// presetNavLevel holds the visible nodes and cursor at one expansion depth.
type presetNavLevel struct {
	nodes  []presetNode
	label  string
	cursor int
	scroll int
}

// presetNavigation manages a stack of expansion levels.
// Current level is always len(stack)-1.
type presetNavigation struct {
	stack []presetNavLevel
}

func (n *presetNavigation) current() *presetNavLevel {
	return &n.stack[len(n.stack)-1]
}

// Selected returns the currently selected node, or nil if empty.
// Returns nil for the ".." entry (caller handles collapse separately).
func (n *presetNavigation) Selected() *presetNode {
	cur := n.current()
	if cur == nil {
		return nil
	}
	nodes := len(cur.nodes)
	hasParent := len(n.stack) > 1
	idx := cur.cursor
	if hasParent {
		if idx == 0 {
			return nil // ".." entry
		}
		idx--
	}
	if idx < 0 || idx >= nodes {
		return nil
	}
	return &cur.nodes[idx]
}

// Expand pushes a directory's children as a new level.
func (n *presetNavigation) Expand(node *presetNode) {
	if node == nil || len(node.children) == 0 {
		return
	}
	n.stack = append(n.stack, presetNavLevel{nodes: node.children, label: node.name})
}

// Collapse pops back to the parent level. Returns false if already at root.
func (n *presetNavigation) Collapse() bool {
	if len(n.stack) <= 1 {
		return false
	}
	n.stack = n.stack[:len(n.stack)-1]
	return true
}

// Depth returns the current expansion depth (1 = root).
func (n *presetNavigation) Depth() int {
	return len(n.stack)
}

// treeEntry is an intermediate node used during tree construction.
type treeEntry struct {
	node     *presetNode
	children map[string]*treeEntry
}

// buildPresetTree builds a hierarchical tree from sorted preset keys.
// Keys use "/" as separator (e.g. "Fractal/Loops/preset.milk").
// Directories come before files at each level, then lexicographic sort.
func buildPresetTree(keys []string) []presetNode {
	if len(keys) == 0 {
		return nil
	}

	root := &treeEntry{children: make(map[string]*treeEntry)}

	for _, key := range keys {
		parts := strings.Split(key, "/")
		cur := root

		for i, part := range parts {
			isLast := i == len(parts)-1
			displayName := part
			if isLast {
				displayName = strings.TrimSuffix(part, filepath.Ext(part))
			}

			e, ok := cur.children[displayName]
			if !ok {
				e = &treeEntry{
					node: &presetNode{
						name:     displayName,
						isLeaf:   isLast,
						children: []presetNode{},
					},
					children: make(map[string]*treeEntry),
				}
				cur.children[displayName] = e
			}
			if isLast {
				e.node.key = key
				e.node.isLeaf = true
			}
			cur = e
		}
	}

	return sortedTreeNodes(root.children)
}

func sortedTreeNodes(m map[string]*treeEntry) []presetNode {
	var dirs, files []presetNode
	for _, e := range m {
		if e.node.isLeaf {
			files = append(files, *e.node)
		} else {
			e.node.children = sortedTreeNodes(e.children)
			dirs = append(dirs, *e.node)
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].name < dirs[j].name })
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return append(dirs, files...)
}

// presetMetaProvider returns metadata for a preset key.
// Used by the UI to display preset info without owning the cache.
type presetMetaProvider func(key string) presets.PresetMeta

// requestPreviewForSelected enqueues a preview if the selected node is a .milk.
func (o *Overlay) requestPreviewForSelected() {
	node := o.presetNav.Selected()
	if node == nil || !node.isLeaf || o.presetPreviewReq == nil {
		return
	}
	o.presetPreviewReq(node.key)
}

// SetPresetMetaProvider sets the metadata provider callback.
func (o *Overlay) SetPresetMetaProvider(fn presetMetaProvider) {
	o.presetMeta = fn
}

// SetPresetPreviewRequest sets the callback to request a thumbnail preview.
func (o *Overlay) SetPresetPreviewRequest(fn func(key string)) {
	o.presetPreviewReq = fn
}

// SetPresetPreviewTex sets the callback to retrieve a cached preview texture.
func (o *Overlay) SetPresetPreviewTex(fn func(key string) (uint32, bool)) {
	o.presetPreviewTex = fn
}

// SetPresetTree updates the preset tree and resets navigation to root.
// Only marks dirty if the tree actually changed.
func (o *Overlay) SetPresetTree(keys []string) {
	tree := buildPresetTree(keys)
	if presetTreeEqual(o.presetTreeRoot, tree) {
		return
	}
	o.presetTreeRoot = tree
	o.presetNav = presetNavigation{
		stack: []presetNavLevel{{nodes: tree}},
	}
	o.syncPresetTree()
	o.presetsDirty = true
	o.breadcrumbDirty = true
}

func presetTreeEqual(a, b []presetNode) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].name != b[i].name || a[i].key != b[i].key || a[i].isLeaf != b[i].isLeaf {
			return false
		}
		if !presetTreeEqual(a[i].children, b[i].children) {
			return false
		}
	}
	return true
}

// SelectedPresetKey returns the full key of the selected preset, or "".
func (o *Overlay) SelectedPresetKey() string {
	node := o.presetNav.Selected()
	if node == nil || !node.isLeaf {
		return ""
	}
	return node.key
}

// syncPresetTree positions the navigation on the currently playing preset.
func (o *Overlay) syncPresetTree() {
	key := o.presetName
	if key == "" || len(o.presetTreeRoot) == 0 {
		return
	}
	parts := strings.Split(key, "/")

	// Build the navigation stack by walking the tree.
	// Each part expands one level; the last part positions the cursor.
	o.presetNav.stack = []presetNavLevel{{nodes: o.presetTreeRoot}}
	cur := o.presetTreeRoot

	for i, part := range parts {
		displayName := part
		if i == len(parts)-1 {
			displayName = strings.TrimSuffix(part, filepath.Ext(part))
		}

		// Find target in current level.
		found := -1
		for j := range cur {
			if cur[j].name == displayName {
				found = j
				break
			}
		}
		if found < 0 {
			return
		}

		// If not the last part, expand into children.
		if i < len(parts)-1 {
			o.presetNav.stack = append(o.presetNav.stack, presetNavLevel{
				nodes: cur[found].children,
				label: cur[found].name,
			})
			cur = cur[found].children
		} else {
			// Last part: set cursor on the found node.
			// Account for ".." entry at position 0 when not at root.
			cursor := found
			if len(o.presetNav.stack) > 1 {
				cursor++
			}
			o.presetNav.stack[len(o.presetNav.stack)-1].cursor = cursor
		}
	}
	o.presetsDirty = true
	o.breadcrumbDirty = true
	o.requestPreviewForSelected()
}

// nodeDisplayLine formats a preset node for display in the left panel.
// Leaf nodes show a playing indicator ("\u25b8 "); directories show a trailing "/".
func nodeDisplayLine(node *presetNode, playingKey string) string {
	if node.isLeaf {
		prefix := "  "
		if node.key == playingKey {
			prefix = "\u25b8 "
		}
		return prefix + node.name
	}
	return node.name + "/"
}

// buildPresetDetailRows returns the text lines for the right panel detail.
// Returns nil when the selected node is a directory or has no metadata.
func (o *Overlay) buildPresetDetailRows() []listRow {
	node := o.presetNav.Selected()
	if node == nil || !node.isLeaf || o.presetMeta == nil {
		return nil
	}

	m := o.presetMeta(node.key)
	complexity := calcComplexity(m)

	return []listRow{
		{text: formatComplexity(complexity)},
		{},
		{text: fmt.Sprintf("Shapes: %d   Waves: %d", m.Shapes, m.Waves)},
		{text: fmt.Sprintf("Equations: %d per-frame, %d per-pixel", m.PerFrameEqs, m.PerPixelEqs)},
	}
}

// Complexity scoring weights. Normalized so a typical complex preset (~20 per-frame,
// ~10 per-pixel, ~5 shapes/waves) scores ~10.
const (
	complexityPerFrameWeight = 0.4
	complexityPerPixelWeight = 0.6
	complexityVisualWeight   = 0.5
	complexityNormFactor     = 16.5 // (20*0.4 + 10*0.6 + 5*0.5)
)

// calcComplexity returns a 0–10 score based on preset complexity factors.
func calcComplexity(m presets.PresetMeta) int {
	score := float64(m.PerFrameEqs)*complexityPerFrameWeight +
		float64(m.PerPixelEqs)*complexityPerPixelWeight +
		float64(m.Shapes+m.Waves)*complexityVisualWeight
	score = score * 10 / complexityNormFactor
	if score > 10 {
		score = 10
	}
	if score < 0 {
		score = 0
	}
	return int(score + 0.5)
}

// formatComplexity renders complexity as a bar: [##########] to [----------].
func formatComplexity(v int) string {
	if v > 10 {
		v = 10
	}
	if v < 0 {
		v = 0
	}
	return "[" + strings.Repeat("#", v) + strings.Repeat("-", 10-v) + "]"
}
