package chatcompletions

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/wire"
)

const fingerprintScheme historyfingerprint.Scheme = "chat-completions/v1"

// chatCompletionsHistoryMessageDTO is the protocol-native assistant value a
// client appends to a later messages array. It is private because only this
// codec owns Chat Completions history grammar.
type chatCompletionsHistoryMessageDTO struct {
	Role       string                       `json:"role"`
	Content    json.RawMessage              `json:"content,omitempty"`
	ToolCalls  []chatCompletionsToolCallDTO `json:"tool_calls,omitempty"`
	ToolCallID string                       `json:"tool_call_id,omitempty"`
}

type chatCompletionsHistoryResult struct {
	previous *historyfingerprint.History
	request  historyfingerprint.Request
	current  []chatCompletionsMessageDTO
}

func fingerprintChatCompletionsHistory(messages []chatCompletionsMessageDTO) (chatCompletionsHistoryResult, error) {
	contextEnd := chatCompletionsLeadingContextEnd(messages)
	var previous *historyfingerprint.History
	requestStart := contextEnd
	for index := contextEnd; index < len(messages); index++ {
		message := messages[index]
		// A terminal assistant value may be supported prefill. It becomes a
		// completed response only after later protocol input proves the boundary.
		if index == len(messages)-1 || strings.TrimSpace(message.Role) != "assistant" { // swobu:io-string source=boundary
			continue
		}
		request, err := fingerprintChatCompletionsRequest(messages[requestStart:index])
		if err != nil {
			return chatCompletionsHistoryResult{}, err
		}
		response, err := fingerprintChatCompletionsResponseMessages([]chatCompletionsMessageDTO{message})
		if err != nil {
			return chatCompletionsHistoryResult{}, err
		}
		history, err := historyfingerprint.Advance(previous, request, response)
		if err != nil {
			return chatCompletionsHistoryResult{}, err
		}
		previous = &history
		requestStart = index + 1
	}
	current, err := fingerprintChatCompletionsRequest(messages[requestStart:])
	if err != nil {
		return chatCompletionsHistoryResult{}, err
	}
	currentMessages := make([]chatCompletionsMessageDTO, 0, contextEnd+len(messages)-requestStart)
	currentMessages = append(currentMessages, messages[:contextEnd]...)
	currentMessages = append(currentMessages, messages[requestStart:]...)
	return chatCompletionsHistoryResult{
		previous: previous,
		request:  current,
		current:  currentMessages,
	}, nil
}

func fingerprintChatCompletionsRequest(messages []chatCompletionsMessageDTO) (historyfingerprint.Request, error) {
	history := make([]chatCompletionsHistoryMessageDTO, len(messages))
	for index, message := range messages {
		var err error
		history[index], err = chatHistoryMessageFromRequest(message)
		if err != nil {
			return historyfingerprint.Request{}, err
		}
	}
	raw, err := json.Marshal(history)
	if err != nil {
		return historyfingerprint.Request{}, err
	}
	material, err := historyfingerprint.FrameJSONValue(raw)
	if err != nil {
		return historyfingerprint.Request{}, err
	}
	return historyfingerprint.FingerprintRequest(fingerprintScheme, material)
}

func chatCompletionsLeadingContextEnd(messages []chatCompletionsMessageDTO) int {
	for index, message := range messages {
		switch strings.TrimSpace(message.Role) { // swobu:io-string source=boundary
		case "system", "developer":
			continue
		default:
			return index
		}
	}
	return len(messages)
}

func fingerprintChatCompletionsResponseMessages(messages []chatCompletionsMessageDTO) (historyfingerprint.Response, error) {
	history := make([]chatCompletionsHistoryMessageDTO, len(messages))
	for index, message := range messages {
		var err error
		history[index], err = chatHistoryMessageFromRequest(message)
		if err != nil {
			return historyfingerprint.Response{}, err
		}
	}
	raw, err := json.Marshal(history)
	if err != nil {
		return historyfingerprint.Response{}, err
	}
	material, err := historyfingerprint.FrameJSONValue(raw)
	if err != nil {
		return historyfingerprint.Response{}, err
	}
	return historyfingerprint.FingerprintResponse(fingerprintScheme, material)
}

func fingerprintChatCompletionsResponse(output canonical.CanonicalResponse) (historyfingerprint.Response, error) {
	return fingerprintChatCompletionsResponseItems(output.Items())
}

func fingerprintChatCompletionsResponseItems(items []canonical.CanonicalItem) (historyfingerprint.Response, error) {
	state := chatCompletionsResponseHistoryState{}
	for _, item := range items {
		if err := state.appendItem(item); err != nil {
			return historyfingerprint.Response{}, err
		}
	}
	return state.fingerprint()
}

// chatCompletionsResponseHistoryState incrementally owns the private assistant
// value that a client later appends to messages. It retains no canonical event
// envelope and performs no terminal canonical reprojection.
type chatCompletionsResponseHistoryState struct {
	text      strings.Builder
	toolCalls []chatCompletionsToolCallDTO
}

