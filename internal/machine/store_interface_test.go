package machine

import (
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// stubEventReader implements canonical.EventReader for testing.
type stubEventReader struct {
	done bool
}

func (s *stubEventReader) Next(ctx context.Context) (canonical.Event, error) {
	if s.done {
		return canonical.Event{}, io.EOF
	}
	s.done = true
	return canonical.Event{}, nil
}

func (s *stubEventReader) Close(ctx context.Context) error { return nil }

// eventReaderState is a cell that uses the real canonical.EventReader interface
// (not a local test interface) so the test proves the store contract for the
// production pipeline.
type eventReaderState struct {
	Reader canonical.EventReader
}

// TestRoundTripCanonicalEventReader exercises store.Put/Get with a real
// canonical.EventReader interface field — the exact scenario that caused a
// nil-panic in the runner machine before the deep-copy fix.
func TestRoundTripCanonicalEventReader(t *testing.T) {
	original := eventReaderState{Reader: &stubEventReader{}}
	store := NewStore(StateCell{Value: reflect.ValueOf(original)})

	var got eventReaderState
	if err := store.Get(&got); err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if got.Reader == nil {
		t.Fatal("canonical.EventReader became nil after store round-trip")
	}
	_, err := got.Reader.Next(t.Context())
	if err != nil {
		t.Fatalf("Reader.Next: %v", err)
	}
}

// TestRoundTripCanonicalEventReaderViaEngine exercises the full engine
// dispatch cycle, which copies stateOut through store.Put.
func TestRoundTripCanonicalEventReaderViaEngine(t *testing.T) {
	reg := NewRegistry()
	reg.Register(func(in eventReaderState, _ eventReaderPing) (eventReaderState, []Event, []Command, error) {
		return in, nil, nil, nil
	})

	eng := NewEngine(reg)
	store := NewStore(StateCell{Value: reflect.ValueOf(eventReaderState{
		Reader: &stubEventReader{},
	})})
	_, err := eng.Run(t.Context(), store, eventReaderPing{})
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}

	var got eventReaderState
	if err := store.Get(&got); err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if got.Reader == nil {
		t.Fatal("canonical.EventReader became nil after engine dispatch")
	}
	_, err = got.Reader.Next(t.Context())
	if err != nil {
		t.Fatalf("Reader.Next: %v", err)
	}
}

type eventReaderPing struct{}
