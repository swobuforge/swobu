package exchange

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestRunnerRun_StreamToBatchResponseProjectsProviderEventsInternally(t *testing.T) {
	providerSSE := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_1\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"ok\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"
	runner := withRuntime(streamingProviderTransport(io.NopCloser(strings.NewReader(providerSSE))))
	out, err := runPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "ex_stream_to_batch",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    "alpha",
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:           provider.NewTargetSnapshot("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses_stream"),
		Contract:         NewExecutionContractForDeliveries(delivery.BufferedDelivery(), delivery.StreamingDelivery(delivery.FramingSSE)),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if ClientTransportForTest(out).Body == nil {
		t.Fatal("buffered response must set transport body")
	}
	if got := strings.ToLower(ClientTransportForTest(out).Header.Get("Content-Type")); got != "application/json" { // swobu:io-string source=domain
		t.Fatalf("content-type = %q, want application/json", got)
	}
	raw, err := io.ReadAll(ClientTransportForTest(out).Body)
	if err != nil {
		t.Fatalf("read buffered body: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, "ok") {
		t.Fatalf("buffered response missing projected text: %s", body)
	}
}

func TestRunnerRun_BatchToStreamResponseWithoutSourceIncrementality(t *testing.T) {
	runner := withRuntime(bufferedProviderTransport([]byte(`{"id":"resp_1","model":"m","output_text":"ok"}`)))
	out, err := runPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "ex_batch_to_stream",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    "alpha",
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           provider.NewTargetSnapshot("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContractForDeliveries(delivery.StreamingDelivery(delivery.FramingSSE), delivery.BufferedDelivery()),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !ClientResponseStreamingForTest(out) {
		t.Fatal("batch-to-stream conversion must return the concrete streaming response variant")
	}
	if ClientTransportForTest(out).Body == nil {
		t.Fatal("streaming response must set transport body")
	}
	if got := strings.ToLower(ClientTransportForTest(out).Header.Get("Content-Type")); got != "text/event-stream" { // swobu:io-string source=domain
		t.Fatalf("content-type = %q, want text/event-stream", got)
	}
	raw, err := io.ReadAll(ClientTransportForTest(out).Body)
	if err != nil {
		t.Fatalf("read streaming body: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, "response.completed") {
		t.Fatalf("streaming response missing completion marker: %s", body)
	}
	if !strings.Contains(body, "response.output_text.delta") {
		t.Fatalf("streaming response missing delta marker: %s", body)
	}
}
