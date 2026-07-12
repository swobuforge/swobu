package corelower

import (
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
)

func lowerBox(n core.Node, env EnvConfig) (layout.RenderNode, error) {
	children, err := lowerChildren(n.ChildrenValue(), n.LayoutValue().Flow.Axis, env)
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
	return box, nil
}

func lowerChildren(nodes []core.Node, axis core.Axis, env EnvConfig) (layout.RenderNode, error) {
	switch len(nodes) {
	case 0:
		return nil, nil
	case 1:
		return lowerNode(nodes[0], env)
	}
	items := make([]layout.FlowChild, 0, len(nodes))
	for _, child := range nodes {
		renderNode, err := lowerNode(child, env)
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

func growFactor(n core.Node, axis core.Axis) int {
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
