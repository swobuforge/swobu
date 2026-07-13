package machine

import (
	"reflect"
	"testing"
)

type testReader interface {
	Read() int
}

type testReaderImpl struct {
	v int
}

func (t testReaderImpl) Read() int { return t.v }

type testState struct {
	R testReader
}

func TestRoundTripInterfaceField(t *testing.T) {
	store := NewStore(StateCell{
		Value: reflect.ValueOf(testState{R: testReaderImpl{42}}),
	})
	var s testState
	if err := store.Get(&s); err != nil {
		t.Fatal(err)
	}
	if s.R == nil {
		t.Fatal("interface field lost during store.Get")
	}
	if s.R.Read() != 42 {
		t.Fatalf("Read() = %d, want 42", s.R.Read())
	}
}

// TestRoundTripWithEngine exercises a full dispatch/store/Get cycle
// through the engine to ensure stateOut → store.Put → store.Get
// preserves interface fields.
func TestRoundTripWithEngine(t *testing.T) {
	reg := NewRegistry()
	reg.Register(func(in testState, _ eventRead) (testState, []Event, []Command, error) {
		return in, nil, nil, nil
	})

	eng := NewEngine(reg)
	store := NewStore(StateCell{Value: reflect.ValueOf(testState{R: testReaderImpl{42}})})
	_, err := eng.Run(t.Context(), store, eventRead{})
	if err != nil {
		t.Fatal(err)
	}
	var s testState
	if err := store.Get(&s); err != nil {
		t.Fatal(err)
	}
	if s.R == nil {
		t.Fatal("interface field lost after engine dispatch")
	}
	if s.R.Read() != 42 {
		t.Fatalf("Read() = %d, want 42", s.R.Read())
	}
}

type eventRead struct{}
