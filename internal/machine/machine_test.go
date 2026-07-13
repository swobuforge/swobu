package machine

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// ---- fixture types ----

type SimpleState struct {
	Counter int
}

type ActivityState struct {
	Lines []string
}

type CounterUpdated struct{}

type ActivityRecorded struct{}

type ExchangeTerminal struct{}

type SendProvider struct{}

type PersistEvidence struct{}

// ---- reducers ----

func IncrementCounter(s SimpleState, e CounterUpdated) (SimpleState, []Event, []Command, error) {
	s.Counter++
	return s, []Event{ActivityRecorded{}}, []Command{SendProvider{}}, nil
}

func ProjectActivity(s ActivityState, e CounterUpdated) (ActivityState, []Event, []Command, error) {
	s.Lines = append(s.Lines, "counter observed")
	return s, nil, nil, nil
}

func MarkTerminal(s SimpleState, e ExchangeTerminal) (SimpleState, []Event, []Command, error) {
	return s, nil, nil, nil
}

// reducer with composite input
func CompositeReducer(s struct {
	Counter  SimpleState
	Activity ActivityState
}, e CounterUpdated) (SimpleState, []Event, []Command, error) {
	ret := s.Counter
	ret.Counter += 100
	return ret, nil, nil, nil
}

// reducer that errors
func ErrorReducer(s SimpleState, e CounterUpdated) (SimpleState, []Event, []Command, error) {
	return s, nil, nil, errors.New("reducer failure")
}

// ---- Mergeable implementation ----

func (a ActivityState) Merge(other ActivityState) (ActivityState, error) {
	a.Lines = append(a.Lines, other.Lines...)
	return a, nil
}

// Command interpreter test helpers.

func sendProviderInterpreter(ctx context.Context, store *Store, cmd Command) ([]Event, error) {
	_, ok := cmd.(SendProvider)
	if !ok {
		return nil, nil
	}
	return []Event{ActivityRecorded{}}, nil
}

func persistEvidenceInterpreter(ctx context.Context, store *Store, cmd Command) ([]Event, error) {
	_, ok := cmd.(PersistEvidence)
	if !ok {
		return nil, nil
	}
	// persist does not emit follow-up events
	return nil, nil
}

// ---- tests ----

func TestRegisterReducerByReflection(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(IncrementCounter); err != nil {
		t.Fatalf("register: %v", err)
	}

	reducers := reg.ReducersFor(reflect.TypeOf(CounterUpdated{}))
	if len(reducers) != 1 {
		t.Fatalf("expected 1 reducer, got %d", len(reducers))
	}
	if reducers[0].stateInput.Name() != "SimpleState" {
		t.Fatalf("expected SimpleState, got %s", reducers[0].stateInput.Name())
	}
}

