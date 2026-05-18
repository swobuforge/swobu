package messages

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/adapters/outbound/protocols"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type bufferedResponseBody struct {
	ID         string                     `json:"id"`
	Model      string                     `json:"model"`
	Content    []bufferedContentBlockBody `json:"content"`
	StopReason string                     `json:"stop_reason"`
}

type bufferedContentBlockBody struct {
	Type      string         `json:"type"`
	Text      string         `json:"text"`
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Input     map[string]any `json:"input"`
	ToolUseID string         `json:"tool_use_id"`
}

var tokenUsagePathSpec = protocols.TokenUsagePathSpec{
	InputPaths: [][]string{
		{"usage", "input_tokens"},
		{"usage", "prompt_tokens"},
		{"usageMetadata", "promptTokenCount"},
		{"usage", "inputTokens"},
	},
	OutputPaths: [][]string{
		{"usage", "output_tokens"},
		{"usage", "completion_tokens"},
		{"usageMetadata", "candidatesTokenCount"},
		{"usage", "outputTokens"},
	},
	CacheReadPaths: [][]string{
		{"usage", "cache_read_input_tokens"},
		{"usage", "input_tokens_details", "cached_tokens"},
		{"usage", "prompt_tokens_details", "cached_tokens"},
		{"usageMetadata", "cachedContentTokenCount"},
		{"usage", "cacheReadInputTokens"},
	},
	CacheWritePaths: [][]string{
		{"usage", "cache_creation_input_tokens"},
		{"usage", "input_tokens_details", "cache_write_tokens"},
		{"usage", "prompt_tokens_details", "cache_write_tokens"},
		{"usage", "cacheWriteInputTokens"},
	},
}

func DecodeResponseBuffered(raw []byte) (canonical.CanonicalOutputValue, error) {
	var dto bufferedResponseBody
	if err := json.Unmarshal(raw, &dto); err != nil {
		return canonical.CanonicalOutputValue{}, canonical.InternalError("messages response is invalid JSON")
	}
	items := make([]canonical.CanonicalItem, 0, len(dto.Content))
	for i, block := range dto.Content {
		blockType := strings.TrimSpace(block.Type) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
		switch blockType {
		case "text":
			items = append(items, canonical.NewTextOutputItem("text_"+strconv.Itoa(i), block.Text))
		case "tool_use":
			itemID := strings.TrimSpace(block.ID) // swobu:io-string source=boundary
			if itemID == "" {
				itemID = "tool_" + strconv.Itoa(i)
			}
			items = append(items, canonical.NewToolUseOutputItem(itemID, strings.TrimSpace(block.ID), strings.TrimSpace(block.Name), cloneInput(block.Input))) // swobu:io-string source=boundary
		default:
			return canonical.CanonicalOutputValue{}, canonical.InternalError("messages response content block is unsupported")
		}
	}
	return canonical.NewConversationOutputWithUsage(
		dto.ID,
		dto.Model,
		items,
		dto.StopReason,
		protocols.ExtractTokenUsage(raw, tokenUsagePathSpec),
	), nil
}

// DecodeResponseStream returns canonical envelope events directly for messages streams.
func DecodeResponseStream(body io.ReadCloser, exchangeID string) canonical.EventReader {
	return &messagesEventReader{
		exchangeID:  exchangeID,
		responseID:  canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID)),
		reader:      protocols.NewSSEReader(body),
		blocks:      map[int]streamContentBlock{},
		latestUsage: canonical.NewUnknownTokenUsage(),
	}
}

type messagesEventReader struct {
	exchangeID   string
	responseID   canonical.EnvelopeID
	reader       *protocols.SSEReaderCloser
	resultID     string
	model        string
	finishReason string
	started      bool
	pending      []canonical.Event
	blocks       map[int]streamContentBlock
	latestUsage  canonical.TokenUsage
	seq          int64
	completed    bool
}

type streamContentBlock struct {
	ItemID    string
	ItemKind  canonical.ItemKind
	ToolUseID string
	Name      string
}

type streamEnvelope struct {
	Type string `json:"type"`
}

type messageStartFrame struct {
	Message struct {
		ID    string `json:"id"`
		Model string `json:"model"`
	} `json:"message"`
}

type contentBlockStartFrame struct {
	Index        int `json:"index"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
}

type contentBlockDeltaFrame struct {
	Index int `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
	} `json:"delta"`
}

type contentBlockStopFrame struct {
	Index int `json:"index"`
}

type messageDeltaFrame struct {
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
}

