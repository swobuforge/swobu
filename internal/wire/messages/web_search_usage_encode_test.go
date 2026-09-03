package messages

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestMessagesWebSearchUsageCountsSuccessfulActionsNotSources(t *testing.T) {
	tests := []struct {
		name  string
		items []canonical.CanonicalItem
		count int
	}{
		{name: "completed search with zero sources", items: messagesSearchLifecycle(t, "zero", nil, ""), count: 1},
		{name: "completed search with five sources", items: messagesSearchLifecycle(t, "many", messagesSearchSources(t, 5), ""), count: 1},
		{name: "failed search", items: messagesSearchLifecycle(t, "failed", nil, "unavailable"), count: 0},
		{name: "two completed searches", items: append(messagesSearchLifecycle(t, "first", nil, ""), messagesSearchLifecycle(t, "second", messagesSearchSources(t, 2), "")...), count: 2},
		{name: "open page", items: messagesNonSearchLifecycle(t, "open", canonical.WebSearchActionOpenPage), count: 0},
		{name: "find in page", items: messagesNonSearchLifecycle(t, "find", canonical.WebSearchActionFindInPage), count: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response, err := canonical.NewCanonicalResponse(
				canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID("resp_usage")},
				"model", test.items, canonical.Completed("stop"), canonical.NewUnknownTokenUsage(),
			)
			if err != nil {
				t.Fatal(err)
			}

			buffered, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(buffered.Document.RawBytes(), []byte(`"usage"`)) {
				t.Fatalf("buffered Messages fabricated token usage to preserve web-search detail: %s", buffered.Document.RawBytes())
			}

			events := canonical.SynthesizeResponseEnvelopeEvents(
				"exchange", response.Response(), response.Model(), response.Items(), response.Completion(), response.Usage(),
			)
			streamed, err := (ResponseStreamEncoder{}).EncodeResponseStream(
				context.Background(), canonical.CanonicalRequest{}, canonical.NewSliceEventReader(events), delivery.StreamingDelivery(delivery.FramingSSE),
			)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(streamed.Stream.Body)
			if err != nil {
				t.Fatal(err)
			}
			assertMessagesWebSearchUsage(t, raw, test.count)
		})
	}
}

func messagesNonSearchLifecycle(t *testing.T, id string, action canonical.WebSearchAction) []canonical.CanonicalItem {
	t.Helper()
	webURL, err := canonical.NewWebURL("https://example.com/source")
	if err != nil {
		t.Fatal(err)
	}
	callValue := canonical.WebSearchCall{Action: action, URL: canonical.Specify(webURL)}
	if action == canonical.WebSearchActionFindInPage {
		callValue.Match = canonical.Specify("needle")
	}
	callID, _ := canonical.NewToolCallID(id)
	input, err := canonical.NewWebSearchToolInput(callValue)
	if err != nil {
		t.Fatal(err)
	}
	call, _ := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), input)
	resultValue, _ := canonical.NewWebSearchResult(nil)
	result, _ := canonical.NewWebSearchResultItem(callID, resultValue)
	return []canonical.CanonicalItem{call, result}
}

func messagesSearchLifecycle(t *testing.T, id string, sources []canonical.WebSource, failure string) []canonical.CanonicalItem {
	t.Helper()
	callID, err := canonical.NewToolCallID(id)
	if err != nil {
		t.Fatal(err)
	}
	input, err := canonical.NewWebSearchToolInput(canonical.WebSearchCall{Action: canonical.WebSearchActionSearch, Queries: []string{id}})
	if err != nil {
		t.Fatal(err)
	}
	call, err := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), input)
	if err != nil {
		t.Fatal(err)
	}
	var resultValue canonical.WebSearchResult
	if failure == "" {
		resultValue, err = canonical.NewWebSearchResult(sources)
	} else {
		resultValue, err = canonical.NewWebSearchFailureResult(failure)
	}
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewWebSearchResultItem(callID, resultValue)
	if err != nil {
		t.Fatal(err)
	}
	return []canonical.CanonicalItem{call, result}
}

func messagesSearchSources(t *testing.T, count int) []canonical.WebSource {
	t.Helper()
	sources := make([]canonical.WebSource, 0, count)
	for index := 0; index < count; index++ {
		webURL, err := canonical.NewWebURL("https://example.com/" + string(rune('a'+index)))
		if err != nil {
			t.Fatal(err)
		}
		source, err := canonical.NewWebSource(webURL, canonical.Unspecified[string]())
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, source)
	}
	return sources
}

func assertMessagesWebSearchUsage(t *testing.T, raw []byte, want int) {
	t.Helper()
	wantJSON := []byte(`"server_tool_use":{"web_search_requests":` + strconv.Itoa(want) + `}`)
	if !bytes.Contains(raw, wantJSON) {
		t.Fatalf("Messages output lacks web-search usage %d: %s", want, raw)
	}
}
