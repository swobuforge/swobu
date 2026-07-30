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
	var changes []compat.Change
	document, err := EncodeCarrierWithChanges(request, delivery.BufferedDelivery(), &changes, "exchange")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, decision := range changes {
		found = found || decision.Capability == canonical.RequestInstructions && decision.Kind == compat.Approximation
	}
	if len(document.RawBytes()) == 0 || !found {
		t.Fatalf("document=%s changes=%#v", document.RawBytes(), changes)
	}
}
