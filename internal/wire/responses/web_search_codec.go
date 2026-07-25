package responses

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func decodeResponsesWebSearchInclude(raw json.RawMessage, sink compat.Sink, exchangeID string) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var values []string
	if err := json.Unmarshal(trimmed, &values); err != nil {
		return canonical.BadRequest("responses include is invalid")
	}
	for index, value := range values {
		if value == "web_search_call.action.sources" {
			if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestTools, compat.Drop, compat.Subject(fmt.Sprintf("wire:/include/%d", index))); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *responsesResponseStream) completeMessageItem(frame streamFrame) (bool, error) {
	item, err := decodeResponsesMessageOutputItem(responsesWireOutputItemDTO{Type: "message", ID: frame.Item.ID, Status: frame.Item.Status, Role: "assistant", Content: frame.Item.Content})
	if err != nil {
		return false, err
	}
	s.emittedOutput = true
	ordinal := s.ordinalFor(fallbackItemID(frame.Item.ID, "message", frame.OutputIndex), frame.OutputIndex)
	if s.textState != nil {
		ordinal = s.textState.ordinal
		s.textState = nil
	}
	s.enqueueItemCompleted("", ordinal, item)
	return true, nil
}

func (s *responsesResponseStream) completeWebSearchItem(frame streamFrame) (bool, error) {
	s.emittedOutput = true
	s.closeOpenText(canonical.EnvelopeStatusCompleted)
	itemID := strings.TrimSpace(frame.Item.ID) // swobu:io-string source=provider-wire
	if itemID == "" {
		itemID = strings.TrimSpace(frame.ItemID)
	} // swobu:io-string source=provider-wire
	lifecycle, err := decodeResponsesWebSearchLifecycle(itemID, frame.Item.Action, responsesWebSearchSucceeded)
	if err != nil {
		return false, err
	}
	base := s.ordinalFor(itemID, frame.OutputIndex)
	for index, item := range lifecycle {
		if err := s.enqueueCompletedOutputItemAt(base+uint32(index), item); err != nil {
			return false, err
		}
	}
	if len(lifecycle) > 1 {
		s.ordinalOffset += int64(len(lifecycle) - 1)
	}
	return true, nil
}

type responsesWebSearchLifecycleState uint8

const (
	responsesWebSearchPending responsesWebSearchLifecycleState = iota + 1
	responsesWebSearchSucceeded
)

// decodeResponsesWebSearchLifecycleState collapses wire-only pending aliases.
// Failed durable history is rejected until canonical has a failure value that
// can preserve its execution meaning without inventing a message.
func decodeResponsesWebSearchLifecycleState(raw string) (responsesWebSearchLifecycleState, error) {
	switch strings.TrimSpace(raw) { // swobu:io-string source=boundary
	case "", "in_progress", "searching":
		return responsesWebSearchPending, nil
	case "completed":
		return responsesWebSearchSucceeded, nil
	case "failed":
		return 0, fmt.Errorf("failed web-search history has no canonical failure detail")
	default:
		return 0, fmt.Errorf("web-search status is unsupported")
	}
}

