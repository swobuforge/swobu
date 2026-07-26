package chatcompletions

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestChatCompatibilityRehomesClosedBatchToolResultImage(t *testing.T) {
	callID, err := canonical.NewToolCallID("call_image")
	if err != nil {
		t.Fatal(err)
	}
	key, err := canonical.NewToolKey(canonical.ToolNamespaceRequest, canonical.ToolKindFunction, "screenshot")
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
	image, err := canonical.NewInlineImage(canonical.ImageMediaPNG, imageTestPNG(), canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{
		canonical.NewTextToolResultPart("before"),
		canonical.NewImageToolResultPart(image),
		canonical.NewTextToolResultPart("after"),
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{call, result},
	})
	sink := &recordingDecisionSink{}
	document, err := EncodeCarrierWithDecisions(request, delivery.BufferedDelivery(), sink, "exchange_1")
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
	if len(payload.Messages) != 3 {
		t.Fatalf("messages = %#v, want assistant, tool, synthetic user", payload.Messages)
	}
	if payload.Messages[0].Role != "assistant" || payload.Messages[1].Role != "tool" || payload.Messages[2].Role != "user" {
		t.Fatalf("roles = %q, %q, %q", payload.Messages[0].Role, payload.Messages[1].Role, payload.Messages[2].Role)
	}
	var toolContent string
	if err := json.Unmarshal(payload.Messages[1].Content, &toolContent); err != nil {
		t.Fatal(err)
	}
	wantRef := `{"_swobu":{"v":1,"kind":"tool_result_image_ref","call_id":"call_image","part":1}}`
	if !strings.Contains(toolContent, "before") || !strings.Contains(toolContent, wantRef) || !strings.Contains(toolContent, "after") {
		t.Fatalf("tool content = %q", toolContent)
	}
	var synthetic []map[string]any
	if err := json.Unmarshal(payload.Messages[2].Content, &synthetic); err != nil {
		t.Fatal(err)
	}
	if len(synthetic) != 2 || synthetic[0]["type"] != "text" || synthetic[1]["type"] != "image_url" {
		t.Fatalf("synthetic content = %#v", synthetic)
	}
	wantMarker := `{"_swobu":{"v":1,"kind":"tool_result_image","call_id":"call_image","part":1}}`
	if synthetic[0]["text"] != wantMarker {
		t.Fatalf("synthetic marker = %#v", synthetic[0]["text"])
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
		name   string
		items  []canonical.CanonicalItem
		reason string
	}{
		{
			name:   "no active batch",
			items:  []canonical.CanonicalItem{resultA},
			reason: "active tool-call batch is not provably closed",
		},
		{
			name:   "partial parallel batch",
			items:  []canonical.CanonicalItem{callA, callB, resultA},
			reason: "active tool-call batch is not provably closed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingDecisionSink{}
			_, err := EncodeCarrierWithDecisions(
				canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: tc.items}),
				delivery.BufferedDelivery(),
				sink,
				"exchange_reject",
			)
			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("error = %v, want %q", err, tc.reason)
			}
			var incompatible provider.IncompatibleTargetError
			if !errors.As(err, &incompatible) {
				t.Fatalf("error = %#v, want candidate incompatibility", err)
			}
			if !decisionRecorded(sink.effects, compat.RequestItemsToolResultImage, compat.Reject) {
				t.Fatalf("compatibility decisions = %#v", sink.effects)
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
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{callA, callB, resultA, resultB},
	})
	sink := &recordingDecisionSink{}
	document, err := LowerProviderRequestDocument(request, delivery.BufferedDelivery(), sink, "exchange_parallel")
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
	if document.Messages[1].SourceStart != 2 || document.Messages[1].SourceEnd != 3 ||
		document.Messages[2].SourceStart != 3 || document.Messages[2].SourceEnd != 4 ||
		document.Messages[3].SourceStart != 2 || document.Messages[3].SourceEnd != 4 {
		t.Fatalf("source associations = %#v", document.Messages)
	}
	synthetic, ok := document.Messages[3].Content.([]any)
	if !ok || len(synthetic) != 4 {
		t.Fatalf("synthetic content = %#v", document.Messages[3].Content)
	}
	firstMarker, _ := synthetic[0].(map[string]any)
	if got, _ := firstMarker["text"].(string); got != `{"_swobu":{"v":1,"kind":"tool_result_image","call_id":"call_\"a","part":0}}` {
		t.Fatalf("escaped marker = %q", got)
	}
	firstImage, _ := synthetic[1].(map[string]any)
	imageURL, _ := firstImage["image_url"].(map[string]string)
	if imageURL["detail"] != string(canonical.ImageDetailHigh) {
		t.Fatalf("original-detail projection = %#v", imageURL)
	}
	secondImage, _ := synthetic[3].(map[string]any)
	secondURL, _ := secondImage["image_url"].(map[string]string)
	if secondURL["url"] != "https://example.test/tool.png" {
		t.Fatalf("URL projection = %#v", secondURL)
	}
	if !decisionRecorded(sink.effects, compat.RequestItemsToolResultImage, compat.Approx) ||
		!decisionRecorded(sink.effects, compat.RequestItemsToolResultImageDetail, compat.Approx) {
		t.Fatalf("compatibility decisions = %#v", sink.effects)
	}
}

