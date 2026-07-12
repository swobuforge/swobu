package corelower

import (
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
)

func lowerText(n core.Node, _ Env) (layout.RenderNode, error) {
	return layout.NewText(n.ContentValue().Text), nil
}
