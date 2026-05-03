// translation in one place so request and stream semantics stay recoverable.
package httpapi

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

type messagesFamilyCodec struct{}

func (messagesFamilyCodec) decodeRequest(raw []byte) (compatibility.CanonicalRequest, compatibility.DeliveryMode, error) {
	var dto messagesRequestDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, "", compatibility.BadRequest("messages request body is invalid JSON")
	}
	if len(dto.Messages) == 0 {
		return nil, "", compatibility.BadRequest("messages request is missing required fields")
	}
	items := make([]compatibility.CanonicalItem, 0, len(dto.Messages))
	for idx, msg := range dto.Messages {
		decoded, err := decodeMessagesItems(msg.Content, idx, strings.TrimSpace(msg.Role))
		if err != nil {
			return nil, "", err
		}
		items = append(items, decoded...)
	}
	return compatibility.NewDialogRequest(strings.TrimSpace(dto.Model), items), deliveryModeFromStream(dto.Stream), nil
}

func (messagesFamilyCodec) encodeBuffered(output compatibility.CanonicalOutput) ([]byte, error) {
	items := output.Items()
	content := make([]messagesResponsePartDTO, 0, len(items))
	for _, item := range items {
		switch item.Kind {
		case compatibility.ItemKindText:
			content = append(content, messagesResponsePartDTO{
				Type: "text",
				Text: item.Text,
			})
		case compatibility.ItemKindToolUse:
			content = append(content, messagesResponsePartDTO{
				Type:  "tool_use",
				ID:    item.ToolUseID,
				Name:  item.Name,
				Input: item.Input,
			})
		}
	}
	stopReason := "end_turn"
	if containsToolUseOutput(items) {
		stopReason = "tool_use"
	}
	return json.Marshal(messagesResponseDTO{
		ID:         fallbackID(output.ResultID(), "msg_swobu"),
		Type:       "message",
		Role:       "assistant",
		Model:      output.Model(),
		Content:    content,
		StopReason: stopReason,
		Usage:      messagesUsageFromCanonical(output.Usage()),
	})
}

func (messagesFamilyCodec) newStreamState() clientStreamEncoder {
	return &messagesClientStreamEncoder{}
}

// family's mixed content variants at the transport edge.
func decodeMessagesItems(raw json.RawMessage, msgIdx int, role string) ([]compatibility.CanonicalItem, error) {
	_ = msgIdx
	if role == "" {
		role = "user"
	}
	author := authorForRole(role)
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if text != "" {
			return []compatibility.CanonicalItem{compatibility.NewTextItem(author, text)}, nil
		}
		return nil, compatibility.BadRequest("messages request content must not be empty")
	}

	var parts []messagesContentPartDTO
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, compatibility.BadRequest("messages request content is invalid")
	}
	if len(parts) == 0 {
		return nil, compatibility.BadRequest("messages request content must not be empty")
	}

	decoded := make([]compatibility.CanonicalItem, 0, len(parts))
	for _, part := range parts {
		switch strings.TrimSpace(part.Type) {
		case "text":
			if part.Text == "" {
				return nil, compatibility.BadRequest("messages request text parts must not be empty")
			}
			decoded = append(decoded, compatibility.NewTextItem(author, part.Text))
		case "tool_use":
			if strings.TrimSpace(part.Name) == "" {
				return nil, compatibility.BadRequest("messages request tool_use parts require a name")
			}
			input, err := decodeJSONObject(part.Input, "messages request tool_use input is invalid")
			if err != nil {
				return nil, err
			}
			decoded = append(decoded, compatibility.NewToolUseItem(author, "", strings.TrimSpace(part.ID), strings.TrimSpace(part.Name), input))
		case "tool_result":
			if strings.TrimSpace(part.ToolUseID) == "" {
				return nil, compatibility.BadRequest("messages request tool_result parts require tool_use_id")
			}
			text, err := decodeToolResultText(part.Content)
			if err != nil {
				return nil, err
			}
			decoded = append(decoded, compatibility.NewToolResultItem(author, strings.TrimSpace(part.ToolUseID), text))
		default:
			return nil, compatibility.BadRequest("messages request content contains an unsupported part type")
		}
	}
	return decoded, nil
}

func decodeToolResultText(raw json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}

	var parts []messagesTextPartDTO
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", compatibility.BadRequest("messages request tool_result content is invalid")
	}
	var builder strings.Builder
	for _, part := range parts {
		if strings.TrimSpace(part.Type) != "text" {
			return "", compatibility.BadRequest("messages request tool_result content must contain text parts only")
		}
		builder.WriteString(part.Text)
	}
	return builder.String(), nil
}

type messagesClientStreamEncoder struct {
	started        bool
	nextIndex      int
	activeTextID   string
	activeBlockID  string
	blockIndexByID map[string]int
	sawToolUse     bool
}

