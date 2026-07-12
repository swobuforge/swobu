package component

import (
	"strconv"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

// LocalScope stores build-scoped slot values for one component instance.
type LocalScope interface {
	Get(slot int) (any, bool)
	Set(slot int, value any)
	WithPrefix(prefix string) LocalScope
}

// Context carries build-scoped local state, model access, and runtime dispatch
// hooks to component authors.
type Context[M any] struct {
	Local     LocalScope
	Model     func() M
	dispatch  func(update.Action)
	emit      func(update.Action)
	building  bool
	hookSlot  int
	childSlot int
}

// Dispatch emits one semantic application action.
//
// Calling it during build is a defect.
func (ctx *Context[M]) Dispatch(action update.Action) {
	if ctx.building {
		panic("component dispatch during build")
	}
	if ctx.dispatch != nil {
		ctx.dispatch(action)
	}
}

// Emit requests one runtime or external side effect.
//
// Calling it during build is a defect.
func (ctx *Context[M]) Emit(action update.Action) {
	if ctx.building {
		panic("component emit during build")
	}
	if ctx.emit != nil {
		ctx.emit(action)
	}
}

// Runtime captures the component runtime hooks needed by migration adapters.
type Runtime[M any] struct {
	Local    LocalScope
	Model    func() M
	Dispatch func(update.Action)
	Emit     func(update.Action)
	Building bool
}

// NewContext constructs one component context from runtime hooks.
func NewContext[M any](runtime Runtime[M]) *Context[M] {
	return &Context[M]{
		Local:    runtime.Local,
		Model:    runtime.Model,
		dispatch: runtime.Dispatch,
		emit:     runtime.Emit,
		building: runtime.Building,
	}
}

// Runtime returns a copy of the context runtime hooks for bridge adapters.
func (ctx *Context[M]) Runtime() Runtime[M] {
	if ctx == nil {
		return Runtime[M]{}
	}
	return Runtime[M]{
		Local:    ctx.Local,
		Model:    ctx.Model,
		Dispatch: ctx.dispatch,
		Emit:     ctx.emit,
		Building: ctx.building,
	}
}

// buildChildContext scopes nested hook state under one child prefix so sibling
// component builds do not collide in local state.
func buildChildContext[M any](parent *Context[M]) *Context[M] {
	slot := parent.childSlot
	parent.childSlot++
	local := parent.Local.WithPrefix("build/" + strconv.Itoa(slot))
	return NewContext(Runtime[M]{
		Local:    local,
		Model:    parent.Model,
		Dispatch: parent.dispatch,
		Emit:     parent.emit,
		Building: parent.building,
	})
}
