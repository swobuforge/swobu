package generatecontent

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func TestStreamEncoderEmitsTextAndOnlyCompletedToolCalls(t *testing.T) {
	encoder := &generateContentStreamEncoder{adapter: sse.NewEnvelopeEventAdapter(), complete: func(*historyfingerprint.Response, []compat.Change) {}}
	textFrame, err := encoder.encode(sse.StreamEvent{Kind: sse.StreamEventTextDelta, TextDelta: "hello"})
	if err != nil || !strings.HasPrefix(string(textFrame), "data: ") || strings.Contains(string(textFrame), "[DONE]") {
		t.Fatalf("text frame = %q, %v", textFrame, err)
	}
	if frame, err := encoder.encode(sse.StreamEvent{Kind: sse.StreamEventToolUseArgumentsDelta, ArgumentsDelta: `{"q"`}); err != nil || frame != nil {
		t.Fatalf("partial args frame = %q, %v", frame, err)
	}
	id, _ := canonical.NewToolCallID("call-a")
	key, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "lookup")
	object, _ := canonical.ParseJSONObject([]byte(`{"q":"a"}`))
	item, _ := canonical.NewToolCallItem(id, key, canonical.NewJSONObjectToolInput(object))
	toolFrame, err := encoder.encode(sse.StreamEvent{Kind: sse.StreamEventItemCompleted, CompletedItem: &item})
	if err != nil || !strings.Contains(string(toolFrame), `"id":"call-a"`) || !strings.Contains(string(toolFrame), `"args":{"q":"a"}`) {
		t.Fatalf("tool frame = %q, %v", toolFrame, err)
	}
}

func TestStreamEncoderRejectsCompletedMessageSemanticsNotEmittedByDeltas(t *testing.T) {
	image, err := canonical.NewInlineImage(canonical.ImageMediaPNG, []byte("png"), canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{
		canonical.NewTextMessagePart("hello"),
		canonical.NewImageMessagePart(image),
	})
	if err != nil {
		t.Fatal(err)
	}
	encoder := &generateContentStreamEncoder{adapter: sse.NewEnvelopeEventAdapter(), complete: func(*historyfingerprint.Response, []compat.Change) {}}
	if _, err := encoder.encode(sse.StreamEvent{Kind: sse.StreamEventItemCompleted, CompletedItem: &message}); err == nil {
		t.Fatal("completed message with un-emitted image was accepted")
	}
	if len(encoder.items) != 0 {
		t.Fatalf("fingerprint history contains rejected item: %#v", encoder.items)
	}
}

func TestBufferedResponseUsesOneNativeCandidate(t *testing.T) {
	id, _ := canonical.NewToolCallID("call-a")
	key, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "lookup")
	object, _ := canonical.ParseJSONObject([]byte(`{"q":"a"}`))
	call, _ := canonical.NewToolCallItem(id, key, canonical.NewJSONObjectToolInput(object))
	message, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart("hello")})
	responseID := canonical.NewSwobuResponseID("resp-a")
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: responseID}, "model-a", []canonical.CanonicalItem{message, call}, canonical.Completed("stop"), canonical.TokenUsage{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	var wire responseDTO
	if err := json.Unmarshal(encoded.Document.RawBytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Candidates) != 1 || wire.Candidates[0].Content.Role != "model" || len(wire.Candidates[0].Content.Parts) != 2 {
		t.Fatalf("response = %+v", wire)
	}
	if wire.Candidates[0].Content.Parts[1].FunctionCall.ID != "call-a" {
		t.Fatalf("call id = %q", wire.Candidates[0].Content.Parts[1].FunctionCall.ID)
	}
	if strings.Contains(string(encoded.Document.RawBytes()), "responseId") || strings.Contains(string(encoded.Document.RawBytes()), "modelVersion") {
		t.Fatalf("response invented provider-native identity: %s", encoded.Document.RawBytes())
	}
}

func TestProjectUsagePreservesKnownTotalsWithoutInventingReasoningSplit(t *testing.T) {
	input, output := 10, 7
	usage, err := canonical.NewTokenUsage(canonical.TokenUsageParams{InputTokens: &input, OutputTokens: &output})
	if err != nil {
		t.Fatal(err)
	}
	projected, changes := projectUsage(usage)
	if projected == nil || projected.PromptTokenCount == nil || *projected.PromptTokenCount != input || projected.TotalTokenCount == nil || *projected.TotalTokenCount != input+output {
		t.Fatalf("usage=%#v", projected)
	}
	if projected.CandidatesTokenCount != nil || projected.ThoughtsTokenCount != nil {
		t.Fatalf("usage invented reasoning split: %#v", projected)
	}
	if len(changes) != 0 {
		t.Fatalf("changes=%#v want exact output recovery from total-input", changes)
	}
}

