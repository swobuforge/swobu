package view

import (
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/layout"
)

type Axis uint8

const (
	AxisColumn Axis = iota
	AxisRow
)

type FlexProps struct {
	Axis Axis
	Gap  int
}

// Flex composes children along one axis with deterministic spacing.
func Flex[M any](_ *Context[M], props FlexProps, kids ...ViewSpec[M]) ViewSpec[M] {
	return flexView[M]{props: props, kids: kids}
}

// VStack composes children vertically.
func VStack[M any](ctx *Context[M], kids ...ViewSpec[M]) ViewSpec[M] {
	return Flex(ctx, FlexProps{Axis: AxisColumn}, kids...)
}

// VStackGap composes children vertically with explicit gap.
func VStackGap[M any](ctx *Context[M], gap int, kids ...ViewSpec[M]) ViewSpec[M] {
	return Flex(ctx, FlexProps{Axis: AxisColumn, Gap: gap}, kids...)
}

// HStack composes children horizontally.
func HStack[M any](ctx *Context[M], kids ...ViewSpec[M]) ViewSpec[M] {
	return Flex(ctx, FlexProps{Axis: AxisRow}, kids...)
}

// HStackGap composes children horizontally with explicit gap.
func HStackGap[M any](ctx *Context[M], gap int, kids ...ViewSpec[M]) ViewSpec[M] {
	return Flex(ctx, FlexProps{Axis: AxisRow, Gap: gap}, kids...)
}

type flexView[M any] struct {
	props FlexProps
	kids  []ViewSpec[M]
}

func (f flexView[M]) BuildRenderNode(ctx *Context[M]) RenderNode {
	nodes := make([]layout.FlowChild, 0, len(f.kids))
	for _, w := range f.kids {
		el := w.BuildRenderNode(ctx)
		if el == nil {
			continue
		}
		nodes = append(nodes, layout.FlowChild{RenderNode: el, Grow: inferGrow(el)})
	}
	switch f.props.Axis {
	case AxisRow:
		row := layout.NewRow(nodes...)
		row.Gap = max(0, f.props.Gap)
		return row
	default:
		col := layout.NewColumn(nodes...)
		col.Gap = max(0, f.props.Gap)
		return col
	}
}

// Box wraps a view in a structural container (for padding, sizing, etc.).
func Box[M any](_ *Context[M], child ViewSpec[M]) ViewSpec[M] {
	return boxView[M]{child: child}
}

type boxView[M any] struct{ child ViewSpec[M] }

func (b boxView[M]) BuildRenderNode(ctx *Context[M]) RenderNode {
	return b.child.BuildRenderNode(ctx)
}

// Padded wraps a view with explicit padding insets.
func Padded[M any](child ViewSpec[M], top, right, bottom, left int) ViewSpec[M] {
	return paddedView[M]{child: child, top: top, right: right, bottom: bottom, left: left}
}

type paddedView[M any] struct {
	child                    ViewSpec[M]
	top, right, bottom, left int
}

func (p paddedView[M]) BuildRenderNode(ctx *Context[M]) RenderNode {
	el := p.child.BuildRenderNode(ctx)
	if el == nil {
		return nil
	}
	box := layout.NewBox(el)
	box.Padding = geom.Insets{Top: p.top, Right: p.right, Bottom: p.bottom, Left: p.left}
	return box
}

// Grow marks a view to fill remaining space.
func Grow[M any](w ViewSpec[M]) ViewSpec[M] {
	return growView[M]{w: w}
}

type growView[M any] struct{ w ViewSpec[M] }

func (g growView[M]) BuildRenderNode(ctx *Context[M]) RenderNode {
	return growRenderNode{RenderNode: g.w.BuildRenderNode(ctx)}
}

type growRenderNode struct{ RenderNode }

func (growRenderNode) GrowFactor() int { return 1 }

func inferGrow(el RenderNode) int {
	if gf, ok := el.(interface{ GrowFactor() int }); ok {
		return gf.GrowFactor()
	}
	return 0
}

type ScrollAxis uint8

const (
	ScrollAxisY ScrollAxis = iota
)

type ScrollProps struct {
	Axis   ScrollAxis
	Offset int
}

// Scroll applies viewport semantics. v0 supports vertical scrolling.
func Scroll[M any](child ViewSpec[M], props ScrollProps) ViewSpec[M] {
	switch props.Axis {
	case ScrollAxisY:
		return ScrollY(child, props.Offset)
	default:
		return ScrollY(child, props.Offset)
	}
}

type StackChild[M any] struct {
	Child     ViewSpec[M]
	Placement layout.Placement
	Z         int
}

// Stack overlays children relative to a base child.
func Stack[M any](_ *Context[M], base ViewSpec[M], extras ...StackChild[M]) ViewSpec[M] {
	return stackView[M]{base: base, extras: extras}
}

type stackView[M any] struct {
	base   ViewSpec[M]
	extras []StackChild[M]
}

func (s stackView[M]) BuildRenderNode(ctx *Context[M]) RenderNode {
	baseEl := s.base.BuildRenderNode(ctx)
	if baseEl == nil {
		return nil
	}
	items := make([]layout.OverlayChild, 0, len(s.extras))
	for _, extra := range s.extras {
		childEl := extra.Child.BuildRenderNode(ctx)
		if childEl == nil {
			continue
		}
		items = append(items, layout.OverlayChild{
			RenderNode: childEl,
			Placement:  extra.Placement,
			Z:          extra.Z,
		})
	}
	return layout.NewOverlay(baseEl, items...)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
