package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
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
	n.stack = append(n.stack, presetNavLevel{nodes: node.children})
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

// buildPresetTree builds a hierarchical tree from sorted preset keys.
// Keys use "/" as separator (e.g. "Fractal/Loops/preset.milk").
// Directories come before files at each level, then lexicographic sort.
//
// treeEntry is an intermediate node used during tree construction.
type treeEntry struct {
	node     *presetNode
	children map[string]*treeEntry
}

// buildPresetTreeLinear builds the tree in a single pass over sorted keys.
// This avoids the map-of-maps approach and builds directly.
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

// SetPresetMetaProvider sets the metadata provider callback.
func (o *Overlay) SetPresetMetaProvider(fn presetMetaProvider) {
	o.presetMeta = fn
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
	cur := o.presetTreeRoot

	// Walk down the tree, expanding each level to find the target.
	for i, part := range parts {
		isLast := i == len(parts)-1
		displayName := part
		if isLast {
			displayName = strings.TrimSuffix(part, filepath.Ext(part))
		}

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

		// Set cursor at this level.
		if len(o.presetNav.stack) > i {
			o.presetNav.stack[i].cursor = found
		}

		if !isLast {
			// Expand to next level.
			o.presetNav.Expand(&cur[found])
			cur = cur[found].children
		}
	}
	o.presetsDirty = true
	o.breadcrumbDirty = true
}

// buildPresetDetailRows returns the text lines for the right panel detail.
// Returns nil when the selected node is a directory or has no metadata.
func (o *Overlay) buildPresetDetailRows(maxTextPx int) []listRow {
	node := o.presetNav.Selected()
	if node == nil || !node.isLeaf {
		return nil
	}
	if o.presetMeta == nil {
		return nil
	}

	m := o.presetMeta(node.key)

	var lines []string

	// Rating as stars.
	lines = append(lines, "Rating: "+formatRating(m.Rating))

	// Core parameters.
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  Decay: %-6s  Warp: %-6s", formatFrac(m.Decay), formatFrac(m.WarpSpeed)))
	lines = append(lines, fmt.Sprintf("  Echo: %-7s  Mode: %d", formatFrac(m.VideoEchoZoom), m.WaveMode))

	// Complexity.
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  Shapes: %-3d  Waves: %d", m.Shapes, m.Waves))
	lines = append(lines, fmt.Sprintf("  Per-frame: %-3d  Per-pixel: %d", m.PerFrameEqs, m.PerPixelEqs))

	var rows []listRow
	for _, line := range lines {
		rows = append(rows, listRow{text: line})
	}
	return rows
}

func formatRating(r float64) string {
	full := int(r)
	if full > 5 {
		full = 5
	}
	half := r-float64(full) >= 0.5
	s := strings.Repeat("\u2605", full)
	if half {
		s += "\u00bd"
	}
	s += strings.Repeat("\u2606", 5-full-boolToInt(half))
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func formatFrac(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if len(s) > 6 {
		s = s[:6]
	}
	return s
}
