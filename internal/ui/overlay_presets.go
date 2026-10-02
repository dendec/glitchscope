package ui

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/dendec/glitchscope/internal/i18n"
	"github.com/dendec/glitchscope/internal/presets"
)

// presetNode is a single node in the hierarchical preset tree.
// Directories have children; leaves (milk files) have a key.
type presetNode struct {
	name     string       // display name (category dir or .milk basename without ext)
	key      string       // full preset key (empty for directories)
	children []presetNode // subcategories + presets (sorted; immutable after build)
	packID   string       // downloadable collection root
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
	if len(n.stack) == 0 {
		return nil
	}
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
	dirs = append(dirs, files...)
	sortPresetNodes(dirs)
	return dirs
}

func sortPresetNodes(nodes []presetNode) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].isLeaf != nodes[j].isLeaf {
			return !nodes[i].isLeaf
		}
		return nodes[i].name < nodes[j].name
	})
}

// presetMetaProvider returns metadata for a preset key.
// Used by the UI to display preset info without owning the cache.
type presetMetaProvider func(key string) presets.PresetMeta

const presetPreviewDelay = 120 * time.Millisecond

// settlePresetSelection updates the right panel and requests a preview for the
// selected .milk after cursor navigation has stopped.
func (o *Overlay) settlePresetSelection() {
	o.presetPreviewDue = time.Time{}
	node := o.presetNav.Selected()
	key := ""
	if node != nil && node.isLeaf {
		key = node.key
	}
	if o.presetDetailKey != key {
		o.presetDetailKey = key
		o.presetsDetailDirty = true
	}
	if key == "" || o.presetPreviewReq == nil {
		return
	}
	o.presetPreviewReq(key)
}

func (o *Overlay) schedulePreviewForSelected(now time.Time) {
	node := o.presetNav.Selected()
	if node == nil || !node.isLeaf || o.presetPreviewReq == nil {
		o.presetPreviewDue = time.Time{}
		return
	}
	o.presetPreviewDue = now.Add(presetPreviewDelay)
}

func (o *Overlay) requestScheduledPreview(now time.Time) {
	if o.presetPreviewDue.IsZero() || now.Before(o.presetPreviewDue) {
		return
	}
	o.settlePresetSelection()
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
func (o *Overlay) SetPresetPreviewTex(fn func(key string) (tex uint32, w, h int, ok bool)) {
	o.presetPreviewTex = fn
}

// SetPresetPreviewFPS sets the callback to retrieve the preview render FPS.
func (o *Overlay) SetPresetPreviewFPS(fn func() float64) {
	o.presetPreviewFPS = fn
}

// PresetPreviewFPS returns the measured preview render FPS, or 0 if unavailable.
func (o *Overlay) PresetPreviewFPS() float64 {
	if o.presetPreviewFPS != nil {
		return o.presetPreviewFPS()
	}
	return 0
}

// SetPresetTree updates the preset tree while retaining the selected collection.
// Only marks dirty if the tree actually changed.
func (o *Overlay) SetPresetTree(keys []string) {
	selectedPackID := o.selectedPresetPackID()
	o.presetTreeKeys = slices.Clone(keys)
	tree := o.buildPresetTree()
	if presetTreeEqual(o.presetTreeRoot, tree) {
		return
	}
	o.replacePresetTree(tree, selectedPackID)
}

func presetTreeEqual(a, b []presetNode) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].name != b[i].name || a[i].key != b[i].key || a[i].packID != b[i].packID || a[i].isLeaf != b[i].isLeaf {
			return false
		}
		if !presetTreeEqual(a[i].children, b[i].children) {
			return false
		}
	}
	return true
}

func (o *Overlay) buildPresetTree() []presetNode {
	tree := buildPresetTree(o.presetTreeKeys)
	for _, item := range o.presetPacks {
		rootIndex := -1
		for i := range tree {
			if !tree[i].isLeaf && tree[i].name == item.Name {
				rootIndex = i
				break
			}
		}
		if rootIndex < 0 {
			tree = append(tree, presetNode{name: item.Name, children: []presetNode{}})
			rootIndex = len(tree) - 1
		}
		root := &tree[rootIndex]
		root.packID = item.ID
	}
	sortPresetNodes(tree)
	return tree
}

func (o *Overlay) replacePresetTree(tree []presetNode, selectedPackID string) {
	o.presetTreeRoot = tree
	o.presetNav = presetNavigation{stack: []presetNavLevel{{nodes: tree}}}
	if selectedPackID == "" || !o.selectPresetPackRoot(selectedPackID) {
		o.syncPresetTree()
	}
	o.presetsDirty = true
	o.presetsDetailDirty = true
	o.breadcrumbDirty = true
	o.settlePresetSelection()
}

func (o *Overlay) selectedPresetPackID() string {
	node := o.presetNav.Selected()
	if node == nil {
		return ""
	}
	return node.packID
}

func (o *Overlay) selectPresetPackRoot(id string) bool {
	for i := range o.presetTreeRoot {
		if o.presetTreeRoot[i].packID == id {
			o.presetNav.stack[0].cursor = i
			return true
		}
	}
	return false
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
	o.settlePresetSelection()
}

// nodeDisplayLine formats a preset node for display in the left panel.
// Leaf nodes show a playing indicator ("\u25b8 "); directories show a trailing "/".
func (o *Overlay) nodeDisplayLine(node *presetNode, playingKey string) string {
	if node.packID != "" {
		line := node.name + "/"
		if status := o.presetPackStatusByID(node.packID); status != "" {
			line += " [" + status + "]"
		}
		return line
	}
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
	if node := o.presetNav.Selected(); node != nil {
		if item := o.presetPackByID(node.packID); item != nil {
			return o.presetPackActionRows(*item)
		}
	}
	if o.presetDetailKey == "" || o.presetMeta == nil {
		return nil
	}

	m := o.presetMeta(o.presetDetailKey)

	rows := []listRow{
		{text: o.catalog.Format(i18n.PresetShapesWaves, m.Shapes, m.Waves)},
		{text: o.catalog.Format(i18n.PresetEquations, m.PerFrameEqs, m.PerPixelEqs)},
		{text: o.catalog.Format(i18n.PresetTextures, m.Textures)},
	}

	// Show preview FPS when available (presets page, preview active).
	if o.presetPreviewFPSNow > 0 {
		rows = append(rows, listRow{}, listRow{text: o.catalog.Format(i18n.PresetPreviewFPS, o.presetPreviewFPSNow)})
	}

	return rows
}