func TestChatCompatibilityImageOnlyToolResultKeepsToolContentNonEmpty(t *testing.T) {
	image := mustChatProjectionImage(t, canonical.Unspecified[canonical.ImageDetail]())
	call := mustChatProjectionCall(t, "call_only")
	result := mustChatProjectionResult(t, "call_only", canonical.NewImageToolResultPart(image))
	document, err := LowerProviderRequestDocument(
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("m"),
			Items: []canonical.CanonicalItem{call, result},
		}),
		delivery.BufferedDelivery(),
		nil,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	content, ok := document.Messages[1].Content.(string)
	if !ok || strings.TrimSpace(content) == "" || !strings.Contains(content, `"kind":"tool_result_image_ref"`) {
		t.Fatalf("image-only tool content = %#v", document.Messages[1].Content)
	}
}

func TestChatToolImageCompatibilityDecisionsComposeIndependently(t *testing.T) {
	image := mustChatProjectionImage(t, canonical.Specify(canonical.ImageDetailOriginal))
	call := mustChatProjectionCall(t, "call_losses")
	callID, _ := canonical.NewToolCallID("call_losses")
	result, err := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{
		canonical.NewTextToolResultPart("before"),
		canonical.NewImageToolResultPart(image),
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingDecisionSink{}
	_, err = LowerProviderRequestDocument(
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("m"),
			Items: []canonical.CanonicalItem{call, result},
		}),
		delivery.BufferedDelivery(), sink, "exchange_losses",
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, feature := range []compat.Feature{
		compat.RequestItemsToolResultImage,
		compat.RequestItemsToolResultImageDetail,
		compat.RequestItemsToolResultContentBoundaries,
		compat.RequestItemsToolResultIsError,
	} {
		if !decisionRecorded(sink.effects, feature, compat.Approx) {
			t.Fatalf("missing independent %q approximation in %#v", feature, sink.effects)
		}
	}
}

func TestChatCompatibilityToolImageProjectionKeepsAcceptedPrefixStable(t *testing.T) {
	image := mustChatProjectionImage(t, canonical.Unspecified[canonical.ImageDetail]())
	history := []canonical.CanonicalItem{
		mustChatProjectionCall(t, "call_prefix"),
		mustChatProjectionResult(t, "call_prefix", canonical.NewImageToolResultPart(image)),
	}
	base, err := LowerProviderRequestDocument(
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("m"),
			Items: history,
		}),
		delivery.BufferedDelivery(), nil, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	assistant, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{
		canonical.NewTextMessagePart("finished"),
	})
	user, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{
		canonical.NewTextMessagePart("next"),
	})
	extended, err := LowerProviderRequestDocument(
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("m"),
			Items: append(append([]canonical.CanonicalItem(nil), history...), assistant, user),
		}),
		delivery.BufferedDelivery(), nil, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	basePrefix, _ := json.Marshal(base.Messages)
	extendedPrefix, _ := json.Marshal(extended.Messages[:len(base.Messages)])
	if string(basePrefix) != string(extendedPrefix) {
		t.Fatalf("accepted prefix changed:\nbase: %s\nextended: %s", basePrefix, extendedPrefix)
	}
	syntheticCount := 0
	for _, message := range extended.Messages {
		content, ok := message.Content.([]any)
		if !ok || len(content) == 0 {
			continue
		}
		marker, _ := content[0].(map[string]any)
		text, _ := marker["text"].(string)
		if strings.Contains(text, `"kind":"tool_result_image"`) {
			syntheticCount++
		}
	}
	if syntheticCount != 1 {
		t.Fatalf("synthetic image message count = %d, messages %#v", syntheticCount, extended.Messages)
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

func decisionRecorded(decisions []compat.Decision, feature compat.Feature, outcome compat.Outcome) bool {
	for _, decision := range decisions {
		if decision.Feature == feature && decision.Outcome == outcome {
			return true
		}
	}
	return false
}
