package core

import (
	"context"
	"testing"
)

func TestEffectEmptyReportsMissingKey(t *testing.T) {
	t.Parallel()

	var e Effect[string]
	if !e.Empty() {
		t.Fatal("zero Effect must report Empty")
	}

	e = Effect[string]{Key: "fetch"}
	if e.Empty() {
		t.Fatal("Effect with key must not report Empty")
	}
}

func TestEffectPoliciesAreDistinct(t *testing.T) {
	t.Parallel()

	if EffectRunOnce == EffectCancelPrevious {
		t.Fatal("EffectRunOnce and EffectCancelPrevious must differ")
	}
	if EffectRunOnce == EffectIgnoreWhileRunning {
		t.Fatal("EffectRunOnce and EffectIgnoreWhileRunning must differ")
	}
	if EffectCancelPrevious == EffectIgnoreWhileRunning {
		t.Fatal("EffectCancelPrevious and EffectIgnoreWhileRunning must differ")
	}
}

type appEvent struct{ Kind string }
type appState struct{ Count int }

func TestAppInterfaceCompiles(t *testing.T) {
	t.Parallel()

	var _ App[appState, appEvent] = (*testApp)(nil)
}

type testApp struct{}

func (testApp) Init() (appState, []Effect[appEvent]) {
	return appState{Count: 0}, nil
}

func (testApp) Update(s appState, e appEvent) (appState, []Effect[appEvent]) {
	s.Count++
	return s, nil
}

func (testApp) View(s appState) Node[appEvent] {
	return Text[appEvent]("hello")
}

func TestMutableAppInterfaceCompiles(t *testing.T) {
	t.Parallel()

	var _ MutableApp[appState, appEvent] = (*testMutableApp)(nil)
}

type testMutableApp struct{}

func (testMutableApp) Init(tx *Tx[appState, appEvent]) {
	tx.State.Count = 1
}

func (testMutableApp) Update(tx *Tx[appState, appEvent], e appEvent) {
	tx.State.Count++
}

func (testMutableApp) View(s *appState) Node[appEvent] {
	return Text[appEvent]("mutable")
}

func TestTxAccumulatesEffects(t *testing.T) {
	t.Parallel()

	tx := &Tx[appState, appEvent]{State: &appState{}}
	tx.Effect(Effect[appEvent]{Key: "a"})
	tx.Effect(Effect[appEvent]{Key: "b"})

	effects := tx.Effects()
	if len(effects) != 2 {
		t.Fatalf("effects count = %d, want 2", len(effects))
	}
	if effects[0].Key != "a" {
		t.Fatalf("first key = %q, want a", effects[0].Key)
	}

	// Second call must return empty because buffer was cleared.
	effects2 := tx.Effects()
	if len(effects2) != 0 {
		t.Fatalf("second effects count = %d, want 0", len(effects2))
	}
}

func TestRuntimeEventKindsAreDistinct(t *testing.T) {
	t.Parallel()

	kinds := []RuntimeKind{
		RuntimeResize,
		RuntimeTick,
		RuntimeFocusChange,
		RuntimeEffectResult,
	}
	seen := make(map[RuntimeKind]struct{}, len(kinds))
	for _, k := range kinds {
		if _, ok := seen[k]; ok {
			t.Fatalf("duplicate RuntimeKind value: %d", k)
		}
		seen[k] = struct{}{}
	}
}

func TestEffectRunInvokesFunction(t *testing.T) {
	t.Parallel()

	called := false
	e := Effect[string]{
		Key:    "test",
		Policy: EffectRunOnce,
		Run: func(ctx context.Context) string {
			called = true
			return "done"
		},
	}

	result := e.Run(context.Background())
	if !called {
		t.Fatal("Effect.Run was not invoked")
	}
	if result != "done" {
		t.Fatalf("result = %q, want done", result)
	}
}
