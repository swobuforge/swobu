package chatcompletions

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestChatCompletionsProjectsOrdinalReasoningCompute(t *testing.T) {
	t.Parallel()

	automatic := canonical.NewAutomaticReasoningCompute()
	disabled := canonical.NewDisabledReasoningCompute()
	budget, err := canonical.NewBudgetReasoningCompute(10_000)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name          string
		compute       canonical.ReasoningCompute
		effort        *canonical.InferenceEffort
		want          string
		wantChange    canonical.CapabilityPath
		wantKind      compat.Kind
		wantPreserved canonical.CapabilityPath
	}{
		{
			name: "budget", compute: budget, want: "medium",
			wantChange: canonical.RequestReasoning, wantKind: compat.Approximation,
			wantPreserved: canonical.RequestControlsEffort,
		},
		{
			name: "automatic", compute: automatic, want: "medium",
			wantChange: canonical.RequestReasoning, wantKind: compat.Approximation,
			wantPreserved: canonical.RequestControlsEffort,
		},
		{name: "automatic with effort", compute: automatic, effort: effortPointer(canonical.InferenceEffortHigh), want: "high"},
		{
			name: "budget with effort", compute: budget, effort: effortPointer(canonical.InferenceEffortLow), want: "low",
			wantChange: canonical.RequestReasoning, wantKind: compat.Approximation,
			wantPreserved: canonical.RequestControlsEffort,
		},
		{
			name: "disabled with effort", compute: disabled, effort: effortPointer(canonical.InferenceEffortHigh), want: "none",
			wantChange: canonical.RequestControlsEffort, wantKind: compat.Omission,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
				Compute: canonical.Specify(test.compute),
			})
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
				wire.ProviderEncodeInput{Request: request}, delivery.BufferedDelivery(), "",
			)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(result.Document.RawBytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if got := payload["reasoning_effort"]; got != test.want {
				t.Fatalf("reasoning_effort = %#v, want %q", got, test.want)
			}
			if test.wantChange == "" {
				if len(result.Changes) != 0 {
					t.Fatalf("changes = %#v, want none", result.Changes)
				}
				return
			}
			if len(result.Changes) != 1 {
				t.Fatalf("changes = %#v, want one", result.Changes)
			}
			change := result.Changes[0]
			if change.Capability != test.wantChange || change.Kind != test.wantKind || change.Preserved != test.wantPreserved {
				t.Fatalf("change = %#v", change)
			}
		})
	}
}

func TestChatCompletionsEffortDecodesAsAutomaticReasoningAndRoundTripsExactly(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"model":"model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"medium"}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{
		Family: protocolkind.ChatCompletions,
		Raw:    raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	compute, ok := decoded.Request.Request.Reasoning().ComputeField().Get()
	if !ok || compute.Kind() != canonical.ReasoningAutomatic {
		t.Fatalf("compute = (%#v, %v), want automatic", compute, ok)
	}
	effort, ok := decoded.Request.Request.Controls().Effort.Get()
	if !ok || effort != canonical.InferenceEffortMedium {
		t.Fatalf("effort = (%q, %v), want medium", effort, ok)
	}
	encoded, err := (ProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(
		wire.ProviderEncodeInput{Request: decoded.Request.Request},
		delivery.BufferedDelivery(),
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded.Changes) != 0 {
		t.Fatalf("round-trip changes = %#v, want exact", encoded.Changes)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded.Document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["reasoning_effort"] != "medium" {
		t.Fatalf("reasoning_effort = %#v, want medium", payload["reasoning_effort"])
	}
}

func effortPointer(value canonical.InferenceEffort) *canonical.InferenceEffort {
	return &value
}
