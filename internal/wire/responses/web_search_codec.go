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

func decodeResponsesWebSearchInclude(raw json.RawMessage, changeLog *[]compat.Change, exchangeID string) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var values []string
	if err := json.Unmarshal(trimmed, &values); err != nil {
		return canonical.BadRequest("responses include is invalid")
	}
	for _, value := range values {
		if value == "web_search_call.action.sources" {
			continue
		}
	}
	return nil
}

func (s *responsesResponseStream) completeMessageItem(frame streamFrame) (bool, error) {
	index := *frame.OutputIndex
	item, present, err := decodeResponsesMessageOutputItem(responsesWireOutputItemDTO{Type: "message", ID: frame.Item.ID, Status: frame.Item.Status, Role: "assistant", Content: frame.Item.Content}, s.changeLog, s.exchangeID, canonical.ResponseItemOccurrence(uint32(index)))
	if err != nil {
		return false, err
	}
	output := s.outputAt(index)
	state := output.text
	if !present {
		if state != nil {
			return false, canonical.NewBackendError("responses", 0, "responses terminal message is missing streamed text content", "")
		}
		var wireParts []json.RawMessage
		if json.Unmarshal(frame.Item.Content, &wireParts) == nil && len(wireParts) > 0 {
			s.erasedOutput = true
			s.omitProviderOutput(frame.OutputIndex)
		}
		return true, nil
	}
	ordinal := uint32(0)
	if state != nil {
		if err := s.reconcileMessageParts(frame, state, item); err != nil {
			return false, err
		}
		ordinal = state.ordinal
		output.text = nil
		s.enqueueItemCompleted(frame.OutputIndex, ordinal, item)
		return true, nil
	}
	if err := s.enqueueCompletedOutputItemAt(frame.OutputIndex, ordinal, item); err != nil {
		return false, err
	}
	return true, nil
}

func (s *responsesResponseStream) reconcileMessageParts(frame streamFrame, state *responsesTextState, item canonical.CanonicalItem) error {
	var wireParts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(frame.Item.Content, &wireParts); err != nil {
		return canonical.InternalError("responses terminal message content is invalid")
	}
	message, _ := item.Message()
	content := message.Content()
	terminalOrdinal := uint32(0)
	for wireIndex, wirePart := range wireParts {
		part := state.parts[wireIndex]
		if part == nil {
			part = &responsesTextPartState{}
			state.parts[wireIndex] = part
		}
		switch strings.TrimSpace(wirePart.Type) {
		case "text", "output_text", "input_text":
			if int(terminalOrdinal) >= len(content) {
				return canonical.InternalError("responses terminal message content mapping is invalid")
			}
			text, ok := content[terminalOrdinal].Text()
			if !ok {
				return canonical.NewBackendError("responses", 0, "responses terminal message changed streamed content kind", "")
			}
			if part.classified && part.erased {
				return canonical.NewBackendError("responses", 0, "responses terminal message changed streamed content kind", "")
			}
			suffix, err := responsesTerminalSuffix(part.text.String(), text.Text())
			if err != nil {
				return err
			}
			if suffix != "" {
				part.text.WriteString(suffix)
				if part.emitted {
					s.enqueueTextDelta(frame.OutputIndex, state.ordinal, part.ordinal, suffix)
				} else {
					part.deltas = append(part.deltas, suffix)
				}
			}
			part.classified = true
			terminalOrdinal++
		default:
			if part.classified && !part.erased {
				return canonical.NewBackendError("responses", 0, "responses terminal message changed streamed content kind", "")
			}
			part.classified = true
			part.erased = true
		}
	}
	if int(terminalOrdinal) != len(content) {
		return canonical.InternalError("responses terminal message content mapping is incomplete")
	}
	for wireIndex, part := range state.parts {
		if wireIndex >= len(wireParts) || !part.classified {
			return canonical.NewBackendError("responses", 0, "responses terminal message is missing streamed content", "")
		}
	}
	s.flushMessagePartFrontier(*frame.OutputIndex, state)
	if state.partFrontier != len(wireParts) {
		return canonical.NewBackendError("responses", 0, "responses terminal message has unresolved content", "")
	}
	return nil
}

// flushMessagePartFrontier assigns compact canonical part ordinals only after
// every earlier wire content index is classified as known or erased.
func (s *responsesResponseStream) flushMessagePartFrontier(outputIndex int, state *responsesTextState) {
	for {
		part := state.parts[state.partFrontier]
		if part == nil || !part.classified {
			return
		}
		if !part.erased {
			part.ordinal = state.nextPartOrdinal
			state.nextPartOrdinal++
			part.emitted = true
			s.enqueueContentStart(&outputIndex, state.ordinal, part.ordinal)
			for _, delta := range part.deltas {
				s.enqueueTextDelta(&outputIndex, state.ordinal, part.ordinal, delta)
			}
			part.deltas = nil
		}
		state.partFrontier++
	}
}