func (s *chatCompletionsResponseHistoryState) appendItem(item canonical.CanonicalItem) error {
	switch item.Kind() {
	case canonical.ItemKindMessage:
		message, _ := item.Message()
		if message.Role() != canonical.MessageRoleAssistant {
			return canonical.InternalError("canonical response contains a non-assistant Chat Completions output message")
		}
		content, err := chatClientTextContent(message.Content(), "chat completions responses")
		if err != nil {
			return err
		}
		s.text.WriteString(content)
		return nil
	case canonical.ItemKindToolCall:
		call, err := chatToolCallFromOutputItem(item)
		if err != nil {
			return err
		}
		converted := chatCompletionsToolCallDTO{ID: call.ID, Type: call.Type}
		if call.Function != nil {
			arguments, err := json.Marshal(call.Function.Arguments)
			if err != nil {
				return err
			}
			converted.Function = &chatCompletionsToolFunctionDTO{Name: call.Function.Name, Arguments: arguments}
		}
		if call.Custom != nil {
			converted.Custom = &chatCompletionsToolCallCustomDTO{Name: call.Custom.Name, Input: call.Custom.Input}
		}
		s.toolCalls = append(s.toolCalls, converted)
		return nil
	case canonical.ItemKindReasoning:
		// Reasoning is intentionally invisible on the standard Chat client
		// contract and therefore absent from its reconstructed history value.
		return nil
	default:
		return canonical.InternalError("Chat Completions response history received an unsupported canonical item kind")
	}
}

func (s *chatCompletionsResponseHistoryState) fingerprint() (historyfingerprint.Response, error) {
	var err error
	history := chatCompletionsHistoryMessageDTO{Role: "assistant", ToolCalls: append([]chatCompletionsToolCallDTO(nil), s.toolCalls...)}
	if s.text.Len() > 0 {
		history.Content, err = json.Marshal(s.text.String())
		if err != nil {
			return historyfingerprint.Response{}, err
		}
	} else if len(s.toolCalls) > 0 {
		history.Content = json.RawMessage("null")
	}
	raw, err := json.Marshal([]chatCompletionsHistoryMessageDTO{history})
	if err != nil {
		return historyfingerprint.Response{}, err
	}
	material, err := historyfingerprint.FrameJSONValue(raw)
	if err != nil {
		return historyfingerprint.Response{}, err
	}
	return historyfingerprint.FingerprintResponse(fingerprintScheme, material)
}

func chatHistoryMessageFromRequest(message chatCompletionsMessageDTO) (chatCompletionsHistoryMessageDTO, error) {
	content, err := normalizeChatHistoryContent(message.Content)
	if err != nil {
		return chatCompletionsHistoryMessageDTO{}, err
	}
	return chatCompletionsHistoryMessageDTO{
		Role:       message.Role,
		Content:    content,
		ToolCalls:  append([]chatCompletionsToolCallDTO(nil), message.ToolCalls...),
		ToolCallID: message.ToolCallID,
	}, nil
}

// normalizeChatHistoryContent removes only invocation-local controls admitted
// inside content-part objects. Sibling protocol fields and part order remain
// client-history identity; scalar content retains its exact representation.
func normalizeChatHistoryContent(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return append(json.RawMessage(nil), raw...), nil
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(trimmed, &parts); err != nil {
		return nil, err
	}
	for index := range parts {
		part := bytes.TrimSpace(parts[index])
		if len(part) == 0 || part[0] != '{' {
			continue
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(part, &object); err != nil {
			return nil, err
		}
		delete(object, "prompt_cache_breakpoint")
		normalized, err := json.Marshal(object)
		if err != nil {
			return nil, err
		}
		parts[index] = normalized
	}
	return json.Marshal(parts)
}

// chatCompletionsFingerprintingEncoder marks completion in the same Encode
// call that returns finish_reason and [DONE]. The exchange delivery wrapper
// therefore gates those exact bytes on checkpoint commit.
func chatCompletionsFingerprintingEncoder(
	encode wire.ResponseEventEncoder,
	complete func(*historyfingerprint.Response),
	fail func(error),
) wire.ResponseEventEncoder {
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
				err := errors.New("chat completions item completion payload is invalid")
				fail(err)
				return nil, err
			}
			completedItems = append(completedItems, completed.Item.Clone())
		}
		status, terminal := chatCompletionsResponseTerminalStatus(event)
		if !terminal {
			return encoded, nil
		}
		if status != canonical.EnvelopeStatusCompleted {
			fail(errors.New("chat completions response did not complete successfully"))
			return encoded, nil
		}
		projected, _, err := projectChatCompletionsWebSearchLifecycles(completedItems)
		if err != nil {
			fail(err)
			return nil, err
		}
		fingerprint, err := fingerprintChatCompletionsResponseItems(projected)
		if err != nil {
			fail(err)
			return encoded, nil
		}
		complete(&fingerprint)
		return encoded, nil
	}
}

func chatCompletionsResponseTerminalStatus(event canonical.Event) (canonical.EnvelopeStatus, bool) {
	if event.Kind != canonical.EventEnvelopeEnd {
		return "", false
	}
	payload, ok := event.Payload.(canonical.EnvelopeEndPayload)
	if !ok || payload.Kind != canonical.EnvResponse {
		return "", false
	}
	return payload.Status, true
}
