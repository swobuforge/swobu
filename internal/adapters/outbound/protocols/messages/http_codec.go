// translation in one place so request and stream semantics stay recoverable.
package messages

import (
	"encoding/json"
	"strings"

	httpcodec "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/httpcodec"
	openaicompat "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/openaicompat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type MessagesFamilyCodec struct{}

func (MessagesFamilyCodec) DecodeRequest(raw []byte) (canonical.CanonicalRequest, bool, error) {
	var dto messagesRequestDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, false, canonical.BadRequest("messages request body is invalid JSON")
	}
	if len(dto.Messages) == 0 {
		return nil, false, canonical.BadRequest("messages request is missing required fields")
	}
	items := make([]canonical.CanonicalItem, 0, len(dto.Messages))
	for idx, msg := range dto.Messages {
		decoded, err := decodeMessagesItems(msg.Content, idx, strings.TrimSpace(msg.Role)) // swobu:io-string source=boundary
		if err != nil {
			return nil, false, err
		}
		items = append(items, decoded...)
	}
	return canonical.NewDialogRequest(strings.TrimSpace(dto.Model), items), dto.Stream, nil // swobu:io-string source=boundary
}

func (MessagesFamilyCodec) EncodeBuffered(output canonical.CanonicalOutput) ([]byte, error) {
	items := output.Items()
	content := make([]messagesResponsePartDTO, 0, len(items))
	for _, item := range items {
		switch item.Kind {
		case canonical.ItemKindText:
			content = append(content, messagesResponsePartDTO{
				Type: "text",
				Text: item.Text,
			})
		case canonical.ItemKindToolUse:
			content = append(content, messagesResponsePartDTO{
				Type:  "tool_use",
				ID:    item.ToolUseID,
				Name:  item.Name,
				Input: item.Input,
			})
		}
	}
	stopReason := "end_turn"
	if httpcodec.ContainsToolUseOutput(items) {
		stopReason = "tool_use"
	}
	return json.Marshal(messagesResponseDTO{
		ID:         httpcodec.FallbackID(output.ResultID(), "msg_swobu"),
		Type:       "message",
		Role:       "assistant",
		Model:      output.Model(),
		Content:    content,
		StopReason: stopReason,
		Usage:      messagesUsageFromCanonical(output.Usage()),
	})
}

func (MessagesFamilyCodec) NewStreamState() httpcodec.EnvelopeStreamEncoder {
	return &messagesEnvelopeStreamEncoder{adapter: httpcodec.NewEnvelopeEventAdapter()}
}

// family's mixed content variants at the transport edge.
func decodeMessagesItems(raw json.RawMessage, msgIdx int, role string) ([]canonical.CanonicalItem, error) {
	_ = msgIdx
	if role == "" {
		role = "user"
	}
	author := openaicompat.AuthorForRole(role)
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if text != "" {
			return []canonical.CanonicalItem{canonical.NewTextItem(author, text)}, nil
		}
		return nil, canonical.BadRequest("messages request content must not be empty")
	}

	var parts []messagesContentPartDTO
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, canonical.BadRequest("messages request content is invalid")
	}
	if len(parts) == 0 {
		return nil, canonical.BadRequest("messages request content must not be empty")
	}

	decoded := make([]canonical.CanonicalItem, 0, len(parts))
	for _, part := range parts {
		partType := strings.TrimSpace(part.Type) // swobu:io-string source=provider-wire
		switch partType {
		case "text":
			if part.Text == "" {
				return nil, canonical.BadRequest("messages request text parts must not be empty")
			}
			decoded = append(decoded, canonical.NewTextItem(author, part.Text))
		case "tool_use":
			if strings.TrimSpace(part.Name) == "" { // swobu:io-string source=boundary
				return nil, canonical.BadRequest("messages request tool_use parts require a name")
			}
			input, err := httpcodec.DecodeJSONObject(part.Input, "messages request tool_use input is invalid")
			if err != nil {
				return nil, err
			}
			decoded = append(decoded, canonical.NewToolUseItem(author, "", strings.TrimSpace(part.ID), strings.TrimSpace(part.Name), input)) // swobu:io-string source=boundary
		case "tool_result":
			if strings.TrimSpace(part.ToolUseID) == "" { // swobu:io-string source=boundary
				return nil, canonical.BadRequest("messages request tool_result parts require tool_use_id")
			}
			text, err := decodeToolResultText(part.Content)
			if err != nil {
				return nil, err
			}
			decoded = append(decoded, canonical.NewToolResultItem(author, strings.TrimSpace(part.ToolUseID), text)) // swobu:io-string source=boundary
		default:
			return nil, canonical.BadRequest("messages request content contains an unsupported part type")
		}
	}
	return decoded, nil
}

func messagesUsageFromCanonical(usage canonical.TokenUsage) *messagesUsageDTO {
	input, hasInput := usage.InputTokens()
	output, hasOutput := usage.OutputTokens()
	cacheRead, hasCacheRead := usage.CacheReadTokens()
	cacheWrite, hasCacheWrite := usage.CacheWriteTokens()
	if !hasInput && !hasOutput && !hasCacheRead && !hasCacheWrite {
		return nil
	}
	return &messagesUsageDTO{
		InputTokens:              input,
		OutputTokens:             output,
		CacheReadInputTokens:     cacheRead,
		CacheCreationInputTokens: cacheWrite,
	}
}

func decodeToolResultText(raw json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}

	var parts []messagesTextPartDTO
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", canonical.BadRequest("messages request tool_result content is invalid")
	}
	var builder strings.Builder
	for _, part := range parts {
		if strings.TrimSpace(part.Type) != "text" { // swobu:io-string source=boundary
			return "", canonical.BadRequest("messages request tool_result content must contain text parts only")
		}
		builder.WriteString(part.Text)
	}
	return builder.String(), nil
}

