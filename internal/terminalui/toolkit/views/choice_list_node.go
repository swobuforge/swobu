package views

import (
	"strings"

	"github.com/metrofun/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/update"
)

type ChoiceListAxis uint8

const (
	ChoiceListAxisHorizontal ChoiceListAxis = iota
	ChoiceListAxisVertical
)

// ChoiceListRenderNode is one selectable list primitive with keyboard and mouse
// selection behavior. Rendering is customizable per option.
type ChoiceListRenderNode struct {
	layout.Sized
	Items      []string
	Selected   int
	Axis       ChoiceListAxis
	Focusable  bool
	RenderItem func(label string, selected bool) string
	OnSelect   func(index int) []update.Action
}

func NewChoiceList(items []string, selected int, axis ChoiceListAxis, renderItem func(label string, selected bool) string, onSelect func(int) []update.Action) *ChoiceListRenderNode {
	cloned := append([]string(nil), items...)
	if selected < 0 {
		selected = 0
	}
	if selected >= len(cloned) {
		selected = maxInt(0, len(cloned)-1)
	}
	return &ChoiceListRenderNode{
		Sized:      layout.Sized{Sizing: layout.Sizing{W: layout.SizeGrow, H: layout.SizeFit}},
		Items:      cloned,
		Selected:   selected,
		Axis:       axis,
		Focusable:  true,
		RenderItem: renderItem,
		OnSelect:   onSelect,
	}
}

func NewChoiceListWithFocusable(items []string, selected int, axis ChoiceListAxis, focusable bool, renderItem func(label string, selected bool) string, onSelect func(int) []update.Action) *ChoiceListRenderNode {
	list := NewChoiceList(items, selected, axis, renderItem, onSelect)
	list.Focusable = focusable
	return list
}

func (l *ChoiceListRenderNode) Measure(c geom.Constraints, _ *layout.LayoutContext) geom.Size {
	return l.ResolveSize(geom.Size{W: runeLen(l.renderLine(false)), H: 1}, c)
}

func (l *ChoiceListRenderNode) Arrange(node *layout.LayoutNode, _ *layout.LayoutContext) layout.NodeLayout {
	return layout.NodeLayout{BorderRect: node.Slot, ContentRect: node.Slot, ViewportRect: node.Slot, ContentSize: node.MeasuredSize}
}

func (l *ChoiceListRenderNode) Paint(p paint.Painter, node *layout.LayoutNode, ctx *layout.PaintContext) {
	p.Text(0, 0, padRight(trimToWidthRaw(l.renderLine(ctx.FocusedID == node.ID), node.BorderRect.W), node.BorderRect.W))
}

func (l *ChoiceListRenderNode) HitTest(local geom.Point, _ *layout.LayoutNode) bool {
	return local.Y == 0 && local.X >= 0
}

func (l *ChoiceListRenderNode) HandleEvent(ev interaction.Event, node *layout.LayoutNode) []update.Action {
	handled, actions := l.HandleScopedEvent(ev, node)
	if !handled {
		return nil
	}
	return actions
}

func (l *ChoiceListRenderNode) HandleScopedEvent(ev interaction.Event, node *layout.LayoutNode) (bool, []update.Action) {
	switch ev.Kind {
	case interaction.EventMouseDown:
		return true, l.selectIndex(l.indexAt(ev.Pos.X - node.BorderRect.X + node.ScrollOffset.X))
	case interaction.EventKey:
		switch ev.Key {
		case interaction.KeyLeft:
			if l.Axis == ChoiceListAxisHorizontal {
				return true, l.selectIndex(l.Selected - 1)
			}
		case interaction.KeyRight:
			if l.Axis == ChoiceListAxisHorizontal {
				return true, l.selectIndex(l.Selected + 1)
			}
		case interaction.KeyShiftTab:
			if l.Axis == ChoiceListAxisHorizontal {
				return true, l.selectIndex(l.Selected - 1)
			}
		case interaction.KeyTab:
			if l.Axis == ChoiceListAxisHorizontal {
				return true, l.selectIndex(l.Selected + 1)
			}
		case interaction.KeyUp:
			if l.Axis == ChoiceListAxisVertical {
				return true, l.selectIndex(l.Selected - 1)
			}
		case interaction.KeyDown:
			if l.Axis == ChoiceListAxisVertical {
				return true, l.selectIndex(l.Selected + 1)
			}
		case interaction.KeyEnter:
			return true, l.selectIndex(l.Selected)
		}
	}
	return false, nil
}

func (l *ChoiceListRenderNode) CanFocus(*layout.LayoutNode) bool { return l.Focusable }

func (l *ChoiceListRenderNode) renderLine(focused bool) string {
	parts := make([]string, 0, len(l.Items))
	for i, label := range l.Items {
		parts = append(parts, l.renderItem(label, i == l.Selected))
	}
	line := strings.Join(parts, " ")
	if focused {
		return "> " + line
	}
	return "  " + line
}

func (l *ChoiceListRenderNode) renderItem(label string, selected bool) string {
	label = strings.TrimSpace(label)
	if l.RenderItem != nil {
		return l.RenderItem(label, selected)
	}
	if selected {
		return "[>" + label + "]"
	}
	return "[ " + label + " ]"
}

func (l *ChoiceListRenderNode) indexAt(localX int) int {
	if localX < 2 {
		return l.Selected
	}
	offset := 2
	for i, label := range l.Items {
		part := l.renderItem(label, i == l.Selected)
		width := runeLen(part)
		if localX >= offset && localX < offset+width {
			return i
		}
		offset += width + 1
	}
	return l.Selected
}

func (l *ChoiceListRenderNode) selectIndex(index int) []update.Action {
	if len(l.Items) == 0 {
		return nil
	}
	if index < 0 {
		index = 0
	}
	if index >= len(l.Items) {
		index = len(l.Items) - 1
	}
	if l.OnSelect == nil {
		return nil
	}
	return l.OnSelect(index)
}
