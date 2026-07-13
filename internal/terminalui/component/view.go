package component

import "github.com/swobuforge/swobu/internal/terminalui/core"

// View[E] is the author-facing semantic component contract.
type View[E any, M any] interface {
	BuildCoreNode(ctx *Context[E, M]) core.Node[E]
}

// ViewFunc[E, M] adapts a build function into a View.
type ViewFunc[E any, M any] func(ctx *Context[E, M]) core.Node[E]

// BuildCoreNode implements View.
func (f ViewFunc[E, M]) BuildCoreNode(ctx *Context[E, M]) (node core.Node[E]) {
	if f == nil {
		return core.Box[E]()
	}
	if ctx == nil {
		return f(nil)
	}
	defer func() {
		ctx.building = false
	}()
	return f(ctx)
}
