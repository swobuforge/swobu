package exchange

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
)

func TestRunnerRun_StreamToBatchResponseProjectsProviderEventsInternally(t *testing.T) {
	providerSSE := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_1\",\"item_id\":\"msg_1\",\"delta\":\"ok\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"
	runner := withRuntime(Runner{ResolveProviderIngress: streamingProviderIngressResolver(io.NopCloser(strings.NewReader(providerSSE)))})
	out, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_stream_to_batch",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses_stream"),
		Contract:         NewExecutionContractForDeliveries(protocolsurface.BufferedDelivery(), protocolsurface.StreamingDelivery(protocolsurface.FramingSSE)),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Transport.Body == nil {
		t.Fatal("buffered response must set transport body")
	}
	if got := strings.ToLower(out.Transport.Header.Get("Content-Type")); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	raw, err := io.ReadAll(out.Transport.Body)
	if err != nil {
		t.Fatalf("read buffered body: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, "ok") {
		t.Fatalf("buffered response missing projected text: %s", body)
	}
}

func TestRunnerRun_BatchToStreamResponseWithoutSourceIncrementality(t *testing.T) {
	runner := withRuntime(Runner{ResolveProviderIngress: bufferedProviderIngressResolver([]byte(`{"id":"resp_1","model":"m","output_text":"ok"}`))})
	out, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_batch_to_stream",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses"),
		Contract:         NewExecutionContractForDeliveries(protocolsurface.StreamingDelivery(protocolsurface.FramingSSE), protocolsurface.BufferedDelivery()),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Progressive {
		t.Fatal("batch-to-stream response should not claim source incrementality")
	}
	if out.Transport.Body == nil {
		t.Fatal("streaming response must set transport body")
	}
	if got := strings.ToLower(out.Transport.Header.Get("Content-Type")); got != "text/event-stream" {
		t.Fatalf("content-type = %q, want text/event-stream", got)
	}
	raw, err := io.ReadAll(out.Transport.Body)
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