func decodeResponsesWebSearchLifecycle(id string, rawAction json.RawMessage, state responsesWebSearchLifecycleState) ([]canonical.CanonicalItem, error) {
	callID, err := canonical.NewToolCallID(strings.TrimSpace(id)) // swobu:io-string source=provider-wire
	if err != nil {
		return nil, canonical.InternalError("responses web-search call is missing id")
	}
	var action responsesWebSearchActionDTO
	if err := json.Unmarshal(rawAction, &action); err != nil {
		return nil, canonical.InternalError("responses web-search action is invalid")
	}
	call := canonical.WebSearchCall{Action: canonical.WebSearchAction(strings.TrimSpace(action.Type))} // swobu:io-string source=provider-wire
	if len(action.Queries) > 0 {
		call.Queries = append([]string(nil), action.Queries...)
	} else if strings.TrimSpace(action.Query) != "" { // swobu:io-string source=provider-wire
		call.Queries = []string{action.Query}
	}
	if strings.TrimSpace(action.URL) != "" { // swobu:io-string source=provider-wire
		webURL, err := canonical.NewWebURL(action.URL)
		if err != nil {
			return nil, canonical.InternalError("responses web-search action URL is invalid")
		}
		call.URL = canonical.Specify(webURL)
	}
	if strings.TrimSpace(action.Pattern) != "" { // swobu:io-string source=provider-wire
		call.Match = canonical.Specify(action.Pattern)
	}
	if err := call.Validate(); err != nil {
		return nil, canonical.InternalError("responses web-search action is invalid")
	}
	input, err := canonical.NewWebSearchToolInput(call)
	if err != nil {
		return nil, canonical.InternalError("responses web-search action is invalid")
	}
	callItem, err := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), input)
	if err != nil {
		return nil, canonical.InternalError("responses web-search call is invalid")
	}
	items := []canonical.CanonicalItem{callItem}
	var wireSources []responsesWebSearchSourceDTO
	sourcesDisclosed := false
	// A completed provider call with undisclosed sources is still a completed
	// successful lifecycle. Canonical success explicitly permits zero sources.
	if rawSources := bytes.TrimSpace(action.Sources); len(rawSources) > 0 && !bytes.Equal(rawSources, []byte("null")) {
		sourcesDisclosed = true
		if err := json.Unmarshal(rawSources, &wireSources); err != nil {
			return nil, canonical.InternalError("responses web-search sources are invalid")
		}
	}
	if state == responsesWebSearchPending && !sourcesDisclosed {
		return items, nil
	}
	sources := make([]canonical.WebSource, 0, len(wireSources))
	for _, wireSource := range wireSources {
		if kind := strings.TrimSpace(wireSource.Type); kind != "" && kind != "url" { // swobu:io-string source=provider-wire
			return nil, canonical.NotImplemented("Swobu cannot project a Responses web-search source without a URL")
		}
		webURL, err := canonical.NewWebURL(wireSource.URL)
		if err != nil {
			return nil, canonical.InternalError("responses web-search source URL is invalid")
		}
		title := canonical.Unspecified[string]()
		if strings.TrimSpace(wireSource.Title) != "" { // swobu:io-string source=provider-wire
			title = canonical.Specify(wireSource.Title)
		}
		source, err := canonical.NewWebSource(webURL, title)
		if err != nil {
			return nil, canonical.InternalError("responses web-search source is invalid")
		}
		sources = append(sources, source)
	}
	result, _ := canonical.NewWebSearchResult(sources)
	resultItem, err := canonical.NewWebSearchResultItem(callID, result)
	if err != nil {
		return nil, canonical.InternalError("responses web-search result is invalid")
	}
	return append(items, resultItem), nil
}

func decodeResponsesAnnotations(text string, raw json.RawMessage) ([]canonical.WebCitation, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var annotations []responsesAnnotationDTO
	if err := json.Unmarshal(raw, &annotations); err != nil {
		return nil, canonical.InternalError("responses output annotations are invalid")
	}
	citations := make([]canonical.WebCitation, 0, len(annotations))
	for _, annotation := range annotations {
		if strings.TrimSpace(annotation.Type) != "url_citation" { // swobu:io-string source=provider-wire
			return nil, canonical.NotImplemented("Swobu has no canonical projection for this Responses output annotation type")
		}
		webURL, err := canonical.NewWebURL(annotation.URL)
		if err != nil {
			return nil, canonical.InternalError("responses URL citation is invalid")
		}
		title := canonical.Unspecified[string]()
		if strings.TrimSpace(annotation.Title) != "" { // swobu:io-string source=provider-wire
			title = canonical.Specify(annotation.Title)
		}
		source, _ := canonical.NewWebSource(webURL, title)
		citation := canonical.WebCitation{Source: source}
		if (annotation.StartIndex == nil) != (annotation.EndIndex == nil) {
			return nil, canonical.InternalError("responses URL citation offsets are incomplete")
		}
		if annotation.StartIndex != nil {
			start, ok := responsesCharacterIndexToByteOffset(text, *annotation.StartIndex)
			if !ok || *annotation.EndIndex < *annotation.StartIndex {
				return nil, canonical.InternalError("responses URL citation offsets are invalid")
			}
			end, ok := responsesCharacterIndexToByteOffset(text, *annotation.EndIndex+1)
			if !ok {
				return nil, canonical.InternalError("responses URL citation offsets are invalid")
			}
			citation.Start = canonical.Specify(uint32(start))
			citation.End = canonical.Specify(uint32(end))
		}
		citations = append(citations, citation)
	}
	return citations, nil
}

