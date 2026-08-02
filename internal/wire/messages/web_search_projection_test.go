package messages

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
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
	}}, messagesProjectionEvidence{})
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
				"model", []canonical.CanonicalItem{call, resultItem, message}, canonical.Completed("stop"), canonical.NewUnknownTokenUsage(),
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
			if len(encoded.Changes) != 1 || encoded.Changes[0] != (compat.Change{
				Capability: canonical.ResponseItemsKind,
				Kind:       compat.Omission,
				Occurrence: canonical.CallOccurrence(callID),
			}) {
				t.Fatalf("changes = %#v", encoded.Changes)
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
		"model", []canonical.CanonicalItem{call, result, message}, canonical.Completed("stop"), canonical.NewUnknownTokenUsage(),
	)
	events := canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		"exchange", response.Response(), response.Model(), response.Items(), response.Completion(), response.Usage(),
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
	changes := encoded.Completion.Snapshot().Changes
	if len(changes) != 1 || changes[0] != (compat.Change{
		Capability: canonical.ResponseItemsKind,
		Kind:       compat.Omission,
		Occurrence: canonical.CallOccurrence(callID),
	}) {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestMessagesProjectionPairsReusedWebSearchIDByOccurrence(t *testing.T) {
	callID, _ := canonical.NewToolCallID("search_reused")
	input := mustWebSearchToolInput(t, canonical.WebSearchCall{
		Action: canonical.WebSearchActionOpenPage,
		URL:    canonical.Specify(mustWebURL(t, "https://example.com/source")),
	})
	call, _ := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), input)
	searchResult, _ := canonical.NewWebSearchResult(nil)
	result, _ := canonical.NewWebSearchResultItem(callID, searchResult)
	message, _ := canonical.NewMessageItem(
		canonical.MessageRoleAssistant,
		[]canonical.MessagePart{canonical.NewTextMessagePart("answer")},
	)

	projected, changes, err := projectMessagesWebSearchLifecycles(
		[]canonical.CanonicalItem{call, result, call, result, message},
		canonical.ResponseItemsKind,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) != 1 || projected[0].Kind() != canonical.ItemKindMessage {
		t.Fatalf("projected items = %#v, want only message", projected)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %#v, want two occurrence-local drops", changes)
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
		"model", []canonical.CanonicalItem{call, result}, canonical.Completed("stop"), canonical.NewUnknownTokenUsage(),
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
	var changes []compat.Change
	_, err := EncodeCarrierWithChanges(completed, nil, delivery.BufferedDelivery(), &changes, "exchange")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("changes = %#v", changes)
	}
	call, ok := changes[0].Occurrence.Call()
	if changes[0].Capability != canonical.RequestItemsKind || changes[0].Kind != compat.Omission || !ok || call != callID {
		t.Fatalf("changes = %#v", changes)
	}

	unresolved := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{unrepresentable}})
	_, err = EncodeCarrierWithChanges(unresolved, nil, delivery.BufferedDelivery(), nil, "")
	var incompatible provider.IncompatibleTargetError
	if !errors.As(err, &incompatible) {
		t.Fatalf("error = %T %v, want candidate incompatibility", err, err)
	}
	var unsupported compat.UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error = %T %v, want canonical unsupported issue", err, err)
	}
	issues := unsupported.Issues()
	if len(issues) != 1 {
		t.Fatalf("unsupported issues = %#v", issues)
	}
	issueCall, ok := issues[0].Occurrence().Call()
	if issues[0].Capability() != canonical.RequestItemsKind || !ok || issueCall != callID {
		t.Fatalf("unsupported issues = %#v", issues)
	}
}

