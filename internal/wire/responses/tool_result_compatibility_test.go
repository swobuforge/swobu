package responses

import (
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestResponsesToolResultErrorRecordsApproximation(t *testing.T) {
	callID, _ := canonical.NewToolCallID("call_1")
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	declarations := canonicaltest.ToolDeclarations(t, canonicaltest.MustFunctionTool(key, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]()))
	call := canonicaltest.ToolCall(t, callID.String(), key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{}`)))
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("denied")}, true)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{declarations, call, result}})
	changeLog := &recordingChanges{}
	if _, err := EncodeCarrierWithChanges(EncodeInput{Request: request, ToolNames: testAttemptToolNames(request)}, delivery.BufferedDelivery(), changeLog, "ex", EncodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(*changeLog) != 1 || (*changeLog)[0].Capability != canonical.RequestItemsToolResultIsError || (*changeLog)[0].Kind != compat.Approximation {
		t.Fatalf("compatibility changes = %#v", *changeLog)
	}
}
