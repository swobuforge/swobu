package responses

import (
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestResponsesToolResultErrorRecordsApproximation(t *testing.T) {
	callID, _ := canonical.NewToolCallID("call_1")
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("denied")}, true)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{result}})
	sink := &recordingDecisionSink{}
	if _, err := EncodeCarrierWithDecisions(EncodeInput{Request: request}, delivery.BufferedDelivery(), sink, "ex", EncodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(sink.effects) != 1 || sink.effects[0].Feature != compat.RequestItemsToolResultIsError || sink.effects[0].Outcome != compat.Approx {
		t.Fatalf("compatibility decisions = %#v", sink.effects)
	}
}
