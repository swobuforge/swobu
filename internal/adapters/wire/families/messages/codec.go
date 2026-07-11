// translation in one place so request and stream semantics stay recoverable.
package messages

import (
	"encoding/json"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type messagesEnvelopeStreamEncoder struct {
	started        bool
	nextIndex      int
	activeTextID   string
	activeBlockID  string
	blockIndexByID map[string]int
	sawToolUse     bool
	adapter        *sse.EnvelopeEventAdapter
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
		return [][]byte{
			sse.SSEEventFrame("message_start", raw),
		}, nil
	case sse.StreamEventItemStarted:
		if !s.started {
			frames, _ := s.Encode(sse.StreamEvent{Kind: sse.StreamEventStarted, ResultID: event.ResultID, Model: event.Model})
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
			return [][]byte{sse.SSEEventFrame("content_block_start", raw)}, nil
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
			return [][]byte{sse.SSEEventFrame("content_block_start", raw)}, nil
		default:
			return nil, canonical.UnsupportedOperation("messages streaming output item kind is not implemented")
		}
	case sse.StreamEventTextDelta:
		if !s.started || s.activeTextID == "" {
			frames, _ := s.Encode(sse.StreamEvent{
				Kind:     sse.StreamEventItemStarted,
				ItemKind: canonical.ItemKindText,
				ResultID: event.ResultID,
				Model:    event.Model,
				ItemID:   sse.FallbackID(event.ItemID, "text_0"),
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
		return [][]byte{sse.SSEEventFrame("content_block_delta", raw)}, nil
	case sse.StreamEventToolUseArgumentsDelta:
		index, ok := s.blockIndexByID[event.ItemID]
		if !ok {
			frames, _ := s.Encode(sse.StreamEvent{
				Kind:      sse.StreamEventItemStarted,
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
		return [][]byte{sse.SSEEventFrame("content_block_delta", raw)}, nil
	case sse.StreamEventItemCompleted:
		index, ok := s.blockIndexByID[event.ItemID]
		if !ok {
			return nil, nil
		}
		delete(s.blockIndexByID, event.ItemID)
		if s.activeTextID == event.ItemID {
			s.activeTextID = ""
		}
		raw, _ := json.Marshal(messagesContentBlockStopDTO{Type: "content_block_stop", Index: index})
		return [][]byte{sse.SSEEventFrame("content_block_stop", raw)}, nil
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
		stopReason := "end_turn"
		if s.sawToolUse {
			stopReason = "tool_use"
		}
		outputTokens := 0
		if usageTokens, ok := event.Usage.OutputTokens(); ok {
			outputTokens = usageTokens
		}
		raw, _ := json.Marshal(messagesDeltaEventDTO{
			Type: "message_delta",
			Delta: messagesDeltaBodyDTO{
				StopReason:   stopReason,
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
		return frames, nil
	default:
		return nil, canonical.UnsupportedOperation("messages streaming event is not implemented")
	}
}

func (s *messagesEnvelopeStreamEncoder) Finish() ([][]byte, error) { return nil, nil }
