package loop

import (
	"context"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
)

type testAction struct{ N int }
type testEffect struct{ N int }

type renderNodeBuilder interface {
	BuildRenderNode(*view.Context[struct{}]) layout.RenderNode
}

type statelessBuilderAdapter struct {
	builder renderNodeBuilder
}

func (s statelessBuilderAdapter) BuildView(ctx *view.Context[struct{}]) view.ViewSpec[struct{}] {
	return view.View[struct{}](func(ctx *view.Context[struct{}]) layout.RenderNode {
		return s.builder.BuildRenderNode(ctx)
	})
}

func (s statelessBuilderAdapter) OnMountEffects() []update.Effect {
	if hooks, ok := s.builder.(interface{ OnMountEffects() []update.Effect }); ok {
		return hooks.OnMountEffects()
	}
	return nil
}

func (s statelessBuilderAdapter) OnUnmountEffects() []update.Effect {
	if hooks, ok := s.builder.(interface{ OnUnmountEffects() []update.Effect }); ok {
		return hooks.OnUnmountEffects()
	}
	return nil
}

func asView(builder renderNodeBuilder) view.ViewSpec[struct{}] {
	adapter := statelessBuilderAdapter{builder: builder}
	return view.BuildWithLifecycle[struct{}](adapter.BuildView, adapter.OnMountEffects, adapter.OnUnmountEffects)
}

func (e testEffect) Execute(ctx context.Context) []update.Action {
	return nil
}

