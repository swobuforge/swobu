package component

// LocalScope stores build-scoped slot values for one component instance.
type LocalScope interface {
	Get(slot int) (any, bool)
	Set(slot int, value any)
	WithPrefix(prefix string) LocalScope
}

// Context[E, M] carries build-scoped local state, model access, and runtime dispatch
// hooks to component authors. E is the application event type.
type Context[E any, M any] struct {
	local     LocalScope
	Model     func() M
	dispatch  func(E)
	emit      func(E)
	building  bool
	hookSlot  int
	childSlot int
}

// Dispatch emits one semantic application event.
//
// Calling it during build is a defect.
func (ctx *Context[E, M]) Dispatch(event E) {
	if ctx.building {
		panic("component dispatch during build")
	}
	if ctx.dispatch != nil {
		ctx.dispatch(event)
	}
}

// Emit requests one runtime or external side effect.
//
// Calling it during build is a defect.
func (ctx *Context[E, M]) Emit(event E) {
	if ctx.building {
		panic("component emit during build")
	}
	if ctx.emit != nil {
		ctx.emit(event)
	}
}

// ComponentRuntimeState[E, M] captures the component runtime hooks needed by migration adapters.
type ComponentRuntimeState[E any, M any] struct {
	Model    func() M
	Dispatch func(E)
	Emit     func(E)
	Building bool
}

// NewContext constructs one component context from runtime hooks.
func NewContext[E any, M any](runtime ComponentRuntimeState[E, M]) *Context[E, M] {
	return &Context[E, M]{
		Model:    runtime.Model,
		dispatch: runtime.Dispatch,
		emit:     runtime.Emit,
		building: runtime.Building,
	}
}

// Runtime returns a copy of the context runtime hooks for bridge adapters.
func (ctx *Context[E, M]) Runtime() ComponentRuntimeState[E, M] {
	if ctx == nil {
		return ComponentRuntimeState[E, M]{}
	}
	return ComponentRuntimeState[E, M]{
		Model:    ctx.Model,
		Dispatch: ctx.dispatch,
		Emit:     ctx.emit,
		Building: ctx.building,
	}
}

// buildChildContext scopes nested hook state under one child prefix so sibling
// component builds do not collide in local state.
func buildChildContext[E any, M any](parent *Context[E, M]) *Context[E, M] {
	return NewContext(ComponentRuntimeState[E, M]{
		Model:    parent.Model,
		Dispatch: parent.dispatch,
		Emit:     parent.emit,
		Building: parent.building,
	})
}
