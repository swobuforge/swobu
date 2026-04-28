package views

import (
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/interaction"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/geom"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/layout"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/paint"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/update"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/view"
)

// FocusScopeRenderNode provides generic focus traversal for its subtree.
// Focused descendants receive key events first; unhandled traversal keys bubble
// to this scope, which then issues engine-level focus traversal actions.
type FocusScopeRenderNode struct {
	layout.Sized
	Child layout.RenderNode
}

func NewFocusScope(child layout.RenderNode) *FocusScopeRenderNode {
	return &FocusScopeRenderNode{
		Sized: layout.Sized{Sizing: layout.Sizing{W: layout.SizeGrow, H: layout.SizeGrow}},
		Child: child,
	}
}

func FocusScope[M any](child view.ViewSpec[M]) view.ViewSpec[M] {
	return view.View[M](func(ctx *view.Context[M]) view.RenderNode {
		return NewFocusScope(view.Materialize(ctx, child))
	})
}

func (f *FocusScopeRenderNode) Measure(c geom.Constraints, ctx *layout.LayoutContext) geom.Size {
	if f.Child == nil {
		return f.ResolveSize(geom.Size{}, c)
	}
	return f.ResolveSize(f.Child.Measure(c, ctx), c)
}

func (f *FocusScopeRenderNode) Arrange(node *layout.LayoutNode, _ *layout.LayoutContext) layout.NodeLayout {
	out := layout.NodeLayout{
		BorderRect:   node.Slot,
		ContentRect:  node.Slot,
		ViewportRect: node.Slot,
		ContentSize:  node.MeasuredSize,
	}
	if f.Child != nil {
		out.ChildSlots = []layout.ChildSlot{{
			Spec: layout.ChildSpec{Hint: "focus-scope", RenderNode: f.Child},
			Rect: node.Slot,
		}}
	}
	return out
}

func (f *FocusScopeRenderNode) Paint(_ paint.Painter, _ *layout.LayoutNode, _ *layout.PaintContext) {}

func (f *FocusScopeRenderNode) HandleScopedEvent(ev interaction.Event, _ *layout.LayoutNode) (bool, []update.Action) {
	if ev.Kind != interaction.EventKey {
		return false, nil
	}
	switch ev.Key {
	case interaction.KeyDown:
		return true, []update.Action{interaction.FocusMoveAction{Move: interaction.FocusMoveNext}}
	case interaction.KeyUp:
		return true, []update.Action{interaction.FocusMoveAction{Move: interaction.FocusMovePrev}}
	default:
		return false, nil
	}
}

func (f *FocusScopeRenderNode) VisitChildren(visit func(hint string, child layout.RenderNode)) {
	if f.Child != nil {
		visit("focus-scope", f.Child)
	}
}

func (f *FocusScopeRenderNode) MapChildren(rewrite func(hint string, child layout.RenderNode) layout.RenderNode) layout.RenderNode {
	clone := *f
	if clone.Child != nil {
		clone.Child = rewrite("focus-scope", clone.Child)
	}
	return &clone
}