func (s *messagesEventReader) Next(context.Context) (canonical.Event, error) {
	if len(s.pending) > 0 {
		return s.shift(), nil
	}
	for {
		frame, err := s.reader.Next()
		if err != nil {
			if err == io.EOF && s.started && !s.completed {
				s.enqueue(canonical.Event{Kind: canonical.EventError, EnvID: s.responseID, Payload: canonical.ErrorPayload{Code: "stream_unexpected_eof", Message: "output stream ended before completed"}})
				for idx, block := range s.blocks {
					s.enqueueEnvelopeEnd(s.blockEnvID(idx, block), s.blockKind(block), canonical.EnvelopeStatusError)
				}
				s.blocks = map[int]streamContentBlock{}
				s.enqueueEnvelopeEnd(s.responseID, canonical.EnvResponse, canonical.EnvelopeStatusError)
				s.completed = true
				if len(s.pending) > 0 {
					return s.shift(), nil
				}
			}
			return canonical.Event{}, err
		}
		if strings.TrimSpace(frame.Data) == "" || frame.Event == "ping" { // swobu:io-string source=boundary
			continue
		}
		frameUsage := protocols.ExtractTokenUsage([]byte(frame.Data), tokenUsagePathSpec)
		if !frameUsage.IsZero() {
			s.latestUsage = frameUsage
		}
		var envelope streamEnvelope
		if err := json.Unmarshal([]byte(frame.Data), &envelope); err != nil {
			return canonical.Event{}, canonical.InternalError("messages stream frame is invalid JSON")
		}
		if err := s.handleFrame(envelope.Type, frame.Data); err != nil {
			return canonical.Event{}, err
		}
		if len(s.pending) > 0 {
			return s.shift(), nil
		}
	}
}

func (s *messagesEventReader) handleFrame(frameType string, raw string) error {
	normalizedFrameType := strings.TrimSpace(frameType) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
	switch normalizedFrameType {
	case "message_start":
		return s.handleMessageStart(raw)
	case "content_block_start":
		return s.handleContentBlockStart(raw)
	case "content_block_delta":
		return s.handleContentBlockDelta(raw)
	case "content_block_stop":
		return s.handleContentBlockStop(raw)
	case "message_delta":
		return s.handleMessageDelta(raw)
	case "message_stop":
		s.handleMessageStop()
		return nil
	case "ping":
		return nil
	default:
		return canonical.InternalError("messages stream frame type is unsupported")
	}
}

func (s *messagesEventReader) handleMessageStart(raw string) error {
	var payload messageStartFrame
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return canonical.InternalError("messages stream message_start frame is invalid")
	}
	s.started = true
	s.resultID = payload.Message.ID
	s.model = payload.Message.Model
	s.enqueueEnvelopeStart(s.responseID, "", canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse})
	s.enqueue(canonical.Event{
		Kind:    canonical.EventMetadata,
		EnvID:   s.responseID,
		Payload: canonical.MetadataPayload{Values: map[string]string{"result_id": s.resultID, "model": s.model}},
	})
	return nil
}

func (s *messagesEventReader) handleContentBlockStart(raw string) error {
	var payload contentBlockStartFrame
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return canonical.InternalError("messages stream content_block_start frame is invalid")
	}
	block := streamContentBlock{ItemID: "block_" + strconv.Itoa(payload.Index)}
	contentBlockType := strings.TrimSpace(payload.ContentBlock.Type) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
	switch contentBlockType {
	case "text":
		block.ItemKind = canonical.ItemKindText
		block.ItemID = "text_" + strconv.Itoa(payload.Index)
		s.enqueueEnvelopeStart(s.blockEnvID(payload.Index, block), s.responseID, canonical.EnvelopeStartPayload{Kind: canonical.EnvMessage, Role: canonical.ItemAuthorAssistant}, canonical.EventMetadataFields{NativeID: block.ItemID})
	case "tool_use":
		block.ItemKind = canonical.ItemKindToolUse
		block.ToolUseID = strings.TrimSpace(payload.ContentBlock.ID) // swobu:io-string source=boundary
		if block.ToolUseID == "" {
			block.ToolUseID = "toolu_swobu_" + strconv.Itoa(payload.Index)
		}
		block.Name = strings.TrimSpace(payload.ContentBlock.Name) // swobu:io-string source=boundary
		block.ItemID = block.ToolUseID
		s.enqueueEnvelopeStart(s.blockEnvID(payload.Index, block), s.responseID, canonical.EnvelopeStartPayload{Kind: canonical.EnvToolCall, Name: block.Name, ToolUseID: block.ToolUseID}, canonical.EventMetadataFields{NativeID: block.ItemID})
	default:
		return canonical.InternalError("messages stream content block type is unsupported")
	}
	s.blocks[payload.Index] = block
	return nil
}

