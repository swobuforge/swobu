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

type inputRenderNode struct {
	layout.Sized
	value     string
	onChange  core.Signal
	onCommit  core.Signal
	onCancel  core.Signal
	focusable bool
}

func lowerInput(n core.Node, _ Env) (layout.RenderNode, error) {
	signals := n.InteractionValue().Signals
	if len(signals) < 3 {
		// Keep the bridge permissive during migration: bare core.Input nodes can
		// still lower, but they only become interactive once the semantic wrapper
		// supplies change/commit/cancel signal slots.
		signals = append(signals, make([]core.Signal, 3-len(signals))...)
	}
	return &inputRenderNode{
		Sized:     layout.Sized{Sizing: layout.Sizing{W: layout.SizeGrow, H: layout.SizeFit}},
		value:     textmetrics.SanitizeTerminalText(n.ContentValue().Text),
		onChange:  signals[0],
		onCommit:  signals[1],
		onCancel:  signals[2],
		focusable: n.InteractionValue().Focus.Mode != core.FocusNone || n.Kind() == core.KindInput,
	}, nil
}

func (i *inputRenderNode) Measure(c geom.Constraints, _ *layout.LayoutContext) geom.Size {
	intrinsic := geom.Size{
		W: maxInt(textmetrics.Width(i.renderLine(false)), textmetrics.Width(i.renderLine(true))),
		H: 1,
	}
	return i.ResolveSize(intrinsic, c)
}

func (i *inputRenderNode) Arrange(node *layout.LayoutNode, _ *layout.LayoutContext) layout.NodeLayout {
	return layout.NodeLayout{
		BorderRect:   node.Slot,
		ContentRect:  node.Slot,
		ViewportRect: node.Slot,
		ContentSize:  node.MeasuredSize,
	}
}

func (i *inputRenderNode) Paint(p paint.Painter, node *layout.LayoutNode, ctx *layout.PaintContext) {
	if node.BorderRect.W <= 0 {
		return
	}
	line := i.renderLine(ctx.FocusedID == node.ID)
	p.Text(0, 0, textmetrics.PadRight(textmetrics.TrimToWidthRaw(line, node.BorderRect.W), node.BorderRect.W))
}

func (i *inputRenderNode) HitTest(local geom.Point, _ *layout.LayoutNode) bool {
	return local.Y == 0 && local.X >= 0 && i.focusable
}

func (i *inputRenderNode) HandleEvent(ev interaction.Event, node *layout.LayoutNode) []update.Action {
	handled, actions := i.HandleScopedEvent(ev, node)
	if !handled {
		return nil
	}
	return actions
}

func (i *inputRenderNode) HandleScopedEvent(ev interaction.Event, _ *layout.LayoutNode) (bool, []update.Action) {
	if !i.focusable {
		return false, nil
	}
	if ev.Kind != interaction.EventKey {
		if ev.Kind == interaction.EventMouseDown {
			return false, nil
		}
		return false, nil
	}
	// Keep the lowered node immutable: input events emit semantic actions and
	// the next frame must come from rebuilding with caller-owned state.
	switch ev.Key {
	case interaction.KeyEnter:
		if i.onCommit.Kind == "" {
			return true, nil
		}
		return true, []update.Action{update.CoreSignalAction{Signal: withData(i.onCommit, i.value)}}
	case interaction.KeyEsc:
		if i.onCancel.Kind == "" {
			return true, nil
		}
		return true, []update.Action{update.CoreSignalAction{Signal: i.onCancel}}
	case interaction.KeyBackspace:
		if i.onChange.Kind == "" {
			return true, nil
		}
		return true, []update.Action{update.CoreSignalAction{Signal: withData(i.onChange, trimLastRune(i.value))}}
	case interaction.KeyRune:
		if ev.Rune == 0 || ev.Rune == '\n' || ev.Rune == '\r' {
			return false, nil
		}
		if i.onChange.Kind == "" {
			return true, nil
		}
		return true, []update.Action{update.CoreSignalAction{Signal: withData(i.onChange, i.value+string(ev.Rune))}}
	}
	// Consume non-text keys while the input is focused so parent navigation does
	// not steal focus or trigger unrelated list actions.
	return true, nil
}

func (i *inputRenderNode) CanFocus(*layout.LayoutNode) bool { return i.focusable }

func (i *inputRenderNode) renderLine(focused bool) string {
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

func withData(signal core.Signal, data any) core.Signal {
	next := signal
	next.Data = data
	return next
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
