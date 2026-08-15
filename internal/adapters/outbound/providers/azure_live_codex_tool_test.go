package providers

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	outboundcredentials "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestLiveAzureCodexResponsesRequiredFunctionTool(t *testing.T) {
	if os.Getenv("SWOBU_LIVE_AZURE_CODEX") != "1" {
		t.Skip("set SWOBU_LIVE_AZURE_CODEX=1 to call the configured Azure Codex deployment")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	d := delivery.StreamingDelivery(delivery.FramingSSE)
	schema, err := canonical.ParseJSONObject([]byte(`{"type":"object","properties":{"action":{"type":"string","enum":["inspect"]}},"required":["action"],"additionalProperties":false}`))
	if err != nil {
		t.Fatal(err)
	}
	tool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "record_action"), "record the required test action", canonical.NewToolSchemaObject(schema), canonical.Unspecified[bool]())
	tools, err := canonical.NewToolSet([]canonical.ToolDeclaration{tool})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt-5.3-codex"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, tools.Declarations()...),
			canonicaltest.Message(t, canonical.MessageRoleUser, `Call the record_action tool exactly once with {"action":"inspect"} and do not answer in prose.`),
		},
		ToolPolicy:    canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil)),
		ToolCallBatch: canonical.Specify(canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne)),
	})

	registry := mustProviderRegistry(t, http.DefaultClient, outboundcredentials.NewResolver())
	target := provider.NewTargetSnapshot(
		"default",
		"azure",
		"https://contact-8837-resource.services.ai.azure.com",
		"secret:azure-live",
		protocolkind.Responses,
		"responses", delivery.BufferedDelivery())
	target.Model = request.Model()
	backend, err := registry.ResolveBackend(target)
	if err != nil {
		t.Fatalf("ResolveBackend returned error: %v", err)
	}
	doc, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: d})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	ingress, err := backend.Transport.Send(ctx, doc)
	if err != nil {
		t.Fatalf("SendProviderRequest returned error: %v", err)
	}
	streamIngress, ok := ingress.(provider.StreamIngress)
	if !ok {
		t.Fatalf("provider transport returned %T, want provider.StreamIngress", ingress)
	}
	stream := streamIngress.Stream
	decoded, err := backend.Codec.Decode(ctx, provider.Request{ExchangeID: "ex_live_codex_tool", Canonical: request}, provider.StreamIngress{Stream: stream})
	if err != nil {
		t.Fatalf("DecodeProviderEnvelope returned error: %v", err)
	}
	reader := decoded.Stream
	closed, err := canonical.ReadClosedEnvelope(ctx, reader, canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope returned error: %v", err)
	}
	output, err := closed.ProjectResponse()
	if err != nil {
		t.Fatalf("ProjectResponse returned error: %v", err)
	}

	var text []string
	for _, item := range output.Items() {
		if item.Kind() == canonical.ItemKindToolCall {
			toolUse, _ := item.ToolCall()
			if toolUse.Tool().Name() != "record_action" {
				t.Fatalf("tool name = %q, want record_action", toolUse.Tool().Name())
			}
			input, ok := toolUse.Input().Object()
			if !ok || !strings.Contains(input.String(), `"inspect"`) {
				t.Fatalf("tool input = %q, want inspect action", input.String())
			}
			return
		}
		if message, ok := item.Message(); ok {
			for _, part := range message.Content() {
				if value, ok := part.Text(); ok {
					text = append(text, strings.TrimSpace(value.Text()))
				}
			}
		}
	}
	t.Fatalf("Azure Codex returned no required tool call; text=%q finish=%q", strings.Join(text, "\n"), output.Completion().Reason())
}
