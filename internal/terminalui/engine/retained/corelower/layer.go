package corelower

import (
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
)

func lowerLayer[E any](n core.Node[E], env EnvConfig, caster EventCaster[E]) (layout.RenderNode, error) {
	children := n.ChildrenValue()
	switch len(children) {
	case 0:
		return layout.NewText(""), nil
	case 1:
		return lowerNode(children[0], env, caster)
	}

	base, err := lowerNode(children[0], env, caster)
	if err != nil {
		return nil, err
	}
	extras := make([]layout.OverlayChild, 0, len(children)-1)
	for i, child := range children[1:] {
		renderNode, err := lowerNode(child, env, caster)
		if err != nil {
			return nil, err
		}
		extras = append(extras, layout.OverlayChild{
			RenderNode: renderNode,
			Placement: layout.Placement{
				Ref:    layout.RefSlot,
				Anchor: layout.AnchorTopLeft,
			},
			Z: i + 1,
		})
	}
	return layout.NewOverlay(base, extras...), nil
}
