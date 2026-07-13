package retained

import (
	"context"
	"reflect"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

type effectState struct {
	deps    []any
	cleanup func()
}

type effectHookRecord struct {
	state     *effectState
	run       func() func()
	shouldRun bool
}

type effectCommitEffect struct {
	state        *effectState
	run          func() func()
	priorCleanup func()
}

// effectCleanupEffect re-reads live hook state at unmount time so the cleanup
// that runs is the one most recently installed by the last committed effect.
type effectCleanupEffect struct {
	state *effectState
}

// RunImmediately marks one internal hook effect as synchronous in the retained
// runtime. Hook effects must complete their state bookkeeping before the next
// rebuild can observe the slot.
func (effectCommitEffect) RunImmediately() {}

func (e effectCommitEffect) Execute(_ context.Context) []update.Action {
	if e.priorCleanup != nil {
		e.priorCleanup()
	}
	if e.run == nil {
		e.state.cleanup = nil
		return nil
	}
	e.state.cleanup = e.run()
	return nil
}

func (effectCleanupEffect) RunImmediately() {}

func (e effectCleanupEffect) Execute(_ context.Context) []update.Action {
	if e.state != nil && e.state.cleanup != nil {
		e.state.cleanup()
	}
	return nil
}

// effectHookRenderNode preserves layout, interaction, and lifecycle interfaces
// while attaching useEffect bookkeeping to the wrapped subtree.
type effectHookRenderNode struct {
	RenderNode
	hooks []effectHookRecord
}

func wrapEffectHooks(node RenderNode, hooks []effectHookRecord) RenderNode {
	if node == nil || len(hooks) == 0 {
		return node
	}
	return effectHookRenderNode{
		RenderNode: node,
		hooks:      append([]effectHookRecord(nil), hooks...),
	}
}

// UseEffect registers one after-commit effect with cleanup semantics.
//
// Do not use — this is retained-runtime state with side effects. Semantic
// nodes should derive layout from props and emit signals for side effects.
// See docs/05-engineering/developer-workflow-and-entrypoints.md § "no retained state".
func UseEffect[M any](ctx *Context[M], effect func() func(), deps ...any) {
	if !ctx.building {
		panic("UseEffect called outside build")
	}
	slot := ctx.hookSlot
	ctx.hookSlot++
	if effect == nil {
		panic("UseEffect requires a non-nil effect function")
	}
	state := loadEffectState(ctx, slot)
	shouldRun := len(deps) == 0 || !depsEqual(state.deps, deps)
	state.deps = cloneDeps(deps)
	ctx.effectHooks = append(ctx.effectHooks, effectHookRecord{
		state:     state,
		run:       effect,
		shouldRun: shouldRun,
	})
}

func loadEffectState[M any](ctx *Context[M], slot int) *effectState {
	if raw, ok := ctx.Local.Get(slot); ok {
		state, typeOK := raw.(*effectState)
		if !typeOK {
			panic("effect state type mismatch")
		}
		return state
	}
	state := &effectState{}
	ctx.Local.Set(slot, state)
	return state
}

func (h effectHookRenderNode) PostCommitEffects() []update.Effect {
	effects := make([]update.Effect, 0, len(h.hooks))
	for _, hook := range h.hooks {
		if !hook.shouldRun {
			continue
		}
		effects = append(effects, effectCommitEffect{
			state:        hook.state,
			run:          hook.run,
			priorCleanup: hook.state.cleanup,
		})
	}
	return effects
}

func (h effectHookRenderNode) OnMountEffects() []update.Effect {
	return nil
}

func (h effectHookRenderNode) OnUnmountEffects() []update.Effect {
	effects := make([]update.Effect, 0, len(h.hooks))
	for _, hook := range h.hooks {
		effects = append(effects, effectCleanupEffect{state: hook.state})
	}
	return effects
}

func (h effectHookRenderNode) HitTest(local geom.Point, node *layout.LayoutNode) bool {
	if hittable, ok := h.RenderNode.(interaction.Hittable); ok {
		return hittable.HitTest(local, node)
	}
	return false
}

func (h effectHookRenderNode) HandleEvent(ev interaction.Event, node *layout.LayoutNode) []update.Action {
	if handler, ok := h.RenderNode.(interaction.EventHandler); ok {
		return handler.HandleEvent(ev, node)
	}
	return nil
}

func (h effectHookRenderNode) HandleEventTransform(ev interaction.Event, node *layout.LayoutNode) (*interaction.Event, []update.Action) {
	if transformer, ok := h.RenderNode.(interaction.EventTransformer); ok {
		return transformer.HandleEventTransform(ev, node)
	}
	if scoped, ok := h.RenderNode.(interaction.ScopedEventHandler); ok {
		handled, actions := scoped.HandleScopedEvent(ev, node)
		if handled {
			return nil, actions
		}
		return &ev, actions
	}
	if handler, ok := h.RenderNode.(interaction.EventHandler); ok {
		actions := handler.HandleEvent(ev, node)
		if len(actions) > 0 {
			return nil, actions
		}
	}
	return &ev, nil
}

func (h effectHookRenderNode) HandleScopedEvent(ev interaction.Event, node *layout.LayoutNode) (bool, []update.Action) {
	if scoped, ok := h.RenderNode.(interaction.ScopedEventHandler); ok {
		return scoped.HandleScopedEvent(ev, node)
	}
	if handler, ok := h.RenderNode.(interaction.EventHandler); ok {
		actions := handler.HandleEvent(ev, node)
		return len(actions) > 0, actions
	}
	return false, nil
}

func (h effectHookRenderNode) CanFocus(node *layout.LayoutNode) bool {
	if focusable, ok := h.RenderNode.(interaction.Focusable); ok {
		return focusable.CanFocus(node)
	}
	return false
}

func (h effectHookRenderNode) OnFocus(node *layout.LayoutNode) []update.Action {
	if focusEvents, ok := h.RenderNode.(interaction.FocusEvents); ok {
		return focusEvents.OnFocus(node)
	}
	return nil
}

func (h effectHookRenderNode) OnBlur(node *layout.LayoutNode) []update.Action {
	if focusEvents, ok := h.RenderNode.(interaction.FocusEvents); ok {
		return focusEvents.OnBlur(node)
	}
	return nil
}

func (h effectHookRenderNode) OnMount(node *layout.LayoutNode) []update.Action {
	if lifecycle, ok := h.RenderNode.(interaction.Lifecycle); ok {
		return lifecycle.OnMount(node)
	}
	return nil
}

func (h effectHookRenderNode) OnUnmount(node *layout.LayoutNode) []update.Action {
	if lifecycle, ok := h.RenderNode.(interaction.Lifecycle); ok {
		return lifecycle.OnUnmount(node)
	}
	return nil
}

func (h effectHookRenderNode) VisitChildren(visit func(hint string, child layout.RenderNode)) {
	if composite, ok := h.RenderNode.(layout.Composite); ok {
		composite.VisitChildren(visit)
	}
}

func (h effectHookRenderNode) MapChildren(rewrite func(hint string, child layout.RenderNode) layout.RenderNode) layout.RenderNode {
	composite, ok := h.RenderNode.(layout.Composite)
	if !ok {
		return h
	}
	clone := h
	clone.RenderNode = composite.MapChildren(rewrite)
	return clone
}

func depsEqual(a, b []any) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	return reflect.DeepEqual(a, b)
}

func cloneDeps(deps []any) []any {
	if len(deps) == 0 {
		return nil
	}
	out := make([]any, len(deps))
	copy(out, deps)
	return out
}