func TestDispatch_ReducesThenFiresEffects(t *testing.T) {
	type model struct{ Sum int }
	var effects []int
	rt := New(model{}, func(m *model, a update.Action) []update.Effect {
		got := a.(testAction)
		m.Sum += got.N
		effects = append(effects, got.N+1)
		return []update.Effect{testEffect{N: got.N + 1}}
	})
	ctx := context.Background()
	rt.SetContext(ctx)
	rt.invalidated = false
	rt.Dispatch([]update.Action{testAction{N: 2}})
	if rt.Model.Sum != 2 {
		t.Fatalf("sum = %d, want 2", rt.Model.Sum)
	}
	if len(effects) != 1 || effects[0] != 3 {
		t.Fatalf("effects = %#v, want [3]", effects)
	}
	if !rt.invalidated {
		t.Fatalf("runtime should be invalidated")
	}
	// Drain the followUp channel
	select {
	case actions := <-rt.FollowUp():
		if len(actions) != 0 {
			t.Fatalf("followUp actions = %d, want 0", len(actions))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no followUp received")
	}
}

type childComponent struct {
	init string
}

func (c childComponent) BuildRenderNode(ctx *view.Context[struct{}]) layout.RenderNode {
	value, _ := view.UseState(ctx, func() string { return c.init })
	return layout.NewText(value)
}

type namedRoot struct {
	order []string
}

func (r namedRoot) BuildRenderNode(ctx *view.Context[struct{}]) layout.RenderNode {
	children := make([]layout.FlowChild, 0, len(r.order))
	for _, item := range r.order {
		children = append(children, layout.FlowChild{RenderNode: view.Materialize(ctx, view.Named(item, asView(childComponent{init: item})))})
	}
	return layout.NewColumn(children...)
}

func TestRebuild_PreservesNamedChildStateAcrossRebuilds(t *testing.T) {
	rt := New(struct{}{}, func(*struct{}, update.Action) []update.Effect { return nil })
	rt.Rebuild(asView(namedRoot{order: []string{"a", "b"}}), geom.Rect{W: 12, H: 4})
	idA := rt.Tree.Kids[0].ID
	rt.locals.Scope(idA).Set(0, "persisted")

	rt.Invalidate()
	rt.Rebuild(asView(namedRoot{order: []string{"b", "a"}}), geom.Rect{W: 12, H: 4})

	if got := rt.Tree.Kids[1].ID; got != idA {
		t.Fatalf("child a identity = %d, want %d", got, idA)
	}
}

type focusRoot struct{}

func (focusRoot) BuildRenderNode(_ *view.Context[struct{}]) layout.RenderNode {
	return layout.NewColumn(
		layout.FlowChild{RenderNode: focusLeaf{id: "first"}},
		layout.FlowChild{RenderNode: focusLeaf{id: "second"}},
	)
}

type focusLeaf struct {
	id string
}

func (f focusLeaf) Measure(c geom.Constraints, _ *layout.LayoutContext) geom.Size {
	return geom.ClampSize(geom.Size{W: 1, H: 1}, c)
}

func (f focusLeaf) Arrange(node *layout.LayoutNode, _ *layout.LayoutContext) layout.NodeLayout {
	return layout.NodeLayout{
		BorderRect:   node.Slot,
		ContentRect:  node.Slot,
		ViewportRect: node.Slot,
		ContentSize:  geom.Size{W: node.Slot.W, H: node.Slot.H},
	}
}

func (f focusLeaf) Paint(paint.Painter, *layout.LayoutNode, *layout.PaintContext) {}

func (f focusLeaf) HitTest(local geom.Point, _ *layout.LayoutNode) bool {
	return local.X >= 0 && local.Y >= 0 && local.X < 1 && local.Y < 1
}

func (f focusLeaf) CanFocus(*layout.LayoutNode) bool { return true }

type scrollFocusRoot struct {
	rows int
}

func (r scrollFocusRoot) BuildRenderNode(_ *view.Context[struct{}]) layout.RenderNode {
	children := make([]layout.FlowChild, 0, r.rows)
	for i := 0; i < r.rows; i++ {
		children = append(children, layout.FlowChild{RenderNode: focusLeaf{id: "row"}})
	}
	return layout.NewScrollY(layout.NewColumn(children...))
}

type overlayRoot struct {
	events *[]string
}

func (r overlayRoot) BuildRenderNode(_ *view.Context[struct{}]) layout.RenderNode {
	return layout.NewOverlay(
		overlayLeaf{name: "base", events: r.events},
		layout.OverlayChild{RenderNode: overlayLeaf{name: "top", events: r.events},
			Placement: layout.Placement{
				Ref:    layout.RefSlot,
				Anchor: layout.AnchorTopLeft,
			},
			Z: 10,
		},
	)
}

type overlayLeaf struct {
	name   string
	events *[]string
}

func (o overlayLeaf) Measure(c geom.Constraints, _ *layout.LayoutContext) geom.Size {
	return geom.ClampSize(geom.Size{W: 1, H: 1}, c)
}

func (o overlayLeaf) Arrange(node *layout.LayoutNode, _ *layout.LayoutContext) layout.NodeLayout {
	return layout.NodeLayout{
		BorderRect:   node.Slot,
		ContentRect:  node.Slot,
		ViewportRect: node.Slot,
		ContentSize:  geom.Size{W: node.Slot.W, H: node.Slot.H},
	}
}

func (o overlayLeaf) Paint(paint.Painter, *layout.LayoutNode, *layout.PaintContext) {}

func (o overlayLeaf) HitTest(local geom.Point, _ *layout.LayoutNode) bool {
	return local.X >= 0 && local.Y >= 0
}

func (o overlayLeaf) HandleEvent(ev interaction.Event, _ *layout.LayoutNode) []update.Action {
	if ev.Kind == interaction.EventMouseDown {
		*o.events = append(*o.events, o.name)
		return nil
	}
	return nil
}

type lifecycleRoot struct {
	show   bool
	events *[]string
}

func (r lifecycleRoot) BuildRenderNode(ctx *view.Context[struct{}]) layout.RenderNode {
	children := []layout.FlowChild{}
	if !r.show {
		return layout.NewColumn(children...)
	}
	children = append(children, layout.FlowChild{RenderNode: view.Materialize(ctx, view.Named("life", asView(lifecycleComponent{events: r.events})))})
	return layout.NewColumn(children...)
}

type lifecycleComponent struct {
	events *[]string
}

func (l lifecycleComponent) BuildRenderNode(_ *view.Context[struct{}]) layout.RenderNode {
	return lifecycleLeaf(l)
}

func (lifecycleComponent) OnMountEffects() []update.Effect {
	return []update.Effect{viewLifecycleEffect{Label: "mount"}}
}

func (lifecycleComponent) OnUnmountEffects() []update.Effect {
	return []update.Effect{viewLifecycleEffect{Label: "unmount"}}
}

type lifecycleLeaf struct {
	events *[]string
}

func (l lifecycleLeaf) Measure(c geom.Constraints, _ *layout.LayoutContext) geom.Size {
	return geom.ClampSize(geom.Size{W: 1, H: 1}, c)
}

func (l lifecycleLeaf) Arrange(node *layout.LayoutNode, _ *layout.LayoutContext) layout.NodeLayout {
	return layout.NodeLayout{
		BorderRect:   node.Slot,
		ContentRect:  node.Slot,
		ViewportRect: node.Slot,
		ContentSize:  geom.Size{W: node.Slot.W, H: node.Slot.H},
	}
}

func (l lifecycleLeaf) Paint(paint.Painter, *layout.LayoutNode, *layout.PaintContext) {}

func (l lifecycleLeaf) OnMount(*layout.LayoutNode) []update.Action {
	*l.events = append(*l.events, "mount")
	return nil
}

func (l lifecycleLeaf) OnUnmount(*layout.LayoutNode) []update.Action {
	*l.events = append(*l.events, "unmount")
	return nil
}

type focusEventLeaf struct {
	events *[]string
}

func (f focusEventLeaf) Measure(c geom.Constraints, _ *layout.LayoutContext) geom.Size {
	return geom.ClampSize(geom.Size{W: 1, H: 1}, c)
}

func (f focusEventLeaf) Arrange(node *layout.LayoutNode, _ *layout.LayoutContext) layout.NodeLayout {
	return layout.NodeLayout{
		BorderRect:   node.Slot,
		ContentRect:  node.Slot,
		ViewportRect: node.Slot,
		ContentSize:  geom.Size{W: node.Slot.W, H: node.Slot.H},
	}
}

func (f focusEventLeaf) Paint(paint.Painter, *layout.LayoutNode, *layout.PaintContext) {}

func (f focusEventLeaf) HitTest(local geom.Point, _ *layout.LayoutNode) bool {
	return local.X >= 0 && local.Y >= 0
}

func (f focusEventLeaf) CanFocus(*layout.LayoutNode) bool { return true }

func (f focusEventLeaf) OnFocus(*layout.LayoutNode) []update.Action {
	*f.events = append(*f.events, "focus")
	return nil
}

func (f focusEventLeaf) OnBlur(*layout.LayoutNode) []update.Action {
	*f.events = append(*f.events, "blur")
	return nil
}

type focusEventRoot struct {
	events *[]string
	show   bool
}

func (r focusEventRoot) BuildRenderNode(_ *view.Context[struct{}]) layout.RenderNode {
	if !r.show {
		return layout.NewText("hidden")
	}
	return focusEventLeaf{events: r.events}
}

type focusRepairRoot struct {
	showMiddle bool
}

func (r focusRepairRoot) BuildRenderNode(ctx *view.Context[struct{}]) layout.RenderNode {
	children := []layout.FlowChild{
		{RenderNode: view.Materialize(ctx, view.Named("first", asView(focusLeafComponent{id: "first"})))},
	}
	if r.showMiddle {
		children = append(children, layout.FlowChild{RenderNode: view.Materialize(ctx, view.Named("middle", asView(focusLeafComponent{id: "middle"})))})
	}
	children = append(children, layout.FlowChild{RenderNode: view.Materialize(ctx, view.Named("last", asView(focusLeafComponent{id: "last"})))})
	return layout.NewColumn(children...)
}

type focusLeafComponent struct {
	id string
}

func (c focusLeafComponent) BuildRenderNode(_ *view.Context[struct{}]) layout.RenderNode {
	return focusLeaf(c)
}

type focusKeyRoot struct {
	showTarget bool
}

func (r focusKeyRoot) BuildRenderNode(ctx *view.Context[struct{}]) layout.RenderNode {
	children := []layout.FlowChild{
		{RenderNode: view.Materialize(ctx, view.Named("other", asView(focusLeafComponent{id: "other"})))},
	}
	if r.showTarget {
		children = append(children, layout.FlowChild{RenderNode: view.Materialize(ctx, view.Named("target", asView(focusLeafComponent{id: "target"})))})
	}
	return layout.NewColumn(children...)
}

type viewLifecycleEffect struct {
	Label string
}

func (e viewLifecycleEffect) Execute(ctx context.Context) []update.Action {
	return []update.Action{viewLifecycleAction(e)}
}

type viewLifecycleAction struct{ Label string }

type lifecycleHookRoot struct {
	show bool
}

func (r lifecycleHookRoot) BuildRenderNode(ctx *view.Context[struct{}]) layout.RenderNode {
	if !r.show {
		return layout.NewColumn()
	}
	return layout.NewColumn(layout.FlowChild{RenderNode: view.Materialize(ctx, view.Named("hook", asView(lifecycleHookView{})))})
}

type lifecycleHookView struct{}

func (lifecycleHookView) BuildRenderNode(_ *view.Context[struct{}]) layout.RenderNode {
	return layout.NewText("hook")
}

func (lifecycleHookView) OnMountEffects() []update.Effect {
	return []update.Effect{viewLifecycleEffect{Label: "mount"}}
}

func (lifecycleHookView) OnUnmountEffects() []update.Effect {
	return []update.Effect{viewLifecycleEffect{Label: "unmount"}}
}

func TestDispatchEvent_ClickFocusesTargetAndTabNeedsRenderNodeHandler(t *testing.T) {
	rt := New(struct{}{}, func(*struct{}, update.Action) []update.Effect { return nil })
	rt.Rebuild(asView(focusRoot{}), geom.Rect{W: 4, H: 4})

	rt.DispatchEvent(interaction.Event{Kind: interaction.EventMouseDown, Pos: geom.Point{X: 0, Y: 1}})
	if rt.Focused == nil || rt.Focused.ID != rt.Tree.Kids[1].ID {
		t.Fatalf("focused after click = %#v, want second child", rt.Focused)
	}

	rt.DispatchEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyTab})
	if rt.Focused == nil || rt.Focused.ID != rt.Tree.Kids[1].ID {
		t.Fatalf("focused after tab = %#v, want unchanged focused node without tab handler", rt.Focused)
	}

	rt.DispatchEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyShiftTab})
	if rt.Focused == nil || rt.Focused.ID != rt.Tree.Kids[1].ID {
		t.Fatalf("focused after shift+tab = %#v, want unchanged focused node without tab handler", rt.Focused)
	}
}