func TestProjectUsageAccountsForOutputWithoutInputOrReasoningSplit(t *testing.T) {
	output := 7
	usage, err := canonical.NewTokenUsage(canonical.TokenUsageParams{OutputTokens: &output})
	if err != nil {
		t.Fatal(err)
	}
	projected, changes := projectUsage(usage)
	if projected == nil {
		t.Fatal("known usage produced no metadata object")
	}
	want := compat.NewOmission(canonical.ResponseUsageOutputTokens, canonical.Occurrence{})
	if len(changes) != 1 || changes[0] != want {
		t.Fatalf("changes=%#v want [%#v]", changes, want)
	}
}

func TestProjectUsageAccountsForUnrepresentableKnownFacts(t *testing.T) {
	output, reasoning, cacheRead, cacheWrite := 7, 2, 3, 4
	usage, err := canonical.NewTokenUsage(canonical.TokenUsageParams{OutputTokens: &output, ReasoningTokens: &reasoning, CacheReadTokens: &cacheRead, CacheWriteTokens: &cacheWrite})
	if err != nil {
		t.Fatal(err)
	}
	projected, changes := projectUsage(usage)
	if projected == nil || projected.CandidatesTokenCount == nil || *projected.CandidatesTokenCount != 5 || projected.ThoughtsTokenCount == nil || *projected.ThoughtsTokenCount != reasoning {
		t.Fatalf("usage=%#v", projected)
	}
	for _, want := range []compat.Change{
		compat.NewOmission(canonical.ResponseUsageCacheReadTokens, canonical.Occurrence{}),
		compat.NewOmission(canonical.ResponseUsageCacheWriteTokens, canonical.Occurrence{}),
	} {
		if !hasResponseChange(changes, want) {
			t.Fatalf("changes=%#v missing %#v", changes, want)
		}
	}
}

func TestStreamEncoderEmitsCompletedToolCallsInCanonicalOrdinalOrder(t *testing.T) {
	newCall := func(id, query string) canonical.CanonicalItem {
		callID, _ := canonical.NewToolCallID(id)
		key, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "lookup")
		object, _ := canonical.ParseJSONObject([]byte(`{"q":"` + query + `"}`))
		item, _ := canonical.NewToolCallItem(callID, key, canonical.NewJSONObjectToolInput(object))
		return item
	}
	first, second := newCall("call-a", "a"), newCall("call-b", "b")
	encoder := &generateContentStreamEncoder{adapter: sse.NewEnvelopeEventAdapter(), complete: func(*historyfingerprint.Response, []compat.Change) {}}
	if frame, err := encoder.encode(sse.StreamEvent{Kind: sse.StreamEventItemCompleted, ItemOrdinal: 1, CompletedItem: &second}); err != nil || frame != nil {
		t.Fatalf("second completion frame=%q err=%v", frame, err)
	}
	frame, err := encoder.encode(sse.StreamEvent{Kind: sse.StreamEventItemCompleted, ItemOrdinal: 0, CompletedItem: &first})
	if err != nil {
		t.Fatal(err)
	}
	text := string(frame)
	firstIndex, secondIndex := strings.Index(text, `"id":"call-a"`), strings.Index(text, `"id":"call-b"`)
	if firstIndex < 0 || secondIndex < 0 || firstIndex >= secondIndex {
		t.Fatalf("frames emitted outside canonical order: %q", text)
	}
	if len(encoder.items) != 2 {
		t.Fatalf("fingerprint items=%d", len(encoder.items))
	}
}

func TestStreamEncoderDoesNotEmitLaterTextBeforeEarlierItem(t *testing.T) {
	callID, _ := canonical.NewToolCallID("call-a")
	key, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "lookup")
	object, _ := canonical.ParseJSONObject([]byte(`{"q":"a"}`))
	call, _ := canonical.NewToolCallItem(callID, key, canonical.NewJSONObjectToolInput(object))
	message, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart("hello")})
	encoder := &generateContentStreamEncoder{adapter: sse.NewEnvelopeEventAdapter(), complete: func(*historyfingerprint.Response, []compat.Change) {}}
	if frame, err := encoder.encode(sse.StreamEvent{Kind: sse.StreamEventTextDelta, ItemOrdinal: 1, TextDelta: "hello"}); err != nil || frame != nil {
		t.Fatalf("later text escaped before ordinal 0: frame=%q err=%v", frame, err)
	}
	if frame, err := encoder.encode(sse.StreamEvent{Kind: sse.StreamEventItemCompleted, ItemOrdinal: 1, CompletedItem: &message}); err != nil || frame != nil {
		t.Fatalf("later message completion escaped before ordinal 0: frame=%q err=%v", frame, err)
	}
	frame, err := encoder.encode(sse.StreamEvent{Kind: sse.StreamEventItemCompleted, ItemOrdinal: 0, CompletedItem: &call})
	if err != nil {
		t.Fatal(err)
	}
	wire := string(frame)
	callIndex, textIndex := strings.Index(wire, `"id":"call-a"`), strings.Index(wire, `"text":"hello"`)
	if callIndex < 0 || textIndex < 0 || callIndex >= textIndex {
		t.Fatalf("wire order = %q", wire)
	}
}

