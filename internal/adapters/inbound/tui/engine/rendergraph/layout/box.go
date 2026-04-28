package layout

import (
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/geom"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/paint"
)

type BorderStyle struct {
	TopLeft     rune
	TopRight    rune
	BottomLeft  rune
	BottomRight rune
	H           rune
	V           rune
}

var SingleBorder = BorderStyle{
	TopLeft: '+', TopRight: '+', BottomLeft: '+', BottomRight: '+', H: '-', V: '|',
}

// BoxRenderNode is local rect partitioning: border, padding, content box.
type BoxRenderNode struct {
	Sized
	Padding geom.Insets
	Border  *BorderStyle
	Child   RenderNode
}

func NewBox(child RenderNode) *BoxRenderNode {
	return &BoxRenderNode{
		Sized: Sized{Sizing: Sizing{W: SizeGrow, H: SizeGrow}},
		Child: child,
	}
}

func (b *BoxRenderNode) borderInsets() geom.Insets {
	if b.Border == nil {
		return geom.Insets{}
	}
	return geom.Insets{Top: 1, Right: 1, Bottom: 1, Left: 1}
}

func (b *BoxRenderNode) Measure(c geom.Constraints, ctx *LayoutContext) geom.Size {
	in := b.borderInsets()
	inner := shrinkConstraints(c, geom.Insets{
		Top: in.Top + b.Padding.Top, Right: in.Right + b.Padding.Right,
		Bottom: in.Bottom + b.Padding.Bottom, Left: in.Left + b.Padding.Left,
	})
	child := b.Child.Measure(inner, ctx)
	intrinsic := geom.Size{
		W: child.W + in.Horizontal() + b.Padding.Horizontal(),
		H: child.H + in.Vertical() + b.Padding.Vertical(),
	}
	return b.ResolveSize(intrinsic, c)
}

func (b *BoxRenderNode) Arrange(node *LayoutNode, _ *LayoutContext) NodeLayout {
	border := node.Slot
	content := b.borderInsets().Apply(border)
	content = b.Padding.Apply(content)
	return NodeLayout{
		BorderRect:   border,
		ContentRect:  content,
		ViewportRect: content,
		ContentSize:  geom.Size{W: content.W, H: content.H},
		ChildSlots: []ChildSlot{{
			Spec: ChildSpec{Hint: "box", RenderNode: b.Child, Participation: InFlow},
			Rect: content,
		}},
	}
}

func (b *BoxRenderNode) VisitChildren(visit func(hint string, child RenderNode)) {
	visit("box", b.Child)
}

func (b *BoxRenderNode) MapChildren(rewrite func(hint string, child RenderNode) RenderNode) RenderNode {
	cloned := *b
	cloned.Child = rewrite("box", b.Child)
	return &cloned
}

func (b *BoxRenderNode) Paint(p paint.Painter, node *LayoutNode, _ *PaintContext) {
	if b.Border == nil {
		return
	}
	drawBorder(p, node.BorderRect, *b.Border)
}

func drawBorder(p paint.Painter, r geom.Rect, bs BorderStyle) {
	if r.W < 2 || r.H < 2 {
		return
	}
	p.Put(r.X, r.Y, bs.TopLeft)
	p.Put(r.Right()-1, r.Y, bs.TopRight)
	p.Put(r.X, r.Bottom()-1, bs.BottomLeft)
	p.Put(r.Right()-1, r.Bottom()-1, bs.BottomRight)
	p.LineH(r.X+1, r.Y, r.W-2, bs.H)
	p.LineH(r.X+1, r.Bottom()-1, r.W-2, bs.H)
	p.LineV(r.X, r.Y+1, r.H-2, bs.V)
	p.LineV(r.Right()-1, r.Y+1, r.H-2, bs.V)
}

func shrinkConstraints(c geom.Constraints, in geom.Insets) geom.Constraints {
	return geom.Constraints{
		W: geom.AxisConstraint{
			Min: max(0, c.W.Min-in.Horizontal()),
			Max: shrinkMax(c.W.Max, in.Horizontal()),
		},
		H: geom.AxisConstraint{
			Min: max(0, c.H.Min-in.Vertical()),
			Max: shrinkMax(c.H.Max, in.Vertical()),
		},
	}
}

func shrinkMax(v, by int) int {
	if v < 0 {
		return -1
	}
	return max(0, v-by)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