func responsesCharacterIndexToByteOffset(text string, index int) (int, bool) {
	if index < 0 {
		return 0, false
	}
	if index == 0 {
		return 0, true
	}
	characters := 0
	for offset := range text {
		if characters == index {
			return offset, true
		}
		characters++
	}
	if characters == index || utf8.RuneCountInString(text) == index {
		return len(text), true
	}
	return 0, false
}

func encodeResponsesWebSearchAction(call canonical.WebSearchCall) (json.RawMessage, error) {
	if err := call.Validate(); err != nil {
		return nil, err
	}
	action := responsesWebSearchActionDTO{Type: string(call.Action)}
	if len(call.Queries) == 1 {
		action.Query = call.Queries[0]
	} else if len(call.Queries) > 1 {
		action.Queries = append([]string(nil), call.Queries...)
	}
	if webURL, ok := call.URL.Get(); ok {
		action.URL = webURL.String()
	}
	if match, ok := call.Match.Get(); ok {
		action.Pattern = match
	}
	return json.Marshal(action)
}

func encodeResponsesWebSearchSources(result canonical.WebSearchResult, rawAction json.RawMessage) (json.RawMessage, error) {
	var action responsesWebSearchActionDTO
	if err := json.Unmarshal(rawAction, &action); err != nil {
		return nil, err
	}
	sources := make([]responsesWebSearchSourceDTO, 0, len(result.Sources()))
	for _, source := range result.Sources() {
		title, _ := source.Title.Get()
		sources = append(sources, responsesWebSearchSourceDTO{Type: "url", URL: source.URL.String(), Title: title})
	}
	rawSources, err := json.Marshal(sources)
	if err != nil {
		return nil, err
	}
	action.Sources = rawSources
	return json.Marshal(action)
}

func encodeResponsesAnnotations(text string, citations []canonical.WebCitation) ([]responsesAnnotationDTO, error) {
	if len(citations) == 0 {
		return nil, nil
	}
	out := make([]responsesAnnotationDTO, 0, len(citations))
	for _, citation := range citations {
		annotation := responsesAnnotationDTO{Type: "url_citation", URL: citation.Source.URL.String()}
		annotation.Title, _ = citation.Source.Title.Get()
		start, hasStart := citation.Start.Get()
		end, hasEnd := citation.End.Get()
		if hasStart != hasEnd {
			return nil, canonical.InternalError("canonical URL citation offsets are incomplete")
		}
		if hasStart {
			startIndex, ok := responsesByteOffsetToCharacterIndex(text, int(start))
			if !ok {
				return nil, canonical.InternalError("canonical URL citation start is invalid")
			}
			endExclusive, ok := responsesByteOffsetToCharacterIndex(text, int(end))
			if !ok || endExclusive <= startIndex {
				return nil, canonical.InternalError("canonical URL citation end is invalid")
			}
			endIndex := endExclusive - 1
			annotation.StartIndex = &startIndex
			annotation.EndIndex = &endIndex
		}
		out = append(out, annotation)
	}
	return out, nil
}

func responsesByteOffsetToCharacterIndex(text string, offset int) (int, bool) {
	if offset < 0 || offset > len(text) || offset < len(text) && !utf8.RuneStart(text[offset]) {
		return 0, false
	}
	return utf8.RuneCountInString(text[:offset]), true
}
