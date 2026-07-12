package corelower

import (
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
)

func lowerScroll(n core.Node, env Env) (layout.RenderNode, error) {
	children := n.ChildrenValue()
	if len(children) == 0 {
		return layout.NewText(""), nil
	}
	child, err := lowerNode(children[0], env)
	if err != nil {
		return nil, err
	}
	scroll := layout.NewScrollY(child)
	scroll.Offset = n.ScrollOffsetValue()
	return scroll, nil
}
