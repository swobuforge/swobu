package zai

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
)

func TestRewriteWebSearchTranslatesOnlyEmptyStandardOptions(t *testing.T) {
	var functionTool chatcompletions.ProviderRequestTool
	if err := json.Unmarshal([]byte(`{
		"type":"function",
		"function":{
			"name":"lookup",
			"description":"look up a value",
			"parameters":{"type":"object","properties":{"key":{"type":"string"}}}
		}
	}`), &functionTool); err != nil {
		t.Fatal(err)
	}
	document := chatcompletions.ProviderRequestDocument{
		Payload: map[string]any{
			"model":              "manual-model",
			"messages":           []any{},
			"web_search_options": map[string]any{},
		},
		Tools: []chatcompletions.ProviderRequestTool{functionTool},
	}

	if err := rewriteWebSearch(&document); err != nil {
		t.Fatal(err)
	}
	encoded, err := chatcompletions.EncodeProviderRequestDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded.RawBytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, exists := body["web_search_options"]; exists {
		t.Fatalf("standard search options survived rewrite: %s", encoded.RawBytes())
	}
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 2 {
		t.Fatalf("tools = %#v", body["tools"])
	}
	function, ok := tools[0].(map[string]any)
	if !ok || function["type"] != "function" {
		t.Fatalf("function tool = %#v", tools[0])
	}
	definition, ok := function["function"].(map[string]any)
	if !ok || definition["name"] != "lookup" {
		t.Fatalf("function definition = %#v", function["function"])
	}
	search, ok := tools[1].(map[string]any)
	if !ok || search["type"] != "web_search" {
		t.Fatalf("search tool = %#v", tools[1])
	}
	options, ok := search["web_search"].(map[string]any)
	if !ok || options["enable"] != true {
		t.Fatalf("search options = %#v", search["web_search"])
	}

	document.Payload["web_search_options"] = map[string]any{"max_results": 5}
	err = rewriteWebSearch(&document)
	var incompatible provider.IncompatibleTargetError
	if !errors.As(err, &incompatible) {
		t.Fatalf("non-empty options error = %T %v, want candidate incompatibility", err, err)
	}
}

func TestCodecInheritsStandardReasoningBudgetProjection(t *testing.T) {
	budget, err := canonical.NewBudgetReasoningCompute(10_000)
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
		Compute: canonical.Specify(budget),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:     canonical.Specify("glm"),
		Items:     []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		Reasoning: reasoning,
	})

	document, changes, err := (codec{}).Encode(provider.Request{
		Canonical: request,
		Delivery:  delivery.BufferedDelivery(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["reasoning_effort"] != "medium" {
		t.Fatalf("reasoning_effort = %#v, want medium", payload["reasoning_effort"])
	}
	if len(changes) != 1 ||
		changes[0].Capability != canonical.RequestReasoning ||
		changes[0].Kind != compat.Approximation ||
		changes[0].Preserved != canonical.RequestControlsEffort {
		t.Fatalf("changes = %#v, want one reasoning-to-effort approximation", changes)
	}
}
