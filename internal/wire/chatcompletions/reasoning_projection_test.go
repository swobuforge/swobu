package chatcompletions

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestBufferedChatProjectionOmitsReasoningAndRecordsPortableDrop(t *testing.T) {
	reasoning := chatReasoningItem(t)
	response, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: "resp"}, "model",
		[]canonical.CanonicalItem{
			reasoning,
			canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer"),
		},
		canonical.Completed("stop"), canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(result.Document.RawBytes(), []byte("brief")) {
		t.Fatalf("standard Chat exposed reasoning: %s", result.Document.RawBytes())
	}
	assertOneChatReasoningDrop(t, result.Decisions)
}

func TestBufferedChatProjectionRejectsReasoningOnlySuccess(t *testing.T) {
	response, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: "resp"}, "model",
		[]canonical.CanonicalItem{chatReasoningItem(t)},
		canonical.Completed("stop"), canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response); err == nil {
		t.Fatal("reasoning-only response became an empty successful Chat response")
	}
}

func TestStreamedChatProjectionRecordsReasoningDropWithVisibleText(t *testing.T) {
	response, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: "resp"}, "model",
		[]canonical.CanonicalItem{
			chatReasoningItem(t),
			canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer"),
		},
		canonical.Completed("stop"), canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := (ResponseStreamEncoder{}).EncodeResponseStream(
		context.Background(), canonical.CanonicalRequest{},
		canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
			"exchange", response.Response(), response.Model(), response.Items(),
			response.Completion(), response.Usage(),
		)),
		delivery.StreamingDelivery(delivery.FramingSSE),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(encoded.Stream.Body); err != nil {
		t.Fatal(err)
	}
	assertOneChatReasoningDrop(t, encoded.TerminalDecisions.Decisions())
}

func TestStreamedChatProjectionRejectsReasoningOnlySuccess(t *testing.T) {
	response, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: "resp"}, "model",
		[]canonical.CanonicalItem{chatReasoningItem(t)},
		canonical.Completed("stop"), canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := (ResponseStreamEncoder{}).EncodeResponseStream(
		context.Background(), canonical.CanonicalRequest{},
		canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
			"exchange", response.Response(), response.Model(), response.Items(),
			response.Completion(), response.Usage(),
		)),
		delivery.StreamingDelivery(delivery.FramingSSE),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(encoded.Stream.Body); err == nil {
		t.Fatal("reasoning-only stream closed as an empty successful Chat response")
	}
	assertOneChatReasoningDrop(t, encoded.TerminalDecisions.Decisions())
}

func chatReasoningItem(t *testing.T) canonical.CanonicalItem {
	t.Helper()
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "brief")
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, canonical.OpaqueThinking{})
	if err != nil {
		t.Fatal(err)
	}
	return reasoning
}

func assertOneChatReasoningDrop(t *testing.T, decisions []compat.Decision) {
	t.Helper()
	count := 0
	for _, decision := range decisions {
		if decision.Feature == compat.ResponseItemsReasoning && decision.Outcome == compat.Drop {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("reasoning drop count = %d in %#v, want 1", count, decisions)
	}
}