func TestMessagesSingleQuerySearchPreservesOriginalCallID(t *testing.T) {
	callID, _ := canonical.NewToolCallID("search_original")
	call, _ := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), mustWebSearchToolInput(t, canonical.WebSearchCall{Action: canonical.WebSearchActionSearch, Queries: []string{"one"}}))
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{call}})
	document, err := EncodeCarrierWithChanges(request, testAttemptToolNames(request), delivery.BufferedDelivery(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(document.RawBytes(), []byte(`"id":"search_original"`)) || !bytes.Contains(document.RawBytes(), []byte(`"query":"one"`)) {
		t.Fatalf("single query lifecycle changed: %s", document.RawBytes())
	}
}

func TestMessagesWebSearchBufferedAndStreamedSemanticsAgree(t *testing.T) {
	callID, _ := canonical.NewToolCallID("search_original")
	call, _ := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), mustWebSearchToolInput(t, canonical.WebSearchCall{
		Action: canonical.WebSearchActionSearch, Queries: []string{"deadline"},
	}))
	webURL := mustWebURL(t, "https://example.com/source")
	source, _ := canonical.NewWebSource(webURL, canonical.Specify("Source"))
	searchResult, _ := canonical.NewWebSearchResult([]canonical.WebSource{source})
	result, _ := canonical.NewWebSearchResultItem(callID, searchResult)
	part, _ := canonical.NewCitedTextMessagePart("Deadline", []canonical.WebCitation{{Source: source}})
	message, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{part})
	inputTokens, outputTokens, cacheRead, cacheWrite := 2, 3, 1, 1
	usage, _ := canonical.NewTokenUsage(canonical.TokenUsageParams{
		InputTokens: &inputTokens, OutputTokens: &outputTokens,
		CacheReadTokens: &cacheRead, CacheWriteTokens: &cacheWrite,
	})
	response, _ := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID("resp_1")},
		"model", []canonical.CanonicalItem{call, result, message}, canonical.Completed("stop"), usage,
	)

	buffered, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	bufferedReader, err := decodeResponseBuffered(
		context.Background(), canonical.CanonicalRequest{}, nil, buffered.Document.RawBytes(), "buffered", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertMessagesWebSearchSemantics(t, bufferedReader, callID)

	events := canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		"exchange", response.Response(), response.Model(), response.Items(), response.Completion(), response.Usage(),
	))
	streamed, err := (ResponseStreamEncoder{}).EncodeResponseStream(
		context.Background(), canonical.CanonicalRequest{}, events, delivery.StreamingDelivery(delivery.FramingSSE),
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(streamed.Stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		`"type":"server_tool_use"`,
		`"type":"web_search_tool_result"`,
		`"type":"citations_delta"`,
		`"input_tokens":2`,
		`"output_tokens":3`,
		`"cache_read_input_tokens":1`,
		`"cache_creation_input_tokens":1`,
	} {
		if !bytes.Contains(raw, []byte(required)) {
			t.Fatalf("Messages stream lost %q: %s", required, raw)
		}
	}
	streamReader := decodeResponseStream(
		canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(bytes.NewReader(raw))},
		"streamed", nil,
	)
	assertMessagesWebSearchSemantics(t, streamReader, callID)
}

func assertMessagesWebSearchSemantics(t *testing.T, reader canonical.ResponseStream, callID canonical.ToolCallID) {
	t.Helper()
	closed, err := canonical.ReadClosedEnvelope(
		context.Background(),
		canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "replayed"}),
		canonical.EnvResponse,
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	items := response.Items()
	if len(items) != 3 {
		t.Fatalf("items = %#v", items)
	}
	call, _ := items[0].ToolCall()
	if call.CallID() != callID || call.Tool().Kind() != canonical.ToolKindWebSearch {
		t.Fatalf("call = %#v", call)
	}
	result, _ := items[1].ToolResult()
	search, _ := result.WebSearch()
	if len(search.Sources()) != 1 || search.Sources()[0].URL.String() != "https://example.com/source" {
		t.Fatalf("sources = %#v", search.Sources())
	}
	message, _ := items[2].Message()
	if len(message.Content()[0].Citations()) != 1 {
		t.Fatalf("citations = %#v", message.Content()[0].Citations())
	}
	if input, ok := response.Usage().InputTokens(); !ok || input != 2 {
		t.Fatalf("input usage = (%d,%t)", input, ok)
	}
	if output, ok := response.Usage().OutputTokens(); !ok || output != 3 {
		t.Fatalf("output usage = (%d,%t)", output, ok)
	}
	if value, ok := response.Usage().CacheReadTokens(); !ok || value != 1 {
		t.Fatalf("cache-read usage = (%d,%t)", value, ok)
	}
	if value, ok := response.Usage().CacheWriteTokens(); !ok || value != 1 {
		t.Fatalf("cache-write usage = (%d,%t)", value, ok)
	}
}
