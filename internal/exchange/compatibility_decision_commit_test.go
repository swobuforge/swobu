package exchange_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	. "github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/provider"
)

type recordingDecisionSink struct {
	effects []compat.Decision
}

func (s *recordingDecisionSink) Commit(_ context.Context, _ string, effects []compat.Decision) error {
	s.effects = append(s.effects, effects...)
	return nil
}

func assertCompatibilityEffect(t *testing.T, got compat.Decision, feature compat.Feature, outcome compat.Outcome, subject compat.Subject) {
	t.Helper()
	compatEffect := got
	if compatEffect.Feature != feature || compatEffect.Outcome != outcome || compatEffect.Subject != subject {
		t.Fatalf("compatibility effect = %#v, want %s/%s/%s", compatEffect, feature, outcome, subject)
	}
}

func TestRunnerRun_RecordsDeliveryStreamingDecisionExact(t *testing.T) {
	sink := &recordingDecisionSink{}
	providerSSE := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_1\",\"item_id\":\"msg_1\",\"delta\":\"ok\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"
	runner := withRuntime(streamingProviderTransport(io.NopCloser(strings.NewReader(providerSSE))))
	runner.DecisionSink = sink

	out, err := RunPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "ex_stream_exact",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    testWorkspaceSlug(),
		Target:           provider.NewTargetSnapshot("backend-a", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses_stream"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !ClientResponseStreamingForTest(out) {
		t.Fatal("expected incremental streaming")
	}
	if len(sink.effects) != 3 {
		t.Fatalf("captured effects len=%d want=3", len(sink.effects))
	}
	assertCompatibilityEffect(t, sink.effects[0], compat.DeliveryStreaming, compat.Exact, compat.Subject("route:provider/openai/protocol/responses"))
	assertCompatibilityEffect(t, sink.effects[1], compat.DeliveryIncremental, compat.Exact, compat.Subject("route:provider/openai/protocol/responses"))
	assertCompatibilityEffect(t, sink.effects[2], compat.DeliveryServerSentEvents, compat.Exact, compat.Subject("route:provider/openai/protocol/responses"))
}