func (s *responsesResponseStream) completeWebSearchItem(frame streamFrame, state responsesWebSearchLifecycleState) (bool, error) {
	itemID := strings.TrimSpace(frame.Item.ID) // swobu:io-string source=provider-wire
	if itemID == "" {
		itemID = strings.TrimSpace(frame.ItemID)
	} // swobu:io-string source=provider-wire
	index := 0
	if frame.OutputIndex != nil {
		index = *frame.OutputIndex
	}
	refinement := responsesWebSearchRefinementFromID(itemID)
	lifecycle, err := decodeResponsesWebSearchLifecycleWithChanges(itemID, frame.Item.Action, state, s.changeLog, s.exchangeID, canonical.ResponseItemOccurrence(uint32(index)), true, refinement)
	if err != nil {
		return false, err
	}
	base := uint32(0)
	for index, item := range lifecycle {
		if err := s.enqueueCompletedOutputItemAt(frame.OutputIndex, base+uint32(index), item); err != nil {
			return false, err
		}
	}
	return true, nil
}

type responsesWebSearchLifecycleState uint8

const (
	responsesWebSearchPending responsesWebSearchLifecycleState = iota + 1
	responsesWebSearchSucceeded
	responsesWebSearchFailed
	// responsesWebSearchUnknown is projection state, not a canonical lifecycle
	// value. It preserves the known call while omitting any result whose meaning
	// depends on an unfamiliar wire status; response settlement remains strict.
	responsesWebSearchUnknown
)

// decodeResponsesWebSearchLifecycleState collapses wire-only pending aliases.
func decodeResponsesWebSearchLifecycleState(raw string) (responsesWebSearchLifecycleState, error) {
	switch strings.TrimSpace(raw) { // swobu:io-string source=boundary
	case "", "in_progress", "searching", "incomplete":
		return responsesWebSearchPending, nil
	case "completed":
		return responsesWebSearchSucceeded, nil
	case "failed":
		return responsesWebSearchFailed, nil
	default:
		return 0, fmt.Errorf("web-search status is unsupported")
	}
}

func decodeResponsesWebSearchLifecycle(id string, rawAction json.RawMessage, state responsesWebSearchLifecycleState) ([]canonical.CanonicalItem, error) {
	return decodeResponsesWebSearchLifecycleWithChanges(id, rawAction, state, nil, "", canonical.Occurrence{}, false, nil)
}

// responsesWebSearchRefinementFromID preserves one provider item id as the exact
// Responses refinement. A blank id yields nil: provider output that omits a
// web_search_call id is rejected by the lifecycle decode (missing id), and the
// refinement must never be minted from a synthetic correlation token.
func responsesWebSearchRefinementFromID(id string) *canonical.ResponsesWebSearchRefinement {
	trimmed := strings.TrimSpace(id) // swobu:io-string source=provider-wire
	if trimmed == "" {
		return nil
	}
	refinement, err := canonical.NewResponsesWebSearchRefinement(canonical.ResponsesItemID(trimmed))
	if err != nil {
		return nil
	}
	return &refinement
}

func responsesWebSearchMalformed(providerOutput bool, message string) error {
	if providerOutput {
		return canonical.NewBackendError("responses", 0, message, "")
	}
	return canonical.BadRequest(message)
}

