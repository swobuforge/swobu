package corelower

import (
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
)

func lowerStack(n core.Node, env EnvConfig) (layout.RenderNode, error) {
	axis := n.LayoutValue().Flow.Axis
	if n.LayoutValue().Flow.Mode != core.FlowStack {
		axis = core.AxisVertical
	}
	children, err := lowerChildren(n.ChildrenValue(), axis, env)
	if err != nil {
		return nil, err
	}
	if children == nil {
		return layout.NewText(""), nil
	}
	return children, nil
}
