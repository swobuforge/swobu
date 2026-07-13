package machine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
)

// Interpreter executes commands and returns follow-up events.
type Interpreter func(ctx context.Context, store *Store, cmd Command) ([]Event, error)

// PlanStep records one step in the machine's execution for debug output.
type PlanStep struct {
	Event    Event
	Reducers []string
	Commands []string
}

// Result is the outcome of running the machine.
type Result struct {
	Terminal bool
	Plan     []PlanStep
}

// Engine is the event-driven state machine.
type Engine struct {
	registry     *Registry
	interpreters []Interpreter
	loopLimit    int
}

// NewEngine creates an engine with a default loop limit of 1000.
func NewEngine(registry *Registry) *Engine {
	return &Engine{
		registry:  registry,
		loopLimit: 1000,
	}
}

// RegisterInterpreter adds a command interpreter. Callers usually add
// one interpreter per command type using a type switch inside.
func (e *Engine) RegisterInterpreter(i Interpreter) {
	e.interpreters = append(e.interpreters, i)
}

// Run executes the machine starting from the given store and seed event.
func (e *Engine) Run(ctx context.Context, store *Store, seed Event) (Result, error) {
	queue := []Event{seed}
	result := Result{}
	steps := make([]PlanStep, 0, 16)
	loopCount := 0

	for len(queue) > 0 {
		if loopCount >= e.loopLimit {
			return Result{Terminal: false, Plan: steps}, fmt.Errorf("machine loop limit %d reached", e.loopLimit)
		}
		loopCount++

		event := queue[0]
		queue = queue[1:]

		reducerNames, newEvents, newCommands, err := e.dispatch(ctx, store, event)
		if err != nil {
			return Result{Terminal: false, Plan: steps}, err
		}

		steps = append(steps, PlanStep{
			Event:    event,
			Reducers: reducerNames,
			Commands: commandNames(newCommands),
		})

		// Run commands through interpreters
		for _, cmd := range newCommands {
			cmdEvents, err := e.runCommand(ctx, store, cmd)
			if err != nil {
				return Result{Terminal: false, Plan: steps}, err
			}
			newEvents = append(newEvents, cmdEvents...)
		}

		queue = append(queue, newEvents...)

		if isTerminal(event) {
			result.Terminal = true
			result.Plan = steps
			return result, nil
		}
	}

	result.Plan = steps
	return result, nil
}

func (e *Engine) dispatch(ctx context.Context, store *Store, event Event) ([]string, []Event, []Command, error) {
	reducers := e.registry.ReducersFor(reflect.TypeOf(event))
	if len(reducers) == 0 {
		// No reducers registered is not an error — events may be facts
		// that only sidecars observe.
		return nil, nil, nil, nil
	}

	outputs := make(map[reflect.Type][]reflect.Value)
	var reducerNames []string
	var allEvents []Event
	var allCommands []Command

	for _, reducer := range reducers {
		// Build input from store snapshot. All reducers for one event
		// read the same pre-event snapshot.
		stateIn, err := buildStateValue(store, reducer.stateInput)
		if err != nil {
			return nil, nil, nil, err
		}

		vals := reducer.fn.Call([]reflect.Value{
			stateIn,
			reflect.ValueOf(event),
		})

		stateOut := vals[0]
		eventsVal := vals[1]
		commandsVal := vals[2]
		errVal := vals[3]

		if !errVal.IsNil() {
			return nil, nil, nil, errVal.Interface().(error)
		}

		// Collect output
		outType := stateOut.Type()
		outputs[outType] = append(outputs[outType], stateOut)
		reducerNames = append(reducerNames, funcName(reducer.fn))

		// Gather events
		for i := 0; i < eventsVal.Len(); i++ {
			allEvents = append(allEvents, eventsVal.Index(i).Interface().(Event))
		}

		// Gather commands
		for i := 0; i < commandsVal.Len(); i++ {
			allCommands = append(allCommands, commandsVal.Index(i).Interface().(Command))
		}
	}

	// Merge outputs of the same type
	for outType, vals := range outputs {
		if len(vals) == 1 {
			store.Put(outType, vals[0])
			continue
		}

		// Check for Mergeable
		merged, err := mergeOutputs(outType, vals)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("machine merge %v: %w", outType, err)
		}
		store.Put(outType, merged)
	}

	return reducerNames, allEvents, allCommands, nil
}

func (e *Engine) runCommand(ctx context.Context, store *Store, cmd Command) ([]Event, error) {
	var all []Event
	for _, interp := range e.interpreters {
		events, err := interp(ctx, store, cmd)
		if err != nil {
			return nil, err
		}
		all = append(all, events...)
	}
	return all, nil
}

func mergeOutputs(outType reflect.Type, vals []reflect.Value) (reflect.Value, error) {
	if len(vals) == 0 {
		return reflect.Value{}, errors.New("no values to merge")
	}

	// Use reflection to call Merge(other T) if it exists.
	mergeMethod, found := outType.MethodByName("Merge")
	if !found {
		// Try pointer receiver
		mergeMethod, found = reflect.PtrTo(outType).MethodByName("Merge")
		if !found {
			return reflect.Value{}, fmt.Errorf("state %v produced by %d reducers but does not implement Mergeable[%v]", outType, len(vals), outType.Name())
		}
		// Pointer-receiver Merge: we need pointer values
		result := reflect.New(outType)
		result.Elem().Set(vals[0])
		for i := 1; i < len(vals); i++ {
			otherPtr := reflect.New(outType)
			otherPtr.Elem().Set(vals[i])
			retVals := mergeMethod.Func.Call([]reflect.Value{result, otherPtr})
			if !retVals[1].IsNil() {
				return reflect.Value{}, retVals[1].Interface().(error)
			}
			result = retVals[0].Addr()
		}
		return result.Elem(), nil
	}

	// Value-receiver Merge
	result := vals[0]
	for i := 1; i < len(vals); i++ {
		retVals := mergeMethod.Func.Call([]reflect.Value{result, vals[i]})
		if !retVals[1].IsNil() {
			return reflect.Value{}, retVals[1].Interface().(error)
		}
		result = retVals[0]
	}
	return result, nil
}

func funcName(v reflect.Value) string {
	if v.IsNil() {
		return "<nil>"
	}
	n := runtime.FuncForPC(v.Pointer()).Name()
	// Strip package path
	if idx := strings.LastIndex(n, "/"); idx >= 0 {
		n = n[idx+1:]
	}
	if idx := strings.LastIndex(n, "."); idx >= 0 {
		n = n[idx+1:]
	}
	return n
}

func isTerminal(event Event) bool {
	// Terminal events by type name convention
	t := reflect.TypeOf(event)
	name := t.Name()
	return strings.HasSuffix(name, "Terminal") || strings.HasSuffix(name, "Stop")
}

func commandNames(cmds []Command) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		t := reflect.TypeOf(c)
		out[i] = t.Name()
	}
	return out
}
