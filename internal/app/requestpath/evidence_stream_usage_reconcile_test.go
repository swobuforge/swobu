package requestpath

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/runtimeevidence"
	"github.com/metrofun/swobu/internal/ports"
)

func TestHandle_StreamingEvidenceReconcilesUsageOnCompletedEvent(t *testing.T) {
	endpoint := testChatCompletionsEndpoint(t)
	reader := endpointReaderStub{endpoint: endpoint}
	evidence := &capturingRequestpathEvidenceSink{}

	input := 120
	output := 9
	cacheRead := 70
	cacheWrite := 5
	usage, err := compatibility.NewTokenUsageWithOptional(&input, &output, &cacheRead, &cacheWrite)
	if err != nil {
		t.Fatalf("NewTokenUsageWithOptional returned error: %v", err)
	}

	stream := compatibility.NewSliceEventStream([]compatibility.OutputEvent{
		{Kind: compatibility.OutputEventStarted, ResultID: "resp_1", Model: "m"},
		{Kind: compatibility.OutputEventItemStarted, ItemKind: compatibility.ItemKindText, ItemID: "text_0"},
		{Kind: compatibility.OutputEventTextDelta, ItemID: "text_0", TextDelta: "ok"},
		{Kind: compatibility.OutputEventItemCompleted, ItemKind: compatibility.ItemKindText, ItemID: "text_0"},
		{Kind: compatibility.OutputEventCompleted, FinishReason: "completed", Usage: usage},
	})

	providers := &scriptedProviderExecutor{
		steps: []providerStep{
			{resp: ports.NewStreamingExecuteResponse(stream)},
		},
	}

	handler := NewRequestHandler(reader, providers, evidence, nil)
	out, err := handler.Handle(context.Background(), HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-stream-usage",
		Request: compatibility.NewDialogRequest(
			"m",
			[]compatibility.CanonicalItem{compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi")},
		),
		Contract: NewExecutionContract(compatibility.DeliveryModeStreaming),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if got := len(evidence.events); got != 2 {
		t.Fatalf("evidence events immediately after Handle = %d, want 2 (inflight + terminal)", got)
	}

	for {
		_, readErr := out.Response.Stream().Next()
		if readErr == nil {
			continue
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		t.Fatalf("stream Next returned error: %v", readErr)
	}

	if got := len(evidence.events); got != 3 {
		t.Fatalf("evidence events after stream completion = %d, want 3", got)
	}
	last := evidence.events[2]
	if got := last.Result(); got != runtimeevidence.ResultClassSuccess {
		t.Fatalf("last result = %q, want %q", got, runtimeevidence.ResultClassSuccess)
	}
	if got, ok := last.TokenUsage().InputTokens(); !ok || got != 120 {
		t.Fatalf("last usage input = (%d,%v), want (120,true)", got, ok)
	}
	if got, ok := last.TokenUsage().OutputTokens(); !ok || got != 9 {
		t.Fatalf("last usage output = (%d,%v), want (9,true)", got, ok)
	}
	if got, ok := last.TokenUsage().CacheReadTokens(); !ok || got != 70 {
		t.Fatalf("last usage cache read = (%d,%v), want (70,true)", got, ok)
	}
	if got, ok := last.TokenUsage().CacheWriteTokens(); !ok || got != 5 {
		t.Fatalf("last usage cache write = (%d,%v), want (5,true)", got, ok)
	}
}

func TestHandle_StreamingEvidenceDoesNotReconcileUsageWhenClosedBeforeCompleted(t *testing.T) {
	endpoint := testChatCompletionsEndpoint(t)
	reader := endpointReaderStub{endpoint: endpoint}
	evidence := &capturingRequestpathEvidenceSink{}

	stream := compatibility.NewSliceEventStream([]compatibility.OutputEvent{
		{Kind: compatibility.OutputEventStarted, ResultID: "resp_1", Model: "m"},
		{Kind: compatibility.OutputEventItemStarted, ItemKind: compatibility.ItemKindText, ItemID: "text_0"},
		{Kind: compatibility.OutputEventTextDelta, ItemID: "text_0", TextDelta: "partial"},
	})

	providers := &scriptedProviderExecutor{
		steps: []providerStep{
			{resp: ports.NewStreamingExecuteResponse(stream)},
		},
	}

	handler := NewRequestHandler(reader, providers, evidence, nil)
	out, err := handler.Handle(context.Background(), HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-stream-closed-early",
		Request: compatibility.NewDialogRequest(
			"m",
			[]compatibility.CanonicalItem{compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi")},
		),
		Contract: NewExecutionContract(compatibility.DeliveryModeStreaming),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if got := len(evidence.events); got != 2 {
		t.Fatalf("evidence events immediately after Handle = %d, want 2 (inflight + initial terminal)", got)
	}

	if closeErr := out.Response.Stream().Close(); closeErr != nil {
		t.Fatalf("stream Close returned error: %v", closeErr)
	}
	if got := len(evidence.events); got != 2 {
		t.Fatalf("evidence events after early close = %d, want 2 (no completion reconciliation)", got)
	}
}

type capturingRequestpathEvidenceSink struct {
	events []runtimeevidence.TrafficEvent
}

func (s *capturingRequestpathEvidenceSink) Append(_ context.Context, event runtimeevidence.TrafficEvent) {
	s.events = append(s.events, event)
}
