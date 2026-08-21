package zai

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func zaiTarget(model string) provider.TargetSnapshot {
	target := provider.NewTargetSnapshot(
		"zai",
		string(profile.ProviderSpecZAI),
		"https://api.z.ai/v1",
		"test-key",
		protocolkind.ChatCompletions,
		"chat_completions",
		delivery.BufferedDelivery(),
	)
	target.Model = model
	return target
}

func TestZAIWebSearchTranslation(t *testing.T) {
	functionKey, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "lookup")
	function := canonicaltest.MustFunctionTool(functionKey, "look up a value", canonicaltest.Schema(t, `{"type":"object","properties":{"key":{"type":"string"}}}`), canonical.Unspecified[bool]())
	webSearch := canonical.NewWebSearchDeclaration()

	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("manual-model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, function, webSearch),
			canonicaltest.Message(t, canonical.MessageRoleUser, "search"),
		},
	})
	backend, err := NewRuntime(nil, nil).BackendResolver.ResolveBackend(zaiTarget("manual-model"))
	if err != nil {
		t.Fatal(err)
	}
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := backend.Codec.Encode(provider.Request{
		Canonical: request,
		ToolNames: names,
		Delivery:  delivery.BufferedDelivery(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(document.RawBytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, exists := body["web_search_options"]; exists {
		t.Fatalf("standard search options survived rewrite: %s", document.RawBytes())
	}
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 2 {
		t.Fatalf("tools = %#v", body["tools"])
	}
	functionMap, ok := tools[0].(map[string]any)
	if !ok || functionMap["type"] != "function" {
		t.Fatalf("function tool = %#v", tools[0])
	}
	searchMap, ok := tools[1].(map[string]any)
	if !ok || searchMap["type"] != "web_search" {
		t.Fatalf("search tool = %#v", tools[1])
	}
	options, ok := searchMap["web_search"].(map[string]any)
	if !ok || options["enable"] != true {
		t.Fatalf("search options = %#v", searchMap["web_search"])
	}
}

func TestCodecAppliesStaticZAIReasoningProjectionWithoutModelBranches(t *testing.T) {
	budgetLow, err := canonical.NewBudgetReasoningCompute(1_000)
	if err != nil {
		t.Fatal(err)
	}
	budgetMedium, err := canonical.NewBudgetReasoningCompute(10_000)
	if err != nil {
		t.Fatal(err)
	}
	budgetHigh, err := canonical.NewBudgetReasoningCompute(30_000)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		compute      *canonical.ReasoningCompute
		effort       *canonical.InferenceEffort
		wantThinking string
		wantEffort   string
		wantChanges  []compat.Change
	}{
		{name: "unspecified"},
		{name: "disabled", compute: reasoningComputePointer(canonical.NewDisabledReasoningCompute()), wantThinking: "disabled"},
		{
			name: "disabled with effort", compute: reasoningComputePointer(canonical.NewDisabledReasoningCompute()),
			effort: effortPointer(canonical.InferenceEffortMax), wantThinking: "disabled",
			wantChanges: []compat.Change{compat.NewOmission(canonical.RequestControlsEffort, canonical.Occurrence{})},
		},
		{name: "automatic", compute: reasoningComputePointer(canonical.NewAutomaticReasoningCompute()), wantThinking: "enabled"},
		{
			name: "automatic with effort", compute: reasoningComputePointer(canonical.NewAutomaticReasoningCompute()),
			effort: effortPointer(canonical.InferenceEffortHigh), wantThinking: "enabled", wantEffort: "high",
		},
		{name: "effort only", effort: effortPointer(canonical.InferenceEffortHigh), wantEffort: "high"},
		{
			name: "low budget", compute: reasoningComputePointer(budgetLow), wantThinking: "enabled", wantEffort: "low",
			wantChanges: []compat.Change{reasoningBudgetApproximation()},
		},
		{
			name: "medium budget", compute: reasoningComputePointer(budgetMedium), wantThinking: "enabled", wantEffort: "medium",
			wantChanges: []compat.Change{reasoningBudgetApproximation()},
		},
		{
			name: "high budget", compute: reasoningComputePointer(budgetHigh), wantThinking: "enabled", wantEffort: "high",
			wantChanges: []compat.Change{reasoningBudgetApproximation()},
		},
		{
			name: "budget with explicit effort", compute: reasoningComputePointer(budgetMedium),
			effort: effortPointer(canonical.InferenceEffortMax), wantThinking: "enabled", wantEffort: "max",
			wantChanges: []compat.Change{reasoningBudgetApproximation()},
		},
	}

	for _, model := range []string{"glm-4.5", "glm-5.1", "glm-5.2", "glm-99", "opaque-future-model"} {
		for _, tt := range tests {
			t.Run(model+"/"+tt.name, func(t *testing.T) {
				reasoning := canonical.ReasoningControls{}
				if tt.compute != nil {
					reasoning, err = canonical.NewReasoningControls(canonical.ReasoningControlsParams{
						Compute: canonical.Specify(*tt.compute),
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: tt.effort})
				if err != nil {
					t.Fatal(err)
				}
				request := canonical.NewCanonicalRequest(canonical.RequestParams{
					Model:     canonical.Specify(model),
					Items:     []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
					Controls:  controls,
					Reasoning: reasoning,
				})
				backend, err := NewRuntime(nil, nil).BackendResolver.ResolveBackend(zaiTarget(model))
				if err != nil {
					t.Fatal(err)
				}
				document, changes, err := backend.Codec.Encode(provider.Request{
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
				thinking, thinkingPresent := payload["thinking"].(map[string]any)
				if tt.wantThinking == "" {
					if thinkingPresent {
						t.Fatalf("thinking = %#v, want absent", thinking)
					}
				} else if !thinkingPresent || thinking["type"] != tt.wantThinking {
					t.Fatalf("thinking = %#v, want %q", payload["thinking"], tt.wantThinking)
				}
				if tt.wantEffort == "" {
					if _, exists := payload["reasoning_effort"]; exists {
						t.Fatalf("reasoning_effort = %#v, want absent", payload["reasoning_effort"])
					}
				} else if payload["reasoning_effort"] != tt.wantEffort {
					t.Fatalf("reasoning_effort = %#v, want %q", payload["reasoning_effort"], tt.wantEffort)
				}
				if len(changes) != len(tt.wantChanges) || !reflect.DeepEqual(changes, tt.wantChanges) {
					t.Fatalf("changes = %#v, want %#v", changes, tt.wantChanges)
				}
			})
		}
	}
}

func reasoningComputePointer(value canonical.ReasoningCompute) *canonical.ReasoningCompute {
	return &value
}

func effortPointer(value canonical.InferenceEffort) *canonical.InferenceEffort {
	return &value
}
