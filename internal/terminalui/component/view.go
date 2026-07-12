package component

import "github.com/swobuforge/swobu/internal/terminalui/core"

// View is the author-facing semantic component contract.
type View[M any] interface {
	BuildCoreNode(ctx *Context[M]) core.Node
}

// ViewFunc adapts a build function into a View.
type ViewFunc[M any] func(ctx *Context[M]) core.Node

// BuildCoreNode implements View.
func (f ViewFunc[M]) BuildCoreNode(ctx *Context[M]) (node core.Node) {
	if f == nil {
		return core.Box()
	}
	if ctx == nil {
		return f(nil)
	}
	defer func() {
		ctx.building = false
	}()
	return f(ctx)
}
