package core

import "fmt"

// FocusNode is one entry in the semantic focus graph.
type FocusNode struct {
	ID       FocusID
	Mode     FocusMode
	Trap     bool
	Index    int     // stable traversal order within this graph
	ParentID FocusID // empty if at root scope
	Children []FocusID
}

// FocusGraph is a compiled, immutable view of all focusable nodes in one
// core.Node tree. It is computed once per rebuild and consumed by the
// retained engine for stable focus identity and traversal.
type FocusGraph struct {
	ByID  map[FocusID]FocusNode
	Order []FocusID // pre-order semantic traversal
	Roots []FocusID // top-level focusable IDs under no scope
}

// Empty reports whether the graph has no focusable nodes.
func (g FocusGraph) Empty() bool {
	return len(g.Order) == 0
}

// CompileFocusGraph walks a core.Node tree and builds the semantic focus graph.
// It preserves explicit FocusSpec.ID when present; auto-generates positional
// IDs when absent so every focusable node has a stable identity within this
// graph snapshot.
func CompileFocusGraph[E any](root Node[E]) FocusGraph {
	g := FocusGraph{
		ByID:  make(map[FocusID]FocusNode),
		Order: make([]FocusID, 0),
		Roots: make([]FocusID, 0),
	}
	var index int
	var walk func(node Node[E], parent FocusID)
	walk = func(node Node[E], parent FocusID) {
		focus := node.InteractionValue().Focus
		if focus.Mode == FocusNone {
			for _, child := range node.ChildrenValue() {
				walk(child, parent)
			}
			return
		}
		id := focus.ID
		if id.Empty() {
			id = FocusID(node.key)
			if id.Empty() {
				id = FocusID(fmt.Sprintf("__focus__%d", index))
			}
		}
		nodeEntry := FocusNode{
			ID:       id,
			Mode:     focus.Mode,
			Trap:     focus.Trap,
			Index:    index,
			ParentID: parent,
			Children: make([]FocusID, 0),
		}
		index++
		g.ByID[id] = nodeEntry
		g.Order = append(g.Order, id)
		if parent.Empty() {
			g.Roots = append(g.Roots, id)
		} else if parentNode, ok := g.ByID[parent]; ok {
			parentNode.Children = append(parentNode.Children, id)
			g.ByID[parent] = parentNode
		}
		nextParent := parent
		if focus.Mode == FocusScope || focus.Mode == FocusGroup {
			nextParent = id
		}
		for _, child := range node.ChildrenValue() {
			walk(child, nextParent)
		}
	}
	walk(root, "")
	return g
}
