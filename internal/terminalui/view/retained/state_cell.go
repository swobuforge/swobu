package retained

// UseState is a temporary compatibility shim for local hook state during
// migration to semantic core. Do not add new uses.
//
// TODO remove after all callers migrated (slice 9+). 61 uses across 15 files.
func UseState[T any, M any](ctx *Context[M], init func() T) (T, func(T)) {
	if !ctx.building {
		panic("UseState called outside build")
	}
	slot := ctx.hookSlot
	ctx.hookSlot++
	if raw, ok := ctx.Local.Get(slot); ok {
		value, typeOK := raw.(T)
		if !typeOK {
			panic("state cell type mismatch")
		}
		return value, func(v T) { ctx.Local.Set(slot, v) }
	}
	value := init()
	ctx.Local.Set(slot, value)
	return value, func(v T) { ctx.Local.Set(slot, v) }
}