func TestResponseProjectionRejectsConsecutiveMessageBoundaries(t *testing.T) {
	first, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart("first")})
	second, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart("second")})
	_, _, err := projectResponseHistory(canonical.CanonicalRequest{}, []canonical.CanonicalItem{first, second})
	if err == nil {
		t.Fatal("consecutive message boundaries were flattened")
	}
	var projected canonical.Error
	if !errors.As(err, &projected) || projected.Code != canonical.ErrorCodeInternal {
		t.Fatalf("projection error = %T %v, want INTERNAL_ERROR", err, err)
	}
}

func TestResponseProjectionRejectsMessageBoundaryCollapseAcrossOmittedItems(t *testing.T) {
	first, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart("first")})
	trace, _ := canonical.NewReasoningPart(canonical.ReasoningPartTrace, "hidden")
	reasoning, _ := canonical.NewReasoningItem([]canonical.ReasoningPart{trace}, canonical.OpaqueThinking{})
	second, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart("second")})
	if _, _, err := projectResponseHistory(canonical.CanonicalRequest{}, []canonical.CanonicalItem{first, reasoning, second}); err == nil {
		t.Fatal("message boundaries collapsed across omitted reasoning")
	}

	callID, _ := canonical.NewToolCallID("search")
	input, _ := canonical.NewWebSearchToolInput(canonical.WebSearchCall{Action: canonical.WebSearchActionSearch})
	call, _ := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), input)
	if _, _, err := projectResponseHistory(canonical.CanonicalRequest{}, []canonical.CanonicalItem{first, call, second}); err == nil {
		t.Fatal("message boundaries collapsed across omitted web search")
	}
}

func TestResponseProjectionRejectsMultipleReasoningSummaries(t *testing.T) {
	first, _ := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "first")
	second, _ := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "second")
	reasoning, _ := canonical.NewReasoningItem([]canonical.ReasoningPart{first, second}, canonical.OpaqueThinking{})
	controls, _ := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Disclosure: canonical.Specify(canonical.ReasoningDisclosureSummary)})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Reasoning: controls})
	if _, _, err := projectResponseHistory(request, []canonical.CanonicalItem{reasoning}); err == nil {
		t.Fatal("one reasoning item projected as multiple replay items")
	}
}

func TestFinishReasonProjectsTruthfulCanonicalClasses(t *testing.T) {
	for _, test := range []struct {
		completion canonical.Completion
		want       string
	}{
		{canonical.Completed("stop"), "STOP"},
		{canonical.Incomplete("max_tokens"), "MAX_TOKENS"},
		{canonical.Declined("safety"), "SAFETY"},
	} {
		got, err := finishReason(test.completion)
		if err != nil || got != test.want {
			t.Fatalf("finishReason(%q) = %q, %v", test.completion.Class(), got, err)
		}
	}
	if _, err := finishReason(canonical.Failed("transport")); err == nil {
		t.Fatal("failed completion acquired a false Google finish reason")
	}
}

func TestResponseProjectionDoesNotExposeTraceAsSummary(t *testing.T) {
	summary, _ := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "summary")
	trace, _ := canonical.NewReasoningPart(canonical.ReasoningPartTrace, "private trace")
	reasoning, _ := canonical.NewReasoningItem([]canonical.ReasoningPart{summary, trace}, canonical.OpaqueThinking{})
	controls, _ := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Disclosure: canonical.Specify(canonical.ReasoningDisclosureSummary)})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Reasoning: controls})
	content, changes, err := projectResponseHistory(request, []canonical.CanonicalItem{reasoning})
	if err != nil {
		t.Fatal(err)
	}
	if len(content.Parts) != 1 || content.Parts[0].Text == nil || *content.Parts[0].Text != "summary" {
		t.Fatalf("content=%#v", content)
	}
	want := compat.NewOmission(canonical.ResponseItemsReasoning, canonical.ResponseItemOccurrence(0))
	if len(changes) != 1 || changes[0] != want {
		t.Fatalf("changes=%#v want [%#v]", changes, want)
	}
}

