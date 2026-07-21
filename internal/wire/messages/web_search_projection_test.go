package messages

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

func TestMessagesCitationExcerptRoundTripsAsCitationEvidence(t *testing.T) {
	start, end := 1, 2
	part, err := decodeMessagesCitedText("A£B", []messagesCitationDTO{{
		Type:           "web_search_result_location",
		URL:            "https://example.com/source",
		Title:          "Source",
		CitedText:      "evidence",
		StartCharIndex: &start,
		EndCharIndex:   &end,
	}})
	if err != nil {
		t.Fatal(err)
	}
	citations := part.Citations()
	if len(citations) != 1 {
		t.Fatalf("citations = %#v", citations)
	}
	excerpt, ok := citations[0].Excerpt.Get()
	if !ok || excerpt != "evidence" {
		t.Fatalf("excerpt = (%q, %t)", excerpt, ok)
	}
	encoded, err := encodeMessagesCitations("A£B", citations)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != 1 || encoded[0].CitedText != "evidence" || *encoded[0].StartCharIndex != start || *encoded[0].EndCharIndex != end {
		t.Fatalf("encoded citation = %#v", encoded)
	}
}

func TestMessagesResponseOmitsCompletedUnrepresentableWebSearchPair(t *testing.T) {
	webURL, _ := canonical.NewWebURL("https://example.com/source")
	source, _ := canonical.NewWebSource(webURL, canonical.Specify("Source"))
	result, _ := canonical.NewWebSearchResult([]canonical.WebSource{source})
	messagePart, _ := canonical.NewCitedTextMessagePart("answer", []canonical.WebCitation{{Source: source}})
	message, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{messagePart})

	tests := []struct {
		name string
		call canonical.WebSearchCall
	}{
		{name: "open page", call: canonical.WebSearchCall{Action: canonical.WebSearchActionOpenPage, URL: canonical.Specify(webURL)}},
		{name: "find in page", call: canonical.WebSearchCall{Action: canonical.WebSearchActionFindInPage, URL: canonical.Specify(webURL), Match: canonical.Specify("needle")}},
		{name: "queryless search", call: canonical.WebSearchCall{Action: canonical.WebSearchActionSearch}},
		{name: "multi query search", call: canonical.WebSearchCall{Action: canonical.WebSearchActionSearch, Queries: []string{"one", "two"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			callID, _ := canonical.NewToolCallID("search_original")
			call, _ := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), mustWebSearchToolInput(t, test.call))
			resultItem, _ := canonical.NewWebSearchResultItem(callID, result)
			response, err := canonical.NewCanonicalResponse(
				canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID("resp_1")},
				"model", []canonical.CanonicalItem{call, resultItem, message}, "stop", canonical.NewUnknownTokenUsage(),
			)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
			if err != nil {
				t.Fatal(err)
			}
			raw := encoded.Document.RawBytes()
			if bytes.Contains(raw, []byte("server_tool_use")) || bytes.Contains(raw, []byte("web_search_tool_result")) {
				t.Fatalf("omitted lifecycle leaked: %s", raw)
			}
			if !bytes.Contains(raw, []byte(`"text":"answer"`)) || !bytes.Contains(raw, []byte(`"url":"https://example.com/source"`)) {
				t.Fatalf("text or citation was lost: %s", raw)
			}
			if len(encoded.Decisions) != 1 || encoded.Decisions[0] != (compat.Decision{
				Feature: compat.ResponseItemsKind,
				Outcome: compat.Drop,
				Subject: compat.Subject("web_search:search_original"),
			}) {
				t.Fatalf("decisions = %#v", encoded.Decisions)
			}
		})
	}
}

