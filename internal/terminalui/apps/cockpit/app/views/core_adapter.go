package views

import (
	"github.com/swobuforge/swobu/internal/terminalui/component"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// CoreNodeAsRetained lowers one semantic core node into the retained view
// contract for cockpit migration slices.
func CoreNodeAsRetained[M any](node core.Node) retained.ViewSpec[M] {
	return retained.FromCore(component.ViewFunc[M](func(*component.Context[M]) core.Node {
		return node
	}))
}
