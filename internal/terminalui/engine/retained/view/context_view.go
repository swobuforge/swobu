// Package view defines the app-facing view contract and composition primitives.
package view

import (
	"strconv"

	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/update"
)

// RenderNode is the app-facing alias for the engine's structural node type.
// App code returns RenderNode from BuildView; the engine owns the implementation.
type RenderNode = layout.RenderNode

// LocalScope is the node-scoped local state capability exposed to views.
type LocalScope interface {
	Get(slot int) (any, bool)
	Set(slot int, value any)
	WithPrefix(prefix string) LocalScope
}

// Context is the typed minimal capability context exposed to
// author-facing views. Runtime identity, reconciliation tables, and other
// engine internals stay private behind this seam.
type Context[M any] struct {
	Local     LocalScope
	Model     func() M
	dispatch  func(update.Action)
	emit      func(update.Action)
	building  bool
	hookSlot  int
	childSlot int
}

// ViewSpec is the declarative composition value used by app and toolkit code.
// The runtime node-builder method is intentionally package-private so only
// engine/view can drive node materialization directly.
type ViewSpec[M any] interface {
	BuildRenderNode(ctx *Context[M]) RenderNode
}

func renderNode[M any](ctx *Context[M], w ViewSpec[M]) RenderNode {
	if w == nil {
		return nil
	}
	return w.BuildRenderNode(ctx)
}

// Materialize converts one declarative view value into a layout node.
// Use this helper when composing wrappers in other packages.
func Materialize[M any](ctx *Context[M], w ViewSpec[M]) RenderNode {
	return renderNode(ctx, w)
}

// View constructs one declarative view from a render function.
func View[M any](render func(ctx *Context[M]) RenderNode) ViewSpec[M] {
	if render == nil {
		return nil
	}
	return viewSpecClosure[M]{render: render}
}

// Build constructs one declarative view from a BuildView-style function that
// returns another ViewSpec.
func Build[M any](buildView func(ctx *Context[M]) ViewSpec[M]) ViewSpec[M] {
	if buildView == nil {
		return nil
	}
	return viewSpecClosure[M]{
		render: func(ctx *Context[M]) RenderNode {
			slot := ctx.childSlot
			ctx.childSlot++
			local := ctx.Local.WithPrefix("build/" + strconv.Itoa(slot))
			childCtx := &Context[M]{
				Local:     local,
				Model:     ctx.Model,
				dispatch:  ctx.dispatch,
				emit:      ctx.emit,
				building:  ctx.building,
				hookSlot:  0,
				childSlot: 0,
			}
			return renderNode(childCtx, buildView(childCtx))
		},
	}
}

// BuildWithLifecycle is Build plus optional lifecycle effect hooks.
func BuildWithLifecycle[M any](
	buildView func(ctx *Context[M]) ViewSpec[M],
	onMount func() []update.Effect,
	onUnmount func() []update.Effect,
) ViewSpec[M] {
	if buildView == nil {
		return nil
	}
	return viewSpecClosure[M]{
		render: func(ctx *Context[M]) RenderNode {
			slot := ctx.childSlot
			ctx.childSlot++
			local := ctx.Local.WithPrefix("build/" + strconv.Itoa(slot))
			childCtx := &Context[M]{
				Local:     local,
				Model:     ctx.Model,
				dispatch:  ctx.dispatch,
				emit:      ctx.emit,
				building:  ctx.building,
				hookSlot:  0,
				childSlot: 0,
			}
			return renderNode(childCtx, buildView(childCtx))
		},
		onMount:   onMount,
		onUnmount: onUnmount,
	}
}

type viewSpecClosure[M any] struct {
	render    func(ctx *Context[M]) RenderNode
	onMount   func() []update.Effect
	onUnmount func() []update.Effect
}

func (v viewSpecClosure[M]) BuildRenderNode(ctx *Context[M]) RenderNode { return v.render(ctx) }

func (v viewSpecClosure[M]) OnMountEffects() []update.Effect {
	if v.onMount == nil {
		return nil
	}
	return v.onMount()
}

func (v viewSpecClosure[M]) OnUnmountEffects() []update.Effect {
	if v.onUnmount == nil {
		return nil
	}
	return v.onUnmount()
}

// Dispatch emits one semantic app update. Calling it during BuildView is a defect.
func (ctx *Context[M]) Dispatch(action update.Action) {
	if ctx.building {
		panic("view dispatch during build")
	}
	if ctx.dispatch != nil {
		ctx.dispatch(action)
	}
}

// Emit requests one runtime or external side effect. Calling it during BuildView is
// a defect: build must be effect-free.
func (ctx *Context[M]) Emit(action update.Action) {
	if ctx.building {
		panic("view emit during build")
	}
	if ctx.emit != nil {
		ctx.emit(action)
	}
}

func buildViewNode[M any](child ViewSpec[M], local LocalScope, dispatch func(update.Action), emit func(update.Action), model func() M) RenderNode {
	ctx := &Context[M]{
		Local:     local,
		Model:     model,
		dispatch:  dispatch,
		emit:      emit,
		building:  true,
		hookSlot:  0,
		childSlot: 0,
	}
	node := child.BuildRenderNode(ctx)
	ctx.building = false
	return node
}
