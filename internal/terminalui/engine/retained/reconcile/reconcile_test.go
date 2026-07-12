package reconcile

import (
	"context"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

type childComponent struct {
	init string
}

func asView(builder interface {
	BuildRenderNode(*retained.Context[struct{}]) layout.RenderNode
}) retained.ViewSpec[struct{}] {
	return retained.View[struct{}](func(ctx *retained.Context[struct{}]) layout.RenderNode {
		return builder.BuildRenderNode(ctx)
	})
}

func (c childComponent) BuildRenderNode(ctx *retained.Context[struct{}]) layout.RenderNode {
	value, _ := retained.UseState(ctx, func() string { return c.init })
	return layout.NewText(value)
}

type namedListRoot struct {
	order []string
}

func (r namedListRoot) BuildRenderNode(ctx *retained.Context[struct{}]) layout.RenderNode {
	children := make([]layout.FlowChild, 0, len(r.order))
	for _, item := range r.order {
		children = append(children, layout.FlowChild{RenderNode: retained.Materialize(ctx, retained.Named(item, asView(childComponent{init: item})))})
	}
	return layout.NewColumn(children...)
}

type effectHarness struct {
	show bool
	dep  string
	log  *[]string
}

func (r *effectHarness) BuildRenderNode(ctx *retained.Context[struct{}]) layout.RenderNode {
	if !r.show {
		return layout.NewColumn()
	}
	child := retained.Named("effect", retained.Build(r.buildView))
	return layout.NewColumn(layout.FlowChild{RenderNode: retained.Materialize(ctx, child)})
}

func (r *effectHarness) buildView(_ *retained.Context[struct{}]) retained.ViewSpec[struct{}] {
	dep := r.dep
	log := r.log
	return retained.View[struct{}](func(ctx *retained.Context[struct{}]) layout.RenderNode {
		retained.UseEffect(ctx, func() func() {
			*log = append(*log, "run:"+dep)
			return func() {
				*log = append(*log, "cleanup:"+dep)
			}
		}, dep)
		return layout.NewText(dep)
	})
}

func runLifecycleEffects(t *testing.T, effects []update.Effect) {
	t.Helper()
	for _, eff := range effects {
		eff.Execute(context.Background())
	}
}

func TestReconcile_PreservesNamedLocalStateAcrossReorder(t *testing.T) {
	locals := NewLocalStore()
	reconciler := New[struct{}](locals)

	// First build: order is ["a", "b"]. Each child initializes state to its key.
	first, _, _, _ := reconciler.Reconcile(
		asView(namedListRoot{order: []string{"a", "b"}}),
		&struct{}{},
		func(update.Action) {},
		func(update.Action) {},
	)
	builder := &layout.TreeBuilder{}
	tree := builder.Build(first, geom.Rect{W: 10, H: 4})
	buffer := paint.NewBuffer(geom.Rect{W: 10, H: 4})
	paintTree(tree, buffer)
	if got := buffer.String(); got != "a\nb" {
		t.Fatalf("initial paint = %q, want a\\nb", got)
	}

	// Now mutate state in the "a" named view via its scoped store.
	// The "a" view uses prefix "a/" under the root node.
	rootID := tree.ID
	locals.values[StateKey{NodeID: rootID, SlotKey: "a/0"}] = "persisted"

	// Rebuild with order ["b", "a"]. The "a" view should retain "persisted".
	second, _, _, _ := reconciler.Reconcile(
		asView(namedListRoot{order: []string{"b", "a"}}),
		&struct{}{},
		func(update.Action) {},
		func(update.Action) {},
	)
	tree = builder.Build(second, geom.Rect{W: 10, H: 4})

	buffer = paint.NewBuffer(geom.Rect{W: 10, H: 4})
	paintTree(tree, buffer)
	if got := buffer.String(); got != "b\npersisted" {
		t.Fatalf("after reorder paint = %q, want b\\npersisted", got)
	}
}

func TestReconcile_CleansLocalStateOnUnmount(t *testing.T) {
	locals := NewLocalStore()
	reconciler := New[struct{}](locals)

	first, _, _, _ := reconciler.Reconcile(
		asView(namedListRoot{order: []string{"a"}}),
		&struct{}{},
		func(update.Action) {},
		func(update.Action) {},
	)
	tree := (&layout.TreeBuilder{}).Build(first, geom.Rect{W: 10, H: 2})
	id := tree.Kids[0].ID
	// The named "a" view stores under root node with "a/" prefix.
	locals.values[StateKey{NodeID: tree.ID, SlotKey: "a/0"}] = "persisted"

	_, _, unmounts, _ := reconciler.Reconcile(
		asView(namedListRoot{}),
		&struct{}{},
		func(update.Action) {},
		func(update.Action) {},
	)
	if len(unmounts) != 1 || unmounts[0] != id {
		t.Fatalf("unmounts = %#v, want [%d]", unmounts, id)
	}
	// After unmount, the named state should be cleaned up via DeletePrefix.
	if _, ok := locals.values[StateKey{NodeID: tree.ID, SlotKey: "a/0"}]; ok {
		t.Fatalf("local state for unmounted named child should be deleted")
	}
}

func TestReconcile_UseEffectRerunsWithCleanupBeforeNextCommitAndUnmount(t *testing.T) {
	locals := NewLocalStore()
	reconciler := New[struct{}](locals)
	log := make([]string, 0, 4)
	root := &effectHarness{show: true, dep: "one", log: &log}

	_, _, _, lifecycle := reconciler.Reconcile(
		asView(root),
		&struct{}{},
		func(update.Action) {},
		func(update.Action) {},
	)
	if len(lifecycle) != 1 {
		t.Fatalf("initial lifecycle effects = %d, want 1", len(lifecycle))
	}
	runLifecycleEffects(t, lifecycle)
	if got, want := log, []string{"run:one"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after initial commit = %#v, want %#v", got, want)
	}

	root.dep = "two"
	_, _, _, lifecycle = reconciler.Reconcile(
		asView(root),
		&struct{}{},
		func(update.Action) {},
		func(update.Action) {},
	)
	if len(lifecycle) != 1 {
		t.Fatalf("rerun lifecycle effects = %d, want 1", len(lifecycle))
	}
	runLifecycleEffects(t, lifecycle)
	if got, want := log, []string{"run:one", "cleanup:one", "run:two"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after dependency change = %#v, want %#v", got, want)
	}

	_, _, _, lifecycle = reconciler.Reconcile(
		asView(root),
		&struct{}{},
		func(update.Action) {},
		func(update.Action) {},
	)
	if len(lifecycle) != 0 {
		t.Fatalf("same-deps lifecycle effects = %d, want 0", len(lifecycle))
	}
	if got, want := log, []string{"run:one", "cleanup:one", "run:two"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after same-deps rebuild = %#v, want %#v", got, want)
	}

	root.show = false
	_, _, _, lifecycle = reconciler.Reconcile(
		asView(root),
		&struct{}{},
		func(update.Action) {},
		func(update.Action) {},
	)
	if len(lifecycle) != 1 {
		t.Fatalf("unmount lifecycle effects = %d, want 1", len(lifecycle))
	}
	runLifecycleEffects(t, lifecycle)
	if got, want := log, []string{"run:one", "cleanup:one", "run:two", "cleanup:two"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after unmount = %#v, want %#v", got, want)
	}
}

func TestReconcile_UseEffectCleansUpOnInitialUnmount(t *testing.T) {
	locals := NewLocalStore()
	reconciler := New[struct{}](locals)
	log := make([]string, 0, 2)
	root := &effectHarness{show: true, dep: "one", log: &log}

	_, _, _, lifecycle := reconciler.Reconcile(
		asView(root),
		&struct{}{},
		func(update.Action) {},
		func(update.Action) {},
	)
	runLifecycleEffects(t, lifecycle)

	root.show = false
	_, _, _, lifecycle = reconciler.Reconcile(
		asView(root),
		&struct{}{},
		func(update.Action) {},
		func(update.Action) {},
	)
	if len(lifecycle) != 1 {
		t.Fatalf("initial unmount lifecycle effects = %d, want 1", len(lifecycle))
	}
	runLifecycleEffects(t, lifecycle)
	if got, want := log, []string{"run:one", "cleanup:one"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after initial unmount = %#v, want %#v", got, want)
	}
}

func TestReconcile_PanicsOnDuplicateSiblingKeys(t *testing.T) {
	locals := NewLocalStore()
	reconciler := New[struct{}](locals)

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate sibling keys")
		}
	}()

	_, _, _, _ = reconciler.Reconcile(
		asView(namedListRoot{order: []string{"dup", "dup"}}),
		&struct{}{},
		func(update.Action) {},
		func(update.Action) {},
	)
}

func paintTree(node *layout.LayoutNode, painter paint.Painter) {
	if node.ClipRect.Empty() || node.BorderRect.Empty() {
		return
	}
	scoped := painter.WithClip(node.ClipRect).WithOrigin(geom.Point{X: node.BorderRect.X, Y: node.BorderRect.Y})
	node.RenderNode.Paint(scoped, node, &layout.PaintContext{})
	for _, child := range node.Kids {
		paintTree(child, scoped)
	}
}
