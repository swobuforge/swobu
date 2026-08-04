package messages

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/wire"
)

type messagesHistoryPartDTO struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	Thinking  *string         `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Data      string          `json:"data,omitempty"`
	Source    json.RawMessage `json:"source,omitempty"`
}

const fingerprintScheme historyfingerprint.Scheme = "messages/v1"

type messagesHistoryResult struct {
	previous *historyfingerprint.History
	request  historyfingerprint.Request
	current  []messagesMessageDTO
}

func fingerprintMessagesHistory(messages []messagesMessageDTO) (messagesHistoryResult, error) {
	var previous *historyfingerprint.History
	requestStart := 0
	for index, message := range messages {
		// Terminal assistant content is current prefill until later user input
		// proves that the assistant value was a completed response.
		if index == len(messages)-1 || strings.TrimSpace(message.Role) != "assistant" { // swobu:io-string source=boundary
			continue
		}
		request, err := fingerprintMessagesRequest(messages[requestStart:index])
		if err != nil {
			return messagesHistoryResult{}, err
		}
		response, err := fingerprintMessagesResponseValue(message)
		if err != nil {
			return messagesHistoryResult{}, err
		}
		history, err := historyfingerprint.Advance(previous, request, response)
		if err != nil {
			return messagesHistoryResult{}, err
		}
		previous = &history
		requestStart = index + 1
	}
	current, err := fingerprintMessagesRequest(messages[requestStart:])
	if err != nil {
		return messagesHistoryResult{}, err
	}
	return messagesHistoryResult{
		previous: previous,
		request:  current,
		current:  append([]messagesMessageDTO(nil), messages[requestStart:]...),
	}, nil
}

func fingerprintMessagesRequest(messages []messagesMessageDTO) (historyfingerprint.Request, error) {
	normalized, err := normalizeMessagesHistory(messages)
	if err != nil {
		return historyfingerprint.Request{}, err
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return historyfingerprint.Request{}, err
	}
	material, err := historyfingerprint.FrameJSONValue(raw)
	if err != nil {
		return historyfingerprint.Request{}, err
	}
	return historyfingerprint.FingerprintRequest(fingerprintScheme, material)
}

func fingerprintMessagesResponseValue(message messagesMessageDTO) (historyfingerprint.Response, error) {
	normalized, err := normalizeMessagesHistory([]messagesMessageDTO{message})
	if err != nil {
		return historyfingerprint.Response{}, err
	}
	raw, err := json.Marshal(normalized[0])
	if err != nil {
		return historyfingerprint.Response{}, err
	}
	material, err := historyfingerprint.FrameJSONValue(raw)
	if err != nil {
		return historyfingerprint.Response{}, err
	}
	return historyfingerprint.FingerprintResponse(fingerprintScheme, material)
}

func normalizeMessagesHistory(messages []messagesMessageDTO) ([]messagesMessageDTO, error) {
	normalized := append([]messagesMessageDTO(nil), messages...)
	for index := range normalized {
		trimmed := bytes.TrimSpace(normalized[index].Content)
		if len(trimmed) == 0 || trimmed[0] != '[' {
			continue
		}
		var parts []messagesHistoryPartDTO
		if err := json.Unmarshal(trimmed, &parts); err != nil {
			return nil, err
		}
		for partIndex := range parts {
			var err error
			parts[partIndex].Input, err = normalizeMessagesRawJSON(parts[partIndex].Input)
			if err != nil {
				return nil, err
			}
			parts[partIndex].Content, err = normalizeMessagesRawJSON(parts[partIndex].Content)
			if err != nil {
				return nil, err
			}
			parts[partIndex].Source, err = normalizeMessagesRawJSON(parts[partIndex].Source)
			if err != nil {
				return nil, err
			}
		}
		content, err := json.Marshal(parts)
		if err != nil {
			return nil, err
		}
		normalized[index].Content = content
	}
	return normalized, nil
}

func normalizeMessagesRawJSON(source json.RawMessage) (json.RawMessage, error) {
	if len(source) == 0 {
		return nil, nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

// messagesResponseHistoryState owns the private assistant content value a
// client appends to a later Messages request.
type messagesResponseHistoryState struct {
	content []messagesResponsePartDTO
	request canonical.CanonicalRequest
}

func (s *messagesResponseHistoryState) appendItem(item canonical.CanonicalItem) error {
	switch item.Kind() {
	case canonical.ItemKindMessage:
		message, _ := item.Message()
		if message.Role() != canonical.MessageRoleAssistant {
			return canonical.InternalError("canonical response contains a non-assistant Messages output item")
		}
		for _, part := range message.Content() {
			text, ok := part.Text()
			if !ok {
				return canonical.NewBackendError(
					"messages",
					0,
					"Messages client output cannot represent the backend image response",
					"",
				)
			}
			citations, err := encodeMessagesCitations(text.Text(), part.Citations())
			if err != nil {
				return err
			}
			s.content = append(s.content, messagesResponsePartDTO{Type: "text", Text: text.Text(), Citations: citations})
		}
		return nil
	case canonical.ItemKindToolCall:
		call, _ := item.ToolCall()
		tool := call.Tool()
		if tool.Kind() == canonical.ToolKindWebSearch {
			search, ok := call.Input().WebSearch()
			if !ok || search.Action != canonical.WebSearchActionSearch || len(search.Queries) != 1 {
				return canonical.InternalError("Messages response history admitted an unsupported web-search call")
			}
			input, err := json.Marshal(map[string]string{"query": search.Queries[0]})
			if err != nil {
				return canonical.InternalError("messages web-search call could not be encoded")
			}
			s.content = append(s.content, messagesResponsePartDTO{Type: "server_tool_use", ID: call.CallID().String(), Name: "web_search", Input: input})
			return nil
		}
		if tool.Kind() != canonical.ToolKindFunction {
			return canonical.InternalError("Messages response history received an unsupported canonical tool-call kind")
		}
		object, ok := call.Input().Object()
		if !ok {
			return canonical.BadRequest("messages response function call requires object input")
		}
		s.content = append(s.content, messagesResponsePartDTO{
			Type: "tool_use", ID: call.CallID().String(), Name: tool.Name(), Input: json.RawMessage(object.Bytes()),
		})
		return nil
	case canonical.ItemKindToolResult:
		result, _ := item.ToolResult()
		search, ok := result.WebSearch()
		if !ok {
			return canonical.InternalError("canonical provider response contains a request-only function tool result")
		}
		content, err := encodeMessagesWebSearchResult(search)
		if err != nil {
			return err
		}
		s.content = append(s.content, messagesResponsePartDTO{Type: "web_search_tool_result", ToolUseID: result.CallID().String(), Content: content})
		return nil
	case canonical.ItemKindReasoning:
		reasoning, _ := item.Reasoning()
		opaque, ok := reasoning.Opaque().Messages()
		if !ok {
			// A foreign opaque-thinking branch is not a Messages thinking block.
			// Keep it in session truth and omit the inexpressible
			// client view; never reinterpret it from a format label.
			return nil
		}
		var native messagesResponsePartDTO
		if err := json.Unmarshal(opaque, &native); err != nil {
			return canonical.InternalError("messages opaque thinking is invalid")
		}
		if native.Type == "redacted_thinking" {
			s.content = append(s.content, native)
			return nil
		}
		disclosure, specified := s.request.Reasoning().DisclosureField().Get()
		if native.Type != "thinking" {
			return canonical.InternalError("messages opaque thinking is invalid")
		}
		if specified && disclosure == canonical.ReasoningDisclosureNone {
			empty := ""
			native.Thinking = &empty
			native.Text = ""
			s.content = append(s.content, native)
			return nil
		}
		if specified && disclosure == canonical.ReasoningDisclosureSummary {
			empty := ""
			native.Thinking = &empty
			for _, part := range reasoning.Parts() {
				if part.Kind() == canonical.ReasoningPartSummary {
					text := part.Text()
					native.Thinking = &text
					break
				}
			}
		}
		s.content = append(s.content, native)
		return nil
	default:
		return canonical.InternalError("Messages response history received a request-only canonical item kind")
	}
}

func (s *messagesResponseHistoryState) fingerprint() (historyfingerprint.Response, error) {
	content, err := json.Marshal(s.content)
	if err != nil {
		return historyfingerprint.Response{}, err
	}
	return fingerprintMessagesResponseValue(messagesMessageDTO{Role: "assistant", Content: content})
}

// messagesFingerprintingEncoder marks completion in the same Encode call that
// returns message_delta and message_stop. The exchange delivery wrapper gates
// those exact bytes on checkpoint commit.
func messagesFingerprintingEncoder(request canonical.CanonicalRequest, encode wire.ResponseEventEncoder, complete func(*historyfingerprint.Response), fail func(error)) wire.ResponseEventEncoder {
	state := &messagesResponseHistoryState{request: request.Clone()}
	var completedItems []canonical.CanonicalItem
	return func(event canonical.Event) ([][]byte, error) {
		encoded, err := encode(event)
		if err != nil {
			fail(err)
			return nil, err
		}
		if event.Kind == canonical.EventItemCompleted {
			itemEvent, ok := event.Payload.(canonical.ItemEvent)
			completed, valid := itemEvent.Payload.(canonical.ItemCompletedPayload)
			if !ok || !valid {
				err := errors.New("messages item completion payload is invalid")
				fail(err)
				return nil, err
			}
			completedItems = append(completedItems, completed.Item.Clone())
		}
		status, terminal := messagesResponseTerminalStatus(event)
		if !terminal {
			return encoded, nil
		}
		if status != canonical.EnvelopeStatusCompleted {
			fail(errors.New("messages response did not complete successfully"))
			return encoded, nil
		}
		projected, _, err := projectMessagesWebSearchLifecycles(completedItems, canonical.ResponseItemsKind)
		if err != nil {
			fail(err)
			return nil, err
		}
		for _, item := range projected {
			if err := state.appendItem(item); err != nil {
				fail(err)
				return nil, err
			}
		}
		fingerprint, err := state.fingerprint()
		if err != nil {
			fail(err)
			return encoded, nil
		}
		complete(&fingerprint)
		return encoded, nil
	}
}

func messagesResponseTerminalStatus(event canonical.Event) (canonical.EnvelopeStatus, bool) {
	if event.Kind != canonical.EventEnvelopeEnd {
		return "", false
	}
	payload, ok := event.Payload.(canonical.EnvelopeEndPayload)
	if !ok || payload.Kind != canonical.EnvResponse {
		return "", false
	}
	return payload.Status, true
}
