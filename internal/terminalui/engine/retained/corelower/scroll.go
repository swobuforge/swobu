package corelower

import (
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
)

func lowerScroll[E any](n core.Node[E], env EnvConfig, caster EventCaster[E]) (layout.RenderNode, error) {
	children := n.ChildrenValue()
	if len(children) == 0 {
		return layout.NewText(""), nil
	}
	child, err := lowerNode(children[0], env, caster)
	if err != nil {
		return nil, err
	}
	scroll := layout.NewScrollY(child)
	scroll.Offset = n.ScrollOffsetValue()
	return scroll, nil
}
