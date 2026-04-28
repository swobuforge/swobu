package view

import (
	"strconv"

	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/layout"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/update"
)

// ResponsiveView renders wide or narrow based on available width.
// Both children are views — the engine handles the branching internally.
type ResponsiveView[M any] struct {
	Threshold int
	Wide      ViewSpec[M]
	Narrow    ViewSpec[M]
}

func (r ResponsiveView[M]) BuildRenderNode(ctx *Context[M]) RenderNode {
	slot := ctx.childSlot
	ctx.childSlot++
	base := ctx.Local.WithPrefix("responsive/" + strconv.Itoa(slot))
	wideCtx := &Context[M]{
		Local:     base.WithPrefix("wide"),
		Model:     ctx.Model,
		dispatch:  ctx.dispatch,
		emit:      ctx.emit,
		building:  ctx.building,
		hookSlot:  0,
		childSlot: 0,
	}
	narrowCtx := &Context[M]{
		Local:     base.WithPrefix("narrow"),
		Model:     ctx.Model,
		dispatch:  ctx.dispatch,
		emit:      ctx.emit,
		building:  ctx.building,
		hookSlot:  0,
		childSlot: 0,
	}
	var wideNode RenderNode
	if r.Wide != nil {
		wideNode = r.Wide.BuildRenderNode(wideCtx)
	}
	var narrowNode RenderNode
	if r.Narrow != nil {
		narrowNode = r.Narrow.BuildRenderNode(narrowCtx)
	}
	return layout.NewResponsiveSwitch(r.Threshold, 0, wideNode, narrowNode)
}

// FromRenderNode lifts one pre-built layout node into a ViewSpec.
func FromRenderNode[M any](node layout.RenderNode) ViewSpec[M] {
	return View[M](func(_ *Context[M]) RenderNode { return node })
}

// Lift adapts a ViewSpec[struct{}] to ViewSpec[M] for contexts that don't use the model.
func Lift[M any, S any](w ViewSpec[S]) ViewSpec[M] {
	return liftedView[M, S]{w: w}
}

type liftedView[M any, S any] struct{ w ViewSpec[S] }

func (l liftedView[M, S]) BuildRenderNode(ctx *Context[M]) RenderNode {
	return l.w.BuildRenderNode(&Context[S]{
		Local:     noopLocalScope{ctx.Local},
		Model:     func() S { var zero S; return zero },
		dispatch:  func(update.Action) {},
		emit:      func(update.Action) {},
		building:  false,
		hookSlot:  0,
		childSlot: 0,
	})
}

type noopLocalScope struct{ base LocalScope }

func (n noopLocalScope) Get(slot int) (any, bool) { return n.base.Get(slot) }
func (n noopLocalScope) Set(slot int, value any)  { n.base.Set(slot, value) }
func (n noopLocalScope) WithPrefix(prefix string) LocalScope {
	return noopLocalScope{n.base.WithPrefix(prefix)}
}

// FillRemainingView marks a view to grow and fill available space.
type FillRemainingView[M any] struct{ W ViewSpec[M] }

func (f FillRemainingView[M]) BuildRenderNode(ctx *Context[M]) RenderNode {
	return fillRenderNode{RenderNode: f.W.BuildRenderNode(ctx)}
}

type fillRenderNode struct{ RenderNode }

func (fillRenderNode) GrowFactor() int { return 1 }
