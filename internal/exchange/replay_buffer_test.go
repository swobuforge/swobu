package exchange

import (
	"context"
	"io"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type replayBufferReader struct {
	events []canonical.Event
	index  int
	closed bool
}

func (r *replayBufferReader) Next(context.Context) (canonical.Event, error) {
	if r.index >= len(r.events) {
		return canonical.Event{}, io.EOF
	}
	ev := r.events[r.index]
	r.index++
	return ev, nil
}

func (r *replayBufferReader) Close(context.Context) error {
	r.closed = true
	return nil
}

func TestReplayable_CapturesAndReplaysEvents(t *testing.T) {
	source := &replayBufferReader{
		events: []canonical.Event{
			{ExchangeID: "ex-1", Seq: 1, Kind: canonical.EventMetadata, EnvID: "r1"},
			{ExchangeID: "ex-1", Seq: 2, Kind: canonical.EventFinish, EnvID: "r1"},
		},
	}

	buffer, err := Replayable(context.Background(), source, 10)
	if err != nil {
		t.Fatalf("Replayable returned error: %v", err)
	}
	if !source.closed {
		t.Fatal("source reader was not closed")
	}
	if len(buffer.Events) != 2 {
		t.Fatalf("buffer len = %d, want 2", len(buffer.Events))
	}

	source.events[0].Seq = 99
	replayed := buffer.Reader()
	defer func() { _ = replayed.Close(context.Background()) }()

	first, err := replayed.Next(context.Background())
	if err != nil {
		t.Fatalf("replayed first event error: %v", err)
	}
	if first.Seq != 1 {
		t.Fatalf("replayed first seq = %d, want 1", first.Seq)
	}
}

func TestReplayable_RejectsOverLimit(t *testing.T) {
	source := &replayBufferReader{
		events: []canonical.Event{
			{ExchangeID: "ex-1", Seq: 1, Kind: canonical.EventMetadata, EnvID: "r1"},
			{ExchangeID: "ex-1", Seq: 2, Kind: canonical.EventFinish, EnvID: "r1"},
		},
	}

	_, err := Replayable(context.Background(), source, 1)
	if err == nil {
		t.Fatal("Replayable returned nil error for over-limit replay")
	}
	if err.Error() != "replay buffer limit exceeded" {
		t.Fatalf("Replayable error = %q, want replay buffer limit exceeded", err.Error())
	}
	if !source.closed {
		t.Fatal("source reader was not closed after limit error")
	}
}
