package retained

import (
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

type eventHookProbe struct {
	seen *[]interaction.Key
}

func (p eventHookProbe) Measure(c geom.Constraints, _ *layout.LayoutContext) geom.Size {
	return geom.ClampSize(geom.Size{W: 1, H: 1}, c)
}

func (p eventHookProbe) Arrange(node *layout.LayoutNode, _ *layout.LayoutContext) layout.NodeLayout {
	return layout.NodeLayout{
		BorderRect:   node.Slot,
		ContentRect:  node.Slot,
		ViewportRect: node.Slot,
		ContentSize:  node.MeasuredSize,
	}
}

func (p eventHookProbe) Paint(_ paint.Painter, _ *layout.LayoutNode, _ *layout.PaintContext) {}

func (p eventHookProbe) HandleEventTransform(ev interaction.Event, _ *layout.LayoutNode) (*interaction.Event, []update.Action) {
	*p.seen = append(*p.seen, ev.Key)
	return nil, nil
}

func TestUseEvent_TransformsBeforeWrappedNode(t *testing.T) {
	hookSeen := make([]interaction.Key, 0, 1)
	leafSeen := make([]interaction.Key, 0, 1)
	root := View[struct{}](func(ctx *Context[struct{}]) RenderNode {
		UseEvent(ctx, func(ev interaction.Event) *interaction.Event {
			hookSeen = append(hookSeen, ev.Key)
			if ev.Key == interaction.KeyEsc {
				next := ev
				next.Key = interaction.KeyEnter
				return &next
			}
			return &ev
		})
		return eventHookProbe{seen: &leafSeen}
	})

	node := BuildViewRootNode(root, mapScope{m: make(map[string]any)}, func(update.Action) {}, func(update.Action) {}, func() struct{} { return struct{}{} })
	transformer, ok := node.(interaction.EventTransformer)
	if !ok {
		t.Fatalf("type = %T, want interaction.EventTransformer", node)
	}

	next, actions := transformer.HandleEventTransform(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEsc}, nil)
	if next != nil {
		t.Fatalf("next = %#v, want nil after wrapped node consumed the transformed event", next)
	}
	if len(actions) != 0 {
		t.Fatalf("actions = %d, want 0", len(actions))
	}
	if got, want := hookSeen, []interaction.Key{interaction.KeyEsc}; !reflect.DeepEqual(got, want) {
		t.Fatalf("hook keys = %#v, want %#v", got, want)
	}
	if got, want := leafSeen, []interaction.Key{interaction.KeyEnter}; !reflect.DeepEqual(got, want) {
		t.Fatalf("leaf keys = %#v, want %#v", got, want)
	}
}

func TestUseEvent_NilConsumesBeforeWrappedNode(t *testing.T) {
	hookSeen := make([]interaction.Key, 0, 1)
	leafSeen := make([]interaction.Key, 0, 1)
	root := View[struct{}](func(ctx *Context[struct{}]) RenderNode {
		UseEvent(ctx, func(ev interaction.Event) *interaction.Event {
			hookSeen = append(hookSeen, ev.Key)
			return nil
		})
		return eventHookProbe{seen: &leafSeen}
	})

	node := BuildViewRootNode(root, mapScope{m: make(map[string]any)}, func(update.Action) {}, func(update.Action) {}, func() struct{} { return struct{}{} })
	transformer, ok := node.(interaction.EventTransformer)
	if !ok {
		t.Fatalf("type = %T, want interaction.EventTransformer", node)
	}

	next, actions := transformer.HandleEventTransform(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEsc}, nil)
	if next != nil {
		t.Fatalf("next = %#v, want nil when hook consumes the event", next)
	}
	if len(actions) != 0 {
		t.Fatalf("actions = %d, want 0", len(actions))
	}
	if got, want := hookSeen, []interaction.Key{interaction.KeyEsc}; !reflect.DeepEqual(got, want) {
		t.Fatalf("hook keys = %#v, want %#v", got, want)
	}
	if len(leafSeen) != 0 {
		t.Fatalf("leaf keys = %#v, want none because hook consumed the event", leafSeen)
	}
}
