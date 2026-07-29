package chatcompletions

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func decisionRecorded(decisions []compat.Decision, feature compat.Feature, outcome compat.Outcome) bool {
	for _, decision := range decisions {
		if decision.Feature == feature && decision.Outcome == outcome {
			return true
		}
	}
	return false
}

func TestChatCompatibilityRehomesClosedBatchToolResultImage(t *testing.T) {
	call := mustChatProjectionCall(t, "call_image")
	image := mustChatProjectionImage(t, canonical.Unspecified[canonical.ImageDetail]())
	result := mustChatProjectionResult(t, "call_image",
		canonical.NewTextToolResultPart("before"),
		canonical.NewImageToolResultPart(image),
		canonical.NewTextToolResultPart("after"),
	)
	sink := &recordingDecisionSink{}
	document, err := EncodeCarrierWithDecisions(canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{call, result},
	}), delivery.BufferedDelivery(), sink, "exchange_1")
	if err != nil {
		t.Fatalf("closed compatibility projection rejected: %v", err)
	}
	var payload struct {
		Messages []struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			ToolCallID string          `json:"tool_call_id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Messages) != 3 ||
		payload.Messages[0].Role != "assistant" ||
		payload.Messages[1].Role != "tool" ||
		payload.Messages[2].Role != "user" {
		t.Fatalf("projected messages = %#v", payload.Messages)
	}
	var toolContent string
	if err := json.Unmarshal(payload.Messages[1].Content, &toolContent); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(toolContent, `"kind":"tool_result_image_ref"`) {
		t.Fatalf("tool content lost image correlation marker: %q", toolContent)
	}
	var synthetic []map[string]any
	if err := json.Unmarshal(payload.Messages[2].Content, &synthetic); err != nil {
		t.Fatal(err)
	}
	if len(synthetic) != 2 || synthetic[0]["type"] != "text" || synthetic[1]["type"] != "image_url" {
		t.Fatalf("synthetic image content = %#v", synthetic)
	}
	if !decisionRecorded(sink.effects, compat.RequestItemsToolResultImage, compat.Approx) {
		t.Fatalf("compatibility decisions = %#v", sink.effects)
	}
}

func TestChatToolResultImageProjectionRequiresClosedActiveBatch(t *testing.T) {
	image := mustChatProjectionImage(t, canonical.Unspecified[canonical.ImageDetail]())
	callA := mustChatProjectionCall(t, "call_a")
	callB := mustChatProjectionCall(t, "call_b")
	resultA := mustChatProjectionResult(t, "call_a", canonical.NewImageToolResultPart(image))
	for _, tc := range []struct {
		name  string
		items []canonical.CanonicalItem
	}{
		{name: "no active batch", items: []canonical.CanonicalItem{resultA}},
		{name: "partial parallel batch", items: []canonical.CanonicalItem{callA, callB, resultA}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingDecisionSink{}
			_, err := EncodeCarrierWithDecisions(
				canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: tc.items}),
				delivery.BufferedDelivery(), sink, "exchange_reject",
			)
			if err == nil || !decisionRecorded(sink.effects, compat.RequestItemsToolResultImage, compat.Reject) {
				t.Fatalf("error = %v decisions = %#v", err, sink.effects)
			}
		})
	}
}

func TestChatCompatibilityProjectsParallelToolImagesAfterAllToolMessages(t *testing.T) {
	inline := mustChatProjectionImage(t, canonical.Specify(canonical.ImageDetailOriginal))
	url, err := canonical.NewURLImage("https://example.test/tool.png", canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	callA := mustChatProjectionCall(t, `call_"a`)
	callB := mustChatProjectionCall(t, "call_b")
	resultA := mustChatProjectionResult(t, `call_"a`,
		canonical.NewImageToolResultPart(inline),
		canonical.NewTextToolResultPart("middle"),
		canonical.NewImageToolResultPart(url),
	)
	resultB := mustChatProjectionResult(t, "call_b", canonical.NewTextToolResultPart("done"))
	sink := &recordingDecisionSink{}
	document, err := LowerProviderRequestDocument(canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{callA, callB, resultA, resultB},
	}), delivery.BufferedDelivery(), sink, "exchange_parallel")
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Messages) != 4 {
		t.Fatalf("messages = %#v, want assistant, tool, tool, synthetic user", document.Messages)
	}
	for index, role := range []string{"assistant", "tool", "tool", "user"} {
		if document.Messages[index].Role != role {
			t.Fatalf("message %d role = %q, want %q", index, document.Messages[index].Role, role)
		}
	}
	synthetic, ok := document.Messages[3].Content.([]any)
	if !ok || len(synthetic) != 4 {
		t.Fatalf("synthetic content = %#v", document.Messages[3].Content)
	}
	if !decisionRecorded(sink.effects, compat.RequestItemsToolResultImage, compat.Approx) ||
		!decisionRecorded(sink.effects, compat.RequestItemsToolResultImageDetail, compat.Approx) {
		t.Fatalf("compatibility decisions = %#v", sink.effects)
	}
}

func TestChatCompatibilityToolImageProjectionKeepsAcceptedPrefixStable(t *testing.T) {
	image := mustChatProjectionImage(t, canonical.Unspecified[canonical.ImageDetail]())
	history := []canonical.CanonicalItem{
		mustChatProjectionCall(t, "call_prefix"),
		mustChatProjectionResult(t, "call_prefix", canonical.NewImageToolResultPart(image)),
	}
	base, err := LowerProviderRequestDocument(canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: history,
	}), delivery.BufferedDelivery(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	assistant, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{
		canonical.NewTextMessagePart("finished"),
	})
	user, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{
		canonical.NewTextMessagePart("next"),
	})
	extended, err := LowerProviderRequestDocument(canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: append(append([]canonical.CanonicalItem(nil), history...), assistant, user),
	}), delivery.BufferedDelivery(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	basePrefix, _ := json.Marshal(base.Messages)
	extendedPrefix, _ := json.Marshal(extended.Messages[:len(base.Messages)])
	if string(basePrefix) != string(extendedPrefix) {
		t.Fatalf("accepted prefix changed:\nbase: %s\nextended: %s", basePrefix, extendedPrefix)
	}
}

func mustChatProjectionCall(t *testing.T, rawCallID string) canonical.CanonicalItem {
	t.Helper()
	callID, err := canonical.NewToolCallID(rawCallID)
	if err != nil {
		t.Fatal(err)
	}
	key, err := canonical.NewRequestToolKey(canonical.ToolKindFunction, "inspect")
	if err != nil {
		t.Fatal(err)
	}
	input, err := canonical.ParseJSONObject([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	call, err := canonical.NewToolCallItem(callID, key, canonical.NewJSONObjectToolInput(input))
	if err != nil {
		t.Fatal(err)
	}
	return call
}

func mustChatProjectionResult(t *testing.T, rawCallID string, parts ...canonical.ToolResultPart) canonical.CanonicalItem {
	t.Helper()
	callID, err := canonical.NewToolCallID(rawCallID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewToolResultItem(callID, parts, false)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustChatProjectionImage(t *testing.T, detail canonical.Specified[canonical.ImageDetail]) canonical.ImagePart {
	t.Helper()
	image, err := canonical.NewInlineImage(canonical.ImageMediaPNG, imageTestPNG(), detail)
	if err != nil {
		t.Fatal(err)
	}
	return image
}
