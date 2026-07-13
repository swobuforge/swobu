package corelower

import (
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
)

func lowerText[E any](n core.Node[E], env EnvConfig) (layout.RenderNode, error) {
	node := layout.NewText(n.ContentValue().Text)
	if env.Resolver != nil {
		node.Style = env.Resolver.Resolve(n.StyleValue())
	}
	return node, nil
}
