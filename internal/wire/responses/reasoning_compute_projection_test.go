package responses

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestResponsesProjectsOrdinalReasoningCompute(t *testing.T) {
	t.Parallel()

	automatic := canonical.NewAutomaticReasoningCompute()
	disabled := canonical.NewDisabledReasoningCompute()
	budget, err := canonical.NewBudgetReasoningCompute(10_000)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		compute    canonical.ReasoningCompute
		effort     *canonical.InferenceEffort
		want       string
		wantChange canonical.CapabilityPath
		wantKind   compat.Kind
	}{
		{name: "budget", compute: budget, want: "medium", wantChange: canonical.RequestReasoning, wantKind: compat.Approximation},
		{name: "automatic", compute: automatic, want: "medium", wantChange: canonical.RequestReasoning, wantKind: compat.Approximation},
		{name: "automatic with effort", compute: automatic, effort: responsesEffortPointer(canonical.InferenceEffortHigh), want: "high"},
		{name: "budget with effort", compute: budget, effort: responsesEffortPointer(canonical.InferenceEffortLow), want: "low", wantChange: canonical.RequestReasoning, wantKind: compat.Approximation},
		{name: "disabled with effort", compute: disabled, effort: responsesEffortPointer(canonical.InferenceEffortHigh), want: "none", wantChange: canonical.RequestControlsEffort, wantKind: compat.Omission},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Compute: canonical.Specify(test.compute)})
			if err != nil {
				t.Fatal(err)
			}
			controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: test.effort})
			if err != nil {
				t.Fatal(err)
			}
			request := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model:    canonical.Specify("model"),
				Items:    []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
				Controls: controls, Reasoning: reasoning,
			})
			result, err := (ProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(
				wire.ProviderEncodeInput{Request: request, ToolNames: testAttemptToolNames(request)}, delivery.BufferedDelivery(), "",
			)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(result.Document.RawBytes(), &payload); err != nil {
				t.Fatal(err)
			}
			wireReasoning, ok := payload["reasoning"].(map[string]any)
			if !ok || wireReasoning["effort"] != test.want {
				t.Fatalf("reasoning = %#v, want effort %q", payload["reasoning"], test.want)
			}
			if test.wantChange == "" {
				if len(result.Changes) != 0 {
					t.Fatalf("changes = %#v, want none", result.Changes)
				}
				return
			}
			if len(result.Changes) != 1 || result.Changes[0].Capability != test.wantChange || result.Changes[0].Kind != test.wantKind {
				t.Fatalf("changes = %#v", result.Changes)
			}
			if test.wantKind == compat.Approximation && result.Changes[0].Preserved != canonical.RequestControlsEffort {
				t.Fatalf("approximation = %#v, want effort preservation", result.Changes[0])
			}
		})
	}
}

func responsesEffortPointer(value canonical.InferenceEffort) *canonical.InferenceEffort {
	return &value
}
