package corelower

import (
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

// focusableRenderNode adds interaction.Focusable and interaction.EventHandler to
// any RenderNode. Used when a container (Stack, Box, Layer, Scroll) declares
// itself focusable via core.InteractionSpec so that semantic composition can
// carry focus and signal behavior without flattening into a single Action.
type focusableRenderNode[E any] struct {
	inner       layout.RenderNode
	signal      core.SignalEvent[E]
	focusSignal core.SignalEvent[E]
	caster      EventCaster[E]
	keymap      map[interaction.Key]core.Intent
}

func newFocusableRenderNode[E any](inner layout.RenderNode, n core.Node[E], caster EventCaster[E]) *focusableRenderNode[E] {
	keymap := make(map[interaction.Key]core.Intent, len(n.InteractionValue().Keymap))
	for _, binding := range n.InteractionValue().Keymap {
		if key := keyMatchToRuntimeKey(binding.Pattern); key != interaction.KeyNone {
			keymap[key] = binding.Intent
		}
	}
	var signal core.SignalEvent[E]
	if signals := n.InteractionValue().Signals; len(signals) > 0 {
		signal = signals[0]
	}
	var focusSignal core.SignalEvent[E]
	if focusSignals := n.InteractionValue().FocusSignals; len(focusSignals) > 0 {
		focusSignal = focusSignals[0]
	}
	return &focusableRenderNode[E]{
		inner:       inner,
		signal:      signal,
		focusSignal: focusSignal,
		caster:      caster,
		keymap:      keymap,
	}
}

func (f *focusableRenderNode[E]) Measure(c geom.Constraints, ctx *layout.LayoutContext) geom.Size {
	return f.inner.Measure(c, ctx)
}

func (f *focusableRenderNode[E]) Arrange(node *layout.LayoutNode, ctx *layout.LayoutContext) layout.NodeLayout {
	return f.inner.Arrange(node, ctx)
}

func (f *focusableRenderNode[E]) Paint(p paint.Painter, node *layout.LayoutNode, ctx *layout.PaintContext) {
	f.inner.Paint(p, node, ctx)
}

// Delegate child discovery to the inner node so the tree builder can see
// through the wrapper and materialize the inner children as layout nodes.
func (f *focusableRenderNode[E]) VisitChildren(visit func(hint string, child layout.RenderNode)) {
	if composite, ok := f.inner.(layout.Composite); ok {
		composite.VisitChildren(visit)
	}
}

// Delegate child rewriting to the inner node so reconciliation can rewrite
// through the wrapper.
func (f *focusableRenderNode[E]) MapChildren(rewrite func(hint string, child layout.RenderNode) layout.RenderNode) layout.RenderNode {
	if composite, ok := f.inner.(layout.Composite); ok {
		return &focusableRenderNode[E]{
			inner:       composite.MapChildren(rewrite),
			signal:      f.signal,
			focusSignal: f.focusSignal,
			caster:      f.caster,
			keymap:      f.keymap,
		}
	}
	return f
}

func (f *focusableRenderNode[E]) CanFocus(*layout.LayoutNode) bool {
	return true
}

func (f *focusableRenderNode[E]) HandleEvent(ev interaction.Event, _ *layout.LayoutNode) []update.Action {
	if ev.Kind == interaction.EventKey && f.signal.Kind != "" {
		intent, ok := f.keymap[ev.Key]
		if ok && intent == core.IntentActivate {
			return []update.Action{f.caster(f.signal.Event)}
		}
	}
	return nil
}

func (f *focusableRenderNode[E]) OnFocus(_ *layout.LayoutNode) []update.Action {
	if f.focusSignal.Kind == "" {
		return nil
	}
	return []update.Action{f.caster(f.focusSignal.Event)}
}

func (f *focusableRenderNode[E]) OnBlur(_ *layout.LayoutNode) []update.Action {
	return nil
}
