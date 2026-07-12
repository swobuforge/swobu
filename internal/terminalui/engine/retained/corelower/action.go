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

type actionRenderNode struct {
	layout.Sized
	label       string
	signal      core.Signal
	focusSignal core.Signal
	focusable   bool
}

func lowerAction(n core.Node, _ Env) (layout.RenderNode, error) {
	signal, err := firstSignal(n)
	if err != nil {
		return nil, err
	}
	return &actionRenderNode{
		Sized:       layout.Sized{Sizing: layout.Sizing{W: layout.SizeFit, H: layout.SizeFit}},
		label:       textmetrics.SanitizeTerminalText(n.ContentValue().Text),
		signal:      signal,
		focusSignal: firstFocusSignal(n),
		focusable:   n.InteractionValue().Focus.Mode != core.FocusNone,
	}, nil
}

func firstSignal(n core.Node) (core.Signal, error) {
	if signals := n.InteractionValue().Signals; len(signals) > 0 {
		for _, signal := range signals {
			if signal.Kind != "" {
				return signal, nil
			}
		}
	}
	if len(n.InteractionValue().FocusSignals) > 0 {
		return core.Signal{}, nil
	}
	if specs := n.ContractValue().Signals; len(specs) > 0 {
		for _, spec := range specs {
			if spec.Kind != "" {
				return core.Signal{Kind: spec.Kind}, nil
			}
		}
	}
	if n.InteractionValue().Focus.Mode == core.FocusNone {
		return core.Signal{}, nil
	}
	return core.Signal{}, fmt.Errorf("action node has no signal")
}

func firstFocusSignal(n core.Node) core.Signal {
	if signals := n.InteractionValue().FocusSignals; len(signals) > 0 {
		return signals[0]
	}
	return core.Signal{}
}

func (a *actionRenderNode) Measure(c geom.Constraints, ctx *layout.LayoutContext) geom.Size {
	return a.ResolveSize(geom.Size{W: textmetrics.Width(a.renderLine(false)), H: 1}, c)
}

func (a *actionRenderNode) Arrange(node *layout.LayoutNode, ctx *layout.LayoutContext) layout.NodeLayout {
	return layout.NodeLayout{
		BorderRect:   node.Slot,
		ContentRect:  node.Slot,
		ViewportRect: node.Slot,
		ContentSize:  node.MeasuredSize,
	}
}

func (a *actionRenderNode) Paint(p paint.Painter, node *layout.LayoutNode, ctx *layout.PaintContext) {
	if node.BorderRect.W <= 0 {
		return
	}
	line := a.renderLine(ctx.FocusedID == node.ID)
	p.Text(0, 0, textmetrics.PadRight(textmetrics.TrimToWidthRaw(line, node.BorderRect.W), node.BorderRect.W))
}

func (a *actionRenderNode) HitTest(local geom.Point, _ *layout.LayoutNode) bool {
	return local.Y == 0 && local.X >= 0
}

func (a *actionRenderNode) HandleEvent(ev interaction.Event, _ *layout.LayoutNode) []update.Action {
	if !a.focusable || a.signal.Kind == "" {
		return nil
	}
	if ev.Kind == interaction.EventKey && ev.Key == interaction.KeyEnter {
		return []update.Action{update.CoreSignalAction{Signal: a.signal}}
	}
	if ev.Kind == interaction.EventMouseDown {
		return []update.Action{update.CoreSignalAction{Signal: a.signal}}
	}
	return nil
}

func (a *actionRenderNode) CanFocus(*layout.LayoutNode) bool { return a.focusable }

func (a *actionRenderNode) OnFocus(*layout.LayoutNode) []update.Action {
	if a.focusSignal.Kind == "" {
		return nil
	}
	return []update.Action{update.CoreSignalAction{Signal: a.focusSignal}}
}

func (a *actionRenderNode) OnBlur(*layout.LayoutNode) []update.Action {
	return nil
}

func (a *actionRenderNode) renderLine(focused bool) string {
	if !a.focusable {
		return a.label
	}
	marker := "  "
	if focused {
		marker = "> "
	}
	return marker + a.label
}
