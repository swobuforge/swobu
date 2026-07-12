package component

import "github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"

// UseState loads or initializes one build-scoped local state value by hook
// order. State identity is scoped to the current component instance and call
// position.
func UseState[M any, T any](ctx *Context[M], initial T) (T, func(T)) {
	if !ctx.building {
		panic("UseState called outside build")
	}

	slot := ctx.hookSlot
	ctx.hookSlot++

	if v, ok := ctx.Local.Get(slot); ok {
		typed, ok := v.(T)
		if !ok {
			panic("component local state type mismatch")
		}
		return typed, func(next T) {
			ctx.Local.Set(slot, next)
			ctx.Dispatch(update.LocalStateChangedAction{})
		}
	}

	ctx.Local.Set(slot, initial)
	return initial, func(next T) {
		ctx.Local.Set(slot, next)
		ctx.Dispatch(update.LocalStateChangedAction{})
	}
}
