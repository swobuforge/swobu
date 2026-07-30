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
	changeLog := &recordingChanges{}
	if _, err := EncodeCarrierWithChanges(EncodeInput{Request: request}, delivery.BufferedDelivery(), changeLog, "ex", EncodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(*changeLog) != 1 || (*changeLog)[0].Capability != canonical.RequestItemsToolResultIsError || (*changeLog)[0].Kind != compat.Approximation {
		t.Fatalf("compatibility changes = %#v", *changeLog)
	}
}
