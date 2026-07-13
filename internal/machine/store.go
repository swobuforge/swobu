package machine

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
)

// storeError is an error type for store operations.
type storeError string

func (e storeError) Error() string { return string(e) }

// ErrStateNotFound is returned when the store does not contain a state cell
// of the requested type.
const ErrStateNotFound = storeError("state not found")

// StateCell is an entry in the machine store.
type StateCell struct {
	Type  reflect.Type
	Value reflect.Value
}

// Store is a type-keyed map of state cells.
type Store struct {
	mu     sync.RWMutex
	states map[reflect.Type]reflect.Value
}

// NewStore creates a store with the given initial state cells.
func NewStore(cells ...StateCell) *Store {
	s := &Store{states: make(map[reflect.Type]reflect.Value, len(cells))}
	for _, c := range cells {
		t := c.Type
		if t == nil {
			t = c.Value.Type()
		}
		// Deep copy via Interface→New→Set to preserve interface fields
		cp := reflect.New(t).Elem()
		cp.Set(reflect.ValueOf(c.Value.Interface()))
		s.states[t] = cp
	}
	return s
}

// Put inserts or overwrites one state cell with a deep copy so interface
// fields inside structs survive round-trips.
func (s *Store) Put(t reflect.Type, v reflect.Value) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Deep copy via Interface→New→Set to preserve interface field values
	cp := reflect.New(t).Elem()
	cp.Set(reflect.ValueOf(v.Interface()))
	s.states[t] = cp
}

// Get reads one typed value from the store.
// The target must be a non-nil pointer. On success the pointed-to value
// is overwritten with a deep copy.
func (s *Store) Get(target any) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ptr := reflect.ValueOf(target)
	if ptr.Kind() != reflect.Ptr || ptr.IsNil() {
		return errors.New("machine store Get requires a non-nil pointer")
	}
	want := ptr.Elem().Type()
	v, ok := s.states[want]
	if !ok {
		return fmt.Errorf("%w: %v", ErrStateNotFound, want)
	}
	ptr.Elem().Set(reflect.ValueOf(v.Interface()))
	return nil
}

// GetValue returns a stored value for the given type, or an error if absent.
func (s *Store) GetValue(t reflect.Type) (reflect.Value, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.states[t]
	if !ok {
		return reflect.Value{}, fmt.Errorf("%w: %v", ErrStateNotFound, t)
	}
	return v, nil
}

// Has reports whether the store contains a value for t.
func (s *Store) Has(t reflect.Type) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.states[t]
	return ok
}

// Snapshot returns a shallow copy of the current state map.
func (s *Store) Snapshot() map[reflect.Type]reflect.Value {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[reflect.Type]reflect.Value, len(s.states))
	for t, v := range s.states {
		out[t] = v
	}
	return out
}

// MustGet panics with a descriptive message if the state is absent.
func (s *Store) MustGet(target any) {
	if err := s.Get(target); err != nil {
		panic(fmt.Sprintf("machine store MustGet: %v", err))
	}
}