type messagesEnvelopeStreamEncoder struct {
	started        bool
	nextIndex      int
	activeTextID   string
	activeBlockID  string
	blockIndexByID map[string]int
	sawToolUse     bool
	adapter        *httpcodec.EnvelopeEventAdapter
}

func (s *messagesEnvelopeStreamEncoder) EncodeEnvelopeEvent(event canonical.Event) ([][]byte, error) {
	streamEvents := s.adapter.Translate(event)
	frames := make([][]byte, 0, len(streamEvents))
	for _, streamEvent := range streamEvents {
		emitted, err := s.Encode(streamEvent)
		if err != nil {
			return nil, err
		}
		frames = append(frames, emitted...)
	}
	return frames, nil
}

// event-to-frame fanout over blocks, tool calls, and terminal envelopes.
// and lifecycle event shapes defined by the wire contract.
func (s *messagesEnvelopeStreamEncoder) Encode(event httpcodec.StreamEvent) ([][]byte, error) {
	if s.blockIndexByID == nil {
		s.blockIndexByID = map[string]int{}
	}
	switch event.Kind {
	case httpcodec.StreamEventStarted:
		s.started = true
		raw, _ := json.Marshal(messagesStartEventDTO{
			Type: "message_start",
			Message: messagesStartMessageDTO{
				ID:   httpcodec.FallbackID(event.ResultID, "msg_swobu"),
				Role: "assistant",
			},
		})
		return [][]byte{
			httpcodec.SSEEventFrame("message_start", raw),
		}, nil
	case httpcodec.StreamEventItemStarted:
		if !s.started {
			frames, _ := s.Encode(httpcodec.StreamEvent{Kind: httpcodec.StreamEventStarted, ResultID: event.ResultID, Model: event.Model})
			more, err := s.Encode(event)
			return append(frames, more...), err
		}
		index := s.nextIndex
		s.nextIndex++
		s.blockIndexByID[event.ItemID] = index
		s.activeBlockID = event.ItemID
		switch event.ItemKind {
		case canonical.ItemKindText:
			s.activeTextID = event.ItemID
			raw, _ := json.Marshal(messagesContentBlockStartDTO{
				Type:  "content_block_start",
				Index: index,
				ContentBlock: messagesContentBlockBodyDTO{
					Type: "text",
					Text: "",
				},
			})
			return [][]byte{httpcodec.SSEEventFrame("content_block_start", raw)}, nil
		case canonical.ItemKindToolUse:
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
			return [][]byte{httpcodec.SSEEventFrame("content_block_start", raw)}, nil
		default:
			return nil, canonical.UnsupportedOperation("messages streaming output item kind is not implemented")
		}
	case httpcodec.StreamEventTextDelta:
		if !s.started || s.activeTextID == "" {
			frames, _ := s.Encode(httpcodec.StreamEvent{
				Kind:     httpcodec.StreamEventItemStarted,
				ItemKind: canonical.ItemKindText,
				ResultID: event.ResultID,
				Model:    event.Model,
				ItemID:   httpcodec.FallbackID(event.ItemID, "text_0"),
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
		return [][]byte{httpcodec.SSEEventFrame("content_block_delta", raw)}, nil
	case httpcodec.StreamEventToolUseArgumentsDelta:
		index, ok := s.blockIndexByID[event.ItemID]
		if !ok {
			frames, _ := s.Encode(httpcodec.StreamEvent{
				Kind:      httpcodec.StreamEventItemStarted,
				ItemKind:  canonical.ItemKindToolUse,
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
		return [][]byte{httpcodec.SSEEventFrame("content_block_delta", raw)}, nil
	case httpcodec.StreamEventItemCompleted:
		index, ok := s.blockIndexByID[event.ItemID]
		if !ok {
			return nil, nil
		}
		delete(s.blockIndexByID, event.ItemID)
		if s.activeTextID == event.ItemID {
			s.activeTextID = ""
		}
		raw, _ := json.Marshal(messagesContentBlockStopDTO{Type: "content_block_stop", Index: index})
		return [][]byte{httpcodec.SSEEventFrame("content_block_stop", raw)}, nil
	case httpcodec.StreamEventCompleted:
		if !s.started {
			raw, _ := json.Marshal(messagesStartEventDTO{
				Type: "message_start",
				Message: messagesStartMessageDTO{
					ID:   "msg_swobu",
					Role: "assistant",
				},
			})
			return [][]byte{
				httpcodec.SSEEventFrame("message_start", raw),
			}, nil
		}
		frames := make([][]byte, 0, len(s.blockIndexByID)+2)
		for _, index := range s.blockIndexByID {
			raw, _ := json.Marshal(messagesContentBlockStopDTO{Type: "content_block_stop", Index: index})
			frames = append(frames, httpcodec.SSEEventFrame("content_block_stop", raw))
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
			frames = append(frames, httpcodec.SSEEventFrame("message_delta", raw))
		}
		raw, _ := json.Marshal(struct {
			Type string `json:"type"`
		}{Type: "message_stop"})
		frames = append(frames, httpcodec.SSEEventFrame("message_stop", raw))
		s.blockIndexByID = map[string]int{}
		s.sawToolUse = false
		return frames, nil
	default:
		return nil, canonical.UnsupportedOperation("messages streaming event is not implemented")
	}
}

func (s *messagesEnvelopeStreamEncoder) Finish() ([][]byte, error) { return nil, nil }
