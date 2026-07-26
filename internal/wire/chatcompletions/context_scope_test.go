package chatcompletions

import (
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestChatCompletionsHoistsMidHistoryToolAdditionWithEvidence(t *testing.T) {
	tool := canonicaltest.MustFunctionTool(
		canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "search"),
		"", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool](),
	)
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{tool})
	declarations, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeHistory)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "before"),
			declarations,
			canonicaltest.Message(t, canonical.MessageRoleUser, "after"),
		},
	})
	sink := &recordingDecisionSink{}
	document, err := EncodeCarrierWithDecisions(request, delivery.BufferedDelivery(), sink, "exchange")
	if err != nil {
		t.Fatal(err)
	}
	if len(document.RawBytes()) == 0 || !decisionRecorded(sink.effects, compat.RequestTools, compat.Approx) {
		t.Fatalf("document=%s decisions=%#v", document.RawBytes(), sink.effects)
	}
}
