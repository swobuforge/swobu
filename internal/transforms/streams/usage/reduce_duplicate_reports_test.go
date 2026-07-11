package usage

import (
	"context"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type UsageEventReader struct {
	inner        canonical.EventReader
	pending      canonical.EventSequence
	terminalByID map[canonical.EnvelopeID]canonical.Event
	order        []canonical.EnvelopeID
}

func Wrap(inner canonical.EventReader) canonical.EventReader {
	if inner == nil {
		return nil
	}
	return &UsageEventReader{inner: inner, terminalByID: map[canonical.EnvelopeID]canonical.Event{}}
}

func (r *UsageEventReader) Next(ctx context.Context) (canonical.Event, error) {
	if len(r.pending) > 0 {
		ev := r.pending[0]
		r.pending = r.pending[1:]
		return ev, nil
	}
	for {
		ev, err := r.inner.Next(ctx)
		if err != nil {
			if err == io.EOF {
				for _, id := range r.order {
					if pending, ok := r.terminalByID[id]; ok {
						r.pending = append(r.pending, pending)
					}
				}
				r.terminalByID = map[canonical.EnvelopeID]canonical.Event{}
				r.order = r.order[:0]
				if len(r.pending) > 0 {
					out := r.pending[0]
					r.pending = r.pending[1:]
					return out, nil
				}
			}
			return canonical.Event{}, err
		}
		if ev.Kind == canonical.EventUsage {
			if _, ok := r.terminalByID[ev.EnvID]; !ok {
				r.order = append(r.order, ev.EnvID)
			}
			r.terminalByID[ev.EnvID] = ev
			continue
		}
		if ev.Kind == canonical.EventEnvelopeEnd {
			if payload, ok := ev.Payload.(canonical.EnvelopeEndPayload); ok && payload.Kind == canonical.EnvResponse {
				if u, ok := r.terminalByID[ev.EnvID]; ok {
					r.pending = append(r.pending, u)
					delete(r.terminalByID, ev.EnvID)
				}
				r.pending = append(r.pending, ev)
				out := r.pending[0]
				r.pending = r.pending[1:]
				return out, nil
			}
		}
		return ev, nil
	}
}

func (r *UsageEventReader) Close(ctx context.Context) error {
	return r.inner.Close(ctx)
}

func mustUsage(t *testing.T, in int, out int) canonical.TokenUsage {
	t.Helper()
	u, err := canonical.NewTokenUsageWithOptional(&in, &out, nil, nil)
	if err != nil {
		t.Fatalf("NewTokenUsageWithOptional: %v", err)
	}
	return u
}

func collect(t *testing.T, r canonical.EventReader) canonical.EventSequence {
	t.Helper()
	out := canonical.EventSequence{}
	for {
		ev, err := r.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("next: %v", err)
		}
		out = append(out, ev)
	}
	return out
}

func TestWrap_ReducesDuplicateUsageToTerminalOne(t *testing.T) {
	events := canonical.EventSequence{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}, Time: time.Now()},
		{Kind: canonical.EventUsage, EnvID: "r1", Payload: canonical.UsagePayload{Usage: mustUsage(t, 1, 2)}},
		{Kind: canonical.EventTextDelta, EnvID: "m1", Payload: canonical.TextDeltaPayload{Text: "a"}},
		{Kind: canonical.EventUsage, EnvID: "r1", Payload: canonical.UsagePayload{Usage: mustUsage(t, 1, 2)}},
		{Kind: canonical.EventUsage, EnvID: "r1", Payload: canonical.UsagePayload{Usage: mustUsage(t, 3, 4)}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	}
	got := collect(t, Wrap(canonical.NewSliceEventReader(events)))

	usageCount := 0
	for _, ev := range got {
		if ev.Kind == canonical.EventUsage {
			usageCount++
		}
	}
	if usageCount != 1 {
		t.Fatalf("usageCount=%d want 1", usageCount)
	}
	if got[len(got)-2].Kind != canonical.EventUsage || got[len(got)-1].Kind != canonical.EventEnvelopeEnd {
		t.Fatalf("terminal order invalid: %#v", got[len(got)-2:])
	}
}

func TestWrap_Idempotent(t *testing.T) {
	events := canonical.EventSequence{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{Kind: canonical.EventUsage, EnvID: "r1", Payload: canonical.UsagePayload{Usage: mustUsage(t, 3, 4)}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	}
	once := collect(t, Wrap(canonical.NewSliceEventReader(events)))
	twice := collect(t, Wrap(canonical.NewSliceEventReader(once)))
	if !reflect.DeepEqual(once, twice) {
		t.Fatalf("not idempotent\nonce=%#v\ntwice=%#v", once, twice)
	}
}

func TestWrap_PreservesFirstSeenEnvelopeOrderAtEOF(t *testing.T) {
	events := canonical.EventSequence{
		{Kind: canonical.EventEnvelopeStart, EnvID: "z", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{Kind: canonical.EventUsage, EnvID: "z", Payload: canonical.UsagePayload{Usage: mustUsage(t, 1, 1)}},
		{Kind: canonical.EventEnvelopeStart, EnvID: "a", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{Kind: canonical.EventUsage, EnvID: "a", Payload: canonical.UsagePayload{Usage: mustUsage(t, 2, 2)}},
	}
	got := collect(t, Wrap(canonical.NewSliceEventReader(events)))
	if len(got) != 4 {
		t.Fatalf("events len=%d want 4", len(got))
	}
	if got[2].Kind != canonical.EventUsage || got[2].EnvID != "z" {
		t.Fatalf("first terminal usage=%#v want env z", got[2])
	}
	if got[3].Kind != canonical.EventUsage || got[3].EnvID != "a" {
		t.Fatalf("second terminal usage=%#v want env a", got[3])
	}
}
