package eventgrammar

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func Wrap(inner canonical.EventReader) canonical.EventReader {
	if inner == nil {
		return nil
	}
	return &EventReader{inner: inner, stateByID: map[canonical.EnvelopeID]canonical.EnvelopeKind{}}
}

type EventReader struct {
	inner     canonical.EventReader
	stateByID map[canonical.EnvelopeID]canonical.EnvelopeKind
}

func (r *EventReader) Next(ctx context.Context) (canonical.Event, error) {
	ev, err := r.inner.Next(ctx)
	if err != nil {
		if err == io.EOF && len(r.stateByID) > 0 {
			return canonical.Event{}, fmt.Errorf("envelope stream ended with open envelopes: %s", openEnvelopeIDs(r.stateByID))
		}
		return canonical.Event{}, err
	}
	switch ev.Kind {
	case canonical.EventEnvelopeStart:
		payload, ok := ev.Payload.(canonical.EnvelopeStartPayload)
		if !ok {
			return canonical.Event{}, fmt.Errorf("envelope start missing payload")
		}
		if _, exists := r.stateByID[ev.EnvID]; exists {
			return canonical.Event{}, fmt.Errorf("envelope start duplicated for %q", ev.EnvID)
		}
		r.stateByID[ev.EnvID] = payload.Kind
	case canonical.EventEnvelopeEnd:
		payload, ok := ev.Payload.(canonical.EnvelopeEndPayload)
		if !ok {
			return canonical.Event{}, fmt.Errorf("envelope end missing payload")
		}
		startedKind, exists := r.stateByID[ev.EnvID]
		if !exists {
			return canonical.Event{}, fmt.Errorf("envelope end without start for %q", ev.EnvID)
		}
		if startedKind != payload.Kind {
			return canonical.Event{}, fmt.Errorf("envelope kind mismatch for %q", ev.EnvID)
		}
		delete(r.stateByID, ev.EnvID)
	}
	return ev, nil
}

func (r *EventReader) Close(ctx context.Context) error {
	if len(r.stateByID) > 0 {
		return fmt.Errorf("cannot close with open envelopes: %s", openEnvelopeIDs(r.stateByID))
	}
	err := r.inner.Close(ctx)
	if err != nil && err != io.EOF {
		return err
	}
	return nil
}

func openEnvelopeIDs(state map[canonical.EnvelopeID]canonical.EnvelopeKind) string {
	if len(state) == 0 {
		return ""
	}
	ids := make([]string, 0, len(state))
	for id := range state {
		ids = append(ids, strings.TrimSpace(string(id))) // swobu:io-string source=boundary
	}
	return strings.Join(ids, ",")
}

func TestWrap_ValidEnvelopePassThrough(t *testing.T) {
	in := canonical.NewSliceEventReader(canonical.EventSequence{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{Kind: canonical.EventTextDelta, EnvID: "r1", Payload: canonical.TextDeltaPayload{Text: "ok"}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	})
	wrapped := Wrap(in)
	count := 0
	for {
		_, err := wrapped.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		count++
	}
	if count != 3 {
		t.Fatalf("count=%d want 3", count)
	}
}

func TestWrap_EndWithoutStartFails(t *testing.T) {
	in := canonical.NewSliceEventReader(canonical.EventSequence{{Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}}})
	wrapped := Wrap(in)
	_, err := wrapped.Next(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWrap_EOFWithOpenEnvelopeFails(t *testing.T) {
	in := canonical.NewSliceEventReader(canonical.EventSequence{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
	})
	wrapped := Wrap(in)
	if _, err := wrapped.Next(context.Background()); err != nil {
		t.Fatalf("first next: %v", err)
	}
	if _, err := wrapped.Next(context.Background()); err == nil {
		t.Fatal("expected eof validation error for open envelope")
	}
}

func TestWrap_CloseWithOpenEnvelopeFails(t *testing.T) {
	in := canonical.NewSliceEventReader(canonical.EventSequence{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
	})
	wrapped := Wrap(in)
	if _, err := wrapped.Next(context.Background()); err != nil {
		t.Fatalf("first next: %v", err)
	}
	if err := wrapped.Close(context.Background()); err == nil {
		t.Fatal("expected close error for open envelope")
	}
}
