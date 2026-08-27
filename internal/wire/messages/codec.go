// translation in one place so request and stream semantics stay recoverable.
package messages

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

type messagesEnvelopeStreamEncoder struct {
	started                  bool
	nextIndex                int
	activeTextID             string
	activeBlockID            string
	blockIndexByID           map[string]int
	sawToolUse               bool
	adapter                  *sse.EnvelopeEventAdapter
	request                  canonical.CanonicalRequest
	pendingWebSearchStarts   map[string]sse.StreamEvent
	unresolvedWebSearchCalls map[string]uint32
	changes                  []compat.Change
}

func (s *messagesEnvelopeStreamEncoder) Changes() []compat.Change {
	return compat.CloneChanges(s.changes)
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
				Model:        event.Model,
				Content:      []messagesResponsePartDTO{},
				StopReason:   nil,
				StopSequence: nil,
				Usage:        messagesUsageDTO{},
			},
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
			if event.ToolType == string(canonical.ToolKindWebSearch) {
				s.pendingWebSearchStarts[event.ItemID] = event
				return nil, nil
			}
			index := s.nextIndex
			s.nextIndex++
			s.blockIndexByID[event.ItemID] = index
			s.activeBlockID = event.ItemID
			s.sawToolUse = true
			blockType := "tool_use"
			if event.ToolType == string(canonical.ToolKindWebSearch) {
				blockType = "server_tool_use"
			}
			raw, _ := json.Marshal(messagesContentBlockStartDTO{
				Type:  "content_block_start",
				Index: index,
				ContentBlock: messagesContentBlockBodyDTO{
					Type:  blockType,
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
			return nil, canonical.InternalError("Messages stream received a request-only canonical item kind")
		}
	case sse.StreamEventContentStarted:
		if event.PartKind != canonical.PartKindText {
			return nil, canonical.NewBackendError(
				"messages",
				0,
				"Messages client stream cannot represent the backend image response",
				"",
			)
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
		if _, pending := s.pendingWebSearchStarts[event.ItemID]; pending {
			return nil, nil
		}
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
		if event.CompletedItem != nil {
			if event.ItemKind == canonical.ItemKindToolResult {
				if result, ok := event.CompletedItem.ToolResult(); ok {
					if _, web := result.WebSearch(); web {
						return s.encodeCompletedWebSearchResult(event, result)
					}
				}
			}
			if event.ItemKind == canonical.ItemKindToolCall {
				if call, ok := event.CompletedItem.ToolCall(); ok && call.Tool().Kind() == canonical.ToolKindWebSearch {
					return s.completeWebSearchCall(event, call)
				}
			}
			if event.ItemKind == canonical.ItemKindMessage {
				citationFrames, err := s.encodeCompletedMessageCitations(event)
				if err != nil {
					return nil, err
				}
				stopFrames := s.stopCompletedItem(event)
				return append(citationFrames, stopFrames...), nil
			}
		}
		return s.stopCompletedItem(event), nil
	case sse.StreamEventCompleted:
		if !s.started {
			frames, _ := s.Encode(sse.StreamEvent{Kind: sse.StreamEventStarted, ResultID: event.ResultID, Model: event.Model})
			more, err := s.Encode(event)
			return append(frames, more...), err
		}
		if len(s.unresolvedWebSearchCalls) > 0 {
			return nil, canonical.NotImplemented("Messages cannot project an unresolved canonical web-search call")
		}
		frames := make([][]byte, 0, len(s.blockIndexByID)+2)
		for _, index := range s.blockIndexByID {
			raw, _ := json.Marshal(messagesContentBlockStopDTO{Type: "content_block_stop", Index: index})
			frames = append(frames, sse.SSEEventFrame("content_block_stop", raw))
		}
		stopReason, err := messagesStopReasonForCompletion(event.Completion, s.sawToolUse)
		if err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(messagesDeltaEventDTO{
			Type: "message_delta",
			Delta: messagesDeltaBodyDTO{
				StopReason:   stopReason,
				StopSequence: nil,
			},
			// Messages terminal usage is cumulative. Carry every known
			// canonical counter here because a cross-family source may not
			// expose input/cache usage before message_start.
			Usage: messagesDeltaUsageFromCanonical(event.Usage),
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
		return nil, canonical.InternalError("Messages stream received an unknown canonical event kind")
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

func (s *messagesEnvelopeStreamEncoder) stopCompletedItem(event sse.StreamEvent) [][]byte {
	frames := make([][]byte, 0, 1)
	for key, index := range s.blockIndexByID {
		if key != event.ItemID && !strings.HasPrefix(key, event.ItemID+"#") {
			continue
		}
		delete(s.blockIndexByID, key)
		raw, _ := json.Marshal(messagesContentBlockStopDTO{Type: "content_block_stop", Index: index})
		frame := sse.SSEEventFrame("content_block_stop", raw)
		logMessagesEgressStreamFrame(frame)
		frames = append(frames, frame)
	}
	return frames
}

func (s *messagesEnvelopeStreamEncoder) completeWebSearchCall(event sse.StreamEvent, call canonical.ToolCallItem) ([][]byte, error) {
	start, ok := s.pendingWebSearchStarts[event.ItemID]
	if !ok {
		return nil, canonical.InternalError("messages web-search stream call has no pending start")
	}
	delete(s.pendingWebSearchStarts, event.ItemID)
	search, ok := call.Input().WebSearch()
	if !ok || search.Action != canonical.WebSearchActionSearch || len(search.Queries) != 1 {
		s.unresolvedWebSearchCalls[call.CallID().String()] = event.ItemOrdinal
		return nil, nil
	}
	startFrames, err := s.startWebSearchBlock(start)
	if err != nil {
		return nil, err
	}
	index := s.blockIndexByID[event.ItemID]
	input, _ := json.Marshal(map[string]string{"query": search.Queries[0]})
	raw, _ := json.Marshal(messagesContentBlockDeltaDTO{Type: "content_block_delta", Index: index, Delta: messagesContentBlockDeltaBodyDTO{Type: "input_json_delta", PartialJSON: string(input)}})
	frame := sse.SSEEventFrame("content_block_delta", raw)
	logMessagesEgressStreamFrame(frame)
	frames := append(startFrames, frame)
	return append(frames, s.stopCompletedItem(event)...), nil
}

func (s *messagesEnvelopeStreamEncoder) encodeCompletedWebSearchResult(event sse.StreamEvent, result canonical.ToolResultItem) ([][]byte, error) {
	callID := result.CallID().String()
	if callOrdinal, omitted := s.unresolvedWebSearchCalls[callID]; omitted {
		delete(s.unresolvedWebSearchCalls, callID)
		s.changes = append(s.changes, compat.Change{
			Capability: canonical.ResponseItemsKind,
			Kind:       compat.Omission,
			Occurrence: canonical.ResponseItemOccurrence(callOrdinal),
		})
		return nil, nil
	}
	search, _ := result.WebSearch()
	content, err := encodeMessagesWebSearchResult(search)
	if err != nil {
		return nil, err
	}
	index := s.nextIndex
	s.nextIndex++
	start, _ := json.Marshal(messagesContentBlockStartDTO{Type: "content_block_start", Index: index, ContentBlock: messagesContentBlockBodyDTO{Type: "web_search_tool_result", ToolUseID: result.CallID().String(), Content: content}})
	stop, _ := json.Marshal(messagesContentBlockStopDTO{Type: "content_block_stop", Index: index})
	frames := [][]byte{sse.SSEEventFrame("content_block_start", start), sse.SSEEventFrame("content_block_stop", stop)}
	for _, frame := range frames {
		logMessagesEgressStreamFrame(frame)
	}
	return frames, nil
}

func (s *messagesEnvelopeStreamEncoder) startWebSearchBlock(event sse.StreamEvent) ([][]byte, error) {
	index := s.nextIndex
	s.nextIndex++
	s.blockIndexByID[event.ItemID] = index
	s.activeBlockID = event.ItemID
	s.sawToolUse = true
	raw, err := json.Marshal(messagesContentBlockStartDTO{
		Type:  "content_block_start",
		Index: index,
		ContentBlock: messagesContentBlockBodyDTO{
			Type: "server_tool_use", ID: event.ToolUseID, Name: event.Name, Input: json.RawMessage(`{}`),
		},
	})
	if err != nil {
		return nil, err
	}
	frame := sse.SSEEventFrame("content_block_start", raw)
	logMessagesEgressStreamFrame(frame)
	return [][]byte{frame}, nil
}

func (s *messagesEnvelopeStreamEncoder) encodeCompletedMessageCitations(event sse.StreamEvent) ([][]byte, error) {
	message, ok := event.CompletedItem.Message()
	if !ok {
		return nil, canonical.InternalError("messages completed message is invalid")
	}
	frames := make([][]byte, 0)
	for ordinal, part := range message.Content() {
		text, ok := part.Text()
		if !ok {
			continue
		}
		citations, err := encodeMessagesCitations(text.Text(), part.Citations())
		if err != nil {
			return nil, err
		}
		index, exists := s.blockIndexByID[messagesStreamPartKey(event.ItemID, uint32(ordinal))]
		if !exists && len(citations) > 0 {
			return nil, canonical.InternalError("messages cited stream part has no active block")
		}
		for citationIndex := range citations {
			citation := citations[citationIndex]
			raw, _ := json.Marshal(messagesContentBlockDeltaDTO{Type: "content_block_delta", Index: index, Delta: messagesContentBlockDeltaBodyDTO{Type: "citations_delta", Citation: &citation}})
			frame := sse.SSEEventFrame("content_block_delta", raw)
			logMessagesEgressStreamFrame(frame)
			frames = append(frames, frame)
		}
	}
	return frames, nil
}

func (s *messagesEnvelopeStreamEncoder) Finish() ([][]byte, error) { return nil, nil }

func messagesStreamPartKey(itemID string, part uint32) string {
	return itemID + "#" + strconv.FormatUint(uint64(part), 10)
}
