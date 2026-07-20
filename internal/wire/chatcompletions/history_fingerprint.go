package chatcompletions

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/wire"
)

const fingerprintScheme historyfingerprint.Scheme = "chat-completions"

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
	var previous *historyfingerprint.History
	requestStart := 0
	for index, message := range messages {
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
	return chatCompletionsHistoryResult{
		previous: previous,
		request:  current,
		current:  append([]chatCompletionsMessageDTO(nil), messages[requestStart:]...),
	}, nil
}

func fingerprintChatCompletionsRequest(messages []chatCompletionsMessageDTO) (historyfingerprint.Request, error) {
	history := make([]chatCompletionsHistoryMessageDTO, len(messages))
	for index, message := range messages {
		history[index] = chatHistoryMessageFromRequest(message)
	}
	raw, err := json.Marshal(history)
	if err != nil {
		return historyfingerprint.Request{}, err
	}
	return historyfingerprint.FingerprintRequest(fingerprintScheme, raw)
}

func fingerprintChatCompletionsResponseMessages(messages []chatCompletionsMessageDTO) (historyfingerprint.Response, error) {
	history := make([]chatCompletionsHistoryMessageDTO, len(messages))
	for index, message := range messages {
		history[index] = chatHistoryMessageFromRequest(message)
	}
	raw, err := json.Marshal(history)
	if err != nil {
		return historyfingerprint.Response{}, err
	}
	return historyfingerprint.FingerprintResponse(fingerprintScheme, raw)
}

func fingerprintChatCompletionsResponse(output canonical.CanonicalResponse) (historyfingerprint.Response, error) {
	state := chatCompletionsResponseHistoryState{}
	for _, item := range output.Items() {
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
			return canonical.UnsupportedOperation("chat completions response messages must be assistant-authored")
		}
		content, err := textOnlyContent(message.Content(), "chat completions responses")
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
	default:
		return canonical.UnsupportedOperation("chat completions protocol only supports text and tool use output items")
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
	return historyfingerprint.FingerprintResponse(fingerprintScheme, raw)
}

func chatHistoryMessageFromRequest(message chatCompletionsMessageDTO) chatCompletionsHistoryMessageDTO {
	return chatCompletionsHistoryMessageDTO{
		Role:       message.Role,
		Content:    append(json.RawMessage(nil), message.Content...),
		ToolCalls:  append([]chatCompletionsToolCallDTO(nil), message.ToolCalls...),
		ToolCallID: message.ToolCallID,
	}
}

// chatCompletionsFingerprintingEncoder marks completion in the same Encode
// call that returns finish_reason and [DONE]. The exchange delivery wrapper
// therefore gates those exact bytes on checkpoint commit.
func chatCompletionsFingerprintingEncoder(
	encode wire.ResponseEventEncoder,
	complete func(*historyfingerprint.Response),
	fail func(error),
) wire.ResponseEventEncoder {
	state := &chatCompletionsResponseHistoryState{}
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
			if err := state.appendItem(completed.Item); err != nil {
				fail(err)
				return nil, err
			}
		}
		status, terminal := chatCompletionsResponseTerminalStatus(event)
		if !terminal {
			return encoded, nil
		}
		if status != canonical.EnvelopeStatusCompleted {
			fail(errors.New("chat completions response did not complete successfully"))
			return encoded, nil
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
