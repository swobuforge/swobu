package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

// InputRenderNode is one editable text primitive with explicit change/commit/cancel
// behavior.
type InputRenderNode struct {
	layout.Sized
	Label      string
	Value      string
	EmptyValue string
	Policy     RowLayoutPolicy
	OnChange   func(string) []update.Action
	OnCommit   func(string) []update.Action
	OnCancel   func() []update.Action
}

func NewInput(label, value, emptyValue string, policy RowLayoutPolicy, onChange func(string) []update.Action, onCommit func(string) []update.Action, onCancel func() []update.Action) *InputRenderNode {
	return &InputRenderNode{
		Sized:      layout.Sized{Sizing: layout.Sizing{W: layout.SizeGrow, H: layout.SizeFit}},
		Label:      strings.TrimSpace(label),
		Value:      value,
		EmptyValue: strings.TrimSpace(emptyValue),
		Policy:     policy,
		OnChange:   onChange,
		OnCommit:   onCommit,
		OnCancel:   onCancel,
	}
}

func (i *InputRenderNode) Measure(c geom.Constraints, _ *layout.LayoutContext) geom.Size {
	return i.ResolveSize(geom.Size{W: i.parts(false).intrinsicWidth(), H: 1}, c)
}

func (i *InputRenderNode) Arrange(node *layout.LayoutNode, _ *layout.LayoutContext) layout.NodeLayout {
	return layout.NodeLayout{BorderRect: node.Slot, ContentRect: node.Slot, ViewportRect: node.Slot, ContentSize: node.MeasuredSize}
}

func (i *InputRenderNode) Paint(p paint.Painter, node *layout.LayoutNode, ctx *layout.PaintContext) {
	if node.BorderRect.W <= 0 {
		return
	}
	policy := i.Policy
	if policy.MaxLabelFractionDiv <= 0 {
		policy = DefaultRowLayoutPolicy()
	}
	p.Text(0, 0, i.parts(ctx.FocusedID == node.ID).render(node.BorderRect.W, policy))
}

func (i *InputRenderNode) HitTest(local geom.Point, _ *layout.LayoutNode) bool {
	return local.X >= 0 && local.Y == 0
}

func (i *InputRenderNode) HandleEvent(ev interaction.Event, _ *layout.LayoutNode) []update.Action {
	handled, actions := i.HandleScopedEvent(ev, nil)
	if !handled {
		return nil
	}
	return actions
}

func (i *InputRenderNode) HandleScopedEvent(ev interaction.Event, _ *layout.LayoutNode) (bool, []update.Action) {
	if ev.Kind != interaction.EventKey {
		if ev.Kind == interaction.EventMouseDown {
			return false, nil
		}
		return false, nil
	}
	switch ev.Key {
	case interaction.KeyEnter:
		return true, i.commit()
	case interaction.KeyEsc:
		return true, i.cancel()
	case interaction.KeyBackspace:
		return true, i.change(trimLastRune(i.Value))
	case interaction.KeyRune:
		if ev.Rune == 0 || ev.Rune == '\n' || ev.Rune == '\r' {
			return false, nil
		}
		return true, i.change(i.Value + string(ev.Rune))
	}
	return false, nil
}

func (i *InputRenderNode) CanFocus(*layout.LayoutNode) bool { return true }

func (i *InputRenderNode) parts(focused bool) rowParts {
	value := i.Value
	if value == "" {
		// Placeholder text is guidance, not editable value. Match input semantics:
		// when focused and empty, show caret on empty content (no placeholder text).
		if !focused {
			value = i.EmptyValue
		}
	}
	return newRowParts(i.Label, value+"_", "save ↵", focused)
}

func (i *InputRenderNode) change(value string) []update.Action {
	i.Value = value
	if i.OnChange == nil {
		return nil
	}
	return i.OnChange(value)
}

func (i *InputRenderNode) commit() []update.Action {
	if i.OnCommit == nil {
		return nil
	}
	return i.OnCommit(i.Value)
}

func (i *InputRenderNode) cancel() []update.Action {
	if i.OnCancel == nil {
		return nil
	}
	return i.OnCancel()
}