func TestDispatchEvent_DefaultRuntimeHasNoKeyNavigationPolicy(t *testing.T) {
	rt := New(struct{}{}, func(*struct{}, update.Action) []update.Effect { return nil })
	rt.Rebuild(asView(focusRoot{}), geom.Rect{W: 4, H: 4})
	if rt.Focused == nil || rt.Focused.ID != rt.Tree.Kids[0].ID {
		t.Fatalf("initial focused = %#v, want first focusable child", rt.Focused)
	}

	rt.DispatchEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyDown})
	if rt.Focused == nil || rt.Focused.ID != rt.Tree.Kids[0].ID {
		t.Fatalf("focused after key down = %#v, want unchanged focused node without injected policy", rt.Focused)
	}
}

func TestDispatch_FocusMoveRuntimeActionAdvancesFocus(t *testing.T) {
	rt := New(struct{}{}, func(*struct{}, update.Action) []update.Effect { return nil })
	rt.Rebuild(asView(focusRoot{}), geom.Rect{W: 4, H: 4})
	if rt.Focused == nil || rt.Focused.ID != rt.Tree.Kids[0].ID {
		t.Fatalf("initial focused = %#v, want first focusable child", rt.Focused)
	}

	rt.Dispatch([]update.Action{interaction.FocusMoveAction{Move: interaction.FocusMoveNext}})
	if rt.Focused == nil || rt.Focused.ID != rt.Tree.Kids[1].ID {
		t.Fatalf("focused after focus-next action = %#v, want second focusable child", rt.Focused)
	}

	rt.Dispatch([]update.Action{interaction.FocusMoveAction{Move: interaction.FocusMoveNext}})
	if rt.Focused == nil || rt.Focused.ID != rt.Tree.Kids[0].ID {
		t.Fatalf("focused after second focus-next action = %#v, want wrapped first focusable child", rt.Focused)
	}
}

