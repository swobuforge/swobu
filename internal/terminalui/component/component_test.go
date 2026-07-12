package component

import (
	"strconv"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

type mapScope struct {
	m      map[string]any
	prefix string
}

func (m mapScope) Get(slot int) (any, bool) {
	key := m.prefix + strconv.Itoa(slot)
	v, ok := m.m[key]
	return v, ok
}

func (m mapScope) Set(slot int, value any) {
	key := m.prefix + strconv.Itoa(slot)
	m.m[key] = value
}

func (m mapScope) WithPrefix(prefix string) LocalScope {
	return mapScope{m: m.m, prefix: m.prefix + prefix + "/"}
}

func TestUseStateRetainsValuePerSlot(t *testing.T) {
	t.Parallel()

	scope := mapScope{m: make(map[string]any)}
	actions := make([]update.Action, 0, 1)
	ctx := &Context[struct{}]{
		Local: scope,
		Model: func() struct{} { return struct{}{} },
		dispatch: func(action update.Action) {
			actions = append(actions, action)
		},
		building: true,
	}

	value, setValue := UseState(ctx, "x")
	if got := value; got != "x" {
		t.Fatalf("initial = %q, want x", got)
	}
	ctx.building = false
	setValue("y")
	if got := len(actions); got != 1 {
		t.Fatalf("dispatch count = %d, want 1", got)
	}
	if _, ok := actions[0].(update.LocalStateChangedAction); !ok {
		t.Fatalf("action = %T, want update.LocalStateChangedAction", actions[0])
	}

	ctx.hookSlot = 0
	ctx.building = true
	value, _ = UseState(ctx, "z")
	if got := value; got != "y" {
		t.Fatalf("retained = %q, want y", got)
	}
}

func TestUseStatePanicsOnTypeMismatch(t *testing.T) {
	t.Parallel()

	ctx := &Context[struct{}]{
		Local:    mapScope{m: map[string]any{"0": 17}},
		Model:    func() struct{} { return struct{}{} },
		building: true,
	}

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on state type mismatch")
		}
	}()

	_, _ = UseState(ctx, "x")
}

func TestUseStatePanicsOutsideBuild(t *testing.T) {
	t.Parallel()

	ctx := &Context[struct{}]{
		Local:    mapScope{m: make(map[string]any)},
		Model:    func() struct{} { return struct{}{} },
		building: false,
	}

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic outside build")
		}
	}()

	_, _ = UseState(ctx, "x")
}

func TestDispatchPanicsDuringBuild(t *testing.T) {
	t.Parallel()

	ctx := &Context[struct{}]{
		Local:    mapScope{m: make(map[string]any)},
		Model:    func() struct{} { return struct{}{} },
		building: true,
	}

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on dispatch during build")
		}
	}()

	ctx.Dispatch(struct{}{})
}

func TestEmitPanicsDuringBuild(t *testing.T) {
	t.Parallel()

	ctx := &Context[struct{}]{
		Local:    mapScope{m: make(map[string]any)},
		Model:    func() struct{} { return struct{}{} },
		building: true,
	}

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on emit during build")
		}
	}()

	ctx.Emit(struct{}{})
}

func TestBuildIsolatesNestedHookScopes(t *testing.T) {
	t.Parallel()

	scope := mapScope{m: make(map[string]any)}
	ctx := &Context[struct{}]{
		Local:    scope,
		Model:    func() struct{} { return struct{}{} },
		building: true,
	}

	root := ViewFunc[struct{}](func(ctx *Context[struct{}]) core.Node {
		rootValue, _ := UseState(ctx, "root")
		child := Build(func(_ *Context[struct{}]) View[struct{}] {
			return ViewFunc[struct{}](func(ctx *Context[struct{}]) core.Node {
				childValue, _ := UseState(ctx, "child")
				return core.Text(childValue)
			})
		})
		childNode := child.BuildCoreNode(ctx)
		return core.Box(core.Text(rootValue), childNode)
	})

	node := root.BuildCoreNode(ctx)
	if node.Kind() != core.KindBox {
		t.Fatalf("root kind = %v, want KindBox", node.Kind())
	}
	if ctx.building {
		t.Fatal("root build should finalize the build scope")
	}
	if got := scope.m["0"]; got != "root" {
		t.Fatalf("root slot = %#v, want root", got)
	}
	if got := scope.m["build/0/0"]; got != "child" {
		t.Fatalf("child slot = %#v, want child", got)
	}
}
