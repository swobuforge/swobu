package layout

import (
	"strconv"

	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/geom"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/paint"
)

type OverlayChild struct {
	RenderNode RenderNode
	Placement  Placement
	Z          int
}

// OverlayRenderNode is public sugar for a base child plus out-of-flow extras.
type OverlayRenderNode struct {
	Sized
	BaseChild RenderNode
	Extras    []OverlayChild
}

func NewOverlay(base RenderNode, extras ...OverlayChild) *OverlayRenderNode {
	return &OverlayRenderNode{
		Sized:     Sized{Sizing: Sizing{W: SizeGrow, H: SizeGrow}},
		BaseChild: base,
		Extras:    extras,
	}
}

func (o *OverlayRenderNode) Measure(c geom.Constraints, ctx *LayoutContext) geom.Size {
	base := o.BaseChild.Measure(c, ctx)
	return o.ResolveSize(base, c)
}

func (o *OverlayRenderNode) Arrange(node *LayoutNode, ctx *LayoutContext) NodeLayout {
	slot := node.Slot
	childSlots := []ChildSlot{{
		Spec: ChildSpec{Hint: "overlay/base", RenderNode: o.BaseChild, Participation: InFlow, Z: 0},
		Rect: slot,
	}}
	for i, extra := range o.Extras {
		ref := slot
		size := extra.RenderNode.Measure(geom.AtMost(ref.W, ref.H), ctx)
		rect := PlaceWithin(ref, size, extra.Placement.Anchor, extra.Placement.Offset)
		childSlots = append(childSlots, ChildSlot{
			Spec: ChildSpec{
				Hint: "overlay/extra/" + strconv.Itoa(i), RenderNode: extra.RenderNode, Participation: OutOfFlow,
				Placement: extra.Placement, Z: extra.Z,
			},
			Rect: rect,
		})
	}
	return NodeLayout{
		BorderRect:   slot,
		ContentRect:  slot,
		ViewportRect: slot,
		ContentSize:  geom.Size{W: slot.W, H: slot.H},
		ChildSlots:   childSlots,
	}
}

func (o *OverlayRenderNode) VisitChildren(visit func(hint string, child RenderNode)) {
	visit("overlay/base", o.BaseChild)
	for i, child := range o.Extras {
		visit("overlay/extra/"+strconv.Itoa(i), child.RenderNode)
	}
}

func (o *OverlayRenderNode) MapChildren(rewrite func(hint string, child RenderNode) RenderNode) RenderNode {
	cloned := *o
	cloned.BaseChild = rewrite("overlay/base", o.BaseChild)
	cloned.Extras = make([]OverlayChild, len(o.Extras))
	for i, child := range o.Extras {
		cloned.Extras[i] = child
		cloned.Extras[i].RenderNode = rewrite("overlay/extra/"+strconv.Itoa(i), child.RenderNode)
	}
	return &cloned
}

func (o *OverlayRenderNode) Paint(paint.Painter, *LayoutNode, *PaintContext) {}
