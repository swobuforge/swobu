package layout

import (
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/paint"
)

// ResponsiveSwitchRenderNode selects one branch at layout time based on slot
// dimensions. This is structural branching, not interaction behavior.
type ResponsiveSwitchRenderNode struct {
	Sized
	MinW     int
	MinH     int
	Primary  RenderNode
	Fallback RenderNode
}

func NewResponsiveSwitch(minW, minH int, primary RenderNode, fallback RenderNode) *ResponsiveSwitchRenderNode {
	return &ResponsiveSwitchRenderNode{
		Sized:    Sized{Sizing: Sizing{W: SizeGrow, H: SizeGrow}},
		MinW:     minW,
		MinH:     minH,
		Primary:  primary,
		Fallback: fallback,
	}
}

func (s *ResponsiveSwitchRenderNode) Measure(c geom.Constraints, ctx *LayoutContext) geom.Size {
	child := s.pickForConstraints(c)
	if child == nil {
		return s.ResolveSize(geom.Size{}, c)
	}
	return s.ResolveSize(child.Measure(c, ctx), c)
}

func (s *ResponsiveSwitchRenderNode) Arrange(node *LayoutNode, _ *LayoutContext) NodeLayout {
	out := NodeLayout{BorderRect: node.Slot, ContentRect: node.Slot, ViewportRect: node.Slot, ContentSize: node.MeasuredSize}
	child := s.pickForSlot(node.Slot)
	hint := "fallback"
	if child == s.Primary {
		hint = "primary"
	}
	if child != nil {
		out.ChildSlots = []ChildSlot{{Spec: ChildSpec{Hint: hint, RenderNode: child}, Rect: node.Slot}}
	}
	return out
}

func (s *ResponsiveSwitchRenderNode) Paint(paint.Painter, *LayoutNode, *PaintContext) {}

func (s *ResponsiveSwitchRenderNode) VisitChildren(visit func(hint string, child RenderNode)) {
	if s.Primary != nil {
		visit("primary", s.Primary)
	}
	if s.Fallback != nil {
		visit("fallback", s.Fallback)
	}
}

func (s *ResponsiveSwitchRenderNode) MapChildren(rewrite func(hint string, child RenderNode) RenderNode) RenderNode {
	clone := *s
	if clone.Primary != nil {
		clone.Primary = rewrite("primary", clone.Primary)
	}
	if clone.Fallback != nil {
		clone.Fallback = rewrite("fallback", clone.Fallback)
	}
	return &clone
}

func (s *ResponsiveSwitchRenderNode) pickForConstraints(c geom.Constraints) RenderNode {
	width := c.W.Max
	if width < 0 {
		width = c.W.Min
	}
	height := c.H.Max
	if height < 0 {
		height = c.H.Min
	}
	if width >= s.MinW && height >= s.MinH {
		return s.Primary
	}
	return s.Fallback
}

func (s *ResponsiveSwitchRenderNode) pickForSlot(slot geom.Rect) RenderNode {
	if slot.W >= s.MinW && slot.H >= s.MinH {
		return s.Primary
	}
	return s.Fallback
}
