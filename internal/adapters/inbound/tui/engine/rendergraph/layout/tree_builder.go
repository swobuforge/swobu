package layout

import "github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/geom"

// TreeBuilder materializes retained LayoutNodes from a structural node tree.
type TreeBuilder struct{}

type identityProvider interface {
	nodeID() NodeID
}

type unwrapIdentity interface {
	unwrap() RenderNode
}

func (b *TreeBuilder) Build(root RenderNode, bounds geom.Rect) *LayoutNode {
	ctx := &LayoutContext{}
	rootNode := &LayoutNode{RenderNode: root, Slot: bounds}
	if identified, ok := root.(identityProvider); ok {
		rootNode.ID = identified.nodeID()
	}
	if wrapped, ok := root.(unwrapIdentity); ok {
		rootNode.RenderNode = wrapped.unwrap()
	}
	nextID := NodeID(1)
	b.assignFallbackID(rootNode, &nextID)
	b.layoutNode(rootNode, bounds, bounds, ctx, &nextID)
	return rootNode
}

func (b *TreeBuilder) assignFallbackID(node *LayoutNode, nextID *NodeID) {
	if node.ID != 0 {
		if *nextID <= node.ID {
			*nextID = node.ID + 1
		}
		return
	}
	node.ID = *nextID
	*nextID = *nextID + 1
}

func (b *TreeBuilder) layoutNode(node *LayoutNode, slot geom.Rect, inheritedClip geom.Rect, ctx *LayoutContext, nextID *NodeID) {
	node.Slot = slot
	node.MeasuredSize = node.RenderNode.Measure(geom.Exact(slot.W, slot.H), ctx)
	arr := node.RenderNode.Arrange(node, ctx)
	node.BorderRect = arr.BorderRect
	node.ContentRect = arr.ContentRect
	node.ViewportRect = arr.ViewportRect
	node.ContentSize = arr.ContentSize
	node.ScrollOffset = arr.ScrollOffset
	node.ClipRect = inheritedClip.Intersect(arr.ViewportRect)
	node.Kids = node.Kids[:0]
	for _, child := range arr.ChildSlots {
		childNode := &LayoutNode{
			RenderNode: child.Spec.RenderNode,
			Parent:     node,
			Z:          child.Spec.Z,
		}
		if identified, ok := child.Spec.RenderNode.(identityProvider); ok {
			childNode.ID = identified.nodeID()
		}
		if wrapped, ok := child.Spec.RenderNode.(unwrapIdentity); ok {
			childNode.RenderNode = wrapped.unwrap()
		}
		b.assignFallbackID(childNode, nextID)
		b.layoutNode(childNode, child.Rect, node.ClipRect, ctx, nextID)
		node.Kids = append(node.Kids, childNode)
	}
}
