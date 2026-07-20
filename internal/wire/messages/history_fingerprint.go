package messages

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/wire"
)

const fingerprintScheme historyfingerprint.Scheme = "messages"

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
	raw, err := json.Marshal(messages)
	if err != nil {
		return historyfingerprint.Request{}, err
	}
	return historyfingerprint.FingerprintRequest(fingerprintScheme, raw)
}

func fingerprintMessagesResponseValue(message messagesMessageDTO) (historyfingerprint.Response, error) {
	raw, err := json.Marshal(message)
	if err != nil {
		return historyfingerprint.Response{}, err
	}
	return historyfingerprint.FingerprintResponse(fingerprintScheme, raw)
}

func fingerprintMessagesResponse(output canonical.CanonicalResponse) (historyfingerprint.Response, error) {
	state := messagesResponseHistoryState{}
	for _, item := range output.Items() {
		if err := state.appendItem(item); err != nil {
			return historyfingerprint.Response{}, err
		}
	}
	return state.fingerprint()
}

// messagesResponseHistoryState owns the private assistant content value a
// client appends to a later Messages request.
type messagesResponseHistoryState struct {
	content []messagesResponsePartDTO
}

func (s *messagesResponseHistoryState) appendItem(item canonical.CanonicalItem) error {
	switch item.Kind() {
	case canonical.ItemKindMessage:
		message, _ := item.Message()
		if message.Role() != canonical.MessageRoleAssistant {
			return canonical.UnsupportedOperation("messages response items must be assistant-authored")
		}
		for _, part := range message.Content() {
			text, ok := part.Text()
			if !ok {
				return canonical.UnsupportedOperation("messages response image output is not implemented")
			}
			s.content = append(s.content, messagesResponsePartDTO{Type: "text", Text: text.Text()})
		}
		return nil
	case canonical.ItemKindToolCall:
		call, _ := item.ToolCall()
		tool := call.Tool()
		if tool.Kind() != canonical.ToolKindFunction {
			return canonical.UnsupportedOperation("messages response only supports function tool calls")
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
		return canonical.UnsupportedOperation("messages protocol does not support tool result output items")
	default:
		return canonical.UnsupportedOperation("messages protocol does not support this output item kind")
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
func messagesFingerprintingEncoder(encode wire.ResponseEventEncoder, complete func(*historyfingerprint.Response), fail func(error)) wire.ResponseEventEncoder {
	state := &messagesResponseHistoryState{}
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
			if err := state.appendItem(completed.Item); err != nil {
				fail(err)
				return nil, err
			}
		}
		status, terminal := messagesResponseTerminalStatus(event)
		if !terminal {
			return encoded, nil
		}
		if status != canonical.EnvelopeStatusCompleted {
			fail(errors.New("messages response did not complete successfully"))
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