func TestStreamEncoderCompletesWithUsageOmissions(t *testing.T) {
	output, cacheRead := 7, 3
	usage, err := canonical.NewTokenUsage(canonical.TokenUsageParams{OutputTokens: &output, CacheReadTokens: &cacheRead})
	if err != nil {
		t.Fatal(err)
	}
	var completed []compat.Change
	encoder := &generateContentStreamEncoder{
		adapter: sse.NewEnvelopeEventAdapter(),
		complete: func(_ *historyfingerprint.Response, changes []compat.Change) {
			completed = changes
		},
	}
	if _, err := encoder.encode(sse.StreamEvent{Kind: sse.StreamEventCompleted, Completion: canonical.Completed("stop"), Usage: usage}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []compat.Change{
		compat.NewOmission(canonical.ResponseUsageOutputTokens, canonical.Occurrence{}),
		compat.NewOmission(canonical.ResponseUsageCacheReadTokens, canonical.Occurrence{}),
	} {
		if !hasResponseChange(completed, want) {
			t.Fatalf("changes=%#v missing %#v", completed, want)
		}
	}
}

func hasResponseChange(changes []compat.Change, want compat.Change) bool {
	for _, change := range changes {
		if change == want {
			return true
		}
	}
	return false
}

func TestCanonicalWireReplayCanonicalPreservesParallelToolCorrelation(t *testing.T) {
	makeCall := func(idRaw, query string) canonical.CanonicalItem {
		id, _ := canonical.NewToolCallID(idRaw)
		key, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "lookup")
		object, _ := canonical.ParseJSONObject([]byte(`{"q":"` + query + `"}`))
		item, _ := canonical.NewToolCallItem(id, key, canonical.NewJSONObjectToolInput(object))
		return item
	}
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID("resp")}, "model", []canonical.CanonicalItem{makeCall("A", "a"), makeCall("B", "b")}, canonical.Completed("tool_calls"), canonical.TokenUsage{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	var native responseDTO
	if err := json.Unmarshal(encoded.Document.RawBytes(), &native); err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(native.Candidates[0].Content)
	if err != nil {
		t.Fatal(err)
	}
	replay := `{"contents":[{"role":"user","parts":[{"text":"lookup"}]},` + string(content) + `,{"role":"user","parts":[{"functionResponse":{"id":"B","name":"lookup","response":{"output":"b"}}},{"functionResponse":{"id":"A","name":"lookup","response":{"output":"a"}}}]}]}`
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Raw: []byte(replay)}, canonical.ClientOperation{Family: canonical.ClientFamilyGenerateContent, Model: canonical.Specify("model"), Streaming: canonical.Specify(false)})
	if err != nil {
		t.Fatal(err)
	}
	items := decoded.Request.Request.Items()
	got := []string{}
	for _, item := range items {
		if call, ok := item.ToolCall(); ok {
			got = append(got, "call:"+call.CallID().String()+":"+call.Tool().Name())
		}
		if result, ok := item.ToolResult(); ok {
			got = append(got, "result:"+result.CallID().String())
		}
	}
	want := []string{"call:A:lookup", "call:B:lookup", "result:B", "result:A"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("correlation = %v", got)
	}
}

func TestResponseProjectionOmitsWebSearchLifecycleAndCitationsExplicitly(t *testing.T) {
	callID, _ := canonical.NewToolCallID("search-1")
	input, _ := canonical.NewWebSearchToolInput(canonical.WebSearchCall{Action: canonical.WebSearchActionSearch})
	call, _ := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), input)
	search, _ := canonical.NewWebSearchResult(nil)
	result, _ := canonical.NewWebSearchResultItem(callID, search)
	webURL, _ := canonical.NewWebURL("https://example.com")
	source, _ := canonical.NewWebSource(webURL, canonical.Unspecified[string]())
	part, _ := canonical.NewCitedTextMessagePart("answer", []canonical.WebCitation{{Source: source, Start: canonical.Specify(uint32(0)), End: canonical.Specify(uint32(6))}})
	message, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{part})
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID("resp")}, "model", []canonical.CanonicalItem{call, result, message}, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded.Document.RawBytes()), `"text":"answer"`) {
		t.Fatalf("response=%s", encoded.Document.RawBytes())
	}
	want := []compat.Change{
		compat.NewOmission(canonical.ResponseItemsKind, canonical.ResponseItemOccurrence(0)),
		compat.NewOmission(canonical.ResponseItemsKind, canonical.ResponseItemOccurrence(1)),
		compat.NewOmission(canonical.ResponseItemsMessageCitations, canonical.ResponsePartOccurrence(canonical.ItemPosition{Item: 2, Part: 0})),
	}
	if len(encoded.Changes) != len(want) {
		t.Fatalf("changes=%#v", encoded.Changes)
	}
	for index := range want {
		if encoded.Changes[index] != want[index] {
			t.Fatalf("change[%d]=%#v want %#v", index, encoded.Changes[index], want[index])
		}
	}
}
