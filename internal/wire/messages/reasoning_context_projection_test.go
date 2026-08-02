package messages

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestMessagesResponsesReasoningContextIsTargetLocalCompatibility(t *testing.T) {
	request := messagesRequestWithResponsesReasoningContext(t)
	var changes []compat.Change
	document, err := EncodeCarrierWithChanges(request, testAttemptToolNames(request), delivery.BufferedDelivery(), &changes, "exchange")
	if err != nil {
		t.Fatal(err)
	}
	assertMessagesReasoningContextDecision(t, changes, compat.Omission)
	if strings.Contains(string(document.RawBytes()), "all_turns") {
		t.Fatalf("Messages request leaked Responses context: %s", document.RawBytes())
	}
}

func messagesRequestWithResponsesReasoningContext(t *testing.T) canonical.CanonicalRequest {
	t.Helper()
	reasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
		ResponsesContext: canonical.Specify(canonical.ResponsesReasoningContextAllTurns),
	})
	if err != nil {
		t.Fatal(err)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("claude"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")}, Reasoning: reasoning,
	})
}

func assertMessagesReasoningContextDecision(t *testing.T, changes []compat.Change, outcome compat.Kind) {
	t.Helper()
	for _, decision := range changes {
		if decision.Capability == canonical.RequestReasoningContextResponses && decision.Kind == outcome {
			return
		}
	}
	t.Fatalf("changes = %#v, want %s/%v", changes, canonical.RequestReasoningContextResponses, outcome)
}