func TestRunnerRun_RecordsDeliveryStreamingDecisionApproxWhenBuffered(t *testing.T) {
	sink := &recordingDecisionSink{}
	runner := withRuntime(bufferedProviderTransport([]byte(`{"id":"resp_1","model":"m","output_text":"ok"}`)))
	runner.DecisionSink = sink

	out, err := RunPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "ex_stream_approx",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    testWorkspaceSlug(),
		Target:           provider.NewTargetSnapshot("backend-a", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContractForDeliveries(delivery.StreamingDelivery(delivery.FramingSSE), delivery.BufferedDelivery()),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !ClientResponseStreamingForTest(out) {
		t.Fatal("buffered-provider conversion must still return the concrete streaming response variant")
	}
	if len(sink.effects) != 4 {
		t.Fatalf("captured effects len=%d want=4", len(sink.effects))
	}
	assertCompatibilityEffect(t, sink.effects[0], compat.DeliveryStreaming, compat.Approx, compat.Subject("route:provider/openai/protocol/responses"))
	assertCompatibilityEffect(t, sink.effects[1], compat.DeliveryIncremental, compat.Approx, compat.Subject("route:provider/openai/protocol/responses"))
	assertCompatibilityEffect(t, sink.effects[2], compat.DeliveryServerSentEvents, compat.Exact, compat.Subject("route:provider/openai/protocol/responses"))
	assertCompatibilityEffect(t, sink.effects[3], compat.ResponseIDResponses, compat.Exact, compat.Subject("wire:/id"))
}

func TestRunnerRun_RecordsDeliveryWebSocketConversionApproxWhenProviderIsSSE(t *testing.T) {
	sink := &recordingDecisionSink{}
	providerSSE := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_1\",\"item_id\":\"msg_1\",\"delta\":\"ok\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"
	runner := withRuntime(streamingProviderTransport(io.NopCloser(strings.NewReader(providerSSE))))
	runner.DecisionSink = sink

	out, err := RunPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "ex_stream_websocket",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingWebSocket),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    testWorkspaceSlug(),
		Target:           provider.NewTargetSnapshot("backend-a", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses_stream"),
		Contract:         NewExecutionContractForDeliveries(delivery.StreamingDelivery(delivery.FramingWebSocket), delivery.StreamingDelivery(delivery.FramingSSE)),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !ClientResponseStreamingForTest(out) {
		t.Fatal("expected incremental streaming")
	}
	if len(sink.effects) != 4 {
		t.Fatalf("captured effects len=%d want=4", len(sink.effects))
	}
	assertCompatibilityEffect(t, sink.effects[0], compat.DeliveryStreaming, compat.Exact, compat.Subject("route:provider/openai/protocol/responses"))
	assertCompatibilityEffect(t, sink.effects[1], compat.DeliveryIncremental, compat.Exact, compat.Subject("route:provider/openai/protocol/responses"))
	assertCompatibilityEffect(t, sink.effects[2], compat.DeliveryServerSentEvents, compat.Approx, compat.Subject("route:provider/openai/protocol/responses"))
	assertCompatibilityEffect(t, sink.effects[3], compat.DeliveryWebSocket, compat.Approx, compat.Subject("route:provider/openai/protocol/responses"))
}

func TestRunnerRun_RecordsDeliveryTerminalEventDropWhenProviderStreamLacksUsage(t *testing.T) {
	sink := &recordingDecisionSink{}
	providerSSE := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_1\",\"item_id\":\"msg_1\",\"delta\":\"ok\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"
	runner := withRuntime(streamingProviderTransport(io.NopCloser(strings.NewReader(providerSSE))))
	runner.DecisionSink = sink

	out, err := RunPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "ex_terminal_drop",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    testWorkspaceSlug(),
		Target:           provider.NewTargetSnapshot("backend-a", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses_stream"),
		Contract:         NewExecutionContractForDeliveries(delivery.BufferedDelivery(), delivery.StreamingDelivery(delivery.FramingSSE)),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if ClientTransportForTest(out).Body == nil {
		t.Fatal("buffered client response must set transport body")
	}
	if _, err := io.ReadAll(ClientTransportForTest(out).Body); err != nil {
		t.Fatalf("consume buffered client response: %v", err)
	}
	if len(sink.effects) != 2 {
		t.Fatalf("captured effects len=%d want=2", len(sink.effects))
	}
	assertCompatibilityEffect(t, sink.effects[0], compat.ResponseIDResponses, compat.Exact, compat.Subject("wire:/id"))
	assertCompatibilityEffect(t, sink.effects[1], compat.DeliveryTerminalEvent, compat.Drop, compat.Subject("wire:/event/terminal"))
}

func TestRunnerRun_RecordsErrorShapeDropOnBackendError(t *testing.T) {
	sink := &recordingDecisionSink{}
	runner := withRuntime(func(_ context.Context, target provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
		return nil, canonical.NewBackendError(target.TargetID, http.StatusBadGateway, `{"error":{"message":"provider failed"}}`, "")
	})
	runner.DecisionSink = sink

	_, err := RunPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "ex_error_shape",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    testWorkspaceSlug(),
		Target:           provider.NewTargetSnapshot("backend-a", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
	})
	if err == nil {
		t.Fatal("Run returned nil error, want backend error")
	}
	var backendErr canonical.BackendError
	if !errors.As(err, &backendErr) {
		t.Fatalf("Run error = %v, want BackendError", err)
	}
	if len(sink.effects) != 1 {
		t.Fatalf("captured effects len=%d want=1", len(sink.effects))
	}
	compatEffect := sink.effects[0]
	if compatEffect.Feature != compat.ErrorShape || compatEffect.Outcome != compat.Drop {
		t.Fatalf("compatibility effect = %#v, want error.shape drop", compatEffect)
	}
	if compatEffect.Subject != compat.Subject("route:provider/openai/protocol/responses") {
		t.Fatalf("compatibility subject = %q, want route subject", compatEffect.Subject)
	}
}