func TestRebuild_RunsViewLifecycleEffects(t *testing.T) {
	rt := New(struct{}{}, func(*struct{}, update.Action) []update.Effect { return nil })
	ctx := context.Background()
	rt.SetContext(ctx)

	rt.Rebuild(asView(lifecycleHookRoot{show: true}), geom.Rect{W: 10, H: 3})
	select {
	case actions := <-rt.FollowUp():
		if len(actions) == 0 {
			t.Fatalf("expected mount lifecycle actions")
		}
		if a, ok := actions[0].(viewLifecycleAction); !ok || a.Label != "mount" {
			t.Fatalf("mount action = %#v, want mount", actions[0])
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no mount effect received")
	}

	rt.Invalidate()
	rt.Rebuild(asView(lifecycleHookRoot{show: false}), geom.Rect{W: 10, H: 3})
	select {
	case actions := <-rt.FollowUp():
		if len(actions) == 0 {
			t.Fatalf("expected unmount lifecycle actions")
		}
		if a, ok := actions[0].(viewLifecycleAction); !ok || a.Label != "unmount" {
			t.Fatalf("unmount action = %#v, want unmount", actions[0])
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no unmount effect received")
	}
}

func TestDispatchEvent_HitTestsTopmostChildFirst(t *testing.T) {
	var events []string
	rt := New(struct{}{}, func(*struct{}, update.Action) []update.Effect { return nil })
	rt.Rebuild(asView(overlayRoot{events: &events}), geom.Rect{W: 3, H: 3})

	rt.DispatchEvent(interaction.Event{Kind: interaction.EventMouseDown, Pos: geom.Point{X: 0, Y: 0}})
	if len(events) != 1 || events[0] != "top" {
		t.Fatalf("events = %#v, want topmost child hit", events)
	}
}

func TestRebuild_RunsLifecycleMountAndUnmount(t *testing.T) {
	rt := New(struct{}{}, func(*struct{}, update.Action) []update.Effect { return nil })
	ctx := context.Background()
	rt.SetContext(ctx)

	rt.Rebuild(asView(lifecycleRoot{show: true}), geom.Rect{W: 3, H: 1})
	select {
	case actions := <-rt.FollowUp():
		if len(actions) == 0 {
			t.Fatalf("expected mount lifecycle actions")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no mount effect received")
	}

	rt.Invalidate()
	rt.Rebuild(asView(lifecycleRoot{show: false}), geom.Rect{W: 3, H: 1})
	select {
	case actions := <-rt.FollowUp():
		if len(actions) == 0 {
			t.Fatalf("expected unmount lifecycle actions")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no unmount effect received")
	}
}

func TestDispatchEvent_HitTestsFocusAndBlur(t *testing.T) {
	var events []string
	rt := New(struct{}{}, func(*struct{}, update.Action) []update.Effect { return nil })
	ctx := context.Background()
	rt.SetContext(ctx)
	rt.Rebuild(asView(focusEventRoot{show: true, events: &events}), geom.Rect{W: 2, H: 1})
	if rt.Focused == nil {
		t.Fatalf("expected focused node after rebuild")
	}
	rt.DispatchEvent(interaction.Event{Kind: interaction.EventMouseDown, Pos: geom.Point{X: 0, Y: 0}})

	rt.Invalidate()
	rt.Rebuild(asView(focusEventRoot{show: false, events: &events}), geom.Rect{W: 2, H: 1})
	if rt.Focused != nil {
		t.Fatalf("focus should be cleared when focused node disappears")
	}
	foundFocus := false
	for _, event := range events {
		if event == "focus" {
			foundFocus = true
			break
		}
	}
	if !foundFocus {
		t.Fatalf("focus events = %#v, want focus before removal", events)
	}
}

func TestRebuild_RepairsFocusToNearestSurvivingFocusable(t *testing.T) {
	rt := New(struct{}{}, func(*struct{}, update.Action) []update.Effect { return nil })
	rt.Rebuild(asView(focusRepairRoot{showMiddle: true}), geom.Rect{W: 2, H: 3})

	rt.DispatchEvent(interaction.Event{Kind: interaction.EventMouseDown, Pos: geom.Point{X: 0, Y: 1}})
	if rt.Focused == nil {
		t.Fatalf("expected focused middle node after click")
	}
	middleID := rt.Focused.ID

	rt.Invalidate()
	rt.Rebuild(asView(focusRepairRoot{showMiddle: false}), geom.Rect{W: 2, H: 2})
	if rt.Focused == nil {
		t.Fatalf("expected repaired focus after middle removal")
	}
	if rt.Focused.ID == middleID {
		t.Fatalf("focus remained on removed node %d", middleID)
	}
	if got := rt.Focused.BorderRect.Y; got != 0 {
		t.Fatalf("repaired focus row y = %d, want first surviving descendant at y=0", got)
	}
}

type focusParentChildRoot struct {
	showChild bool
}

func (r focusParentChildRoot) BuildRenderNode(_ *view.Context[struct{}]) layout.RenderNode {
	return layout.NewColumn(layout.FlowChild{RenderNode: focusParentChildLeaf(r)})
}

type focusParentChildLeaf struct {
	showChild bool
}

func (f focusParentChildLeaf) Measure(c geom.Constraints, _ *layout.LayoutContext) geom.Size {
	return geom.ClampSize(geom.Size{W: 1, H: 1}, c)
}

func (f focusParentChildLeaf) Arrange(node *layout.LayoutNode, _ *layout.LayoutContext) layout.NodeLayout {
	out := layout.NodeLayout{
		BorderRect:   node.Slot,
		ContentRect:  node.Slot,
		ViewportRect: node.Slot,
		ContentSize:  geom.Size{W: node.Slot.W, H: node.Slot.H},
	}
	if f.showChild {
		out.ChildSlots = []layout.ChildSlot{{
			Spec: layout.ChildSpec{Hint: "child", RenderNode: focusLeaf{id: "child"}},
			Rect: node.Slot,
		}}
	}
	return out
}

func (f focusParentChildLeaf) Paint(paint.Painter, *layout.LayoutNode, *layout.PaintContext) {}
func (f focusParentChildLeaf) HitTest(local geom.Point, _ *layout.LayoutNode) bool {
	return local.X >= 0 && local.Y >= 0
}
func (f focusParentChildLeaf) CanFocus(*layout.LayoutNode) bool { return true }

func TestRebuild_RepairsFocusToSurvivingFocusableParentBeforeIndexFallback(t *testing.T) {
	rt := New(struct{}{}, func(*struct{}, update.Action) []update.Effect { return nil })
	rt.Rebuild(asView(focusParentChildRoot{showChild: true}), geom.Rect{W: 2, H: 2})
	parentID := rt.Tree.Kids[0].ID

	rt.DispatchEvent(interaction.Event{Kind: interaction.EventMouseDown, Pos: geom.Point{X: 0, Y: 0}})
	if rt.Focused == nil {
		t.Fatalf("expected focused child after click")
	}
	if rt.Focused.ID == parentID {
		t.Fatalf("focus remained on parent, want child before rebuild")
	}

	rt.Invalidate()
	rt.Rebuild(asView(focusParentChildRoot{showChild: false}), geom.Rect{W: 2, H: 2})
	if rt.Focused == nil {
		t.Fatalf("expected repaired focus on surviving parent")
	}
	if rt.Focused.ID != parentID {
		t.Fatalf("repaired focus id=%d, want parent id=%d", rt.Focused.ID, parentID)
	}
}

type focusSiblingRepairRoot struct {
	showDetail bool
}

func (r focusSiblingRepairRoot) BuildRenderNode(_ *view.Context[struct{}]) layout.RenderNode {
	return layout.NewColumn(layout.FlowChild{RenderNode: focusSiblingRepairLeaf(r)})
}

type focusSiblingRepairLeaf struct {
	showDetail bool
}

func (f focusSiblingRepairLeaf) Measure(c geom.Constraints, _ *layout.LayoutContext) geom.Size {
	return geom.ClampSize(geom.Size{W: 1, H: 3}, c)
}

func (f focusSiblingRepairLeaf) Arrange(node *layout.LayoutNode, _ *layout.LayoutContext) layout.NodeLayout {
	slots := []layout.ChildSlot{
		{
			Spec: layout.ChildSpec{Hint: "parent", RenderNode: focusLeaf{id: "parent"}},
			Rect: geom.Rect{X: node.Slot.X, Y: node.Slot.Y, W: node.Slot.W, H: 1},
		},
		{
			Spec: layout.ChildSpec{Hint: "sibling", RenderNode: focusLeaf{id: "sibling"}},
			Rect: geom.Rect{X: node.Slot.X, Y: node.Slot.Y + 1, W: node.Slot.W, H: 1},
		},
	}
	if f.showDetail {
		slots = append(slots, layout.ChildSlot{
			Spec: layout.ChildSpec{Hint: "detail", RenderNode: focusLeaf{id: "detail"}},
			Rect: geom.Rect{X: node.Slot.X, Y: node.Slot.Y + 2, W: node.Slot.W, H: 1},
		})
	}
	return layout.NodeLayout{
		BorderRect:   node.Slot,
		ContentRect:  node.Slot,
		ViewportRect: node.Slot,
		ContentSize:  geom.Size{W: node.Slot.W, H: node.Slot.H},
		ChildSlots:   slots,
	}
}

func (f focusSiblingRepairLeaf) Paint(paint.Painter, *layout.LayoutNode, *layout.PaintContext) {}
func (f focusSiblingRepairLeaf) HitTest(local geom.Point, _ *layout.LayoutNode) bool {
	return local.X >= 0 && local.Y >= 0
}

func TestRebuild_RepairsFocusToSurvivingFocusableDescendantBeforeIndexFallback(t *testing.T) {
	rt := New(struct{}{}, func(*struct{}, update.Action) []update.Effect { return nil })
	rt.Rebuild(asView(focusSiblingRepairRoot{showDetail: true}), geom.Rect{W: 2, H: 3})

	rt.DispatchEvent(interaction.Event{Kind: interaction.EventMouseDown, Pos: geom.Point{X: 0, Y: 2}})
	if rt.Focused == nil {
		t.Fatalf("expected focused detail after click")
	}
	if got := rt.Focused.BorderRect.Y; got != 2 {
		t.Fatalf("focused detail row y=%d want 2", got)
	}

	rt.Invalidate()
	rt.Rebuild(asView(focusSiblingRepairRoot{showDetail: false}), geom.Rect{W: 2, H: 2})
	if rt.Focused == nil {
		t.Fatalf("expected repaired focus after detail removal")
	}
	if got := rt.Focused.BorderRect.Y; got != 0 {
		t.Fatalf("repaired focus row y=%d want parent row at 0", got)
	}
}

func TestFocusMove_AutoScrollsFocusedNodeIntoViewport(t *testing.T) {
	rt := New(struct{}{}, func(*struct{}, update.Action) []update.Effect { return nil })
	viewport := geom.Rect{W: 8, H: 2}

	rt.Rebuild(asView(scrollFocusRoot{rows: 6}), viewport)
	if rt.Focused == nil {
		t.Fatal("expected initial focus")
	}

	for i := 0; i < 5; i++ {
		rt.Dispatch([]update.Action{interaction.FocusMoveAction{Move: interaction.FocusMoveNext}})
		rt.Rebuild(asView(scrollFocusRoot{rows: 6}), viewport)
	}
	if rt.Focused == nil {
		t.Fatal("expected focused node after traversal")
	}
	if rt.Tree == nil {
		t.Fatal("expected retained tree")
	}
	if rt.Tree.ScrollOffset.Y == 0 {
		t.Fatalf("expected scroll offset to move for offscreen focus traversal; got=%d", rt.Tree.ScrollOffset.Y)
	}
	if rt.Focused.BorderRect.Y < rt.Tree.BorderRect.Y || rt.Focused.BorderRect.Bottom() > rt.Tree.BorderRect.Bottom() {
		t.Fatalf("focused row outside viewport after auto-scroll: focused=%#v viewport=%#v", rt.Focused.BorderRect, rt.Tree.BorderRect)
	}
}

func TestDispatch_FocusKeyAction_DefersUntilKeyAppearsAfterRebuild(t *testing.T) {
	rt := New(struct{}{}, func(*struct{}, update.Action) []update.Effect { return nil })
	rt.Rebuild(asView(focusKeyRoot{showTarget: false}), geom.Rect{W: 2, H: 2})
	if rt.Focused == nil {
		t.Fatalf("expected initial focus")
	}
	initialID := rt.Focused.ID

	rt.Dispatch([]update.Action{interaction.FocusKeyAction{Key: "target"}})
	if rt.Focused == nil || rt.Focused.ID != initialID {
		t.Fatalf("focus changed before target exists; got=%#v want id=%d", rt.Focused, initialID)
	}

	rt.Invalidate()
	rt.Rebuild(asView(focusKeyRoot{showTarget: true}), geom.Rect{W: 2, H: 2})
	if rt.Focused == nil {
		t.Fatalf("expected focus after target appears")
	}
	_, key, _ := view.NamedNodeMetadata(layout.UnwrapIdentity(rt.Focused.RenderNode))
	if key != "target" {
		t.Fatalf("focused key=%q want target", key)
	}
}

func TestDispatchEvent_BubblesToNearestScopedHandler(t *testing.T) {
	var calls []string
	rt := New(struct{}{}, func(*struct{}, update.Action) []update.Effect { return nil })
	rt.Rebuild(asView(bubbleRoot{calls: &calls, childHandles: true}), geom.Rect{W: 3, H: 3})

	rt.DispatchEvent(interaction.Event{Kind: interaction.EventMouseDown, Pos: geom.Point{X: 0, Y: 0}})
	if len(calls) != 1 || calls[0] != "child" {
		t.Fatalf("calls = %#v, want child handler to consume before parent", calls)
	}
}

func TestDispatchEvent_BubblesWhenChildDoesNotHandle(t *testing.T) {
	var calls []string
	rt := New(struct{}{}, func(*struct{}, update.Action) []update.Effect { return nil })
	rt.Rebuild(asView(bubbleRoot{calls: &calls, childHandles: false}), geom.Rect{W: 3, H: 3})

	rt.DispatchEvent(interaction.Event{Kind: interaction.EventMouseDown, Pos: geom.Point{X: 0, Y: 0}})
	if len(calls) != 1 || calls[0] != "parent" {
		t.Fatalf("calls = %#v, want parent handler after child ignored", calls)
	}
}

type bubbleRoot struct {
	calls        *[]string
	childHandles bool
}

func (r bubbleRoot) BuildRenderNode(_ *view.Context[struct{}]) layout.RenderNode {
	return layout.NewColumn(layout.FlowChild{RenderNode: bubbleParent{
		calls: r.calls,
		child: bubbleChild{calls: r.calls, handles: r.childHandles},
	},
	})
}

type bubbleParent struct {
	calls *[]string
	child bubbleChild
}

func (p bubbleParent) Measure(c geom.Constraints, _ *layout.LayoutContext) geom.Size {
	return geom.ClampSize(geom.Size{W: 1, H: 1}, c)
}

func (p bubbleParent) Arrange(node *layout.LayoutNode, _ *layout.LayoutContext) layout.NodeLayout {
	return layout.NodeLayout{
		BorderRect:   node.Slot,
		ContentRect:  node.Slot,
		ViewportRect: node.Slot,
		ContentSize:  node.MeasuredSize,
		ChildSlots: []layout.ChildSlot{{
			Spec: layout.ChildSpec{Hint: "child", RenderNode: p.child},
			Rect: node.Slot,
		}},
	}
}

func (p bubbleParent) Paint(paint.Painter, *layout.LayoutNode, *layout.PaintContext) {}

func (p bubbleParent) HandleScopedEvent(ev interaction.Event, _ *layout.LayoutNode) (bool, []update.Action) {
	if ev.Kind != interaction.EventMouseDown {
		return false, nil
	}
	*p.calls = append(*p.calls, "parent")
	return true, nil
}

type bubbleChild struct {
	calls   *[]string
	handles bool
}

func (c bubbleChild) Measure(cs geom.Constraints, _ *layout.LayoutContext) geom.Size {
	return geom.ClampSize(geom.Size{W: 1, H: 1}, cs)
}

func (c bubbleChild) Arrange(node *layout.LayoutNode, _ *layout.LayoutContext) layout.NodeLayout {
	return layout.NodeLayout{BorderRect: node.Slot, ContentRect: node.Slot, ViewportRect: node.Slot, ContentSize: node.MeasuredSize}
}

func (c bubbleChild) Paint(paint.Painter, *layout.LayoutNode, *layout.PaintContext) {}

func (c bubbleChild) HitTest(local geom.Point, _ *layout.LayoutNode) bool {
	return local.X >= 0 && local.Y >= 0
}

func (c bubbleChild) HandleScopedEvent(ev interaction.Event, _ *layout.LayoutNode) (bool, []update.Action) {
	if ev.Kind != interaction.EventMouseDown {
		return false, nil
	}
	if !c.handles {
		return false, nil
	}
	*c.calls = append(*c.calls, "child")
	return true, nil
}
