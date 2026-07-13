package loop

import (
	"context"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

// --- Proof-of-concept typed application ---

type demoModel struct {
	Counter int
}

type demoEvent struct {
	Increment int
}

type demoApp struct {
	counter int
}

func (a demoApp) Init() (demoModel, []core.Effect[demoEvent]) {
	return demoModel{Counter: 0}, nil
}

func (a demoApp) Update(model demoModel, event demoEvent) (demoModel, []core.Effect[demoEvent]) {
	model.Counter += event.Increment
	if model.Counter == 5 {
		return model, []core.Effect[demoEvent]{
			{
				Key:    "double",
				Policy: core.EffectRunOnce,
				Run: func(ctx context.Context) demoEvent {
					return demoEvent{Increment: 5}
				},
			},
		}
	}
	return model, nil
}

func (a demoApp) View(model demoModel) core.Node[demoEvent] {
	label := "count:"
	return core.Stack[demoEvent](core.AxisVertical,
		core.Text[demoEvent](label),
	).Key(core.K("root"))
}

// --- Adapter proof ---

func TestCoreAppReducer_RoutesTypedEventThroughUpdate(t *testing.T) {
	app := demoApp{}
	model, _ := app.Init()

	// Bridge from typed app into retained runtime.
	reduce := CoreAppReducer[demoModel, demoEvent](app)
	effects := reduce(&model, demoEvent{Increment: 3})

	if model.Counter != 3 {
		t.Fatalf("model.Counter = %d, want 3", model.Counter)
	}
	if len(effects) != 0 {
		t.Fatalf("effects = %d, want 0", len(effects))
	}
}

func TestCoreAppReducer_BridgesEffectThroughUntypedPath(t *testing.T) {
	app := demoApp{}
	model, _ := app.Init()
	reduce := CoreAppReducer[demoModel, demoEvent](app)

	// Counter is 0. Update with +2 → no effect.
	reduce(&model, demoEvent{Increment: 2})
	if model.Counter != 2 {
		t.Fatalf("model.Counter = %d, want 2", model.Counter)
	}

	// Update with +3 → total 5 → triggers effect.
	effects := reduce(&model, demoEvent{Increment: 3})
	if model.Counter != 5 {
		t.Fatalf("model.Counter = %d, want 5", model.Counter)
	}
	if len(effects) != 1 {
		t.Fatalf("effects = %d, want 1", len(effects))
	}

	// Execute bridged effect and assert it yields another typed event.
	result := effects[0].Execute(context.Background())
	if len(result) != 1 {
		t.Fatalf("effect actions = %d, want 1", len(result))
	}
	typed, ok := result[0].(demoEvent)
	if !ok {
		t.Fatalf("action type = %T, want demoEvent", result[0])
	}
	if typed.Increment != 5 {
		t.Fatalf("event.Increment = %d, want 5", typed.Increment)
	}
}

func TestCoreAppLoop_RunsTypedDispatch(t *testing.T) {
	app := demoApp{}
	model, _ := app.Init()

	// Wire AppLoop with the typed reducer adapter.
	reduce := CoreAppReducer[demoModel, demoEvent](app)
	loop := New(model, reduce)
	loop.SetContext(context.Background())
	loop.Reduce = reduce

	// Dispatch a typed event through the engine.
	loop.Dispatch([]update.Action{demoEvent{Increment: 4}})

	if loop.Model.Counter != 4 {
		t.Fatalf("model.Counter = %d, want 4", loop.Model.Counter)
	}

	// Dispatch another to trigger effect (+1 more = 5).
	loop.Dispatch([]update.Action{demoEvent{Increment: 1}})

	if loop.Model.Counter != 5 {
		t.Fatalf("model.Counter = %d, want 5", loop.Model.Counter)
	}

	// Drain follow-up from the async effect.
	select {
	case actions := <-loop.FollowUp():
		if len(actions) != 1 {
			t.Fatalf("followUp actions = %d, want 1", len(actions))
		}
		typed := actions[0].(demoEvent)
		if typed.Increment != 5 {
			t.Fatalf("followUp event.Increment = %d, want 5", typed.Increment)
		}
		// Redispatch the follow-up through the loop.
		loop.Dispatch(actions)
		if loop.Model.Counter != 10 {
			t.Fatalf("model.Counter after effect = %d, want 10", loop.Model.Counter)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no followUp received from typed effect")
	}
}

func TestLowerCoreNode_ProducesLayoutNode(t *testing.T) {
	app := demoApp{}
	model, _ := app.Init()
	node := app.View(model)

	caster := func(e demoEvent) update.Action { return e }
	render, err := LowerCoreNode(node, caster)
	if err != nil {
		t.Fatalf("LowerCoreNode: %v", err)
	}
	if render == nil {
		t.Fatal("LowerCoreNode returned nil layout.RenderNode")
	}
	// render is a layout.RenderNode — the lowerer succeeded.
	// Deeper shape assertions live in corelower's own tests.
}
