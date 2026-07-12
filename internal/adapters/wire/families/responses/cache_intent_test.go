package responses

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestEncode_PreservesToolsAndOmitsProviderCacheFields(t *testing.T) {
	functionTool := canonical.NewFunctionToolDecl("get_weather", "get_weather", "retrieve weather", canonical.NewToolSchemaObject(`{"type":"object","properties":{"location":{"type":"string"}}}`))
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "gpt-4o-mini",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		Tools: []canonical.ToolDecl{
			functionTool,
		},
		CacheIntent: canonical.NewCacheIntent(canonical.CacheIntentParams{Key: "repo", Retention: canonical.CacheRetention24H}),
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
		t.Fatalf("prompt_cache_key must be provider transform concern")
	}
	if _, ok := body["prompt_cache_retention"]; ok {
		t.Fatalf("prompt_cache_retention must be provider transform concern")
	}
	tools, ok := body["tools"].([]any)
	if !ok {
		t.Fatalf("tools = %T, want []any", body["tools"])
	}
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	tool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("tool = %T, want map[string]any", tools[0])
	}
	if tool["type"] != "function" {
		t.Fatalf("tool type = %v, want %q", tool["type"], "function")
	}
	if tool["name"] != "get_weather" {
		t.Fatalf("tool name = %v, want %q", tool["name"], "get_weather")
	}
	if _, ok := tool["parameters"].(map[string]any); !ok {
		t.Fatalf("tool parameters = %T, want map[string]any", tool["parameters"])
	}
}

func TestDecodeRequest_IgnoresPromptCacheFields(t *testing.T) {
	codec := ClientRequestDecoder{}
	functionTool := canonical.NewFunctionToolDecl("get_weather", "get_weather", "retrieve weather", canonical.NewToolSchemaObject(`{"type":"object","properties":{"location":{"type":"string"}}}`))
	req := []byte(`{"model":"gpt-4o-mini","tools":[{"type":"function","name":"` + functionTool.ToolName() + `","description":"retrieve weather","parameters":{"type":"object","properties":{"location":{"type":"string"}}}}],"prompt_cache_key":"repo","prompt_cache_retention":"24h","input":"hi"}`)
	got, _, err := codec.DecodeClientRequest(carrier.WireDocument{Family: protocolkind.Responses, Raw: req})
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}
	if !got.CacheIntent().IsZero() {
		t.Fatalf("cache intent=%+v want zero", got.CacheIntent())
	}
	tools := got.Tools()
	if len(tools) != 1 || tools[0].ToolName() != "get_weather" {
		t.Fatalf("tools = %#v", tools)
	}
}