func (s *messagesEventReader) handleContentBlockDelta(raw string) error {
	var payload contentBlockDeltaFrame
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return canonical.InternalError("messages stream content_block_delta frame is invalid")
	}
	block, ok := s.blocks[payload.Index]
	if !ok {
		return nil
	}
	deltaType := strings.TrimSpace(payload.Delta.Type) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
	switch deltaType {
	case "text_delta":
		s.enqueue(canonical.Event{
			Kind:    canonical.EventTextDelta,
			EnvID:   s.blockEnvID(payload.Index, block),
			Payload: canonical.TextDeltaPayload{Text: payload.Delta.Text},
		})
	case "input_json_delta":
		s.enqueue(canonical.Event{
			Kind:    canonical.EventArgsDelta,
			EnvID:   s.blockEnvID(payload.Index, block),
			Payload: canonical.ArgsDeltaPayload{Args: payload.Delta.PartialJSON},
		})
	default:
		return canonical.InternalError("messages stream delta type is unsupported")
	}
	return nil
}

func (s *messagesEventReader) handleContentBlockStop(raw string) error {
	var payload contentBlockStopFrame
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return canonical.InternalError("messages stream content_block_stop frame is invalid")
	}
	block, ok := s.blocks[payload.Index]
	if !ok {
		return nil
	}
	s.enqueueEnvelopeEnd(s.blockEnvID(payload.Index, block), s.blockKind(block), canonical.EnvelopeStatusCompleted)
	delete(s.blocks, payload.Index)
	return nil
}

func (s *messagesEventReader) handleMessageDelta(raw string) error {
	var payload messageDeltaFrame
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return canonical.InternalError("messages stream message_delta frame is invalid")
	}
	s.finishReason = strings.TrimSpace(payload.Delta.StopReason) // swobu:io-string source=boundary
	return nil
}

func (s *messagesEventReader) handleMessageStop() {
	s.completed = true
	finishReason := s.finishReason
	if finishReason == "" {
		finishReason = "completed"
	}
	s.enqueue(canonical.Event{Kind: canonical.EventUsage, EnvID: s.responseID, Payload: canonical.UsagePayload{Usage: s.latestUsage}})
	s.enqueue(canonical.Event{Kind: canonical.EventFinish, EnvID: s.responseID, Payload: canonical.FinishPayload{Reason: finishReason}})
	s.enqueueEnvelopeEnd(s.responseID, canonical.EnvResponse, canonical.EnvelopeStatusCompleted)
}

func (s *messagesEventReader) Close(context.Context) error {
	return s.reader.Close()
}

func (s *messagesEventReader) shift() canonical.Event {
	event := s.pending[0]
	s.pending = s.pending[1:]
	return event
}

func (s *messagesEventReader) nextSeq() int64 {
	s.seq++
	return s.seq
}

func (s *messagesEventReader) enqueue(ev canonical.Event) {
	ev.ExchangeID = s.exchangeID
	ev.Seq = s.nextSeq()
	ev.Time = time.Now().UTC()
	s.pending = append(s.pending, ev)
}

func (s *messagesEventReader) enqueueEnvelopeStart(id canonical.EnvelopeID, parent canonical.EnvelopeID, payload canonical.EnvelopeStartPayload, meta ...canonical.EventMetadataFields) {
	ev := canonical.Event{Kind: canonical.EventEnvelopeStart, EnvID: id, ParentID: parent, Payload: payload}
	if len(meta) > 0 {
		ev.Meta = meta[0]
	}
	s.enqueue(ev)
}

func (s *messagesEventReader) enqueueEnvelopeEnd(id canonical.EnvelopeID, kind canonical.EnvelopeKind, status canonical.EnvelopeStatus) {
	s.enqueue(canonical.Event{Kind: canonical.EventEnvelopeEnd, EnvID: id, Payload: canonical.EnvelopeEndPayload{Kind: kind, Status: status}})
}

func (s *messagesEventReader) blockEnvID(index int, _ streamContentBlock) canonical.EnvelopeID {
	return canonical.EnvelopeID(fmt.Sprintf("%s:item:%d", s.responseID, index))
}

func (s *messagesEventReader) blockKind(block streamContentBlock) canonical.EnvelopeKind {
	if block.ItemKind == canonical.ItemKindToolUse {
		return canonical.EnvToolCall
	}
	return canonical.EnvMessage
}