func TestMessagesStreamingResponseOmitsCompletedUnrepresentableWebSearchPair(t *testing.T) {
	callID, _ := canonical.NewToolCallID("search_original")
	call, _ := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), mustWebSearchToolInput(t, canonical.WebSearchCall{
		Action: canonical.WebSearchActionOpenPage,
		URL:    canonical.Specify(mustWebURL(t, "https://example.com/source")),
	}))
	searchResult, _ := canonical.NewWebSearchResult(nil)
	result, _ := canonical.NewWebSearchResultItem(callID, searchResult)
	message, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart("answer")})
	response, _ := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID("resp_1")},
		"model", []canonical.CanonicalItem{call, result, message}, "stop", canonical.NewUnknownTokenUsage(),
	)
	events := canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		"exchange", response.Response(), response.Model(), response.Items(), response.CompletionReason(), response.Usage(),
	))
	encoded, err := (ResponseStreamEncoder{}).EncodeResponseStream(context.Background(), canonical.CanonicalRequest{}, events, delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(encoded.Stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("server_tool_use")) || bytes.Contains(raw, []byte("web_search_tool_result")) {
		t.Fatalf("omitted lifecycle leaked: %s", raw)
	}
	if !bytes.Contains(raw, []byte("answer")) {
		t.Fatalf("assistant text was lost: %s", raw)
	}
	decisions := encoded.TerminalDecisions.Decisions()
	if len(decisions) != 1 || decisions[0] != (compat.Decision{
		Feature: compat.ResponseItemsKind,
		Outcome: compat.Drop,
		Subject: "web_search:search_original",
	}) {
		t.Fatalf("decisions = %#v", decisions)
	}
}

func TestMessagesWebSearchFailureProjectionUsesObjectContent(t *testing.T) {
	callID, _ := canonical.NewToolCallID("search_original")
	call, _ := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), mustWebSearchToolInput(t, canonical.WebSearchCall{
		Action:  canonical.WebSearchActionSearch,
		Queries: []string{"one"},
	}))
	failure, _ := canonical.NewWebSearchFailureResult("unavailable")
	result, _ := canonical.NewWebSearchResultItem(callID, failure)
	response, _ := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID("resp_1")},
		"model", []canonical.CanonicalItem{call, result}, "stop", canonical.NewUnknownTokenUsage(),
	)
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	raw := encoded.Document.RawBytes()
	if !bytes.Contains(raw, []byte(`"content":{"type":"web_search_tool_result_error","error_code":"unavailable"}`)) {
		t.Fatalf("failure content is not the Messages object form: %s", raw)
	}
	if bytes.Contains(raw, []byte(`"is_error"`)) {
		t.Fatalf("web-search failure used client-tool is_error: %s", raw)
	}
}

func mustWebURL(t *testing.T, raw string) canonical.WebURL {
	t.Helper()
	value, err := canonical.NewWebURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustWebSearchToolInput(t *testing.T, call canonical.WebSearchCall) canonical.ToolInput {
	t.Helper()
	input, err := canonical.NewWebSearchToolInput(call)
	if err != nil {
		t.Fatal(err)
	}
	return input
}

func TestMessagesRequestHistoryOmitsPairOnceAndRejectsUnresolvedCall(t *testing.T) {
	callID, _ := canonical.NewToolCallID("search_original")
	unrepresentable, _ := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), mustWebSearchToolInput(t, canonical.WebSearchCall{Action: canonical.WebSearchActionSearch, Queries: []string{"one", "two"}}))
	result, _ := canonical.NewWebSearchResult(nil)
	resultItem, _ := canonical.NewWebSearchResultItem(callID, result)
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("continue")})

	completed := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{unrepresentable, resultItem, message}})
	_, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (struct{}, error) {
		_, err := EncodeCarrierWithDecisions(completed, delivery.BufferedDelivery(), sink, "exchange", EncodeOptions{})
		return struct{}{}, err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Feature != compat.RequestItemsKind || decisions[0].Outcome != compat.Drop || decisions[0].Subject != "web_search:search_original" {
		t.Fatalf("decisions = %#v", decisions)
	}

	unresolved := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{unrepresentable}})
	_, err = EncodeCarrierWithDecisions(unresolved, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
	var canonicalErr canonical.Error
	if !errors.As(err, &canonicalErr) || canonicalErr.Code != canonical.ErrorCodeUnsupportedOperation {
		t.Fatal("unresolved unrepresentable call was omitted")
	}
}

func TestMessagesSingleQuerySearchPreservesOriginalCallID(t *testing.T) {
	callID, _ := canonical.NewToolCallID("search_original")
	call, _ := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), mustWebSearchToolInput(t, canonical.WebSearchCall{Action: canonical.WebSearchActionSearch, Queries: []string{"one"}}))
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{call}})
	document, err := EncodeCarrierWithDecisions(request, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(document.RawBytes(), []byte(`"id":"search_original"`)) || !bytes.Contains(document.RawBytes(), []byte(`"query":"one"`)) {
		t.Fatalf("single query lifecycle changed: %s", document.RawBytes())
	}
}
