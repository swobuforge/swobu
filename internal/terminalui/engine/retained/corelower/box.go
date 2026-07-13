package corelower

import (
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
)

func lowerBox[E any](n core.Node[E], env EnvConfig, caster EventCaster[E]) (layout.RenderNode, error) {
	children, err := lowerChildren(n.ChildrenValue(), n.LayoutValue().Flow.Axis, env, caster)
	if err != nil {
		return nil, err
	}
	if children == nil {
		children = layout.NewText("")
	}
	box := layout.NewBox(children)
	inset := n.LayoutValue().Inset
	if inset != (core.Insets{}) {
		box.Padding = geom.Insets{Top: inset.Top, Right: inset.Right, Bottom: inset.Bottom, Left: inset.Left}
	}
	box.Sizing = coreSizeToSizing(n.LayoutValue().Size)
	return box, nil
}

// coreSizeToSizing maps core.Size to retained layout.Sizing.
func coreSizeToSizing(sz core.Size) layout.Sizing {
	var s layout.Sizing
	s.W = coreDimSizeToSizeMode(sz.Width)
	s.H = coreDimSizeToSizeMode(sz.Height)
	if sz.Width.Mode == core.DimFixed {
		s.Fixed.W = sz.Width.Value
	}
	if sz.Height.Mode == core.DimFixed {
		s.Fixed.H = sz.Height.Value
	}
	if sz.Width.Mode == core.DimMinMax {
		s.Min.W = sz.Width.Min
		s.Max.W = sz.Width.Max
	}
	if sz.Height.Mode == core.DimMinMax {
		s.Min.H = sz.Height.Min
		s.Max.H = sz.Height.Max
	}
	return s
}

func coreDimSizeToSizeMode(ds core.DimSize) layout.SizeMode {
	switch ds.Mode {
	case core.DimFit:
		return layout.SizeFit
	case core.DimFixed:
		return layout.SizeFixed
	case core.DimFill:
		return layout.SizeGrow
	case core.DimMinMax:
		return layout.SizeFit
	default:
		return layout.SizeFit
	}
}

func lowerChildren[E any](nodes []core.Node[E], axis core.Axis, env EnvConfig, caster EventCaster[E]) (layout.RenderNode, error) {
	switch len(nodes) {
	case 0:
		return nil, nil
	case 1:
		return lowerNode(nodes[0], env, caster)
	}
	items := make([]layout.FlowChild, 0, len(nodes))
	for _, child := range nodes {
		renderNode, err := lowerNode(child, env, caster)
		if err != nil {
			return nil, err
		}
		items = append(items, layout.FlowChild{RenderNode: renderNode, Grow: growFactor(child, axis)})
	}
	switch axis {
	case core.AxisHorizontal:
		return layout.NewRow(items...), nil
	default:
		return layout.NewColumn(items...), nil
	}
}

func growFactor[E any](n core.Node[E], axis core.Axis) int {
	lay := n.LayoutValue()
	if axis == core.AxisHorizontal {
		if lay.Size.Width.Mode == core.DimFill {
			return max(1, lay.Size.Width.Weight)
		}
		return 0
	}
	if lay.Size.Height.Mode == core.DimFill {
		return max(1, lay.Size.Height.Weight)
	}
	return 0
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
