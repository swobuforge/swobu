package corelower

import (
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
)

func lowerStack[E any](n core.Node[E], env EnvConfig, caster EventCaster[E]) (layout.RenderNode, error) {
	axis := n.LayoutValue().Flow.Axis
	if n.LayoutValue().Flow.Mode != core.FlowStack {
		axis = core.AxisVertical
	}
	children, err := lowerChildren(n.ChildrenValue(), axis, env, caster)
	if err != nil {
		return nil, err
	}
	if children == nil {
		return layout.NewText(""), nil
	}
	sizing := coreSizeToSizing(n.LayoutValue().Size)
	switch typed := children.(type) {
	case *layout.RowRenderNode:
		typed.Sizing = sizing
	case *layout.ColumnRenderNode:
		typed.Sizing = sizing
	}
	return children, nil
}
