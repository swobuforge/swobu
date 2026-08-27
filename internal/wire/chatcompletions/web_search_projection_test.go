package chatcompletions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestChatRequestProjectsSettledWebSearchHistory(t *testing.T) {
	items := append(chatWebSearchResponse(t).Items(), canonicaltest.Message(t, canonical.MessageRoleUser, "Explain it"))
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("local"), Items: items})
	var changes []compat.Change

	document, err := EncodeCarrierWithChanges(request, nil, delivery.BufferedDelivery(), &changes, "exchange")
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Messages []ProviderRequestMessage `json:"messages"`
	}
	if err := json.Unmarshal(document.RawBytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Messages) != 2 || body.Messages[0].Role != "assistant" || body.Messages[0].Content != "Deadline" ||
		body.Messages[1].Role != "user" || body.Messages[1].Content != "Explain it" {
		t.Fatalf("messages = %#v", body.Messages)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %#v, want lifecycle and citation omissions", changes)
	}
	item, ok := changes[0].Occurrence.RequestItem()
	if changes[0].Capability != canonical.RequestItemsKind || changes[0].Kind != compat.Omission || !ok || item != 0 {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestChatRequestSettledWebSearchRecordsOmission(t *testing.T) {
	items := chatWebSearchResponse(t).Items()
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("local"), Items: items})
	var changes []compat.Change
	if _, err := EncodeCarrierWithChanges(request, nil, delivery.BufferedDelivery(), &changes, "exchange"); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %#v, want lifecycle and citation omissions", changes)
	}
	item, ok := changes[0].Occurrence.RequestItem()
	if changes[0].Capability != canonical.RequestItemsKind || changes[0].Kind != compat.Omission || !ok || item != 0 {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestChatRequestProjectionDoesNotMutateCanonicalRequest(t *testing.T) {
	items := append(chatWebSearchResponse(t).Items(), canonicaltest.Message(t, canonical.MessageRoleUser, "Explain it"))
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("local"), Items: items})
	before := request.Clone()
	if _, err := EncodeCarrier(request, delivery.BufferedDelivery()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, request) {
		t.Fatal("canonical request mutated during projection")
	}
}

func TestChatRequestRejectsUnsettledWebSearchHistory(t *testing.T) {
	items := chatWebSearchResponse(t).Items()
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("local"),
		Items: []canonical.CanonicalItem{items[0], canonicaltest.Message(t, canonical.MessageRoleAssistant, "Still searching")},
	})

	_, err := EncodeCarrier(request, delivery.BufferedDelivery())
	var swobuErr canonical.Error
	if !errors.As(err, &swobuErr) || swobuErr.Code != canonical.ErrorCodeNotImplemented {
		t.Fatalf("error = %T %v, want NOT_IMPLEMENTED", err, err)
	}
}

func TestChatRequestProjectsReusedWebSearchIDsByOccurrence(t *testing.T) {
	responseItems := chatWebSearchResponse(t).Items()
	items := []canonical.CanonicalItem{
		canonicaltest.Message(t, canonical.MessageRoleUser, "First"),
		responseItems[0], responseItems[1],
		canonicaltest.Message(t, canonical.MessageRoleAssistant, "First answer"),
		canonicaltest.Message(t, canonical.MessageRoleUser, "Second"),
		responseItems[0], responseItems[1],
		canonicaltest.Message(t, canonical.MessageRoleAssistant, "Second answer"),
	}

	projected, changes, err := projectChatCompletionsRequestWebSearchLifecycles(items)
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) != 4 {
		t.Fatalf("projected items = %#v", projected)
	}
	for index, want := range []string{"First", "First answer", "Second", "Second answer"} {
		message, ok := projected[index].Message()
		if !ok || messageText(message) != want {
			t.Fatalf("projected[%d] = %#v, want %q", index, projected[index], want)
		}
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %#v, want one omission per occurrence", changes)
	}
	unique := make([]compat.Change, 0, len(changes))
	for index, change := range changes {
		item, ok := change.Occurrence.RequestItem()
		if change.Capability != canonical.RequestItemsKind || change.Kind != compat.Omission || !ok || item != uint32(1+index*4) {
			t.Fatalf("change = %#v", change)
		}
		unique = compat.AppendUnique(unique, change)
	}
	if len(unique) != 2 {
		t.Fatalf("deduplicated changes = %#v, want independently addressable occurrences", unique)
	}
}

func TestChatRequestCitedAnswerRecordsCitationOmission(t *testing.T) {
	items := chatWebSearchResponse(t).Items()
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("local"), Items: items})
	var changes []compat.Change
	if _, err := EncodeCarrierWithChanges(request, nil, delivery.BufferedDelivery(), &changes, "exchange"); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %#v, want lifecycle and citation omissions", changes)
	}
	part, ok := changes[1].Occurrence.RequestPart()
	if changes[1].Capability != canonical.RequestItemsMessageCitations || changes[1].Kind != compat.Omission || !ok || part.Item != 2 || part.Part != 0 {
		t.Fatalf("citation change = %#v", changes[1])
	}
}

