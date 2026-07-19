package messages

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestEncode_DoesNotEmbedProviderCacheFields(t *testing.T) {
	functionTool := canonical.NewFunctionToolDecl("Read", "Read", "read files", canonical.NewToolSchemaObject(`{"type":"object","properties":{"path":{"type":"string"}}}`))
	projectedName, err := canonical.ProjectedToolName(functionTool)
	if err != nil {
		t.Fatalf("ProjectedToolName(function) returned error: %v", err)
	}
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "claude",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		Tools: []canonical.ToolDecl{
			functionTool,
		},
	})
	wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeRequest: %v", err)
	}
	raw := wire.Raw
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if _, ok := body["prompt_cache_key"]; ok {
		t.Fatalf("prompt_cache_key must be provider adapter concern")
	}
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want one tool declaration", body["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("tools[0] = %T, want map[string]any", tools[0])
	}
	if gotName, _ := tool["name"].(string); gotName != projectedName {
		t.Fatalf("tool name = %q, want %q", gotName, projectedName)
	}
}

func TestDecodeRequest_IgnoresPromptCacheFields(t *testing.T) {
	codec := legacyClientRequestDecoder{}
	req := []byte(`{"model":"claude","prompt_cache_key":"repo","messages":[{"role":"user","content":"hi"}]}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Messages, Raw: req})
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}
	if got.Model() != "claude" {
		t.Fatalf("model=%q want claude", got.Model())
	}
}

func TestDecodeRequest_IgnoresAnthropicCacheMarkers(t *testing.T) {
	codec := legacyClientRequestDecoder{}
	tool := canonical.NewFunctionToolDecl("Read", "Read", "read files", canonical.NewToolSchemaObject(`{"type":"object","properties":{"path":{"type":"string"}}}`))
	projectedToolName, err := canonical.ProjectedToolName(tool)
	if err != nil {
		t.Fatalf("ProjectedToolName: %v", err)
	}
	req := []byte(`{"model":"claude","tools":[{"name":"` + projectedToolName + `","description":"read files","input_schema":{"type":"object","properties":{"path":{"type":"string"}}},"cache_control":{"type":"ephemeral","ttl":"1h"}}],"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral","ttl":"1h"}}]}]}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Messages, Raw: req})
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}
	if got.Model() != "claude" {
		t.Fatalf("model=%q want claude", got.Model())
	}
	if len(got.Tools()) != 1 {
		t.Fatalf("tools len=%d want 1", len(got.Tools()))
	}
	if len(got.Items()) != 1 {
		t.Fatalf("items len=%d want 1", len(got.Items()))
	}
}

func TestDecodeRequest_IgnoresBedrockCachePointParts(t *testing.T) {
	codec := legacyClientRequestDecoder{}
	req := []byte(`{"model":"claude","messages":[{"role":"user","content":[{"type":"text","text":"hi"},{"cachePoint":{"type":"default","ttl":"5m"}}]}]}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Messages, Raw: req})
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}
	if len(got.Items()) != 1 {
		t.Fatalf("items len=%d want 1", len(got.Items()))
	}
}