// event-to-frame fanout over blocks, tool calls, and terminal envelopes.
// and lifecycle event shapes defined by the wire contract.
func (s *messagesClientStreamEncoder) Encode(event compatibility.OutputEvent) ([][]byte, error) {
	if s.blockIndexByID == nil {
		s.blockIndexByID = map[string]int{}
	}
	switch event.Kind {
	case compatibility.OutputEventStarted:
		s.started = true
		raw, _ := json.Marshal(messagesStartEventDTO{
			Type: "message_start",
			Message: messagesStartMessageDTO{
				ID:   fallbackID(event.ResultID, "msg_swobu"),
				Role: "assistant",
			},
		})
		return [][]byte{
			sseEventFrame("message_start", raw),
		}, nil
	case compatibility.OutputEventItemStarted:
		if !s.started {
			frames, _ := s.Encode(compatibility.OutputEvent{Kind: compatibility.OutputEventStarted, ResultID: event.ResultID, Model: event.Model})
			more, err := s.Encode(event)
			return append(frames, more...), err
		}
		index := s.nextIndex
		s.nextIndex++
		s.blockIndexByID[event.ItemID] = index
		s.activeBlockID = event.ItemID
		switch event.ItemKind {
		case compatibility.ItemKindText:
			s.activeTextID = event.ItemID
			raw, _ := json.Marshal(messagesContentBlockStartDTO{
				Type:  "content_block_start",
				Index: index,
				ContentBlock: messagesContentBlockBodyDTO{
					Type: "text",
					Text: "",
				},
			})
			return [][]byte{sseEventFrame("content_block_start", raw)}, nil
		case compatibility.ItemKindToolUse:
			s.sawToolUse = true
			raw, _ := json.Marshal(messagesContentBlockStartDTO{
				Type:  "content_block_start",
				Index: index,
				ContentBlock: messagesContentBlockBodyDTO{
					Type:  "tool_use",
					ID:    event.ToolUseID,
					Name:  event.Name,
					Input: map[string]any{},
				},
			})
			return [][]byte{sseEventFrame("content_block_start", raw)}, nil
		default:
			return nil, compatibility.UnsupportedOperation("messages streaming output item kind is not implemented")
		}
	case compatibility.OutputEventTextDelta:
		if !s.started || s.activeTextID == "" {
			frames, _ := s.Encode(compatibility.OutputEvent{
				Kind:     compatibility.OutputEventItemStarted,
				ItemKind: compatibility.ItemKindText,
				ResultID: event.ResultID,
				Model:    event.Model,
				ItemID:   fallbackID(event.ItemID, "text_0"),
			})
			more, err := s.Encode(event)
			return append(frames, more...), err
		}
		index := s.blockIndexByID[s.activeTextID]
		raw, _ := json.Marshal(messagesContentBlockDeltaDTO{
			Type:  "content_block_delta",
			Index: index,
			Delta: messagesContentBlockDeltaBodyDTO{
				Type: "text_delta",
				Text: event.TextDelta,
			},
		})
		return [][]byte{sseEventFrame("content_block_delta", raw)}, nil
	case compatibility.OutputEventToolUseArgumentsDelta:
		index, ok := s.blockIndexByID[event.ItemID]
		if !ok {
			frames, _ := s.Encode(compatibility.OutputEvent{
				Kind:      compatibility.OutputEventItemStarted,
				ItemKind:  compatibility.ItemKindToolUse,
				ResultID:  event.ResultID,
				Model:     event.Model,
				ItemID:    event.ItemID,
				ToolUseID: event.ToolUseID,
				Name:      event.Name,
			})
			more, err := s.Encode(event)
			return append(frames, more...), err
		}
		raw, _ := json.Marshal(messagesContentBlockDeltaDTO{
			Type:  "content_block_delta",
			Index: index,
			Delta: messagesContentBlockDeltaBodyDTO{
				Type:        "input_json_delta",
				PartialJSON: event.ArgumentsDelta,
			},
		})
		return [][]byte{sseEventFrame("content_block_delta", raw)}, nil
	case compatibility.OutputEventItemCompleted:
		index, ok := s.blockIndexByID[event.ItemID]
		if !ok {
			return nil, nil
		}
		delete(s.blockIndexByID, event.ItemID)
		if s.activeTextID == event.ItemID {
			s.activeTextID = ""
		}
		raw, _ := json.Marshal(messagesContentBlockStopDTO{Type: "content_block_stop", Index: index})
		return [][]byte{sseEventFrame("content_block_stop", raw)}, nil
	case compatibility.OutputEventCompleted:
		if !s.started {
			raw, _ := json.Marshal(messagesStartEventDTO{
				Type: "message_start",
				Message: messagesStartMessageDTO{
					ID:   "msg_swobu",
					Role: "assistant",
				},
			})
			return [][]byte{
				sseEventFrame("message_start", raw),
			}, nil
		}
		frames := make([][]byte, 0, len(s.blockIndexByID)+2)
		for _, index := range s.blockIndexByID {
			raw, _ := json.Marshal(messagesContentBlockStopDTO{Type: "content_block_stop", Index: index})
			frames = append(frames, sseEventFrame("content_block_stop", raw))
		}
		if s.sawToolUse {
			raw, _ := json.Marshal(struct {
				Type  string `json:"type"`
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
			}{
				Type: "message_delta",
				Delta: struct {
					StopReason string `json:"stop_reason"`
				}{StopReason: "tool_use"},
			})
			frames = append(frames, sseEventFrame("message_delta", raw))
		}
		raw, _ := json.Marshal(struct {
			Type string `json:"type"`
		}{Type: "message_stop"})
		frames = append(frames, sseEventFrame("message_stop", raw))
		s.blockIndexByID = map[string]int{}
		s.sawToolUse = false
		return frames, nil
	default:
		return nil, compatibility.UnsupportedOperation("messages streaming event is not implemented")
	}
}

func (s *messagesClientStreamEncoder) Finish() ([][]byte, error) { return nil, nil }
