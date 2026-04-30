package views

import (
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/update"
)

// ActionRenderNode is one focusable activatable primitive. It is behavior-oriented and
// can render arbitrary single-line content.
type ActionRenderNode struct {
	layout.Sized
	IntrinsicWidth int
	Focusable      bool
	AllowSpace     bool
	RenderContent  func(focused bool, width int) string
	OnActivate     func(trigger string) []update.Action
	OnCancel       func() []update.Action
	OnFocusAction  func() []update.Action
	OnBlurAction   func() []update.Action
}

func NewAction(intrinsicWidth int, focusable bool, allowSpace bool, renderContent func(bool, int) string, onActivate func(string) []update.Action, onCancel func() []update.Action) *ActionRenderNode {
	return &ActionRenderNode{
		Sized:          layout.Sized{Sizing: layout.Sizing{W: layout.SizeGrow, H: layout.SizeFit}},
		IntrinsicWidth: intrinsicWidth,
		Focusable:      focusable,
		AllowSpace:     allowSpace,
		RenderContent:  renderContent,
		OnActivate:     onActivate,
		OnCancel:       onCancel,
	}
}

func (a *ActionRenderNode) Measure(c geom.Constraints, _ *layout.LayoutContext) geom.Size {
	return a.ResolveSize(geom.Size{W: maxInt(0, a.IntrinsicWidth), H: 1}, c)
}

func (a *ActionRenderNode) Arrange(node *layout.LayoutNode, _ *layout.LayoutContext) layout.NodeLayout {
	return layout.NodeLayout{BorderRect: node.Slot, ContentRect: node.Slot, ViewportRect: node.Slot, ContentSize: node.MeasuredSize}
}

func (a *ActionRenderNode) Paint(p paint.Painter, node *layout.LayoutNode, ctx *layout.PaintContext) {
	if node.BorderRect.W <= 0 || a.RenderContent == nil {
		return
	}
	line := a.RenderContent(ctx.FocusedID == node.ID, node.BorderRect.W)
	p.Text(0, 0, padRight(trimToWidthRaw(line, node.BorderRect.W), node.BorderRect.W))
}

func (a *ActionRenderNode) HitTest(local geom.Point, _ *layout.LayoutNode) bool {
	return local.X >= 0 && local.Y == 0 && a.Focusable
}

func (a *ActionRenderNode) HandleEvent(ev interaction.Event, _ *layout.LayoutNode) []update.Action {
	handled, actions := a.HandleScopedEvent(ev, nil)
	if !handled {
		return nil
	}
	return actions
}

func (a *ActionRenderNode) HandleScopedEvent(ev interaction.Event, _ *layout.LayoutNode) (bool, []update.Action) {
	if !a.Focusable {
		return false, nil
	}
	switch ev.Kind {
	case interaction.EventMouseDown:
		return true, a.activate("mouse")
	case interaction.EventKey:
		if ev.Key == interaction.KeyEnter || (ev.Key == interaction.KeySpace && a.AllowSpace) {
			return true, a.activate(ev.Key.String())
		}
		if ev.Key == interaction.KeyEsc {
			if a.OnCancel == nil {
				return false, nil
			}
			actions := a.cancel()
			if len(actions) == 0 {
				return false, nil
			}
			return true, actions
		}
	}
	return false, nil
}

func (a *ActionRenderNode) CanFocus(*layout.LayoutNode) bool { return a.Focusable }

func (a *ActionRenderNode) OnFocus(*layout.LayoutNode) []update.Action {
	if a.OnFocusAction == nil {
		return nil
	}
	return a.OnFocusAction()
}

func (a *ActionRenderNode) OnBlur(*layout.LayoutNode) []update.Action {
	if a.OnBlurAction == nil {
		return nil
	}
	return a.OnBlurAction()
}

func (a *ActionRenderNode) activate(trigger string) []update.Action {
	if a.OnActivate == nil {
		return nil
	}
	return a.OnActivate(trigger)
}

func (a *ActionRenderNode) cancel() []update.Action {
	if a.OnCancel == nil {
		return nil
	}
	return a.OnCancel()
}