func decodeResponsesWebSearchLifecycleWithChanges(id string, rawAction json.RawMessage, state responsesWebSearchLifecycleState, changeLog *[]compat.Change, exchangeID string, occurrence canonical.Occurrence, providerOutput bool, refinement *canonical.ResponsesWebSearchRefinement) ([]canonical.CanonicalItem, error) {
	callID, err := canonical.NewToolCallID(strings.TrimSpace(id)) // swobu:io-string source=provider-wire
	if err != nil {
		return nil, responsesWebSearchMalformed(providerOutput, "responses web-search call is missing id")
	}
	var action responsesWebSearchActionDTO
	if err := json.Unmarshal(rawAction, &action); err != nil {
		return nil, responsesWebSearchMalformed(providerOutput, "responses web-search action is invalid")
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
			return nil, responsesWebSearchMalformed(providerOutput, "responses web-search action URL is invalid")
		}
		call.URL = canonical.Specify(webURL)
	}
	if strings.TrimSpace(action.Pattern) != "" { // swobu:io-string source=provider-wire
		call.Match = canonical.Specify(action.Pattern)
	}
	if err := call.Validate(); err != nil {
		return nil, responsesWebSearchMalformed(providerOutput, "responses web-search action is invalid")
	}
	input, err := canonical.NewWebSearchToolInput(call)
	if err != nil {
		return nil, canonical.InternalError("responses web-search action is invalid")
	}
	callItem, err := canonical.NewToolCallItemWithResponsesWebSearch(callID, canonical.WebSearchToolKey(), input, refinement)
	if err != nil {
		return nil, canonical.InternalError("responses web-search call is invalid")
	}
	items := []canonical.CanonicalItem{callItem}
	if state == responsesWebSearchUnknown {
		return items, nil
	}
	if state == responsesWebSearchFailed {
		result, err := canonical.NewWebSearchFailureResult("provider reported failed web search")
		if err != nil {
			return nil, canonical.InternalError("responses web-search failure is invalid")
		}
		resultItem, err := canonical.NewWebSearchResultItem(callID, result)
		if err != nil {
			return nil, canonical.InternalError("responses web-search failure result is invalid")
		}
		return append(items, resultItem), nil
	}
	var wireSources []responsesWebSearchSourceDTO
	sourcesDisclosed := false
	// A completed provider call with undisclosed sources is still a completed
	// successful lifecycle. Canonical success explicitly permits zero sources.
	if rawSources := bytes.TrimSpace(action.Sources); len(rawSources) > 0 && !bytes.Equal(rawSources, []byte("null")) {
		sourcesDisclosed = true
		if err := json.Unmarshal(rawSources, &wireSources); err != nil {
			return nil, responsesWebSearchMalformed(providerOutput, "responses web-search sources are invalid")
		}
	}
	if state == responsesWebSearchPending && !sourcesDisclosed {
		return items, nil
	}
	sources := make([]canonical.WebSource, 0, len(wireSources))
	for _, wireSource := range wireSources {
		kind := strings.TrimSpace(wireSource.Type) // swobu:io-string source=provider-wire
		if providerOutput {
			if err := admitResponsesProviderOutputChild(kind); err != nil {
				return nil, err
			}
		}
		if kind != "" && kind != "url" {
			if err := appendResponsesOccurrenceChange(changeLog, exchangeID, canonical.ResponseItemsKind, compat.Omission, occurrence); err != nil {
				return nil, err
			}
			continue
		}
		webURL, err := canonical.NewWebURL(wireSource.URL)
		if err != nil {
			return nil, responsesWebSearchMalformed(providerOutput, "responses web-search source URL is invalid")
		}
		title := canonical.Unspecified[string]()
		if strings.TrimSpace(wireSource.Title) != "" { // swobu:io-string source=provider-wire
			title = canonical.Specify(wireSource.Title)
		}
		source, err := canonical.NewWebSource(webURL, title)
		if err != nil {
			return nil, responsesWebSearchMalformed(providerOutput, "responses web-search source is invalid")
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

func decodeResponsesAnnotations(text string, raw json.RawMessage, changeLog *[]compat.Change, exchangeID string, occurrence canonical.Occurrence) ([]canonical.WebCitation, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var annotations []responsesAnnotationDTO
	if err := json.Unmarshal(raw, &annotations); err != nil {
		return nil, canonical.NewBackendError("responses", 0, "responses output annotations are invalid", "")
	}
	citations := make([]canonical.WebCitation, 0, len(annotations))
	for _, annotation := range annotations {
		annotationType := strings.TrimSpace(annotation.Type) // swobu:io-string source=provider-wire
		if err := admitResponsesProviderOutputChild(annotationType); err != nil {
			return nil, err
		}
		if annotationType != "url_citation" {
			if err := appendResponsesOccurrenceChange(changeLog, exchangeID, canonical.ResponseItemsKind, compat.Omission, occurrence); err != nil {
				return nil, err
			}
			continue
		}
		webURL, err := canonical.NewWebURL(annotation.URL)
		if err != nil {
			return nil, canonical.NewBackendError("responses", 0, "responses URL citation is invalid", "")
		}
		title := canonical.Unspecified[string]()
		if strings.TrimSpace(annotation.Title) != "" { // swobu:io-string source=provider-wire
			title = canonical.Specify(annotation.Title)
		}
		source, _ := canonical.NewWebSource(webURL, title)
		citation := canonical.WebCitation{Source: source}
		if (annotation.StartIndex == nil) != (annotation.EndIndex == nil) {
			return nil, canonical.NewBackendError("responses", 0, "responses URL citation offsets are incomplete", "")
		}
		if annotation.StartIndex != nil {
			start, ok := responsesCharacterIndexToByteOffset(text, *annotation.StartIndex)
			if !ok || *annotation.EndIndex < *annotation.StartIndex {
				return nil, canonical.NewBackendError("responses", 0, "responses URL citation offsets are invalid", "")
			}
			end, ok := responsesCharacterIndexToByteOffset(text, *annotation.EndIndex+1)
			if !ok {
				return nil, canonical.NewBackendError("responses", 0, "responses URL citation offsets are invalid", "")
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
