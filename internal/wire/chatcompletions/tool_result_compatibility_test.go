package chatcompletions

import (
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestChatToolResultLossesRecordApproximation(t *testing.T) {
	callID, _ := canonical.NewToolCallID("call_1")
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("a"), canonical.NewTextToolResultPart("b")}, true)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{result}})

	sink := &recordingDecisionSink{}
	if _, err := EncodeCarrierWithDecisions(request, delivery.BufferedDelivery(), sink, "ex"); err != nil {
		t.Fatal(err)
	}
	if len(sink.effects) != 2 || sink.effects[0].Feature != compat.RequestItemsToolResultIsError || sink.effects[0].Outcome != compat.Approx || sink.effects[1].Feature != compat.RequestItemsToolResultContentBoundaries || sink.effects[1].Outcome != compat.Approx {
		t.Fatalf("compatibility decisions = %#v", sink.effects)
	}
}