func TestChatRequestCitedHistoryWithoutSearchPairRecordsCitationOmission(t *testing.T) {
	items := chatWebSearchResponse(t).Items()
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("local"), Items: []canonical.CanonicalItem{items[2]}})
	var changes []compat.Change
	if _, err := EncodeCarrierWithChanges(request, nil, delivery.BufferedDelivery(), &changes, "exchange"); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("changes = %#v, want citation omission", changes)
	}
	part, ok := changes[0].Occurrence.RequestPart()
	if changes[0].Capability != canonical.RequestItemsMessageCitations || changes[0].Kind != compat.Omission || !ok || part.Item != 0 || part.Part != 0 {
		t.Fatalf("citation change = %#v", changes[0])
	}
}

func TestChatRequestProjectionPreservesSiblingOrder(t *testing.T) {
	responseItems := chatWebSearchResponse(t).Items()
	functionKey := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	functionCall := canonicaltest.ToolCall(t, "function_1", functionKey, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{}`)))
	functionID, _ := canonical.NewToolCallID("function_1")
	functionResult, err := canonical.NewToolResultItem(functionID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("function result")}, false)
	if err != nil {
		t.Fatal(err)
	}
	items := []canonical.CanonicalItem{
		canonicaltest.Message(t, canonical.MessageRoleSystem, "system"),
		canonicaltest.Message(t, canonical.MessageRoleUser, "question"),
		responseItems[0], responseItems[1], responseItems[2],
		functionCall, functionResult,
		canonicaltest.Message(t, canonical.MessageRoleUser, "follow-up"),
	}

	projected, _, err := projectChatCompletionsRequestWebSearchLifecycles(items)
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) != 6 || projected[3].Kind() != canonical.ItemKindToolCall || projected[4].Kind() != canonical.ItemKindToolResult {
		t.Fatalf("projected siblings = %#v", projected)
	}
	for index, want := range map[int]string{0: "system", 1: "question", 2: "Deadline", 5: "follow-up"} {
		message, ok := projected[index].Message()
		if !ok || messageText(message) != want {
			t.Fatalf("projected[%d] = %#v, want %q", index, projected[index], want)
		}
	}
}

func TestCurrentWebSearchDeclarationIsOmittedForStandardChat(t *testing.T) {
	set, err := canonical.NewToolSet([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("local"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, set.Declarations()...),
			canonicaltest.Message(t, canonical.MessageRoleUser, "Search now"),
		},
	})

	var changes []compat.Change
	document, err := EncodeCarrierWithChanges(request, nil, delivery.BufferedDelivery(), &changes, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(document.RawBytes()), "web_search") {
		t.Fatalf("web search leaked into request: %s", document.RawBytes())
	}
	want := compat.NewOmission(canonical.RequestToolsKind, canonical.ToolOccurrence(canonical.WebSearchToolKey()))
	if len(changes) != 1 || changes[0] != want {
		t.Fatalf("changes = %#v, want %#v", changes, want)
	}
}

func messageText(message canonical.MessageItem) string {
	var text string
	for _, part := range message.Content() {
		if value, ok := part.Text(); ok {
			text += value.Text()
		}
	}
	return text
}

func TestChatResponseProjectsCompletedWebSearchToCitedFinalText(t *testing.T) {
	response := chatWebSearchResponse(t)
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	raw := encoded.Document.RawBytes()
	if !bytes.Contains(raw, []byte(`"content":"Deadline"`)) {
		t.Fatalf("final text was lost: %s", raw)
	}
	for _, forbidden := range []string{"web_search", "search_1", "https://example.com/rules"} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatalf("Chat response leaked unrepresentable %q semantics: %s", forbidden, raw)
		}
	}
	assertChatWebSearchProjectionDecisions(t, encoded.Changes)
}

func TestChatStreamingResponseProjectsCompletedWebSearchToCitedFinalText(t *testing.T) {
	response := chatWebSearchResponse(t)
	events := canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		"exchange", response.Response(), response.Model(), response.Items(), response.Completion(), response.Usage(),
	))
	encoded, err := (ResponseStreamEncoder{}).EncodeResponseStream(
		context.Background(), canonical.CanonicalRequest{}, events, delivery.StreamingDelivery(delivery.FramingSSE),
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(encoded.Stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"content":"Deadline"`)) {
		t.Fatalf("final text was lost: %s", raw)
	}
	for _, forbidden := range []string{"web_search", "search_1", "https://example.com/rules"} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatalf("Chat stream leaked unrepresentable %q semantics: %s", forbidden, raw)
		}
	}
	assertChatWebSearchProjectionDecisions(t, encoded.Completion.Snapshot().Changes)
}

