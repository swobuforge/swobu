package usage

import (
	"context"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

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
