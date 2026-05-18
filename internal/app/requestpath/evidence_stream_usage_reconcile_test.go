package requestpath

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/runtimeevidence"
	"github.com/swobuforge/swobu/internal/ports"
)

func TestHandle_StreamingEvidenceReconcilesUsageOnCompletedEvent(t *testing.T) {
	endpoint := testChatCompletionsEndpoint(t)
	reader := endpointReaderStub{endpoint: endpoint}
	evidence := &capturingRequestpathEvidenceSink{}

	input := 120
	output := 9
	cacheRead := 70
	cacheWrite := 5
	usage, err := canonical.NewTokenUsageWithOptional(&input, &output, &cacheRead, &cacheWrite)
	if err != nil {
		t.Fatalf("NewTokenUsageWithOptional returned error: %v", err)
	}

	stream := canonical.NewSliceEventReader([]canonical.Event{
		{ExchangeID: "test_exchange", Seq: 1, Kind: canonical.EventEnvelopeStart, EnvID: "res_1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{ExchangeID: "test_exchange", Seq: 2, Kind: canonical.EventMetadata, EnvID: "res_1", Payload: canonical.MetadataPayload{Values: map[string]string{"result_id": "resp_1", "model": "m"}}},
		{ExchangeID: "test_exchange", Seq: 3, Kind: canonical.EventEnvelopeStart, EnvID: "msg_1", ParentID: "res_1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvMessage, Role: canonical.ItemAuthorAssistant}, Meta: canonical.EventMetadataFields{NativeID: "text_0"}},
		{ExchangeID: "test_exchange", Seq: 4, Kind: canonical.EventTextDelta, EnvID: "msg_1", ParentID: "res_1", Payload: canonical.TextDeltaPayload{Text: "ok"}},
		{ExchangeID: "test_exchange", Seq: 5, Kind: canonical.EventEnvelopeEnd, EnvID: "msg_1", ParentID: "res_1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvMessage, Status: canonical.EnvelopeStatusCompleted}},
		{ExchangeID: "test_exchange", Seq: 6, Kind: canonical.EventUsage, EnvID: "res_1", Payload: canonical.UsagePayload{Usage: usage}},
		{ExchangeID: "test_exchange", Seq: 7, Kind: canonical.EventFinish, EnvID: "res_1", Payload: canonical.FinishPayload{Reason: "completed"}},
		{ExchangeID: "test_exchange", Seq: 8, Kind: canonical.EventEnvelopeEnd, EnvID: "res_1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	})

	providers := &scriptedProviderExecutor{
		steps: []providerStep{
			{resp: ports.NewEnvelopeStreamingProviderResponse(stream)},
		},
	}

	handler := NewRequestHandler(reader, providers, evidence, nil)
	out, err := handler.Handle(context.Background(), HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-stream-usage",
		Request: canonical.NewDialogRequest(
			"m",
			[]canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		),
		Contract: NewExecutionContract(true),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if got := len(evidence.events); got != 2 {
		t.Fatalf("evidence events immediately after Handle = %d, want 2 (inflight + terminal)", got)
	}

	for {
		_, readErr := out.Response.EnvelopeStream().Next(context.Background())
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

	stream := canonical.NewSliceEventReader([]canonical.Event{
		{ExchangeID: "test_exchange", Seq: 1, Kind: canonical.EventEnvelopeStart, EnvID: "res_1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{ExchangeID: "test_exchange", Seq: 2, Kind: canonical.EventEnvelopeStart, EnvID: "msg_1", ParentID: "res_1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvMessage, Role: canonical.ItemAuthorAssistant}, Meta: canonical.EventMetadataFields{NativeID: "text_0"}},
		{ExchangeID: "test_exchange", Seq: 3, Kind: canonical.EventTextDelta, EnvID: "msg_1", ParentID: "res_1", Payload: canonical.TextDeltaPayload{Text: "partial"}},
	})

	providers := &scriptedProviderExecutor{
		steps: []providerStep{
			{resp: ports.NewEnvelopeStreamingProviderResponse(stream)},
		},
	}

	handler := NewRequestHandler(reader, providers, evidence, nil)
	out, err := handler.Handle(context.Background(), HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-stream-closed-early",
		Request: canonical.NewDialogRequest(
			"m",
			[]canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		),
		Contract: NewExecutionContract(true),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if got := len(evidence.events); got != 2 {
		t.Fatalf("evidence events immediately after Handle = %d, want 2 (inflight + initial terminal)", got)
	}

	if closeErr := out.Response.EnvelopeStream().Close(context.Background()); closeErr != nil {
		t.Fatalf("stream Close returned error: %v", closeErr)
	}
	if got := len(evidence.events); got != 2 {
		t.Fatalf("evidence events after early close = %d, want 2 (no completion reconciliation)", got)
	}
}

func TestHandle_StreamingEvidenceReconcilesTimingWithoutUsage(t *testing.T) {
	endpoint := testChatCompletionsEndpoint(t)
	reader := endpointReaderStub{endpoint: endpoint}
	evidence := &capturingRequestpathEvidenceSink{}

	stream := canonical.NewSliceEventReader([]canonical.Event{
		{ExchangeID: "test_exchange", Seq: 1, Kind: canonical.EventEnvelopeStart, EnvID: "res_1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{ExchangeID: "test_exchange", Seq: 2, Kind: canonical.EventFinish, EnvID: "res_1", Payload: canonical.FinishPayload{Reason: "completed"}},
		{ExchangeID: "test_exchange", Seq: 3, Kind: canonical.EventEnvelopeEnd, EnvID: "res_1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	})

	providers := &scriptedProviderExecutor{
		steps: []providerStep{
			{resp: ports.NewEnvelopeStreamingProviderResponse(stream)},
		},
	}

	handler := NewRequestHandler(reader, providers, evidence, nil)
	out, err := handler.Handle(context.Background(), HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-stream-timing-only",
		Request: canonical.NewDialogRequest(
			"m",
			[]canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		),
		Contract: NewExecutionContract(true),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	for {
		_, readErr := out.Response.EnvelopeStream().Next(context.Background())
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
	if _, ok := last.Timing().DurationMillis(); !ok {
		t.Fatal("last timing duration missing, want present")
	}
	if _, ok := last.Timing().TTFBMillis(); !ok {
		t.Fatal("last timing ttfb missing, want present")
	}
	if !last.TokenUsage().IsZero() {
		t.Fatal("last usage should remain unknown when stream completed without usage payload")
	}
}

func TestWrapEvidenceEnvelopeWithUsageReconciliation_CompletedResponseEmitsTerminalUsageEvent(t *testing.T) {
	evidence := &capturingRequestpathEvidenceSink{}
	input := 11
	output := 4
	usage, err := canonical.NewTokenUsageWithOptional(&input, &output, nil, nil)
	if err != nil {
		t.Fatalf("NewTokenUsageWithOptional returned error: %v", err)
	}
	reader := canonical.NewSliceEventReader([]canonical.Event{
		{ExchangeID: "ex_ev_env", Seq: 1, Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{ExchangeID: "ex_ev_env", Seq: 2, Kind: canonical.EventUsage, EnvID: "r1", Payload: canonical.UsagePayload{Usage: usage}},
		{ExchangeID: "ex_ev_env", Seq: 3, Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	})
	target := ports.NewRoutableTarget(
		"backend-a",
		"openai_compatible",
		"http://localhost:8080/v1",
		"cred-1",
		"chat_completions",
		"",
	)
	wrapped := wrapEvidenceEnvelopeWithUsageReconciliation(
		context.Background(),
		evidence,
		reader,
		"req-ev-env-1",
		mustEndpointName(t, "alpha"),
		target,
		IngressProvenance{},
		1,
		false,
		"",
		"",
		"",
		"",
	)
	for {
		_, readErr := wrapped.Next(context.Background())
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			t.Fatalf("wrapped.Next returned error: %v", readErr)
		}
	}
	if got := len(evidence.events); got != 1 {
		t.Fatalf("reconciled evidence events = %d, want 1", got)
	}
	last := evidence.events[0]
	if got, ok := last.TokenUsage().InputTokens(); !ok || got != 11 {
		t.Fatalf("input tokens = (%d,%v), want (11,true)", got, ok)
	}
	if got, ok := last.TokenUsage().OutputTokens(); !ok || got != 4 {
		t.Fatalf("output tokens = (%d,%v), want (4,true)", got, ok)
	}
}

func TestWrapEvidenceEnvelopeWithUsageReconciliation_NoCompletedResponseDoesNotEmit(t *testing.T) {
	evidence := &capturingRequestpathEvidenceSink{}
	reader := canonical.NewSliceEventReader([]canonical.Event{
		{ExchangeID: "ex_ev_env", Seq: 1, Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{ExchangeID: "ex_ev_env", Seq: 2, Kind: canonical.EventError, EnvID: "r1", Payload: canonical.ErrorPayload{Code: "backend_stream_error", Message: "dropped"}},
	})
	target := ports.NewRoutableTarget(
		"backend-a",
		"openai_compatible",
		"http://localhost:8080/v1",
		"cred-1",
		"chat_completions",
		"",
	)
	wrapped := wrapEvidenceEnvelopeWithUsageReconciliation(
		context.Background(),
		evidence,
		reader,
		"req-ev-env-2",
		mustEndpointName(t, "alpha"),
		target,
		IngressProvenance{},
		1,
		false,
		"",
		"",
		"",
		"",
	)
	for {
		_, readErr := wrapped.Next(context.Background())
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			t.Fatalf("wrapped.Next returned error: %v", readErr)
		}
	}
	if got := len(evidence.events); got != 0 {
		t.Fatalf("reconciled evidence events = %d, want 0", got)
	}
}

type capturingRequestpathEvidenceSink struct {
	events []runtimeevidence.TrafficEvent
}

func (s *capturingRequestpathEvidenceSink) Append(_ context.Context, event runtimeevidence.TrafficEvent) {
	s.events = append(s.events, event)
}

func mustEndpointName(t *testing.T, raw string) endpointintent.EndpointName {
	t.Helper()
	name, err := endpointintent.ParseEndpointName(raw)
	if err != nil {
		t.Fatalf("ParseEndpointName(%q) error: %v", raw, err)
	}
	return name
}
