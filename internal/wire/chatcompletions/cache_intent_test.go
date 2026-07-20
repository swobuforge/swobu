package chatcompletions

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/testkit/providertest"
)

func TestEncode_PreservesToolsAndExcludesProviderCacheFields(t *testing.T) {
	functionTool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "get_weather"), "retrieve weather", canonicaltest.Schema(t, `{"type":"object","properties":{"location":{"type":"string"}}}`), canonical.Unspecified[bool]())
	projectedName := providertest.ProjectedToolName(t, functionTool)
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt-4o-mini"),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		Tools: canonicaltest.SpecifiedToolSet(t, functionTool),
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
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want one tool declaration", body["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("tools[0] = %T, want map[string]any", tools[0])
	}
	function, ok := tool["function"].(map[string]any)
	if !ok {
		t.Fatalf("tools[0].function = %T, want map[string]any", tool["function"])
	}
	if gotName, _ := function["name"].(string); gotName != projectedName {
		t.Fatalf("function name = %q, want %q", gotName, projectedName)
	}
}

func TestDecodeRequest_IgnoresPromptCacheFields(t *testing.T) {
	codec := legacyClientRequestDecoder{}
	functionTool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "get_weather"), "retrieve weather", canonicaltest.Schema(t, `{"type":"object","properties":{"location":{"type":"string"}}}`), canonical.Unspecified[bool]())
	projectedName := providertest.ProjectedToolName(t, functionTool)
	req := []byte(`{"model":"gpt-4o-mini","tools":[{"type":"function","function":{"name":"` + projectedName + `","description":"retrieve weather","parameters":{"type":"object","properties":{"location":{"type":"string"}}}}}],"prompt_cache_key":"repo","prompt_cache_retention":"24h","messages":[{"role":"user","content":"hi"}]}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.ChatCompletions, Raw: req})
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}
	tools := got.Tools()
	if len(tools) != 1 || tools[0].Key().Name() != "get_weather" {
		t.Fatalf("tools = %#v key=%q", tools, tools[0].Key().String())
	}
}
