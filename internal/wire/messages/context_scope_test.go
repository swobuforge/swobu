package messages

import (
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestMessagesHoistsMidHistoryContextWithEvidence(t *testing.T) {
	directive, _ := canonical.NewScopedMessageItem(
		canonical.MessageRoleDeveloper,
		[]canonical.MessagePart{canonical.NewTextMessagePart("late")},
		canonical.ContextScopeHistory,
	)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "before"),
			directive,
			canonicaltest.Message(t, canonical.MessageRoleUser, "after"),
		},
	})
	sink := &recordingDecisionSink{}
	document, err := EncodeCarrierWithDecisions(request, delivery.BufferedDelivery(), sink, "exchange")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, decision := range sink.effects {
		found = found || decision.Feature == compat.RequestInstructions && decision.Outcome == compat.Approx
	}
	if len(document.RawBytes()) == 0 || !found {
		t.Fatalf("document=%s decisions=%#v", document.RawBytes(), sink.effects)
	}
}
