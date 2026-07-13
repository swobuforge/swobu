package corelower

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/textmetrics"
)

type actionRenderNode[E any] struct {
	layout.Sized
	label       string
	signal      core.SignalEvent[E]
	focusSignal core.SignalEvent[E]
	focusable   bool
	caster      EventCaster[E]
	intentMap   map[interaction.Key]core.Intent
	style       paint.Style
}

func lowerAction[E any](n core.Node[E], env EnvConfig, caster EventCaster[E]) (layout.RenderNode, error) {
	signal, err := firstSignal(n)
	if err != nil {
		return nil, err
	}
	intentMap := make(map[interaction.Key]core.Intent, len(n.InteractionValue().Keymap))
	for _, binding := range n.InteractionValue().Keymap {
		if key := keyMatchToRuntimeKey(binding.Pattern); key != interaction.KeyNone {
			intentMap[key] = binding.Intent
		}
	}
	resolver := env.Resolver
	if resolver == nil {
		resolver = &StyleResolver{ColorConfig: DefaultColorConfig}
	}
	sizing := layout.Sizing{W: layout.SizeFit, H: layout.SizeFit}
	sz := n.LayoutValue().Size
	if sz.Width.Mode == core.DimFixed {
		sizing.W = layout.SizeFixed
		sizing.Fixed.W = sz.Width.Value
	} else if sz.Width.Mode == core.DimFill {
		sizing.W = layout.SizeGrow
	}
	if sz.Height.Mode == core.DimFixed {
		sizing.H = layout.SizeFixed
		sizing.Fixed.H = sz.Height.Value
	} else if sz.Height.Mode == core.DimFill {
		sizing.H = layout.SizeGrow
	}
	node := &actionRenderNode[E]{
		Sized:       layout.Sized{Sizing: sizing},
		label:       textmetrics.SanitizeTerminalText(n.ContentValue().Text),
		signal:      signal,
		focusSignal: firstFocusSignal(n),
		focusable:   n.InteractionValue().Focus.Mode != core.FocusNone,
		caster:      caster,
		intentMap:   intentMap,
		style:       resolver.Resolve(n.StyleValue()),
	}
	if fid := n.InteractionValue().Focus.ID; !fid.Empty() {
		return layout.WithFocusID(string(fid), node), nil
	}
	return node, nil
}

func firstSignal[E any](n core.Node[E]) (core.SignalEvent[E], error) {
	if signals := n.InteractionValue().Signals; len(signals) > 0 {
		for _, signal := range signals {
			if signal.Kind != "" {
				return signal, nil
			}
		}
	}
	if len(n.InteractionValue().FocusSignals) > 0 {
		return core.SignalEvent[E]{}, nil
	}
	if specs := n.ContractValue().Signals; len(specs) > 0 {
		for _, spec := range specs {
			if spec.Kind != "" {
				return core.SignalEvent[E]{Kind: spec.Kind}, nil
			}
		}
	}
	if n.InteractionValue().Focus.Mode == core.FocusNone {
		return core.SignalEvent[E]{}, nil
	}
	return core.SignalEvent[E]{}, fmt.Errorf("action node has no signal")
}

func firstFocusSignal[E any](n core.Node[E]) core.SignalEvent[E] {
	if signals := n.InteractionValue().FocusSignals; len(signals) > 0 {
		return signals[0]
	}
	return core.SignalEvent[E]{}
}

func (a *actionRenderNode[E]) Measure(c geom.Constraints, ctx *layout.LayoutContext) geom.Size {
	return a.ResolveSize(geom.Size{W: textmetrics.Width(a.renderLine(false)), H: 1}, c)
}

func (a *actionRenderNode[E]) Arrange(node *layout.LayoutNode, ctx *layout.LayoutContext) layout.NodeLayout {
	return layout.NodeLayout{
		BorderRect:   node.Slot,
		ContentRect:  node.Slot,
		ViewportRect: node.Slot,
		ContentSize:  node.MeasuredSize,
	}
}

func (a *actionRenderNode[E]) Paint(p paint.Painter, node *layout.LayoutNode, ctx *layout.PaintContext) {
	if node.BorderRect.W <= 0 {
		return
	}
	line := a.renderLine(ctx.FocusedID == node.ID)
	p.WithStyle(a.style).Text(0, 0, textmetrics.PadRight(textmetrics.TrimToWidthRaw(line, node.BorderRect.W), node.BorderRect.W))
}

func (a *actionRenderNode[E]) HitTest(local geom.Point, _ *layout.LayoutNode) bool {
	return local.Y == 0 && local.X >= 0
}

func (a *actionRenderNode[E]) HandleEvent(ev interaction.Event, _ *layout.LayoutNode) []update.Action {
	if !a.focusable || a.signal.Kind == "" {
		return nil
	}
	if ev.Kind == interaction.EventKey {
		intent, ok := a.intentMap[ev.Key]
		if ok && intent == core.IntentActivate {
			return []update.Action{a.caster(a.signal.Event)}
		}
	}
	if ev.Kind == interaction.EventMouseDown {
		return []update.Action{a.caster(a.signal.Event)}
	}
	return nil
}

func (a *actionRenderNode[E]) CanFocus(*layout.LayoutNode) bool { return a.focusable }

func (a *actionRenderNode[E]) OnFocus(*layout.LayoutNode) []update.Action {
	if a.focusSignal.Kind == "" {
		return nil
	}
	return []update.Action{a.caster(a.focusSignal.Event)}
}

func (a *actionRenderNode[E]) OnBlur(*layout.LayoutNode) []update.Action {
	return nil
}

func (a *actionRenderNode[E]) renderLine(focused bool) string {
	if !a.focusable {
		return a.label
	}
	marker := "  "
	if focused {
		marker = "> "
	}
	return marker + a.label
}

// swobu:lint ignore string-switch because=bridging from semantic KeyMatch string names to typed interaction.Key enum at the boundary between core algebra and retained runtime
func keyMatchToRuntimeKey(match core.KeyMatch) interaction.Key {
	switch match.Name {
	case "enter":
		return interaction.KeyEnter
	case "esc":
		return interaction.KeyEsc
	case "space":
		return interaction.KeySpace
	case "backspace":
		return interaction.KeyBackspace
	case "up":
		return interaction.KeyUp
	case "down":
		return interaction.KeyDown
	case "tab":
		return interaction.KeyTab
	case "shift+tab":
		return interaction.KeyShiftTab
	default:
		if len(match.Name) == 1 {
			return interaction.KeyRune
		}
		return interaction.KeyNone
	}
}
