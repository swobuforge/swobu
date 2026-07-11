package grammar

import (
	"context"
	"io"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

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
