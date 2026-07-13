package corelower

import (
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
)

func lowerText[E any](n core.Node[E], _ EnvConfig) (layout.RenderNode, error) {
	return layout.NewText(n.ContentValue().Text), nil
}
