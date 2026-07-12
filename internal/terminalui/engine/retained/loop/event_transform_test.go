package loop

import (
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

type bubbleEventRoot struct {
	hookSeen   *[]interaction.Key
	childSeen  *[]interaction.Key
	parentSeen *[]interaction.Key
}

func (r bubbleEventRoot) BuildRenderNode(ctx *retained.Context[struct{}]) layout.RenderNode {
	child := retained.Build(func(_ *retained.Context[struct{}]) retained.ViewSpec[struct{}] {
		return retained.View[struct{}](func(ctx *retained.Context[struct{}]) layout.RenderNode {
			retained.UseEvent(ctx, func(ev interaction.Event) *interaction.Event {
				*r.hookSeen = append(*r.hookSeen, ev.Key)
				if ev.Key == interaction.KeyEsc {
					next := ev
					next.Key = interaction.KeyEnter
					return &next
				}
				return &ev
			})
			return bubbleLeafNode{seen: r.childSeen}
		})
	})
	return bubbleParentNode{child: retained.Materialize(ctx, child), seen: r.parentSeen}
}

type bubbleParentNode struct {
	child layout.RenderNode
	seen  *[]interaction.Key
}

func (p bubbleParentNode) Measure(c geom.Constraints, _ *layout.LayoutContext) geom.Size {
	if p.child != nil {
		return p.child.Measure(c, &layout.LayoutContext{})
	}
	return geom.ClampSize(geom.Size{W: 1, H: 1}, c)
}

func (p bubbleParentNode) Arrange(node *layout.LayoutNode, _ *layout.LayoutContext) layout.NodeLayout {
	out := layout.NodeLayout{
		BorderRect:   node.Slot,
		ContentRect:  node.Slot,
		ViewportRect: node.Slot,
		ContentSize:  node.MeasuredSize,
	}
	if p.child != nil {
		out.ChildSlots = []layout.ChildSlot{{
			Spec: layout.ChildSpec{Hint: "child", RenderNode: p.child},
			Rect: node.Slot,
		}}
	}
	return out
}

func (p bubbleParentNode) Paint(paint.Painter, *layout.LayoutNode, *layout.PaintContext) {}

func (p bubbleParentNode) HandleEventTransform(ev interaction.Event, _ *layout.LayoutNode) (*interaction.Event, []update.Action) {
	*p.seen = append(*p.seen, ev.Key)
	return nil, nil
}

func (p bubbleParentNode) VisitChildren(visit func(hint string, child layout.RenderNode)) {
	if p.child != nil {
		visit("child", p.child)
	}
}

func (p bubbleParentNode) MapChildren(rewrite func(hint string, child layout.RenderNode) layout.RenderNode) layout.RenderNode {
	clone := p
	if clone.child != nil {
		clone.child = rewrite("child", clone.child)
	}
	return clone
}

type bubbleLeafNode struct {
	seen *[]interaction.Key
}

func (b bubbleLeafNode) Measure(c geom.Constraints, _ *layout.LayoutContext) geom.Size {
	return geom.ClampSize(geom.Size{W: 1, H: 1}, c)
}

func (b bubbleLeafNode) Arrange(node *layout.LayoutNode, _ *layout.LayoutContext) layout.NodeLayout {
	return layout.NodeLayout{
		BorderRect:   node.Slot,
		ContentRect:  node.Slot,
		ViewportRect: node.Slot,
		ContentSize:  node.MeasuredSize,
	}
}

func (b bubbleLeafNode) Paint(paint.Painter, *layout.LayoutNode, *layout.PaintContext) {}

func (b bubbleLeafNode) HandleEventTransform(ev interaction.Event, _ *layout.LayoutNode) (*interaction.Event, []update.Action) {
	*b.seen = append(*b.seen, ev.Key)
	return &ev, nil
}

func TestDispatchEvent_TransformedEventBubblesToParent(t *testing.T) {
	hookSeen := make([]interaction.Key, 0, 1)
	childSeen := make([]interaction.Key, 0, 1)
	parentSeen := make([]interaction.Key, 0, 1)
	rt := New(struct{}{}, func(*struct{}, update.Action) []update.Effect { return nil })
	rt.Rebuild(asView(bubbleEventRoot{
		hookSeen:   &hookSeen,
		childSeen:  &childSeen,
		parentSeen: &parentSeen,
	}), geom.Rect{W: 4, H: 4})

	if rt.Tree == nil || len(rt.Tree.Kids) == 0 {
		t.Fatal("expected retained tree with one child")
	}
	rt.Focused = rt.Tree.Kids[0]

	if !rt.DispatchEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEsc}) {
		t.Fatal("expected event to be handled after parent consumed the transformed event")
	}
	if got, want := hookSeen, []interaction.Key{interaction.KeyEsc}; !reflect.DeepEqual(got, want) {
		t.Fatalf("hook keys = %#v, want %#v", got, want)
	}
	if got, want := childSeen, []interaction.Key{interaction.KeyEnter}; !reflect.DeepEqual(got, want) {
		t.Fatalf("child keys = %#v, want %#v", got, want)
	}
	if got, want := parentSeen, []interaction.Key{interaction.KeyEnter}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parent keys = %#v, want %#v", got, want)
	}
}
