package loop

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

// focusIDNode is a minimal RenderNode with a focus ID used to test semantic
// focus repair.
type focusIDNode struct {
	layout.Sized
	id string
}

func (f focusIDNode) Measure(c geom.Constraints, ctx *layout.LayoutContext) geom.Size {
	return f.ResolveSize(geom.Size{W: 1, H: 1}, c)
}
func (f focusIDNode) Arrange(node *layout.LayoutNode, ctx *layout.LayoutContext) layout.NodeLayout {
	return layout.NodeLayout{BorderRect: node.Slot, ContentRect: node.Slot, ViewportRect: node.Slot}
}
func (f focusIDNode) Paint(p paint.Painter, node *layout.LayoutNode, ctx *layout.PaintContext) {}

func (f focusIDNode) CanFocus(*layout.LayoutNode) bool { return true }

// containerNode is a non-focusable RenderNode used as a layout shell.
type containerNode struct{}

func (containerNode) Measure(geom.Constraints, *layout.LayoutContext) geom.Size { return geom.Size{} }
func (containerNode) Arrange(*layout.LayoutNode, *layout.LayoutContext) layout.NodeLayout {
	return layout.NodeLayout{}
}
func (containerNode) Paint(paint.Painter, *layout.LayoutNode, *layout.PaintContext) {}

func TestRepairFocus_UsesSemanticID(t *testing.T) {
	noopReduce := func(m *struct{}, a update.Action) []update.Effect { return nil }
	loop := New[struct{}](struct{}{}, noopReduce)

	// Build an initial tree with a focusable node carrying semantic ID "target"
	root1 := &layout.LayoutNode{
		ID:         layout.NodeID(1),
		FocusID:    "target",
		RenderNode: focusIDNode{id: "target"},
	}
	loop.Tree = root1
	loop.Focused = root1
	loop.focusedID = "target"

	// Simulate rebuild: new tree with same semantic ID but different NodeID
	root2 := &layout.LayoutNode{
		ID:         layout.NodeID(99), // different NodeID
		FocusID:    "target",          // same semantic ID
		RenderNode: focusIDNode{id: "target"},
	}
	loop.Tree = root2
	nodes := map[layout.NodeID]*layout.LayoutNode{root2.ID: root2}
	loop.repairFocus(nodes, -1)

	if loop.Focused != root2 {
		t.Fatalf("repair did not restore focus by semantic ID")
	}
	if loop.focusedID != "target" {
		t.Fatalf("focusedID not preserved")
	}
}

func TestRepairFocus_FallsBackToPositionalWhenNoSemanticID(t *testing.T) {
	noopReduce := func(m *struct{}, a update.Action) []update.Effect { return nil }
	loop := New[struct{}](struct{}{}, noopReduce)

	// Three focusable nodes, no semantic IDs, focus on third
	child1 := &layout.LayoutNode{
		ID:         layout.NodeID(1),
		RenderNode: focusIDNode{id: "first"},
	}
	child2 := &layout.LayoutNode{
		ID:         layout.NodeID(2),
		RenderNode: focusIDNode{id: "second"},
	}
	child3 := &layout.LayoutNode{
		ID:         layout.NodeID(3),
		RenderNode: focusIDNode{id: "third"},
	}
	root := &layout.LayoutNode{
		ID:         layout.NodeID(0),
		Kids:       []*layout.LayoutNode{child1, child2, child3},
		RenderNode: containerNode{},
	}
	child1.Parent = root
	child2.Parent = root
	child3.Parent = root
	loop.Tree = root
	loop.Focused = child2
	loop.focusedID = "" // no semantic ID

	// Rebuild: new tree with different NodeIDs and nodes, previously focused
	// node removed (no child2 equivalent)
	newChild1 := &layout.LayoutNode{
		ID:         layout.NodeID(10),
		RenderNode: focusIDNode{id: "first"},
	}
	newChild3 := &layout.LayoutNode{
		ID:         layout.NodeID(30),
		RenderNode: focusIDNode{id: "third"},
	}
	newRoot := &layout.LayoutNode{
		ID:         layout.NodeID(100), // different ID from old root (0)
		Kids:       []*layout.LayoutNode{newChild1, newChild3},
		RenderNode: containerNode{},
	}
	newChild1.Parent = newRoot
	newChild3.Parent = newRoot
	loop.Tree = newRoot

	// Previous focused index was 1 (second child of three)
	// New tree has 2 children: indices 0 and 1
	// Fallback should clamp to index 1 (last child)
	nodes := map[layout.NodeID]*layout.LayoutNode{
		newRoot.ID:   newRoot,
		newChild1.ID: newChild1,
		newChild3.ID: newChild3,
	}
	loop.repairFocus(nodes, 1)

	if loop.Focused != newChild3 {
		t.Fatalf("repair did not fall back to positional index: got %v", loop.Focused)
	}
}

func TestRepairFocus_ClearsFocusWhenNodeRemoved(t *testing.T) {
	noopReduce := func(m *struct{}, a update.Action) []update.Effect { return nil }
	loop := New[struct{}](struct{}{}, noopReduce)

	child := &layout.LayoutNode{
		ID:         layout.NodeID(1),
		FocusID:    "removed",
		RenderNode: focusIDNode{id: "child"},
	}
	root := &layout.LayoutNode{
		ID:         layout.NodeID(0),
		Kids:       []*layout.LayoutNode{child},
		RenderNode: containerNode{},
	}
	child.Parent = root
	loop.Tree = root
	loop.Focused = child
	loop.focusedID = "removed"

	// Rebuild: empty tree
	emptyRoot := &layout.LayoutNode{
		ID:         layout.NodeID(0),
		RenderNode: containerNode{},
	}
	loop.Tree = emptyRoot
	nodes := map[layout.NodeID]*layout.LayoutNode{emptyRoot.ID: emptyRoot}
	loop.repairFocus(nodes, -1)

	if loop.Focused != nil {
		t.Fatalf("focus should be cleared when node removed and no fallback")
	}
	if loop.focusedID != "" {
		t.Fatalf("focusedID should be cleared")
	}
}
