package machine

import (
	"errors"
	"fmt"
	"reflect"
)

// registeredReducer holds the reflected metadata for one reducer function.
type registeredReducer struct {
	fn          reflect.Value
	stateInput  reflect.Type // first arg (may be a composite struct)
	eventInput  reflect.Type // second arg
	stateOutput reflect.Type // first return value
}

// Registry maps event types to the reducers that react to them.
type Registry struct {
	reducers map[reflect.Type][]registeredReducer
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{reducers: make(map[reflect.Type][]registeredReducer)}
}

// Register accepts a function with one of these shapes:
//
//	func(state SomeState, event SomeEvent) (SomeState, []Event, []Command, error)
//
// or a composite-input variant:
//
//	func(state struct { A StateA; B StateB }, event SomeEvent) (StateC, []Event, []Command, error)
//
// Registration is idempotent for the same function value.
func (r *Registry) Register(fn interface{}) error {
	v := reflect.ValueOf(fn)
	if v.Kind() != reflect.Func {
		return errors.New("machine.Register: argument must be a function")
	}
	ft := v.Type()
	if ft.NumIn() != 2 {
		return fmt.Errorf("machine.Register: reducer must take exactly 2 arguments, got %d", ft.NumIn())
	}
	if ft.NumOut() != 4 {
		return fmt.Errorf("machine.Register: reducer must return exactly 4 values, got %d", ft.NumOut())
	}
	// Expected out: (State, []Event, []Command, error)
	// Validate last output is error
	if !ft.Out(3).Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		return errors.New("machine.Register: reducer's fourth return must be error")
	}

	eventType := ft.In(1)
	reg := registeredReducer{
		fn:          v,
		stateInput:  ft.In(0),
		eventInput:  eventType,
		stateOutput: ft.Out(0),
	}

	// Check for duplicate registration
	for _, existing := range r.reducers[eventType] {
		if existing.fn.Pointer() == v.Pointer() {
			return nil // idempotent
		}
	}

	r.reducers[eventType] = append(r.reducers[eventType], reg)
	return nil
}

// ReducersFor returns all reducers registered for a given event type.
func (r *Registry) ReducersFor(eventType reflect.Type) []registeredReducer {
	return r.reducers[eventType]
}

// RegisteredEventTypes returns all event types with at least one reducer.
func (r *Registry) RegisteredEventTypes() []reflect.Type {
	out := make([]reflect.Type, 0, len(r.reducers))
	for t := range r.reducers {
		out = append(out, t)
	}
	return out
}

// buildStateValue constructs the first argument for a reducer from the store.
// If stateInput is a composite struct, each exported field is fetched
// independently from the store and gathered into a new struct value.
func buildStateValue(store *Store, stateInput reflect.Type) (reflect.Value, error) {
	// If the exact type is already in the store, use it directly — even if
	// it is a struct (structs stored directly take precedence over composite
	// assembly).
	if store.Has(stateInput) {
		val, err := store.GetValue(stateInput)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("machine buildStateValue: missing %v: %w", stateInput, err)
		}
		return val, nil
	}

	if stateInput.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("machine buildStateValue: missing %v: %w", stateInput, ErrStateNotFound)
	}

	// Composite struct: fill each exported field from store
	sv := reflect.New(stateInput).Elem()
	for i := 0; i < stateInput.NumField(); i++ {
		f := stateInput.Field(i)
		if !f.IsExported() {
			continue
		}
		val, err := store.GetValue(f.Type)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("machine buildStateValue: field %s (%v): %w", f.Name, f.Type, err)
		}
		sv.Field(i).Set(val)
	}
	return sv, nil
}
