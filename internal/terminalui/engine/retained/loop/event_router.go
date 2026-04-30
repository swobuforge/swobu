package loop

import (
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/update"
)

func (loop *AppLoop[M]) DispatchEvent(ev interaction.Event) bool {
	if loop.Tree == nil {
		return false
	}
	target := loop.pickEventTarget(ev)
	if target == nil {
		return false
	}
	if ev.Kind == interaction.EventMouseDown {
		loop.SetFocus(nearestFocusable(target))
	}
	return loop.dispatchWithPropagation(target, ev)
}

func (loop *AppLoop[M]) dispatchWithPropagation(target *layout.LayoutNode, ev interaction.Event) bool {
	for current := target; current != nil; current = current.Parent {
		if handled, actions, ok := handleNodeEvent(current, ev); ok {
			if len(actions) > 0 {
				loop.Dispatch(actions)
			}
			if handled {
				return true
			}
		}
	}
	return false
}

func handleNodeEvent(node *layout.LayoutNode, ev interaction.Event) (handled bool, actions []update.Action, ok bool) {
	if node == nil {
		return false, nil, false
	}
	if handler, has := node.RenderNode.(interaction.ScopedEventHandler); has {
		handled, actions = handler.HandleScopedEvent(ev, node)
		return handled, actions, true
	}
	if handler, has := node.RenderNode.(interaction.EventHandler); has {
		actions = handler.HandleEvent(ev, node)
		return len(actions) > 0, actions, true
	}
	return false, nil, false
}

func (loop *AppLoop[M]) pickEventTarget(ev interaction.Event) *layout.LayoutNode {
	if ev.Kind == interaction.EventKey {
		if loop.Focused != nil {
			return loop.Focused
		}
		return loop.Tree
	}
	return hitTestNode(loop.Tree, ev.Pos)
}

func hitTestNode(node *layout.LayoutNode, point geom.Point) *layout.LayoutNode {
	if node == nil || node.ClipRect.Empty() || !node.ClipRect.Contains(point) {
		return nil
	}
	children := stableKids(node.Kids)
	for i := len(children) - 1; i >= 0; i-- {
		if hit := hitTestNode(children[i], point); hit != nil {
			return hit
		}
	}
	hittable, ok := node.RenderNode.(interaction.Hittable)
	if !ok {
		return nil
	}
	local := geom.Point{
		X: point.X - node.BorderRect.X + node.ScrollOffset.X,
		Y: point.Y - node.BorderRect.Y + node.ScrollOffset.Y,
	}
	if hittable.HitTest(local, node) {
		return node
	}
	return nil
}
