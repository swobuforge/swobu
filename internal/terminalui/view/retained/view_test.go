package retained

import (
	"context"
	"strconv"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

type mapScope struct {
	m      map[string]any
	prefix string
}

func (m mapScope) Get(slot int) (any, bool) {
	key := m.prefix + slotKey(slot)
	v, ok := m.m[key]
	return v, ok
}
func (m mapScope) Set(slot int, value any) {
	key := m.prefix + slotKey(slot)
	m.m[key] = value
}
func (m mapScope) WithPrefix(prefix string) LocalScope {
	return mapScope{m: m.m, prefix: m.prefix + prefix + "/"}
}

func slotKey(slot int) string {
	return strconv.Itoa(slot)
}

func TestUseState_RetainsValuePerSlot(t *testing.T) {
	scope := mapScope{m: make(map[string]any)}
	ctx := &Context[struct{}]{
		Local:    scope,
		Model:    func() struct{} { return struct{}{} },
		building: true,
	}
	value, setValue := UseState(ctx, func() string { return "x" })
	if got := value; got != "x" {
		t.Fatalf("initial = %q, want x", got)
	}
	setValue("y")
	ctx.hookSlot = 0
	value, _ = UseState(ctx, func() string { return "z" })
	if got := value; got != "y" {
		t.Fatalf("retained = %q, want y", got)
	}
}

func TestUseMemo_CachesByDependencyIdentity(t *testing.T) {
	scope := mapScope{m: make(map[string]any)}
	ctx := &Context[struct{}]{
		Local:    scope,
		Model:    func() struct{} { return struct{}{} },
		building: true,
	}
	calls := 0

	got := UseMemo(ctx, func() string {
		calls++
		return "first"
	}, "dep")
	if got != "first" {
		t.Fatalf("first memo = %q, want first", got)
	}
	if calls != 1 {
		t.Fatalf("compute calls = %d, want 1", calls)
	}

	ctx.hookSlot = 0
	got = UseMemo(ctx, func() string {
		calls++
		return "second"
	}, "dep")
	if got != "first" {
		t.Fatalf("cached memo = %q, want first", got)
	}
	if calls != 1 {
		t.Fatalf("compute calls after cache hit = %d, want 1", calls)
	}

	ctx.hookSlot = 0
	got = UseMemo(ctx, func() string {
		calls++
		return "third"
	}, "next")
	if got != "third" {
		t.Fatalf("invalidated memo = %q, want third", got)
	}
	if calls != 2 {
		t.Fatalf("compute calls after dep change = %d, want 2", calls)
	}
}

func TestBuildRoot_PanicsOnDispatchDuringBuild(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on dispatch during build")
		}
	}()

	BuildViewRootNode(View[struct{}](func(ctx *Context[struct{}]) layout.RenderNode {
		ctx.Dispatch(struct{}{})
		return nil
	}), mapScope{}, func(update.Action) {}, func(update.Action) {}, func() struct{} { return struct{}{} })
}

func TestBuildRoot_PanicsOnEmitDuringBuild(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on emit during build")
		}
	}()

	BuildViewRootNode(View[struct{}](func(ctx *Context[struct{}]) layout.RenderNode {
		ctx.Emit(struct{}{})
		return nil
	}), mapScope{}, func(update.Action) {}, func(update.Action) {}, func() struct{} { return struct{}{} })
}

func TestApply_ComposesModifiersInDeclarationOrder(t *testing.T) {
	root := View[struct{}](func(_ *Context[struct{}]) layout.RenderNode {
		return layout.NewText("x")
	})
	composed := WithConstrain[struct{}](ConstrainSpec{MaxW: 17})(WithPadLeft[struct{}](2)(root))
	out := BuildViewRootNode(composed, mapScope{}, func(update.Action) {}, func(update.Action) {}, func() struct{} { return struct{}{} })

	outer, ok := out.(*layout.BoxRenderNode)
	if !ok {
		t.Fatalf("outer type = %T, want *layout.BoxRenderNode", out)
	}
	if got := outer.Sized.Sizing.Max.W; got != 17 {
		t.Fatalf("outer max width = %d, want 17", got)
	}

	inner, ok := outer.Child.(*layout.BoxRenderNode)
	if !ok {
		t.Fatalf("inner type = %T, want *layout.BoxRenderNode", outer.Child)
	}
	if got := inner.Padding; got != (geom.Insets{Left: 2}) {
		t.Fatalf("inner padding = %#v, want left=2", got)
	}
}

type hookBuilder struct{}
type stubEffect struct{}
type dispatchedAction string

func (hookBuilder) BuildView(_ *Context[struct{}]) ViewSpec[struct{}] {
	return View[struct{}](func(_ *Context[struct{}]) layout.RenderNode {
		return layout.NewText("ok")
	})
}

func (hookBuilder) OnMountEffects() []update.Effect {
	return []update.Effect{stubEffect{}}
}

func (stubEffect) Execute(_ context.Context) []update.Action { return nil }

