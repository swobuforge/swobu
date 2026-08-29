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
		{name: "automatic", compute: automatic},
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
			if test.want == "" {
				if ok {
					if _, hasEffort := wireReasoning["effort"]; hasEffort {
						t.Fatalf("reasoning = %#v, want automatic effort omission", wireReasoning)
					}
				}
			} else if !ok || wireReasoning["effort"] != test.want {
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
		})
	}
}

// TestCodex0146DisabledReasoningStillReachesResponsesTarget reproduces the
// outgoing field reported in Codex #36735. This is intentionally a behavioral
// witness, not an approval to omit the field: generic Responses lowering has
// no exact-target capability authority from which to decide that omission.
func TestCodex0146DisabledReasoningStillReachesResponsesTarget(t *testing.T) {
	disabled := canonical.NewDisabledReasoningCompute()
	reasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
		Compute: canonical.Specify(disabled),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:     canonical.Specify("gpt-4o-mini"),
		Items:     []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "just say 1")},
		Reasoning: reasoning,
	})
	result, err := (ProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(
		wire.ProviderEncodeInput{Request: request, ToolNames: testAttemptToolNames(request)}, delivery.BufferedDelivery(), "codex-36735",
	)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(result.Document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	reasoningPayload, ok := payload["reasoning"].(map[string]any)
	if !ok || reasoningPayload["effort"] != "none" {
		t.Fatalf("Responses reasoning = %#v, want disabled effort to remain observable", payload["reasoning"])
	}
}

func responsesEffortPointer(value canonical.InferenceEffort) *canonical.InferenceEffort {
	return &value
}
