package views

import (
	"fmt"

	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/geom"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/layout"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/paint"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/view"
)

// ViewportGuardRenderNode swaps the cockpit body for a minimum-size message when the
// available terminal surface is below the supported viewport.
type ViewportGuardRenderNode struct {
	layout.Sized
	MinW  int
	MinH  int
	Child layout.RenderNode
}

func NewViewportGuard(minW, minH int, child layout.RenderNode) *ViewportGuardRenderNode {
	return &ViewportGuardRenderNode{
		Sized: layout.Sized{Sizing: layout.Sizing{W: layout.SizeGrow, H: layout.SizeGrow}},
		MinW:  minW,
		MinH:  minH,
		Child: child,
	}
}

func ViewportGuard[M any](minW, minH int, child view.ViewSpec[M]) view.ViewSpec[M] {
	return view.View[M](func(ctx *view.Context[M]) view.RenderNode {
		return NewViewportGuard(minW, minH, view.Materialize(ctx, child))
	})
}

func (v *ViewportGuardRenderNode) Measure(c geom.Constraints, ctx *layout.LayoutContext) geom.Size {
	if v.Child == nil {
		return v.ResolveSize(geom.Size{}, c)
	}
	return v.ResolveSize(v.Child.Measure(c, ctx), c)
}

func (v *ViewportGuardRenderNode) Arrange(node *layout.LayoutNode, ctx *layout.LayoutContext) layout.NodeLayout {
	out := layout.NodeLayout{
		BorderRect:   node.Slot,
		ContentRect:  node.Slot,
		ViewportRect: node.Slot,
		ContentSize:  node.MeasuredSize,
	}
	if v.Child != nil && node.Slot.W >= v.MinW && node.Slot.H >= v.MinH {
		out.ChildSlots = []layout.ChildSlot{{
			Spec: layout.ChildSpec{RenderNode: v.Child},
			Rect: node.Slot,
		}}
	}
	return out
}

func (v *ViewportGuardRenderNode) Paint(p paint.Painter, node *layout.LayoutNode, _ *layout.PaintContext) {
	if node.BorderRect.W >= v.MinW && node.BorderRect.H >= v.MinH {
		return
	}
	message := fmt.Sprintf("Terminal too small (need >=%dx%d)", v.MinW, v.MinH)
	x := max(0, (node.BorderRect.W-runeLen(message))/2)
	y := max(0, node.BorderRect.H/2)
	p.Text(x, y, trimToWidth(message, node.BorderRect.W))
}

func (v *ViewportGuardRenderNode) VisitChildren(visit func(hint string, child layout.RenderNode)) {
	if v.Child != nil {
		visit("child", v.Child)
	}
}

func (v *ViewportGuardRenderNode) MapChildren(rewrite func(hint string, child layout.RenderNode) layout.RenderNode) layout.RenderNode {
	clone := *v
	if clone.Child != nil {
		clone.Child = rewrite("child", clone.Child)
	}
	return &clone
}
