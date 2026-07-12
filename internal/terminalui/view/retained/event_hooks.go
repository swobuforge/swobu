package retained

import (
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

type eventHookRecord struct {
	handle func(interaction.Event) *interaction.Event
}

// UseEvent registers one local event transformer for the current build scope.
// Returning nil consumes the event; returning the same or a modified event
// lets bubbling continue with that value.
func UseEvent[M any](ctx *Context[M], handle func(interaction.Event) *interaction.Event) {
	if !ctx.building {
		panic("UseEvent called outside build")
	}
	ctx.hookSlot++
	if handle == nil {
		return
	}
	ctx.eventHooks = append(ctx.eventHooks, eventHookRecord{handle: handle})
}

func wrapEventHooks(node RenderNode, hooks []eventHookRecord) RenderNode {
	if node == nil || len(hooks) == 0 {
		return node
	}
	return eventHookRenderNode{
		RenderNode: node,
		hooks:      append([]eventHookRecord(nil), hooks...),
	}
}

// eventHookRenderNode preserves the wrapped node's layout and interaction
// interfaces while applying local event transformers before the wrapped node.
type eventHookRenderNode struct {
	RenderNode
	hooks []eventHookRecord
}

func (h eventHookRenderNode) HandleEventTransform(ev interaction.Event, node *layout.LayoutNode) (*interaction.Event, []update.Action) {
	current := ev
	for _, hook := range h.hooks {
		next := hook.handle(current)
		if next == nil {
			return nil, nil
		}
		current = *next
	}
	if transformer, ok := h.RenderNode.(interaction.EventTransformer); ok {
		return transformer.HandleEventTransform(current, node)
	}
	if scoped, ok := h.RenderNode.(interaction.ScopedEventHandler); ok {
		handled, actions := scoped.HandleScopedEvent(current, node)
		if handled {
			return nil, actions
		}
		return &current, actions
	}
	if handler, ok := h.RenderNode.(interaction.EventHandler); ok {
		actions := handler.HandleEvent(current, node)
		if len(actions) > 0 {
			return nil, actions
		}
	}
	return &current, nil
}
func (h eventHookRenderNode) PostCommitEffects() []update.Effect {
	if hooker, ok := h.RenderNode.(interface{ PostCommitEffects() []update.Effect }); ok {
		return hooker.PostCommitEffects()
	}
	return nil
}

func (h eventHookRenderNode) OnMountEffects() []update.Effect {
	if hooker, ok := h.RenderNode.(interface{ OnMountEffects() []update.Effect }); ok {
		return hooker.OnMountEffects()
	}
	return nil
}

func (h eventHookRenderNode) OnUnmountEffects() []update.Effect {
	if hooker, ok := h.RenderNode.(interface{ OnUnmountEffects() []update.Effect }); ok {
		return hooker.OnUnmountEffects()
	}
	return nil
}

func (h eventHookRenderNode) HitTest(local geom.Point, node *layout.LayoutNode) bool {
	if hittable, ok := h.RenderNode.(interaction.Hittable); ok {
		return hittable.HitTest(local, node)
	}
	return false
}

func (h eventHookRenderNode) CanFocus(node *layout.LayoutNode) bool {
	if focusable, ok := h.RenderNode.(interaction.Focusable); ok {
		return focusable.CanFocus(node)
	}
	return false
}

func (h eventHookRenderNode) OnFocus(node *layout.LayoutNode) []update.Action {
	if focusEvents, ok := h.RenderNode.(interaction.FocusEvents); ok {
		return focusEvents.OnFocus(node)
	}
	return nil
}

func (h eventHookRenderNode) OnBlur(node *layout.LayoutNode) []update.Action {
	if focusEvents, ok := h.RenderNode.(interaction.FocusEvents); ok {
		return focusEvents.OnBlur(node)
	}
	return nil
}

func (h eventHookRenderNode) OnMount(node *layout.LayoutNode) []update.Action {
	if lifecycle, ok := h.RenderNode.(interaction.Lifecycle); ok {
		return lifecycle.OnMount(node)
	}
	return nil
}

func (h eventHookRenderNode) OnUnmount(node *layout.LayoutNode) []update.Action {
	if lifecycle, ok := h.RenderNode.(interaction.Lifecycle); ok {
		return lifecycle.OnUnmount(node)
	}
	return nil
}

func (h eventHookRenderNode) VisitChildren(visit func(hint string, child layout.RenderNode)) {
	if composite, ok := h.RenderNode.(layout.Composite); ok {
		composite.VisitChildren(visit)
	}
}

func (h eventHookRenderNode) MapChildren(rewrite func(hint string, child layout.RenderNode) layout.RenderNode) layout.RenderNode {
	composite, ok := h.RenderNode.(layout.Composite)
	if !ok {
		return h
	}
	clone := h
	clone.RenderNode = composite.MapChildren(rewrite)
	return clone
}
