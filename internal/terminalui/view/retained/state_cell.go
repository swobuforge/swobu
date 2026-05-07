package retained

type localStateChangedAction struct{}

// UseState loads or initializes one retained local state value by hook order.
// State identity is scoped to the current view instance and call position.
func UseState[M any, T any](ctx *Context[M], init func() T) (T, func(T)) {
	if !ctx.building {
		panic("UseState called outside build")
	}
	slot := ctx.hookSlot
	ctx.hookSlot++
	if raw, ok := ctx.Local.Get(slot); ok {
		typed, typeOK := raw.(T)
		if !typeOK {
			panic("state type mismatch")
		}
		return typed, func(v T) {
			ctx.Local.Set(slot, v)
			if ctx.dispatch != nil {
				ctx.dispatch(localStateChangedAction{})
			}
		}
	}
	initial := init()
	ctx.Local.Set(slot, initial)
	return initial, func(v T) {
		ctx.Local.Set(slot, v)
		if ctx.dispatch != nil {
			ctx.dispatch(localStateChangedAction{})
		}
	}
}
