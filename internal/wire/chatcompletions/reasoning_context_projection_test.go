package chatcompletions

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestChatResponsesReasoningContextIsTargetLocalCompatibility(t *testing.T) {
	request := chatRequestWithResponsesReasoningContext(t)
	for _, tc := range []struct {
		name    string
		mode    compat.CompatibilityMode
		outcome compat.Outcome
		wantErr bool
	}{
		{name: "strict", mode: compat.CompatibilityStrict, outcome: compat.Drop},
		{name: "compat", mode: compat.CompatibilityCompat, outcome: compat.Drop},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := &compat.RecordingSink{}
			document, err := EncodeCarrierWithDecisions(request, delivery.BufferedDelivery(), sink, "exchange", EncodeOptions{
				Compatibility: compat.CompatibilityPolicy{Mode: tc.mode},
			})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr=%t", err, tc.wantErr)
			}
			assertRecordedReasoningContextDecision(t, sink.Decisions(), tc.outcome)
			if !tc.wantErr && strings.Contains(string(document.RawBytes()), "all_turns") {
				t.Fatalf("Chat request leaked Responses context: %s", document.RawBytes())
			}
		})
	}
}

func chatRequestWithResponsesReasoningContext(t *testing.T) canonical.CanonicalRequest {
	t.Helper()
	reasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
		ResponsesContext: canonical.Specify(canonical.ResponsesReasoningContextAllTurns),
	})
	if err != nil {
		t.Fatal(err)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")}, Reasoning: reasoning,
	})
}

func assertRecordedReasoningContextDecision(t *testing.T, decisions []compat.Decision, outcome compat.Outcome) {
	t.Helper()
	for _, decision := range decisions {
		if decision.Feature == compat.RequestReasoningContextResponses && decision.Outcome == outcome {
			return
		}
	}
	t.Fatalf("decisions = %#v, want %s/%s", decisions, compat.RequestReasoningContextResponses, outcome)
}