func TestChatStreamingResponseAddressesReusedWebSearchIDByPosition(t *testing.T) {
	response := chatWebSearchResponse(t)
	items := response.Items()
	plain := canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer")
	reused, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: "resp_reused"}, "model",
		[]canonical.CanonicalItem{items[0], items[1], items[0], items[1], plain},
		canonical.Completed("stop"), canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	events := canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		"exchange", reused.Response(), reused.Model(), reused.Items(), reused.Completion(), reused.Usage(),
	))
	encoded, err := (ResponseStreamEncoder{}).EncodeResponseStream(
		context.Background(), canonical.CanonicalRequest{}, events, delivery.StreamingDelivery(delivery.FramingSSE),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(encoded.Stream.Body); err != nil {
		t.Fatal(err)
	}
	changes := encoded.Completion.Snapshot().Changes
	if len(changes) != 2 {
		t.Fatalf("changes = %#v, want two lifecycle omissions", changes)
	}
	unique := make([]compat.Change, 0, len(changes))
	for index, change := range changes {
		item, ok := change.Occurrence.ResponseItem()
		if change.Capability != canonical.ResponseItemsKind || change.Kind != compat.Omission || !ok || item != uint32(index*2) {
			t.Fatalf("change = %#v", change)
		}
		unique = compat.AppendUnique(unique, change)
	}
	if len(unique) != 2 {
		t.Fatalf("deduplicated changes = %#v, want two streamed occurrences", unique)
	}
}

func TestChatResponseProjectsReusedWebSearchIDByOccurrence(t *testing.T) {
	response := chatWebSearchResponse(t)
	items := response.Items()
	reused := append([]canonical.CanonicalItem{items[0], items[1], items[0], items[1]}, items[2])
	projected, changes, err := projectChatCompletionsWebSearchLifecycles(reused)
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) != 1 || projected[0].Kind() != canonical.ItemKindMessage {
		t.Fatalf("projected items = %#v, want only message", projected)
	}
	if len(changes) != 3 {
		t.Fatalf("changes = %#v, want two lifecycle drops and one citation drop", changes)
	}
	unique := make([]compat.Change, 0, len(changes))
	for index, change := range changes[:2] {
		item, ok := change.Occurrence.ResponseItem()
		if change.Capability != canonical.ResponseItemsKind || change.Kind != compat.Omission || !ok || item != uint32(index*2) {
			t.Fatalf("change = %#v", change)
		}
		unique = compat.AppendUnique(unique, change)
	}
	if len(unique) != 2 {
		t.Fatalf("deduplicated changes = %#v, want independently addressable response occurrences", unique)
	}
}

func chatWebSearchResponse(t *testing.T) canonical.CanonicalResponse {
	t.Helper()
	callID, err := canonical.NewToolCallID("search_1")
	if err != nil {
		t.Fatal(err)
	}
	input, err := canonical.NewWebSearchToolInput(canonical.WebSearchCall{
		Action:  canonical.WebSearchActionSearch,
		Queries: []string{"deadline"},
	})
	if err != nil {
		t.Fatal(err)
	}
	call, err := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), input)
	if err != nil {
		t.Fatal(err)
	}
	webURL, err := canonical.NewWebURL("https://example.com/rules")
	if err != nil {
		t.Fatal(err)
	}
	source, err := canonical.NewWebSource(webURL, canonical.Specify("Rules"))
	if err != nil {
		t.Fatal(err)
	}
	searchResult, err := canonical.NewWebSearchResult([]canonical.WebSource{source})
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewWebSearchResultItem(callID, searchResult)
	if err != nil {
		t.Fatal(err)
	}
	part, err := canonical.NewCitedTextMessagePart("Deadline", []canonical.WebCitation{{Source: source}})
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{part})
	if err != nil {
		t.Fatal(err)
	}
	response, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID("resp_1")},
		"model", []canonical.CanonicalItem{call, result, message}, canonical.Completed("stop"), canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func assertChatWebSearchProjectionDecisions(t *testing.T, changes []compat.Change) {
	t.Helper()
	if len(changes) != 2 {
		t.Fatalf("changes = %#v", changes)
	}
	item, itemOK := changes[0].Occurrence.ResponseItem()
	position, partOK := changes[1].Occurrence.ResponsePart()
	if changes[0].Capability != canonical.ResponseItemsKind || changes[0].Kind != compat.Omission ||
		!itemOK || item != 0 ||
		changes[1].Capability != canonical.ResponseItemsMessageCitations || changes[1].Kind != compat.Omission ||
		!partOK || position.Item != 2 || position.Part != 0 {
		t.Fatalf("changes = %#v", changes)
	}
}
