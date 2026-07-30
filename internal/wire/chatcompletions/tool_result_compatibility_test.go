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

	changeLog := &recordingChanges{}
	if _, err := EncodeCarrierWithChanges(request, delivery.BufferedDelivery(), changeLog, "ex"); err != nil {
		t.Fatal(err)
	}
	if len(*changeLog) != 2 || (*changeLog)[0].Capability != canonical.RequestItemsToolResultIsError || (*changeLog)[0].Kind != compat.Approximation || (*changeLog)[1].Capability != canonical.RequestItemsToolResultContentBoundaries || (*changeLog)[1].Kind != compat.Approximation {
		t.Fatalf("compatibility changes = %#v", *changeLog)
	}
}
