package responses

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestEncode_PreservesToolsAndOmitsProviderCacheFields(t *testing.T) {
	functionTool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "get_weather"), "retrieve weather", canonicaltest.Schema(t, `{"type":"object","properties":{"location":{"type":"string"}}}`), canonical.Unspecified[bool]())
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt-4o-mini"),
		Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, functionTool), canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
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
	if _, ok := body["prompt_cache_retention"]; ok {
		t.Fatalf("prompt_cache_retention must be provider adapter concern")
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

func TestDecodeRequest_CapturesPromptCacheKeyOutsideCanonicalRequest(t *testing.T) {
	functionTool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "get_weather"), "retrieve weather", canonicaltest.Schema(t, `{"type":"object","properties":{"location":{"type":"string"}}}`), canonical.Unspecified[bool]())
	req := []byte(`{"model":"gpt-4o-mini","tools":[{"type":"function","name":"` + functionTool.Key().Name() + `","description":"retrieve weather","parameters":{"type":"object","properties":{"location":{"type":"string"}}}}],"prompt_cache_key":"repo","prompt_cache_retention":"24h","input":"hi"}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: req})
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}
	if decoded.Request.CacheLocality.Key() != "repo" {
		t.Fatalf("cache locality = %q", decoded.Request.CacheLocality.Key())
	}
	tools := canonicaltest.Tools(decoded.Request.Request)
	if len(tools) != 1 || tools[0].Key().Name() != "get_weather" {
		t.Fatalf("tools = %#v", tools)
	}
}

func TestDecodeRequest_RejectsEmptyPromptCacheKey(t *testing.T) {
	req := []byte(`{"model":"gpt-4o-mini","prompt_cache_key":"","input":"hi"}`)
	if _, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: req}); err == nil {
		t.Fatal("empty prompt_cache_key was accepted")
	}
}

func TestDecodeRequest_OmittedPromptCacheKeyIsZero(t *testing.T) {
	req := []byte(`{"model":"gpt-4o-mini","input":"hi"}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: req})
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.Request.CacheLocality.IsZero() {
		t.Fatalf("cache locality = %q", decoded.Request.CacheLocality.Key())
	}
}

func TestPromptCacheKeyStaysOuterExecutionIntentAcrossResponsesRebase(t *testing.T) {
	raw := []byte(`{"model":"gpt-4o-mini","prompt_cache_key":"outer","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"turn one"}]},{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"answer one"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"turn two"}]}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Request.CacheLocality.Key() != "outer" || decoded.Request.RebasedRequest == nil {
		t.Fatalf("cache locality/rebase = %q, %#v", decoded.Request.CacheLocality.Key(), decoded.Request.RebasedRequest)
	}
	rawOther := bytes.Replace(raw, []byte(`"outer"`), []byte(`"other"`), 1)
	other, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: rawOther})
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Request.RequestFingerprint != other.Request.RequestFingerprint || decoded.Request.RebasedRequest.Previous != other.Request.RebasedRequest.Previous {
		t.Fatal("prompt_cache_key changed Responses history fingerprinting")
	}
}