func TestBuildViewRootNode_MaterializesBuildFunction(t *testing.T) {
	out := BuildViewRootNode(Build(hookBuilder{}.BuildView), mapScope{}, func(update.Action) {}, func(update.Action) {}, func() struct{} { return struct{}{} })
	text, ok := out.(*layout.TextRenderNode)
	if !ok {
		t.Fatalf("type = %T, want *layout.TextRenderNode", out)
	}
	if got := text.Value; got != "ok" {
		t.Fatalf("text = %q, want ok", got)
	}
}

func TestBuildWithLifecycle_ForwardsLifecycleHooks(t *testing.T) {
	view := BuildWithLifecycle[struct{}](hookBuilder{}.BuildView, hookBuilder{}.OnMountEffects, nil)
	effects := CaptureLifecycle(view)
	if len(effects.OnMount) != 1 {
		t.Fatalf("on mount effects = %d, want 1", len(effects.OnMount))
	}
}

func TestBuild_AllowsEffectDispatchAfterCommit(t *testing.T) {
	scope := mapScope{m: make(map[string]any)}
	var dispatches []update.Action
	root := Build[struct{}](func(ctx *Context[struct{}]) ViewSpec[struct{}] {
		UseEffect(ctx, func() func() {
			ctx.Dispatch(dispatchedAction("done"))
			return nil
		}, "dep")
		return View[struct{}](func(_ *Context[struct{}]) layout.RenderNode {
			return layout.NewText("ok")
		})
	})

	node := BuildViewRootNode(root, scope, func(action update.Action) {
		dispatches = append(dispatches, action)
	}, func(update.Action) {}, func() struct{} { return struct{}{} })
	hooks, ok := node.(interface{ PostCommitEffects() []update.Effect })
	if !ok {
		t.Fatalf("node type = %T, want PostCommitEffects provider", node)
	}
	effects := hooks.PostCommitEffects()
	if len(effects) != 1 {
		t.Fatalf("post-commit effects = %d, want 1", len(effects))
	}
	for _, eff := range effects {
		eff.Execute(context.Background())
	}
	if len(dispatches) != 1 {
		t.Fatalf("dispatches = %d, want 1", len(dispatches))
	}
	got, ok := dispatches[0].(dispatchedAction)
	if !ok {
		t.Fatalf("dispatch[0]=%T want dispatchedAction", dispatches[0])
	}
	if got != "done" {
		t.Fatalf("dispatch[0]=%q want done", got)
	}
}

type responsiveStateModel struct {
	WideExtra      bool
	SetNarrowState bool
}

func TestResponsiveView_IsolatesBranchLocalState(t *testing.T) {
	scope := mapScope{m: make(map[string]any)}
	root := View[responsiveStateModel](func(ctx *Context[responsiveStateModel]) layout.RenderNode {
		wide := View[responsiveStateModel](func(ctx *Context[responsiveStateModel]) layout.RenderNode {
			parts := []ViewSpec[responsiveStateModel]{
				Build(func(_ *Context[responsiveStateModel]) ViewSpec[responsiveStateModel] {
					return View[responsiveStateModel](func(_ *Context[responsiveStateModel]) layout.RenderNode {
						return layout.NewText("wide-a")
					})
				}),
			}
			if ctx.Model().WideExtra {
				parts = append(parts, Build(func(_ *Context[responsiveStateModel]) ViewSpec[responsiveStateModel] {
					return View[responsiveStateModel](func(_ *Context[responsiveStateModel]) layout.RenderNode {
						return layout.NewText("wide-b")
					})
				}))
			}
			return Materialize(ctx, VStack(ctx, parts...))
		})
		narrow := Build(func(ctx *Context[responsiveStateModel]) ViewSpec[responsiveStateModel] {
			return View[responsiveStateModel](func(ctx *Context[responsiveStateModel]) layout.RenderNode {
				value, setValue := UseState(ctx, func() string { return "closed" })
				if ctx.Model().SetNarrowState && value != "open" {
					setValue("open")
				}
				return layout.NewText(value)
			})
		})
		return Materialize(ctx, ResponsiveView[responsiveStateModel]{
			Threshold: 9999,
			Wide:      wide,
			Narrow:    narrow,
		})
	})
	_ = BuildViewRootNode(root, scope, func(update.Action) {}, func(update.Action) {}, func() responsiveStateModel {
		return responsiveStateModel{WideExtra: false, SetNarrowState: true}
	})
	out := BuildViewRootNode(root, scope, func(update.Action) {}, func(update.Action) {}, func() responsiveStateModel {
		return responsiveStateModel{WideExtra: true}
	})
	sw, ok := out.(*layout.ResponsiveSwitchRenderNode)
	if !ok {
		t.Fatalf("type = %T, want *layout.ResponsiveSwitchRenderNode", out)
	}
	text, ok := sw.Fallback.(*layout.TextRenderNode)
	if !ok {
		t.Fatalf("fallback type = %T, want *layout.TextRenderNode", sw.Fallback)
	}
	if got := text.Value; got != "open" {
		t.Fatalf("narrow state = %q, want open", got)
	}
}
