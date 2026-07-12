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

// Build adapts a child-producing function into a View with isolated child
// hook state.
func Build[M any](fn func(ctx *Context[M]) View[M]) View[M] {
	if fn == nil {
		return ViewFunc[M](func(*Context[M]) core.Node {
			return core.Box()
		})
	}
	return ViewFunc[M](func(ctx *Context[M]) core.Node {
		if ctx == nil {
			return core.Box()
		}
		childCtx := buildChildContext(ctx)
		defer func() {
			childCtx.building = false
		}()
		child := fn(childCtx)
		if child == nil {
			return core.Box()
		}
		return child.BuildCoreNode(childCtx)
	})
}