func TestInvokeReducerByEventType(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(IncrementCounter)

	store := NewStore(StateCell{Value: reflect.ValueOf(SimpleState{Counter: 5})})
	eng := NewEngine(reg)
	eng.RegisterInterpreter(sendProviderInterpreter)

	result, err := eng.Run(context.Background(), store, CounterUpdated{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Terminal {
		t.Fatal("expected non-terminal result")
	}

	var state SimpleState
	if err := store.Get(&state); err != nil {
		t.Fatalf("get state: %v", err)
	}
	if state.Counter != 6 {
		t.Fatalf("expected counter 6, got %d", state.Counter)
	}
}

func TestBuildCompositeInput(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(CompositeReducer)

	store := NewStore(
		StateCell{Value: reflect.ValueOf(SimpleState{Counter: 1})},
		StateCell{Value: reflect.ValueOf(ActivityState{Lines: []string{"a"}})},
	)
	eng := NewEngine(reg)

	_, err := eng.Run(context.Background(), store, CounterUpdated{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	var state SimpleState
	if err := store.Get(&state); err != nil {
		t.Fatalf("get state: %v", err)
	}
	if state.Counter != 101 {
		t.Fatalf("expected counter 101, got %d", state.Counter)
	}
}

func TestMissingStateFailsClearly(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(IncrementCounter)

	// Store does NOT contain SimpleState
	store := NewStore(StateCell{Value: reflect.ValueOf(ActivityState{})})
	eng := NewEngine(reg)

	_, err := eng.Run(context.Background(), store, CounterUpdated{})
	if err == nil {
		t.Fatal("expected error for missing state")
	}
	if !strings.Contains(err.Error(), "missing") && !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error should mention missing state, got: %v", err)
	}
}

func TestMultipleSameStateRequireMerge(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(IncrementCounter)
	_ = reg.Register(func(s SimpleState, e CounterUpdated) (SimpleState, []Event, []Command, error) {
		s.Counter += 10
		return s, nil, nil, nil
	})

	store := NewStore(StateCell{Value: reflect.ValueOf(SimpleState{Counter: 0})})
	eng := NewEngine(reg)

	_, err := eng.Run(context.Background(), store, CounterUpdated{})
	if err == nil {
		t.Fatal("expected error for unmergeable duplicate output")
	}
	if !strings.Contains(err.Error(), "Mergeable") {
		t.Fatalf("error should mention Mergeable, got: %v", err)
	}
}

func TestMergeConflictFails(t *testing.T) {
	reg := NewRegistry()

	reg.Register(func(s ConflictState, e CounterUpdated) (ConflictState, []Event, []Command, error) {
		s.Value = 1
		return s, nil, nil, nil
	})
	reg.Register(func(s ConflictState, e CounterUpdated) (ConflictState, []Event, []Command, error) {
		s.Value = 2
		return s, nil, nil, nil
	})

	store := NewStore(StateCell{Value: reflect.ValueOf(ConflictState{Value: 0})})
	eng := NewEngine(reg)

	_, err := eng.Run(context.Background(), store, CounterUpdated{})
	if err == nil {
		t.Fatal("expected merge conflict error")
	}
	if !strings.Contains(err.Error(), "conflict") && !strings.Contains(err.Error(), "Merge") {
		t.Fatalf("error should mention merge failure, got: %v", err)
	}
}

func TestCommandsEmitEvents(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(IncrementCounter)

	store := NewStore(StateCell{Value: reflect.ValueOf(SimpleState{Counter: 0})})
	eng := NewEngine(reg)
	eng.RegisterInterpreter(sendProviderInterpreter)

	result, err := eng.Run(context.Background(), store, CounterUpdated{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Terminal {
		t.Fatal("expected non-terminal: command emitted CounterUpdated again")
	}

	// The increment reducer triggers SendProvider which emits CounterUpdated
	// The loop continues until it processes CounterUpdated and then
	// ActivityRecorded with no reducer.
	// Actually: CounterUpdated -> IncrementCounter -> ActivityRecorded + SendProvider -> command emits CounterUpdated
	// This loops until loop limit because every CounterUpdated generates another.
	// So it should hit the loop limit.
	if len(result.Plan) < 2 {
		t.Fatalf("expected at least 2 plan steps from command+reducer, got %d", len(result.Plan))
	}
}

func TestTerminalStopsMachine(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(MarkTerminal)

	store := NewStore(StateCell{Value: reflect.ValueOf(SimpleState{Counter: 0})})
	eng := NewEngine(reg)

	result, err := eng.Run(context.Background(), store, ExchangeTerminal{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !result.Terminal {
		t.Fatal("expected terminal result")
	}
	if len(result.Plan) != 1 {
		t.Fatalf("expected 1 plan step, got %d", len(result.Plan))
	}
}

func TestLoopLimitStopsMachine(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(IncrementCounter)

	store := NewStore(StateCell{Value: reflect.ValueOf(SimpleState{Counter: 0})})
	eng := NewEngine(reg)

	// Use an interpreter that re-emits CounterUpdated to create an infinite loop.
	loopInterp := func(ctx context.Context, s *Store, c Command) ([]Event, error) {
		_, ok := c.(SendProvider)
		if !ok {
			return nil, nil
		}
		return []Event{CounterUpdated{}}, nil
	}
	eng.RegisterInterpreter(loopInterp)
	eng.loopLimit = 5

	_, err := eng.Run(context.Background(), store, CounterUpdated{})
	if err == nil {
		t.Fatal("expected loop limit error")
	}
	if !strings.Contains(err.Error(), "loop limit") {
		t.Fatalf("error should mention loop limit, got: %v", err)
	}
}

func TestDebugPlanPrintsEventReducerCommand(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(IncrementCounter)

	store := NewStore(StateCell{Value: reflect.ValueOf(SimpleState{Counter: 0})})
	eng := NewEngine(reg)
	eng.RegisterInterpreter(sendProviderInterpreter)

	result, err := eng.Run(context.Background(), store, CounterUpdated{})
	if err != nil {
		// Loop limit hit due to command recursion, but plan is available
		_ = err // ignore for plan test
	}

	out := PrintPlan(result.Plan)
	if out == "" {
		t.Fatal("expected non-empty plan output")
	}
	if !strings.Contains(out, "event=machine.CounterUpdated") {
		t.Fatalf("plan should show CounterUpdated, got:\n%s", out)
	}
	if !strings.Contains(out, "command: SendProvider") {
		t.Fatalf("plan should show SendProvider, got:\n%s", out)
	}
}

func TestMergeableActivityState(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(func(s ActivityState, e CounterUpdated) (ActivityState, []Event, []Command, error) {
		return ActivityState{Lines: []string{"a"}}, nil, nil, nil
	})
	_ = reg.Register(func(s ActivityState, e CounterUpdated) (ActivityState, []Event, []Command, error) {
		return ActivityState{Lines: []string{"b"}}, nil, nil, nil
	})

	store := NewStore(StateCell{Value: reflect.ValueOf(ActivityState{})})
	eng := NewEngine(reg)

	_, err := eng.Run(context.Background(), store, CounterUpdated{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	var state ActivityState
	if err := store.Get(&state); err != nil {
		t.Fatalf("get state: %v", err)
	}
	joined := strings.Join(state.Lines, ",")
	if !strings.Contains(joined, "a") || !strings.Contains(joined, "b") {
		t.Fatalf("expected merged lines a,b, got: %v", state.Lines)
	}
}

// ConflictState intentionally fails Merge.
type ConflictState struct {
	Value int
}

func (a ConflictState) Merge(b ConflictState) (ConflictState, error) {
	return ConflictState{}, errors.New("conflict: cannot merge")
}

func TestRegisterIdempotency(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(IncrementCounter); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := reg.Register(IncrementCounter); err != nil {
		t.Fatalf("second register (idempotent): %v", err)
	}
	if len(reg.ReducersFor(reflect.TypeOf(CounterUpdated{}))) != 1 {
		t.Fatal("duplicate registration should be idempotent")
	}
}
