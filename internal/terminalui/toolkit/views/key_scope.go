package views

import (
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// KeyScopeRenderNode is a transparent wrapper that can intercept key events for a
// subtree and emit actions when configured bindings match.
type KeyScopeRenderNode struct {
	layout.Sized
	Child    layout.RenderNode
	Handle   func(interaction.Event) (bool, []update.Action)
	Fallback func(interaction.Event) (bool, []update.Action)
}

func NewKeyScope(child layout.RenderNode, handle func(interaction.Event) (bool, []update.Action)) *KeyScopeRenderNode {
	return &KeyScopeRenderNode{
		Sized:  layout.Sized{Sizing: layout.Sizing{W: layout.SizeGrow, H: layout.SizeGrow}},
		Child:  child,
		Handle: handle,
	}
}

func KeyScope[M any](child retained.ViewSpec[M], handle func(*retained.Context[M], interaction.Event) (bool, []update.Action)) retained.ViewSpec[M] {
	return retained.View[M](func(ctx *retained.Context[M]) retained.RenderNode {
		return NewKeyScope(retained.Materialize(ctx, child), func(ev interaction.Event) (bool, []update.Action) {
			if handle == nil {
				return false, nil
			}
			return handle(ctx, ev)
		})
	})
}

func (k *KeyScopeRenderNode) Measure(c geom.Constraints, ctx *layout.LayoutContext) geom.Size {
	if k.Child == nil {
		return k.ResolveSize(geom.Size{}, c)
	}
	return k.ResolveSize(k.Child.Measure(c, ctx), c)
}

func (k *KeyScopeRenderNode) Arrange(node *layout.LayoutNode, _ *layout.LayoutContext) layout.NodeLayout {
	out := layout.NodeLayout{
		BorderRect:   node.Slot,
		ContentRect:  node.Slot,
		ViewportRect: node.Slot,
		ContentSize:  node.MeasuredSize,
	}
	if k.Child != nil {
		out.ChildSlots = []layout.ChildSlot{{
			Spec: layout.ChildSpec{Hint: "key-scope", RenderNode: k.Child},
			Rect: node.Slot,
		}}
	}
	return out
}

func (k *KeyScopeRenderNode) Paint(_ paint.Painter, _ *layout.LayoutNode, _ *layout.PaintContext) {}

func (k *KeyScopeRenderNode) HandleScopedEvent(ev interaction.Event, _ *layout.LayoutNode) (bool, []update.Action) {
	if k.Handle != nil {
		handled, actions := k.Handle(ev)
		if handled {
			return true, actions
		}
	}
	if k.Fallback != nil {
		return k.Fallback(ev)
	}
	return false, nil
}

func (k *KeyScopeRenderNode) VisitChildren(visit func(hint string, child layout.RenderNode)) {
	if k.Child != nil {
		visit("key-scope", k.Child)
	}
}

func (k *KeyScopeRenderNode) MapChildren(rewrite func(hint string, child layout.RenderNode) layout.RenderNode) layout.RenderNode {
	clone := *k
	if clone.Child != nil {
		clone.Child = rewrite("key-scope", clone.Child)
	}
	return &clone
}
