package chatcompletions

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

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
	callID, callOK := changes[0].Occurrence.Call()
	position, partOK := changes[1].Occurrence.ResponsePart()
	if changes[0].Capability != canonical.ResponseItemsKind || changes[0].Kind != compat.Omission ||
		!callOK || callID.String() != "search_1" ||
		changes[1].Capability != canonical.ResponseItemsMessageCitations || changes[1].Kind != compat.Omission ||
		!partOK || position.Item != 2 || position.Part != 0 {
		t.Fatalf("changes = %#v", changes)
	}
}
