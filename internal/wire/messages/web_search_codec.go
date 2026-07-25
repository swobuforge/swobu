package messages

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type messagesWebSearchResultBlock struct {
	Type      string `json:"type"`
	URL       string `json:"url,omitempty"`
	Title     string `json:"title,omitempty"`
	PageAge   string `json:"page_age,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

func decodeMessagesWebSearchCall(id string, input json.RawMessage) (canonical.CanonicalItem, error) {
	callID, err := canonical.NewToolCallID(id)
	if err != nil {
		return canonical.CanonicalItem{}, canonical.InternalError("messages web-search call is missing id")
	}
	var body struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &body); err != nil || strings.TrimSpace(body.Query) == "" { // swobu:io-string source=provider-wire
		return canonical.CanonicalItem{}, canonical.InternalError("messages web-search call input is invalid")
	}
	call := canonical.WebSearchCall{Action: canonical.WebSearchActionSearch, Queries: []string{body.Query}}
	toolInput, err := canonical.NewWebSearchToolInput(call)
	if err != nil {
		return canonical.CanonicalItem{}, canonical.InternalError("messages web-search call input is invalid")
	}
	return canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), toolInput)
}

func decodeMessagesWebSearchResult(callIDText string, content json.RawMessage, isError bool) (canonical.CanonicalItem, error) {
	callID, err := canonical.NewToolCallID(callIDText)
	if err != nil {
		return canonical.CanonicalItem{}, canonical.InternalError("messages web-search result is missing tool_use_id")
	}
	// Messages defines content as an array for successful searches and one
	// object for a search failure. Keep that wire union here at the codec edge.
	var blocks []messagesWebSearchResultBlock
	switch firstJSONByte(content) {
	case '[':
		if err := json.Unmarshal(content, &blocks); err != nil {
			return canonical.CanonicalItem{}, canonical.InternalError("messages web-search result content is invalid")
		}
	case '{':
		var block messagesWebSearchResultBlock
		if err := json.Unmarshal(content, &block); err != nil || block.Type != "web_search_tool_result_error" {
			return canonical.CanonicalItem{}, canonical.InternalError("messages web-search result content is invalid")
		}
		blocks = []messagesWebSearchResultBlock{block}
	default:
		return canonical.CanonicalItem{}, canonical.InternalError("messages web-search result content is invalid")
	}
	if len(blocks) == 1 && blocks[0].Type == "web_search_tool_result_error" {
		failure, err := canonical.NewWebSearchFailureResult(blocks[0].ErrorCode)
		if err != nil {
			return canonical.CanonicalItem{}, err
		}
		return canonical.NewWebSearchResultItem(callID, failure)
	}
	if isError {
		return canonical.CanonicalItem{}, canonical.InternalError("messages web-search error result content is invalid")
	}
	sources := make([]canonical.WebSource, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != "web_search_result" {
			return canonical.CanonicalItem{}, canonical.NotImplemented("Swobu has no canonical projection for this Messages web-search result block type")
		}
		webURL, err := canonical.NewWebURL(block.URL)
		if err != nil {
			return canonical.CanonicalItem{}, canonical.InternalError("messages web-search result URL is invalid")
		}
		title := canonical.Unspecified[string]()
		if strings.TrimSpace(block.Title) != "" { // swobu:io-string source=provider-wire
			title = canonical.Specify(block.Title)
		}
		source, err := canonical.NewWebSource(webURL, title)
		if err != nil {
			return canonical.CanonicalItem{}, err
		}
		sources = append(sources, source)
	}
	result, err := canonical.NewWebSearchResult(sources)
	if err != nil {
		return canonical.CanonicalItem{}, err
	}
	return canonical.NewWebSearchResultItem(callID, result)
}

func firstJSONByte(raw json.RawMessage) byte {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=provider-wire
	if trimmed == "" {
		return 0
	}
	return trimmed[0]
}

func decodeMessagesCitedText(text string, citations []messagesCitationDTO) (canonical.MessagePart, error) {
	canonicalCitations := make([]canonical.WebCitation, 0, len(citations))
	for _, citation := range citations {
		if citation.Type != "web_search_result_location" && citation.Type != "web_search_result" {
			return canonical.MessagePart{}, canonical.NotImplemented("Swobu has no canonical projection for this Messages citation type")
		}
		webURL, err := canonical.NewWebURL(citation.URL)
		if err != nil {
			return canonical.MessagePart{}, canonical.InternalError("messages citation URL is invalid")
		}
		title := canonical.Unspecified[string]()
		if strings.TrimSpace(citation.Title) != "" { // swobu:io-string source=provider-wire
			title = canonical.Specify(citation.Title)
		}
		excerpt := canonical.Unspecified[string]()
		if strings.TrimSpace(citation.CitedText) != "" { // swobu:io-string source=provider-wire
			excerpt = canonical.Specify(citation.CitedText)
		}
		source, err := canonical.NewWebSource(webURL, title)
		if err != nil {
			return canonical.MessagePart{}, err
		}
		value := canonical.WebCitation{Source: source, Excerpt: excerpt}
		if citation.StartCharIndex != nil && citation.EndCharIndex != nil {
			start, ok := messagesRuneOffset(text, *citation.StartCharIndex)
			if !ok {
				return canonical.MessagePart{}, canonical.InternalError("messages citation start index is invalid")
			}
			end, ok := messagesRuneOffset(text, *citation.EndCharIndex)
			if !ok {
				return canonical.MessagePart{}, canonical.InternalError("messages citation end index is invalid")
			}
			value.Start, value.End = canonical.Specify(uint32(start)), canonical.Specify(uint32(end))
		}
		canonicalCitations = append(canonicalCitations, value)
	}
	return canonical.NewCitedTextMessagePart(text, canonicalCitations)
}

func messagesRuneOffset(text string, index int) (int, bool) {
	if index < 0 {
		return 0, false
	}
	if index == utf8.RuneCountInString(text) {
		return len(text), true
	}
	position := 0
	for offset := range text {
		if position == index {
			return offset, true
		}
		position++
	}
	return 0, false
}

func encodeMessagesCitations(text string, citations []canonical.WebCitation) ([]messagesCitationDTO, error) {
	out := make([]messagesCitationDTO, 0, len(citations))
	for _, citation := range citations {
		wire := messagesCitationDTO{Type: "web_search_result_location", URL: citation.Source.URL.String()}
		if title, ok := citation.Source.Title.Get(); ok {
			wire.Title = title
		}
		if excerpt, ok := citation.Excerpt.Get(); ok {
			wire.CitedText = excerpt
		}
		if start, ok := citation.Start.Get(); ok {
			end, hasEnd := citation.End.Get()
			if !hasEnd || int(end) > len(text) {
				return nil, canonical.InternalError("canonical citation offsets are invalid")
			}
			startRunes, endRunes := utf8.RuneCountInString(text[:start]), utf8.RuneCountInString(text[:end])
			wire.StartCharIndex, wire.EndCharIndex = &startRunes, &endRunes
		}
		out = append(out, wire)
	}
	return out, nil
}

func encodeMessagesWebSearchResult(result canonical.WebSearchResult) (json.RawMessage, error) {
	if failure, ok := result.Failure(); ok {
		raw, err := json.Marshal(messagesWebSearchResultBlock{Type: "web_search_tool_result_error", ErrorCode: failure})
		// Unlike client tool_result blocks, web-search failures are identified by
		// the content object's type and do not use the is_error field.
		return raw, err
	}
	blocks := make([]messagesWebSearchResultBlock, 0, len(result.Sources()))
	for _, source := range result.Sources() {
		block := messagesWebSearchResultBlock{Type: "web_search_result", URL: source.URL.String()}
		if title, ok := source.Title.Get(); ok {
			block.Title = title
		}
		blocks = append(blocks, block)
	}
	raw, err := json.Marshal(blocks)
	if err != nil {
		return nil, canonical.InternalError("messages web-search result could not be encoded")
	}
	return raw, nil
}
