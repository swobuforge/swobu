package canonical

import (
	"context"
	"io"
)

type EventReader interface {
	// Next returns the next canonical event or io.EOF when the stream is done.
	Next(ctx context.Context) (Event, error)
	// Close releases stream resources. Slice-backed readers are no-op.
	Close(ctx context.Context) error
}

// SliceEventReader exposes a finite event slice through EventReader.
type SliceEventReader struct {
	events []Event
	index  int
}

// NewSliceEventReader clones the input to prevent caller mutation from
// rewriting canonical stream truth after construction.
func NewSliceEventReader(events []Event) *SliceEventReader {
	cloned := make([]Event, len(events))
	copy(cloned, events)
	return &SliceEventReader{events: cloned}
}

func (r *SliceEventReader) Next(context.Context) (Event, error) {
	if r.index >= len(r.events) {
		return Event{}, io.EOF
	}
	ev := r.events[r.index]
	r.index++
	return ev, nil
}

func (r *SliceEventReader) Close(context.Context) error {
	return nil
}
