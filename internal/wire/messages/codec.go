// translation in one place so request and stream semantics stay recoverable.
package messages

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

type messagesEnvelopeStreamEncoder struct {
	started        bool
	nextIndex      int
	activeTextID   string
	activeBlockID  string
	blockIndexByID map[string]int
	sawToolUse     bool
	adapter        *sse.EnvelopeEventAdapter
	request        canonical.CanonicalRequest
}

func (s *messagesEnvelopeStreamEncoder) EncodeEnvelopeEvent(event canonical.Event) ([][]byte, error) {
	streamEvents, err := s.adapter.Translate(event)
	if err != nil {
		return nil, err
	}
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
// swobu:lint ignore function-length because=messages stream encoder keeps SSE event kind dispatch in one wire seam.
// swobu:lint ignore function-complexity because=messages stream encoder keeps SSE event kind dispatch in one wire seam.
func (s *messagesEnvelopeStreamEncoder) Encode(event sse.StreamEvent) ([][]byte, error) {
	if s.blockIndexByID == nil {
		s.blockIndexByID = map[string]int{}
	}
	switch event.Kind {
	case sse.StreamEventStarted:
		s.started = true
		raw, _ := json.Marshal(messagesStartEventDTO{
			Type: "message_start",
			Message: messagesStartMessageDTO{
				ID:           sse.FallbackID(event.ResultID, "msg_swobu"),
				Type:         "message",
				Role:         "assistant",
				Content:      []messagesResponsePartDTO{},
				StopReason:   nil,
				StopSequence: nil,
			},
			Usage: messagesUsageDTO{},
		})
		frames := [][]byte{sse.SSEEventFrame("message_start", raw)}
		for _, frame := range frames {
			logMessagesEgressStreamFrame(frame)
		}
		return frames, nil
	case sse.StreamEventItemStarted:
		if !s.started {
			frames, _ := s.Encode(sse.StreamEvent{Kind: sse.StreamEventStarted, ResultID: event.ResultID, Model: event.Model})
			more, err := s.Encode(event)
			return append(frames, more...), err
		}
		switch event.ItemKind {
		case canonical.ItemKindMessage:
			return nil, nil
		case canonical.ItemKindToolCall:
			index := s.nextIndex
			s.nextIndex++
			s.blockIndexByID[event.ItemID] = index
			s.activeBlockID = event.ItemID
			s.sawToolUse = true
			raw, _ := json.Marshal(messagesContentBlockStartDTO{
				Type:  "content_block_start",
				Index: index,
				ContentBlock: messagesContentBlockBodyDTO{
					Type:  "tool_use",
					ID:    event.ToolUseID,
					Name:  event.Name,
					Input: json.RawMessage(`{}`),
				},
			})
			frames := [][]byte{sse.SSEEventFrame("content_block_start", raw)}
			for _, frame := range frames {
				logMessagesEgressStreamFrame(frame)
			}
			return frames, nil
		case canonical.ItemKindReasoning:
			return nil, nil
		default:
			return nil, canonical.UnsupportedOperation("messages streaming output item kind is not implemented")
		}
	case sse.StreamEventContentStarted:
		if event.PartKind != canonical.PartKindText {
			return nil, canonical.UnsupportedOperation("messages streaming content part kind is not implemented")
		}
		key := messagesStreamPartKey(event.ItemID, event.PartOrdinal)
		if _, exists := s.blockIndexByID[key]; exists {
			return nil, nil
		}
		index := s.nextIndex
		s.nextIndex++
		s.blockIndexByID[key] = index
		raw, _ := json.Marshal(messagesContentBlockStartDTO{Type: "content_block_start", Index: index, ContentBlock: messagesContentBlockBodyDTO{Type: "text", Text: ""}})
		frame := sse.SSEEventFrame("content_block_start", raw)
		logMessagesEgressStreamFrame(frame)
		return [][]byte{frame}, nil
	case sse.StreamEventTextDelta:
		key := messagesStreamPartKey(event.ItemID, event.PartOrdinal)
		if _, ok := s.blockIndexByID[key]; !ok {
			frames, err := s.Encode(sse.StreamEvent{Kind: sse.StreamEventContentStarted, ItemID: event.ItemID, ItemOrdinal: event.ItemOrdinal, PartOrdinal: event.PartOrdinal, PartKind: canonical.PartKindText})
			if err != nil {
				return nil, err
			}
			more, err := s.Encode(event)
			return append(frames, more...), err
		}
		index := s.blockIndexByID[key]
		raw, _ := json.Marshal(messagesContentBlockDeltaDTO{
			Type:  "content_block_delta",
			Index: index,
			Delta: messagesContentBlockDeltaBodyDTO{
				Type: "text_delta",
				Text: event.TextDelta,
			},
		})
		frames := [][]byte{sse.SSEEventFrame("content_block_delta", raw)}
		for _, frame := range frames {
			logMessagesEgressStreamFrame(frame)
		}
		return frames, nil
	case sse.StreamEventToolUseArgumentsDelta:
		index, ok := s.blockIndexByID[event.ItemID]
		if !ok {
			frames, _ := s.Encode(sse.StreamEvent{
				Kind:      sse.StreamEventItemStarted,
				ItemKind:  canonical.ItemKindToolCall,
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
		frames := [][]byte{sse.SSEEventFrame("content_block_delta", raw)}
		for _, frame := range frames {
			logMessagesEgressStreamFrame(frame)
		}
		return frames, nil
	case sse.StreamEventItemCompleted:
		if event.ItemKind == canonical.ItemKindReasoning {
			return s.encodeCompletedReasoning(event)
		}
		frames := make([][]byte, 0, 1)
		for key, index := range s.blockIndexByID {
			if key != event.ItemID && !strings.HasPrefix(key, event.ItemID+"#") {
				continue
			}
			delete(s.blockIndexByID, key)
			raw, _ := json.Marshal(messagesContentBlockStopDTO{Type: "content_block_stop", Index: index})
			frames = append(frames, sse.SSEEventFrame("content_block_stop", raw))
		}
		for _, frame := range frames {
			logMessagesEgressStreamFrame(frame)
		}
		return frames, nil
	case sse.StreamEventCompleted:
		if !s.started {
			frames, _ := s.Encode(sse.StreamEvent{Kind: sse.StreamEventStarted, ResultID: event.ResultID, Model: event.Model})
			more, err := s.Encode(event)
			return append(frames, more...), err
		}
		frames := make([][]byte, 0, len(s.blockIndexByID)+2)
		for _, index := range s.blockIndexByID {
			raw, _ := json.Marshal(messagesContentBlockStopDTO{Type: "content_block_stop", Index: index})
			frames = append(frames, sse.SSEEventFrame("content_block_stop", raw))
		}
		outputTokens := 0
		if usageTokens, ok := event.Usage.OutputTokens(); ok {
			outputTokens = usageTokens
		}
		raw, _ := json.Marshal(messagesDeltaEventDTO{
			Type: "message_delta",
			Delta: messagesDeltaBodyDTO{
				StopReason:   messagesStopReasonForFinishReason(event.FinishReason, s.sawToolUse),
				StopSequence: nil,
			},
			Usage: messagesDeltaUsageDTO{OutputTokens: outputTokens},
		})
		frames = append(frames, sse.SSEEventFrame("message_delta", raw))
		raw, _ = json.Marshal(struct {
			Type string `json:"type"`
		}{Type: "message_stop"})
		frames = append(frames, sse.SSEEventFrame("message_stop", raw))
		s.blockIndexByID = map[string]int{}
		s.sawToolUse = false
		for _, frame := range frames {
			logMessagesEgressStreamFrame(frame)
		}
		return frames, nil
	case sse.StreamEventFailed:
		raw, _ := json.Marshal(map[string]any{
			"type": "error",
			"error": map[string]any{
				"message": sse.FallbackID(event.ErrorMessage, "output stream failed"),
				"type":    "swobu_stream_error",
				"code":    sse.FallbackID(event.ErrorCode, "stream_error"),
			},
		})
		frame := sse.SSEEventFrame("error", raw)
		logMessagesEgressStreamFrame(frame)
		return [][]byte{frame}, nil
	default:
		return nil, canonical.UnsupportedOperation("messages streaming event is not implemented")
	}
}

func (s *messagesEnvelopeStreamEncoder) encodeCompletedReasoning(event sse.StreamEvent) ([][]byte, error) {
	if event.CompletedItem == nil {
		return nil, canonical.InternalError("messages reasoning completion is missing canonical item")
	}
	state := messagesResponseHistoryState{request: s.request.Clone()}
	if err := state.appendItem(*event.CompletedItem); err != nil {
		return nil, err
	}
	frames := make([][]byte, 0, len(state.content)*4)
	for _, block := range state.content {
		index := s.nextIndex
		s.nextIndex++
		startBlock := messagesContentBlockBodyDTO{Type: block.Type, Thinking: block.Thinking, Data: block.Data}
		if block.Type == "thinking" {
			empty := ""
			startBlock.Thinking = &empty
		}
		raw, _ := json.Marshal(messagesContentBlockStartDTO{Type: "content_block_start", Index: index, ContentBlock: startBlock})
		frames = append(frames, sse.SSEEventFrame("content_block_start", raw))
		if block.Type == "thinking" && block.Thinking != nil && *block.Thinking != "" {
			raw, _ = json.Marshal(messagesContentBlockDeltaDTO{Type: "content_block_delta", Index: index, Delta: messagesContentBlockDeltaBodyDTO{Type: "thinking_delta", Thinking: *block.Thinking}})
			frames = append(frames, sse.SSEEventFrame("content_block_delta", raw))
		}
		if block.Type == "thinking" && block.Signature != "" {
			raw, _ = json.Marshal(messagesContentBlockDeltaDTO{Type: "content_block_delta", Index: index, Delta: messagesContentBlockDeltaBodyDTO{Type: "signature_delta", Signature: block.Signature}})
			frames = append(frames, sse.SSEEventFrame("content_block_delta", raw))
		}
		raw, _ = json.Marshal(messagesContentBlockStopDTO{Type: "content_block_stop", Index: index})
		frames = append(frames, sse.SSEEventFrame("content_block_stop", raw))
	}
	for _, frame := range frames {
		logMessagesEgressStreamFrame(frame)
	}
	return frames, nil
}

func (s *messagesEnvelopeStreamEncoder) Finish() ([][]byte, error) { return nil, nil }

func messagesStreamPartKey(itemID string, part uint32) string {
	return itemID + "#" + strconv.FormatUint(uint64(part), 10)
}
