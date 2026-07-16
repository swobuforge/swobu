package providers

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	outboundcredentials "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestLiveAzureCodexResponsesRequiredFunctionTool(t *testing.T) {
	if os.Getenv("SWOBU_LIVE_AZURE_CODEX") != "1" {
		t.Skip("set SWOBU_LIVE_AZURE_CODEX=1 to call the configured Azure Codex deployment")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	codecs := codecresolver.NewRuntimeCodecResolver()
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

	wireRequestResult, err := codecs.ProviderRequestDocumentEncoder(protocolkind.Responses).EncodeProviderRequestDocument(wire.ProviderEncodeInput{Request: request}, d, "live-azure-codex-required-tool")
	if err != nil {
		t.Fatalf("EncodeProviderRequestDocument returned error: %v", err)
	}
	registry := NewProviderRegistry(http.DefaultClient, outboundcredentials.NewResolver(), "")
	providerReq := exchange.NewProviderRequest(
		"live-azure-codex-required-tool",
		canonical.ClientFamilyResponses,
		request,
		wireRequestResult.Value,
		exchange.NewExecutionContract(d),
		exchange.NewRoutableTarget(
			"default",
			"azure",
			"https://contact-8837-resource.services.ai.azure.com",
			"keychain",
			protocolkind.Responses,
			"credential_ref",
			"",
			"responses_stream",
		),
	)

	ingress, err := registry.ResolveProviderIngress(ctx, providerReq)
	if err != nil {
		t.Fatalf("ResolveProviderIngress returned error: %v", err)
	}
	stream, ok := ingress.(carrier.CarrierStream)
	if !ok {
		t.Fatalf("ResolveProviderIngress returned %T, want carrier.CarrierStream", ingress)
	}
	readerResult, err := codecs.ProviderEnvelopeDecoder(protocolkind.Responses, d).DecodeProviderEnvelope(stream, providerReq.ExchangeID)
	if err != nil {
		t.Fatalf("DecodeProviderEnvelope returned error: %v", err)
	}
	closed, err := canonical.ReadClosedEnvelope(ctx, readerResult.Value, canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope returned error: %v", err)
	}
	output, err := closed.ProjectResponse()
	if err != nil {
		t.Fatalf("ProjectResponse returned error: %v", err)
	}

	var text []string
	for _, item := range output.Items() {
		if item.Kind == canonical.ItemKindToolUse {
			if item.Name != "record_action" {
				t.Fatalf("tool name = %q, want record_action", item.Name)
			}
			if !strings.Contains(item.Input.RawObject(), `"inspect"`) {
				t.Fatalf("tool input = %q, want inspect action", item.Input.RawObject())
			}
			return
		}
		if item.Kind == canonical.ItemKindText {
			text = append(text, strings.TrimSpace(item.Text))
		}
	}
	t.Fatalf("Azure Codex returned no required tool call; text=%q finish=%q", strings.Join(text, "\n"), output.FinishReason())
}
