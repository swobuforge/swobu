package layout

import (
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
)

// ScrollYRenderNode is viewport + content extent + vertical offset.
type ScrollYRenderNode struct {
	Sized
	Offset int
	Child  RenderNode
}

func NewScrollY(child RenderNode) *ScrollYRenderNode {
	return &ScrollYRenderNode{
		Sized: Sized{Sizing: Sizing{W: SizeGrow, H: SizeGrow}},
		Child: child,
	}
}

func (s *ScrollYRenderNode) Measure(c geom.Constraints, ctx *LayoutContext) geom.Size {
	content := s.Child.Measure(geom.Constraints{
		W: c.W,
		H: geom.AxisConstraint{Min: 0, Max: -1},
	}, ctx)
	intrinsic := geom.Size{W: content.W, H: min(content.H, constraintMaxOr(content.H, c.H))}
	return s.ResolveSize(intrinsic, c)
}

func (s *ScrollYRenderNode) Arrange(node *LayoutNode, ctx *LayoutContext) NodeLayout {
	viewport := node.Slot
	contentSize := s.Child.Measure(geom.Constraints{
		W: geom.AxisConstraint{Min: viewport.W, Max: viewport.W},
		H: geom.AxisConstraint{Min: 0, Max: -1},
	}, ctx)
	maxOffset := max(0, contentSize.H-viewport.H)
	offset := geom.Clamp(s.Offset, 0, maxOffset)
	contentRect := geom.Rect{
		X: viewport.X, Y: viewport.Y - offset, W: viewport.W, H: contentSize.H,
	}
	return NodeLayout{
		BorderRect:   viewport,
		ContentRect:  contentRect,
		ViewportRect: viewport,
		ContentSize:  contentSize,
		ScrollOffset: geom.Point{Y: offset},
		ChildSlots: []ChildSlot{{
			Spec: ChildSpec{Hint: "scroll", RenderNode: s.Child, Participation: InFlow},
			Rect: contentRect,
		}},
	}
}

func (s *ScrollYRenderNode) VisitChildren(visit func(hint string, child RenderNode)) {
	visit("scroll", s.Child)
}

func (s *ScrollYRenderNode) MapChildren(rewrite func(hint string, child RenderNode) RenderNode) RenderNode {
	cloned := *s
	cloned.Child = rewrite("scroll", s.Child)
	return &cloned
}

func (s *ScrollYRenderNode) Paint(paint.Painter, *LayoutNode, *PaintContext) {}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func constraintMaxOr(fallback int, c geom.AxisConstraint) int {
	if c.Max >= 0 {
		return c.Max
	}
	return fallback
}
