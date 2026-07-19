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
)

func TestLiveAzureCodexResponsesRequiredFunctionTool(t *testing.T) {
	if os.Getenv("SWOBU_LIVE_AZURE_CODEX") != "1" {
		t.Skip("set SWOBU_LIVE_AZURE_CODEX=1 to call the configured Azure Codex deployment")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	d := delivery.StreamingDelivery(delivery.FramingSSE)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "gpt-5.3-codex",
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, `Call the record_action tool exactly once with {"action":"inspect"} and do not answer in prose.`),
		},
		Tools: []canonical.ToolDecl{
			canonical.NewFunctionToolDecl(
				"record_action",
				"record_action",
				"record the required test action",
				canonical.NewToolSchemaObject(`{"type":"object","properties":{"action":{"type":"string","enum":["inspect"]}},"required":["action"],"additionalProperties":false}`),
			),
		},
		ToolPolicy:    canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil),
		ToolCallBatch: canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne),
	})

	registry := mustProviderRegistry(t, http.DefaultClient, outboundcredentials.NewResolver())
	target := provider.NewTargetSnapshot(
		"default",
		"azure",
		"https://contact-8837-resource.services.ai.azure.com",
		"secret:azure-live",
		protocolkind.Responses,

		"",
		"responses_stream")
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
	decoded, err := backend.Codec.Decode(ctx, "ex_live_codex_tool", provider.StreamIngress{Stream: stream})
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
		if item.Kind() == canonical.ItemKindToolUse {
			toolUse, _ := item.ToolUse()
			if toolUse.Name != "record_action" {
				t.Fatalf("tool name = %q, want record_action", toolUse.Name)
			}
			if !strings.Contains(toolUse.Input.RawObject(), `"inspect"`) {
				t.Fatalf("tool input = %q, want inspect action", toolUse.Input.RawObject())
			}
			return
		}
		if item.Kind() == canonical.ItemKindText {
			textItem, _ := item.TextItem()
			text = append(text, strings.TrimSpace(textItem.Text))
		}
	}
	t.Fatalf("Azure Codex returned no required tool call; text=%q finish=%q", strings.Join(text, "\n"), output.FinishReason())
}
