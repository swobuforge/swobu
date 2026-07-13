package corelower

import (
	"unicode/utf8"

	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/textmetrics"
)

type inputRenderNode[E any] struct {
	layout.Sized
	value     string
	onChange  core.SignalEvent[E]
	onCommit  core.SignalEvent[E]
	onCancel  core.SignalEvent[E]
	focusable bool
	caster    EventCaster[E]
	intentMap map[interaction.Key]core.Intent
}

// SignalState holds the named signals that an input node may emit.
// Zero or more fields may be populated; absent signals are ignored safely.
type SignalState[E any] struct {
	Change core.SignalEvent[E]
	Commit core.SignalEvent[E]
	Cancel core.SignalEvent[E]
}

func lowerInput[E any](n core.Node[E], _ EnvConfig, caster EventCaster[E]) (layout.RenderNode, error) {
	signals := extractInputSignals(n)
	intentMap := make(map[interaction.Key]core.Intent, len(n.InteractionValue().Keymap))
	for _, binding := range n.InteractionValue().Keymap {
		if key := keyMatchToRuntimeKey(binding.Pattern); key != interaction.KeyNone {
			intentMap[key] = binding.Intent
		}
	}
	node := &inputRenderNode[E]{
		Sized:     layout.Sized{Sizing: layout.Sizing{W: layout.SizeGrow, H: layout.SizeFit}},
		value:     textmetrics.SanitizeTerminalText(n.ContentValue().Text),
		onChange:  signals.Change,
		onCommit:  signals.Commit,
		onCancel:  signals.Cancel,
		focusable: n.InteractionValue().Focus.Mode != core.FocusNone || n.Kind() == core.KindInput,
		caster:    caster,
		intentMap: intentMap,
	}
	if fid := n.InteractionValue().Focus.ID; !fid.Empty() {
		return layout.WithFocusID(string(fid), node), nil
	}
	return node, nil
}

// extractInputSignals reads named signals from the node interaction or
// falls back to the positional convention for migration compatibility.
// TODO: Remove fallback once all callers migrate to named slots.
// swobu:lint ignore string-switch because=signal kinds are string constants in the core API boundary.
func extractInputSignals[E any](n core.Node[E]) SignalState[E] {
	var out SignalState[E]
	interaction := n.InteractionValue()
	// Named lookup: scan for well-known signal kinds.
	// swobu:lint ignore string-switch because=signal kinds are string constants in the core API boundary.
	for _, s := range interaction.Signals {
		switch s.Kind {
		case "input.change":
			out.Change = s
		case "input.commit":
			out.Commit = s
		case "input.cancel":
			out.Cancel = s
		}
	}
	// TODO: Fallback to positional slots (compatibility shim).
	if out.Change.Kind == "" || out.Commit.Kind == "" || out.Cancel.Kind == "" {
		signals := interaction.Signals
		if len(signals) > 0 && out.Change.Kind == "" {
			out.Change = signals[0]
		}
		if len(signals) > 1 && out.Commit.Kind == "" {
			out.Commit = signals[1]
		}
		if len(signals) > 2 && out.Cancel.Kind == "" {
			out.Cancel = signals[2]
		}
	}
	return out
}

func (i *inputRenderNode[E]) Measure(c geom.Constraints, _ *layout.LayoutContext) geom.Size {
	intrinsic := geom.Size{
		W: maxInt(textmetrics.Width(i.renderLine(false)), textmetrics.Width(i.renderLine(true))),
		H: 1,
	}
	return i.ResolveSize(intrinsic, c)
}

func (i *inputRenderNode[E]) Arrange(node *layout.LayoutNode, _ *layout.LayoutContext) layout.NodeLayout {
	return layout.NodeLayout{
		BorderRect:   node.Slot,
		ContentRect:  node.Slot,
		ViewportRect: node.Slot,
		ContentSize:  node.MeasuredSize,
	}
}

func (i *inputRenderNode[E]) Paint(p paint.Painter, node *layout.LayoutNode, ctx *layout.PaintContext) {
	if node.BorderRect.W <= 0 {
		return
	}
	line := i.renderLine(ctx.FocusedID == node.ID)
	p.Text(0, 0, textmetrics.PadRight(textmetrics.TrimToWidthRaw(line, node.BorderRect.W), node.BorderRect.W))
}

func (i *inputRenderNode[E]) HitTest(local geom.Point, _ *layout.LayoutNode) bool {
	return local.Y == 0 && local.X >= 0 && i.focusable
}

func (i *inputRenderNode[E]) HandleEvent(ev interaction.Event, node *layout.LayoutNode) []update.Action {
	handled, actions := i.HandleScopedEvent(ev, node)
	if !handled {
		return nil
	}
	return actions
}

func (i *inputRenderNode[E]) HandleScopedEvent(ev interaction.Event, _ *layout.LayoutNode) (bool, []update.Action) {
	if !i.focusable {
		return false, nil
	}
	if ev.Kind != interaction.EventKey {
		if ev.Kind == interaction.EventMouseDown {
			return false, nil
		}
		return false, nil
	}
	intent, ok := i.intentMap[ev.Key]
	if !ok {
		return true, nil // consume non-mapped keys while focused
	}
	switch intent {
	case core.IntentActivate:
		if i.onCommit.Kind == "" {
			return true, nil
		}
		return true, []update.Action{i.caster(i.onCommit.Event)}
	case core.IntentCancel:
		if i.onCancel.Kind == "" {
			return true, nil
		}
		return true, []update.Action{i.caster(i.onCancel.Event)}
	case core.IntentEdit:
		if i.onChange.Kind == "" {
			return true, nil
		}
		return true, []update.Action{i.caster(i.onChange.Event)}
	default:
		return true, nil
	}
}

func (i *inputRenderNode[E]) CanFocus(*layout.LayoutNode) bool { return i.focusable }

func (i *inputRenderNode[E]) renderLine(focused bool) string {
	value := i.value
	if focused {
		if value == "" {
			value = "_"
		} else {
			value += "_"
		}
	}
	marker := "  "
	if focused {
		marker = "> "
	}
	return marker + value
}

func trimLastRune(value string) string {
	if value == "" {
		return ""
	}
	_, size := utf8.DecodeLastRuneInString(value)
	if size <= 0 {
		return ""
	}
	return value[:len(value)-size]
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
