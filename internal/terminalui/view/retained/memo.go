package retained

// memoState stores one cached derived value at one hook slot.
type memoState[T any] struct {
	deps  []any
	value T
}

// UseMemo caches one derived value by hook order and dependency identity.
// Zero dependencies means "recompute on every build" so callers do not get a
// silent forever-cache when they omit a dependency list.
func UseMemo[M any, T any](ctx *Context[M], compute func() T, deps ...any) T {
	if !ctx.building {
		panic("UseMemo called outside build")
	}
	slot := ctx.hookSlot
	ctx.hookSlot++
	if compute == nil {
		panic("UseMemo requires a non-nil compute function")
	}
	if raw, ok := ctx.Local.Get(slot); ok && len(deps) > 0 {
		state, typeOK := raw.(memoState[T])
		if !typeOK {
			panic("memo state type mismatch")
		}
		if depsEqual(state.deps, deps) {
			return state.value
		}
	}
	value := compute()
	ctx.Local.Set(slot, memoState[T]{deps: cloneDeps(deps), value: value})
	return value
}
