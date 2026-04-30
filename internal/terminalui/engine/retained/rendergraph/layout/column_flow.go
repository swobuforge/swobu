package layout

import (
	"strconv"

	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/paint"
)

type FlowChild struct {
	RenderNode RenderNode
	Grow       int
}

// ColumnRenderNode is the vertical allocation policy.
type ColumnRenderNode struct {
	Sized
	Children []FlowChild
	Gap      int
}

func NewColumn(children ...FlowChild) *ColumnRenderNode {
	return &ColumnRenderNode{
		Sized:    Sized{Sizing: Sizing{W: SizeGrow, H: SizeGrow}},
		Children: children,
	}
}

func (c *ColumnRenderNode) Measure(cs geom.Constraints, ctx *LayoutContext) geom.Size {
	maxW, totalH := 0, 0
	for i, child := range c.Children {
		measured := child.RenderNode.Measure(geom.Constraints{
			W: geom.AxisConstraint{Min: 0, Max: cs.W.Max},
			H: geom.AxisConstraint{Min: 0, Max: -1},
		}, ctx)
		if measured.W > maxW {
			maxW = measured.W
		}
		totalH += measured.H
		if i > 0 {
			totalH += c.Gap
		}
	}
	return c.ResolveSize(geom.Size{W: maxW, H: totalH}, cs)
}

func (c *ColumnRenderNode) Arrange(node *LayoutNode, ctx *LayoutContext) NodeLayout {
	slot := node.Slot
	type measured struct {
		FlowChild
		Size geom.Size
	}
	var measuredKids []measured
	fixedH := 0
	totalGrow := 0
	for _, child := range c.Children {
		m := child.RenderNode.Measure(geom.Constraints{
			W: geom.AxisConstraint{Min: 0, Max: slot.W},
			H: geom.AxisConstraint{Min: 0, Max: -1},
		}, ctx)
		measuredKids = append(measuredKids, measured{FlowChild: child, Size: m})
		if child.Grow > 0 {
			totalGrow += child.Grow
		} else {
			fixedH += m.H
		}
	}
	totalGap := 0
	if len(c.Children) > 1 {
		totalGap = (len(c.Children) - 1) * c.Gap
	}
	remaining := max(0, slot.H-fixedH-totalGap)
	y := slot.Y
	childSlots := make([]ChildSlot, 0, len(c.Children))
	for i, child := range measuredKids {
		h := child.Size.H
		if child.Grow > 0 && totalGrow > 0 {
			h = remaining * child.Grow / totalGrow
		}
		r := geom.Rect{X: slot.X, Y: y, W: slot.W, H: h}
		childSlots = append(childSlots, ChildSlot{
			Spec: ChildSpec{Hint: "column/" + strconv.Itoa(i), RenderNode: child.RenderNode, Participation: InFlow},
			Rect: r,
		})
		y += h + c.Gap
	}
	return NodeLayout{
		BorderRect:   slot,
		ContentRect:  slot,
		ViewportRect: slot,
		ContentSize:  geom.Size{W: slot.W, H: max(slot.H, y-slot.Y-c.Gap)},
		ChildSlots:   childSlots,
	}
}

func (c *ColumnRenderNode) VisitChildren(visit func(hint string, child RenderNode)) {
	for i, child := range c.Children {
		visit("column/"+strconv.Itoa(i), child.RenderNode)
	}
}

func (c *ColumnRenderNode) MapChildren(rewrite func(hint string, child RenderNode) RenderNode) RenderNode {
	cloned := *c
	cloned.Children = make([]FlowChild, len(c.Children))
	for i, child := range c.Children {
		cloned.Children[i] = child
		cloned.Children[i].RenderNode = rewrite("column/"+strconv.Itoa(i), child.RenderNode)
	}
	return &cloned
}

func (c *ColumnRenderNode) Paint(paint.Painter, *LayoutNode, *PaintContext) {}

// RowRenderNode is the horizontal allocation policy.
type RowRenderNode struct {
	Sized
	Children []FlowChild
	Gap      int
}

func NewRow(children ...FlowChild) *RowRenderNode {
	return &RowRenderNode{
		Sized:    Sized{Sizing: Sizing{W: SizeGrow, H: SizeGrow}},
		Children: children,
	}
}

func (r *RowRenderNode) Measure(cs geom.Constraints, ctx *LayoutContext) geom.Size {
	maxH, totalW := 0, 0
	for i, child := range r.Children {
		measured := child.RenderNode.Measure(geom.Constraints{
			W: geom.AxisConstraint{Min: 0, Max: -1},
			H: geom.AxisConstraint{Min: 0, Max: cs.H.Max},
		}, ctx)
		if measured.H > maxH {
			maxH = measured.H
		}
		totalW += measured.W
		if i > 0 {
			totalW += r.Gap
		}
	}
	return r.ResolveSize(geom.Size{W: totalW, H: maxH}, cs)
}

func (r *RowRenderNode) Arrange(node *LayoutNode, ctx *LayoutContext) NodeLayout {
	slot := node.Slot
	type measured struct {
		FlowChild
		Size geom.Size
	}
	var measuredKids []measured
	fixedW := 0
	totalGrow := 0
	for _, child := range r.Children {
		m := child.RenderNode.Measure(geom.Constraints{
			W: geom.AxisConstraint{Min: 0, Max: -1},
			H: geom.AxisConstraint{Min: 0, Max: slot.H},
		}, ctx)
		measuredKids = append(measuredKids, measured{FlowChild: child, Size: m})
		if child.Grow > 0 {
			totalGrow += child.Grow
		} else {
			fixedW += m.W
		}
	}
	totalGap := 0
	if len(r.Children) > 1 {
		totalGap = (len(r.Children) - 1) * r.Gap
	}
	remaining := max(0, slot.W-fixedW-totalGap)
	x := slot.X
	childSlots := make([]ChildSlot, 0, len(r.Children))
	for i, child := range measuredKids {
		w := child.Size.W
		if child.Grow > 0 && totalGrow > 0 {
			w = remaining * child.Grow / totalGrow
		}
		cr := geom.Rect{X: x, Y: slot.Y, W: w, H: slot.H}
		childSlots = append(childSlots, ChildSlot{
			Spec: ChildSpec{Hint: "row/" + strconv.Itoa(i), RenderNode: child.RenderNode, Participation: InFlow},
			Rect: cr,
		})
		x += w + r.Gap
	}
	return NodeLayout{
		BorderRect:   slot,
		ContentRect:  slot,
		ViewportRect: slot,
		ContentSize:  geom.Size{W: max(slot.W, x-slot.X-r.Gap), H: slot.H},
		ChildSlots:   childSlots,
	}
}

func (r *RowRenderNode) VisitChildren(visit func(hint string, child RenderNode)) {
	for i, child := range r.Children {
		visit("row/"+strconv.Itoa(i), child.RenderNode)
	}
}

func (r *RowRenderNode) MapChildren(rewrite func(hint string, child RenderNode) RenderNode) RenderNode {
	cloned := *r
	cloned.Children = make([]FlowChild, len(r.Children))
	for i, child := range r.Children {
		cloned.Children[i] = child
		cloned.Children[i].RenderNode = rewrite("row/"+strconv.Itoa(i), child.RenderNode)
	}
	return &cloned
}

func (r *RowRenderNode) Paint(paint.Painter, *LayoutNode, *PaintContext) {}
