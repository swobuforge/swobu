package chatcompletions

import (
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestChatToolResultLossesFollowExchangeCompatibility(t *testing.T) {
	callID, _ := canonical.NewToolCallID("call_1")
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("a"), canonical.NewTextToolResultPart("b")}, true)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{result}})

	strict := EncodeOptions{Compatibility: compat.CompatibilityPolicy{Mode: compat.CompatibilityStrict}}
	if _, err := EncodeCarrierWithDecisions(request, delivery.BufferedDelivery(), nil, "", strict); err == nil {
		t.Fatal("strict lowering silently erased tool-result semantics")
	}
	sink := &recordingDecisionSink{}
	if _, err := EncodeCarrierWithDecisions(request, delivery.BufferedDelivery(), sink, "ex", EncodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(sink.effects) != 2 || sink.effects[0].Feature != compat.RequestItemsToolResultIsError || sink.effects[0].Outcome != compat.Approx || sink.effects[1].Feature != compat.RequestItemsToolResultContentBoundaries || sink.effects[1].Outcome != compat.Approx {
		t.Fatalf("compatibility decisions = %#v", sink.effects)
	}
}
